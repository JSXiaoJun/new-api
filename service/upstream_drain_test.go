package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDrainUsageParserReadsEveryRelayedFormat(t *testing.T) {
	for _, tt := range []struct {
		name           string
		stream         string
		wantPrompt     int
		wantCompletion int
	}{
		{
			name: "openai chat usage chunk",
			stream: "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
				"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":500,\"total_tokens\":1500}}\n\ndata: [DONE]\n\n",
			wantPrompt: 1000, wantCompletion: 500,
		},
		{
			name: "openai responses completed event",
			stream: "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"usage\":null}}\n\n" +
				"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1000,\"output_tokens\":500,\"total_tokens\":1500}}}\n\n",
			wantPrompt: 1000, wantCompletion: 500,
		},
		{
			name: "claude messages split across start and delta",
			stream: "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1000,\"output_tokens\":1}}}\n\n" +
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":500}}\n\n",
			wantPrompt: 1000, wantCompletion: 500,
		},
		{
			name:       "gemini usage metadata",
			stream:     "data: {\"candidates\":[],\"usageMetadata\":{\"promptTokenCount\":1000,\"candidatesTokenCount\":500,\"totalTokenCount\":1500}}\n\n",
			wantPrompt: 1000, wantCompletion: 500,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parser := &drainUsageParser{}
			// Split the stream at an arbitrary point to exercise line buffering.
			cut := len(tt.stream) / 3
			parser.feed([]byte(tt.stream[:cut]))
			parser.feed([]byte(tt.stream[cut:]))

			usage := effectiveBillingUsage(parser.result())
			require.NotNil(t, usage)
			assert.Equal(t, tt.wantPrompt, usage.PromptTokens)
			assert.Equal(t, tt.wantCompletion, usage.CompletionTokens)
		})
	}
	assert.Nil(t, (&drainUsageParser{}).result(), "a stream without usage reports none")
}

// drainUpstream serves an OpenAI chat stream: two chunks, then the rest with
// the usage once released.
type drainUpstream struct {
	server      *httptest.Server
	release     chan struct{}
	releaseOnce sync.Once
	headerDelay time.Duration
}

func (u *drainUpstream) releaseRest() { u.releaseOnce.Do(func() { close(u.release) }) }

func newDrainUpstream(t *testing.T, withUsage bool, headerDelay time.Duration) *drainUpstream {
	t.Helper()
	upstream := &drainUpstream{release: make(chan struct{}), headerDelay: headerDelay}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(upstream.headerDelay)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, word := range []string{"alpha", "bravo"} {
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\""+word+"\"}}]}\n\n")
			flusher.Flush()
		}
		select {
		case <-upstream.release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"charlie\"}}]}\n\n")
		if withUsage {
			_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":500,\"total_tokens\":1500}}\n\n")
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(upstream.server.Close)
	// Runs first: a failed assertion must not leave the handler waiting.
	t.Cleanup(upstream.releaseRest)
	return upstream
}

func drainRelayInfo(info *relaycommon.RelayInfo) {
	info.IsStream = true
	info.ChannelMeta.ChannelType = constant.ChannelTypeSub2API
}

// sendDrainable sends the request the way doRequest does for a draining
// channel and returns the response the handler would read.
func sendDrainable(t *testing.T, ctx *gin.Context, info *relaycommon.RelayInfo, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx.Request.Context(), http.MethodPost, url, strings.NewReader(`{}`))
	require.NoError(t, err)
	info.MarkUpstreamStart()
	attempt := info.UpstreamAttempt()
	req, drain := PrepareUpstreamDrain(ctx, info, req)
	require.NotNil(t, drain, "a Sub2API stream drains by default")
	req = req.WithContext(attempt.Trace(req.Context()))
	resp, err := drain.Do(req, http.DefaultClient.Do, func() bool {
		sent := attempt.RequestSent()
		attempt.Freeze()
		return sent
	})
	if err != nil {
		attempt.Fail(err, true)
		return nil, err
	}
	attempt.MarkResponse()
	drain.WrapBody(resp)
	return resp, nil
}

// A stream handler closes the body as soon as it read [DONE], usually before
// the end of the chunked body arrived. The next request on the same
// keep-alive connection must still get its whole reply.
func TestUpstreamDrainClosedAfterDoneLeavesTheConnectionUsable(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/next" {
			_, _ = io.WriteString(w, `{"ok":true}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[]}\n\ndata: [DONE]\n\n")
		w.(http.Flusher).Flush()
		// The terminating chunk follows a moment later, soon enough for
		// net/http to keep the connection.
		time.Sleep(5 * time.Millisecond)
	}))
	t.Cleanup(upstream.Close)
	client := &http.Client{Transport: &http.Transport{MaxConnsPerHost: 1}, Timeout: 2 * time.Second}
	var reused atomic.Int32

	for round := range 20 {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
		drainRelayInfo(info)
		req, err := http.NewRequestWithContext(ctx.Request.Context(), http.MethodPost, upstream.URL+"/stream", strings.NewReader(`{}`))
		require.NoError(t, err)
		info.MarkUpstreamStart()
		attempt := info.UpstreamAttempt()
		req, drain := PrepareUpstreamDrain(ctx, info, req)
		require.NotNil(t, drain)
		resp, err := drain.Do(req.WithContext(attempt.Trace(req.Context())), client.Do, attempt.RequestSent)
		require.NoError(t, err)
		attempt.MarkResponse()
		drain.WrapBody(resp)
		reply := make([]byte, 4096)
		n, _ := io.ReadAtLeast(resp.Body, reply, len("data: [DONE]"))
		require.Contains(t, string(reply[:n]), "[DONE]")

		done := make(chan string, 1)
		go func() {
			_ = resp.Body.Close()
			trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
				if info.Reused {
					reused.Add(1)
				}
			}}
			nextReq, _ := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace), http.MethodGet, upstream.URL+"/next", nil)
			next, err := client.Do(nextReq)
			if err != nil {
				done <- err.Error()
				return
			}
			body, _ := io.ReadAll(next.Body)
			_ = next.Body.Close()
			done <- string(body)
		}()
		select {
		case body := <-done:
			assert.JSONEq(t, `{"ok":true}`, body, "round %d", round)
		case <-time.After(3 * time.Second):
			require.FailNow(t, "closing the stream or the next request on its connection hung", "round %d", round)
		}
	}
	assert.Positive(t, reused.Load(), "a stream that ended in time keeps its connection")
}

func waitAbnormalRecord(t *testing.T) model.AbnormalCharge {
	t.Helper()
	var records []model.AbnormalCharge
	require.Eventually(t, func() bool {
		records = abnormalChargeRecords(t)
		return len(records) > 0
	}, 5*time.Second, 20*time.Millisecond, "the drain must write the record")
	require.Len(t, records, 1)
	return records[0]
}

// A client leaving a Sub2API stream mid-way is settled exactly as before; the
// drained upstream usage only marks how much upstream charged.
func TestUpstreamDrainRecordsWhatUpstreamChargedAfterTheClientLeft(t *testing.T) {
	for _, tt := range []struct {
		name          string
		policyBlocked bool
		wantCharged   int
		wantOutcome   string
	}{
		{name: "default settlement charges the usage produced so far", wantCharged: 1_020, wantOutcome: model.AbnormalChargeOutcomePartial},
		{name: "a waived request owes everything upstream charged", policyBlocked: true, wantCharged: 0, wantOutcome: model.AbnormalChargeOutcomeWaived},
	} {
		t.Run(tt.name, func(t *testing.T) {
			upstream := newDrainUpstream(t, true, 0)
			ctx, info, cancel := newDisconnectBillingCase(t, tokenPriced())
			drainRelayInfo(info)

			resp, err := sendDrainable(t, ctx, info, upstream.server.URL)
			require.NoError(t, err)
			buf := make([]byte, 64)
			_, err = resp.Body.Read(buf)
			require.NoError(t, err, "the handler reads upstream through the drain as before")
			cancel()
			_, err = io.ReadAll(resp.Body)
			require.ErrorIs(t, err, context.Canceled, "a gone client stops the handler at once")
			require.NoError(t, resp.Body.Close())
			info.StreamStatus = relaycommon.NewStreamStatus()
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, context.Canceled)
			if tt.policyBlocked {
				setPolicyBlock(info)
			}
			upstream.releaseRest()

			PostTextConsumeQuota(ctx, info, &dto.Usage{PromptTokens: 1_000, CompletionTokens: 20, TotalTokens: 1_020}, nil)

			assert.Equal(t, tt.wantCharged, disconnectStartQuota-getUserQuota(t, disconnectUserId), "settlement is unchanged")
			record := waitAbnormalRecord(t)
			assert.Equal(t, model.UpstreamBillingBilled, record.UpstreamBilling)
			assert.Equal(t, 1_500, record.UpstreamQuota)
			assert.Equal(t, tt.wantCharged, record.ChargedQuota)
			assert.Equal(t, 1_500-tt.wantCharged, record.ForgoneQuota)
			assert.Equal(t, 1_000, record.UpstreamPromptTokens)
			assert.Equal(t, 500, record.UpstreamCompletionTokens)
			assert.Equal(t, tt.wantOutcome, record.Outcome)
			assert.Equal(t, "client_during_response", record.Case)
			assert.True(t, record.UserCaused)
		})
	}
}

func TestUpstreamDrainRecordsAClientThatLeftBeforeUpstreamAnswered(t *testing.T) {
	defaultCharge := 0
	t.Run("default settlement", func(t *testing.T) { defaultCharge = settleClientLeftBeforeResponse(t) })
	upstream := newDrainUpstream(t, true, 300*time.Millisecond)
	upstream.releaseRest()
	ctx, info, cancel := newDisconnectBillingCase(t, tokenPriced())
	drainRelayInfo(info)

	time.AfterFunc(100*time.Millisecond, cancel)
	_, err := sendDrainable(t, ctx, info, upstream.server.URL)
	require.ErrorIs(t, err, context.Canceled, "the relay fails as before")
	SettleClientDisconnectBilling(ctx, info)
	info.Billing.Refund(ctx)
	FinishUpstreamDrain(ctx, info, true)

	assert.Equal(t, defaultCharge, disconnectStartQuota-getUserQuota(t, disconnectUserId), "settlement is unchanged")
	record := waitAbnormalRecord(t)
	assert.Equal(t, model.UpstreamBillingBilled, record.UpstreamBilling)
	assert.Equal(t, 1_500, record.UpstreamQuota)
	assert.Equal(t, defaultCharge, record.ChargedQuota)
	assert.Equal(t, 1_500-defaultCharge, record.ForgoneQuota)
	assert.Equal(t, "client_before_response", record.Case)
}

func TestUpstreamDrainWithoutUsageKeepsTheWaiverMarkedUnknown(t *testing.T) {
	upstream := newDrainUpstream(t, false, 0)
	ctx, info, cancel := newDisconnectBillingCase(t, tokenPriced())
	drainRelayInfo(info)

	resp, err := sendDrainable(t, ctx, info, upstream.server.URL)
	require.NoError(t, err)
	cancel()
	require.NoError(t, resp.Body.Close())
	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, context.Canceled)
	setPolicyBlock(info)
	upstream.releaseRest()

	PostTextConsumeQuota(ctx, info, &dto.Usage{PromptTokens: 1_000, CompletionTokens: 20, TotalTokens: 1_020}, nil)

	record := waitAbnormalRecord(t)
	assert.Equal(t, model.UpstreamBillingUnknown, record.UpstreamBilling)
	assert.Equal(t, 1_020, record.ForgoneQuota, "the waived charge stays recoverable")
	assert.Equal(t, model.AbnormalChargeOutcomeWaived, record.Outcome)
}

func TestUpstreamDrainStaysOutOfRequestsTheClientFinished(t *testing.T) {
	upstream := newDrainUpstream(t, true, 0)
	upstream.releaseRest()
	ctx, info, _ := newDisconnectBillingCase(t, tokenPriced())
	drainRelayInfo(info)

	resp, err := sendDrainable(t, ctx, info, upstream.server.URL)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "[DONE]", "the handler receives the whole stream")
	require.NoError(t, resp.Body.Close())

	PostTextConsumeQuota(ctx, info, &dto.Usage{PromptTokens: 1_000, CompletionTokens: 500, TotalTokens: 1_500}, nil)

	assert.Equal(t, 1_500, disconnectStartQuota-getUserQuota(t, disconnectUserId))
	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, abnormalChargeRecords(t))
}

// The handler read the whole reply, then the client left before settlement
// waived the charge: the record is still written with what upstream billed.
func TestUpstreamDrainRecordsAWaiverAfterTheReplyWasRead(t *testing.T) {
	upstream := newDrainUpstream(t, true, 0)
	upstream.releaseRest()
	ctx, info, cancel := newDisconnectBillingCase(t, tokenPriced())
	drainRelayInfo(info)

	resp, err := sendDrainable(t, ctx, info, upstream.server.URL)
	require.NoError(t, err)
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	cancel()
	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, context.Canceled)
	setPolicyBlock(info)

	require.NoError(t, resp.Body.Close())

	PostTextConsumeQuota(ctx, info, &dto.Usage{PromptTokens: 1_000, CompletionTokens: 20, TotalTokens: 1_020}, nil)

	assert.Equal(t, 0, disconnectStartQuota-getUserQuota(t, disconnectUserId), "the safety policy block still waives the charge")
	record := waitAbnormalRecord(t)
	assert.Equal(t, model.UpstreamBillingBilled, record.UpstreamBilling)
	assert.Equal(t, 1_500, record.ForgoneQuota)
	assert.Equal(t, model.AbnormalChargeOutcomeWaived, record.Outcome)
}

func setMatchUpstreamCharge(t *testing.T) {
	t.Helper()
	setAbnormalOption(t, "match_upstream_charge", true, false)
}

// With "match upstream" on, an abandoned request is charged afterwards what
// upstream billed, whatever the switches waived; the user sees an ordinary
// consume entry and the record is closed as recovered.
func TestMatchUpstreamChargeCollectsWhatUpstreamBilled(t *testing.T) {
	for _, tt := range []struct {
		name    string
		settled int
	}{
		{name: "after a partial charge", settled: 1_020},
	} {
		t.Run(tt.name, func(t *testing.T) {
			setMatchUpstreamCharge(t)
			upstream := newDrainUpstream(t, true, 0)
			ctx, info, cancel := newDisconnectBillingCase(t, tokenPriced())
			drainRelayInfo(info)

			resp, err := sendDrainable(t, ctx, info, upstream.server.URL)
			require.NoError(t, err)
			cancel()
			require.NoError(t, resp.Body.Close())
			info.StreamStatus = relaycommon.NewStreamStatus()
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, context.Canceled)
			upstream.releaseRest()

			PostTextConsumeQuota(ctx, info, &dto.Usage{PromptTokens: 1_000, CompletionTokens: 20, TotalTokens: 1_020}, nil)

			require.Eventually(t, func() bool {
				return disconnectStartQuota-getUserQuota(t, disconnectUserId) == 1_500
			}, 5*time.Second, 20*time.Millisecond, "the user ends up paying what upstream billed")
			record := waitAbnormalRecord(t)
			assert.Equal(t, 1_500-tt.settled, record.ForgoneQuota)
			assert.NotEmpty(t, record.RecoveryId, "the difference is recovered automatically")
			var logs []model.Log
			require.NoError(t, model.LOG_DB.Where("type = ? AND quota = ?", model.LogTypeConsume, 1_500-tt.settled).Find(&logs).Error)
			require.Len(t, logs, 1, "the charge appears as a consume entry")
			assert.Contains(t, logs[0].Content, "upstream billed")
			assert.Equal(t, disconnectStartQuota-1_500, getUserQuota(t, disconnectUserId))
		})
	}
}

// A request upstream blocked by a safety policy stays free under "match
// upstream": the drained usage is only recorded, never charged afterwards.
func TestMatchUpstreamChargeLeavesASafetyPolicyBlockFree(t *testing.T) {
	setMatchUpstreamCharge(t)
	upstream := newDrainUpstream(t, true, 0)
	ctx, info, cancel := newDisconnectBillingCase(t, tokenPriced())
	drainRelayInfo(info)

	resp, err := sendDrainable(t, ctx, info, upstream.server.URL)
	require.NoError(t, err)
	cancel()
	require.NoError(t, resp.Body.Close())
	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, context.Canceled)
	setPolicyBlock(info)
	upstream.releaseRest()

	PostTextConsumeQuota(ctx, info, &dto.Usage{PromptTokens: 1_000, CompletionTokens: 20, TotalTokens: 1_020}, nil)

	record := waitAbnormalRecord(t)
	assert.Equal(t, 1_500, record.UpstreamQuota)
	assert.Empty(t, record.RecoveryId, "nothing is recovered from the user")
	assert.Zero(t, disconnectStartQuota-getUserQuota(t, disconnectUserId))
}

// Without a reply to read, "match upstream" still never lets a user off the
// usage produced before the request ended: upstream billed it too.
func TestMatchUpstreamChargeIgnoresWaiversOnChannelsThatStopReading(t *testing.T) {
	for _, tt := range []struct {
		name        string
		reason      relaycommon.StreamEndReason
		endErr      error
		wantCharged int
	}{
		{name: "client left mid-stream", reason: relaycommon.StreamEndReasonClientGone, endErr: context.Canceled, wantCharged: 1_200},
		{name: "upstream reset mid-stream", reason: relaycommon.StreamEndReasonScannerErr, endErr: io.ErrUnexpectedEOF, wantCharged: 1_200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			setMatchUpstreamCharge(t)
			ctx, info, _ := newDisconnectBillingCase(t, tokenPriced())
			info.IsStream = true
			info.ChannelMeta.ChannelType = constant.ChannelTypeNewAPI
			info.MarkUpstreamStart()
			info.UpstreamAttempt().MarkRequestSent()
			info.UpstreamAttempt().MarkResponse()
			info.StreamStatus = relaycommon.NewStreamStatus()
			info.StreamStatus.RequireTerminal()
			info.StreamStatus.SetEndReason(tt.reason, tt.endErr)

			PostTextConsumeQuota(ctx, info, &dto.Usage{PromptTokens: 1_000, CompletionTokens: 200, TotalTokens: 1_200}, nil)

			assert.Equal(t, tt.wantCharged, disconnectStartQuota-getUserQuota(t, disconnectUserId))
			assert.Empty(t, abnormalChargeRecords(t))
		})
	}

	t.Run("a failure before upstream answered stays free", func(t *testing.T) {
		setMatchUpstreamCharge(t)
		ctx, info, cancel := newDisconnectBillingCase(t, tokenPriced())
		info.MarkUpstreamStart()
		info.UpstreamAttempt().MarkRequestSent()
		info.UpstreamAttempt().Fail(io.ErrUnexpectedEOF, false)
		cancel()
		assert.False(t, SettleClientDisconnectBilling(ctx, info))
	})

	t.Run("the official alignment no longer refunds a client that left", func(t *testing.T) {
		defaultCharge := defaultClientLeftBeforeResponseCharge(t)
		require.Positive(t, defaultCharge)
		setOfficialAlignedBilling(t)
		setMatchUpstreamCharge(t)
		assert.Equal(t, defaultCharge, settleClientLeftBeforeResponse(t))
	})
}

func TestMatchUpstreamChargeReadsAnAbandonedNonStreamReply(t *testing.T) {
	setMatchUpstreamCharge(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"x","choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":1000,"completion_tokens":500,"total_tokens":1500}}`)
	}))
	t.Cleanup(server.Close)
	ctx, info, cancel := newDisconnectBillingCase(t, tokenPriced())
	drainRelayInfo(info)
	info.IsStream = false

	time.AfterFunc(100*time.Millisecond, cancel)
	_, err := sendDrainable(t, ctx, info, server.URL)
	require.ErrorIs(t, err, context.Canceled)
	SettleClientDisconnectBilling(ctx, info)
	info.Billing.Refund(ctx)
	FinishUpstreamDrain(ctx, info, true)

	require.Eventually(t, func() bool {
		return disconnectStartQuota-getUserQuota(t, disconnectUserId) == 1_500
	}, 5*time.Second, 20*time.Millisecond)
	record := waitAbnormalRecord(t)
	assert.Equal(t, model.UpstreamBillingBilled, record.UpstreamBilling)
	assert.False(t, record.IsStream)
}

func TestDrainUsageParserReadsNonStreamReplies(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
	}{
		{name: "openai chat", body: `{"choices":[],"usage":{"prompt_tokens":1000,"completion_tokens":500,"total_tokens":1500}}`},
		{name: "openai responses", body: `{"object":"response","status":"completed","usage":{"input_tokens":1000,"output_tokens":500,"total_tokens":1500}}`},
		{name: "claude message", body: `{"type":"message","role":"assistant","usage":{"input_tokens":1000,"output_tokens":500}}`},
		{name: "gemini", body: `{"candidates":[],"usageMetadata":{"promptTokenCount":1000,"candidatesTokenCount":500,"totalTokenCount":1500}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parser := &drainUsageParser{}
			parser.feed([]byte(tt.body[:10]))
			parser.feed([]byte(tt.body[10:]))
			usage := effectiveBillingUsage(parser.result())
			require.NotNil(t, usage)
			assert.Equal(t, 1_000, usage.PromptTokens)
			assert.Equal(t, 500, usage.CompletionTokens)
		})
	}
}

// A non-stream reply longer than the kept start of the body still yields
// its usage, which every format writes after the long content; a "usage"
// inside the content does not count.
func TestDrainUsageParserReadsTheUsageAtTheEndOfAHugeReply(t *testing.T) {
	filler := strings.Repeat("x", maxDrainLine) + `\"usage\":{\"prompt_tokens\":9}`
	for _, tt := range []struct {
		name string
		body string
	}{
		{name: "openai chat", body: `{"id":"c","choices":[{"message":{"content":"` + filler + `"}}],"usage":{"prompt_tokens":1000,"completion_tokens":500,"total_tokens":1500}}`},
		{name: "openai responses", body: `{"object":"response","output":[{"content":[{"text":"` + filler + `"}]}],"usage":{"input_tokens":1000,"output_tokens":500,"total_tokens":1500}}`},
		{name: "claude message", body: `{"id":"m","type":"message","content":[{"type":"text","text":"` + filler + `"}],"usage":{"input_tokens":1000,"output_tokens":500}}`},
		{name: "gemini", body: `{"candidates":[{"content":{"parts":[{"text":"` + filler + `"}]}}],"usageMetadata":{"promptTokenCount":1000,"candidatesTokenCount":500,"totalTokenCount":1500}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parser := &drainUsageParser{}
			for body := []byte(tt.body); len(body) > 0; {
				n := min(len(body), 32<<10)
				parser.feed(body[:n])
				body = body[n:]
			}
			usage := effectiveBillingUsage(parser.result())
			require.NotNil(t, usage)
			assert.Equal(t, 1_000, usage.PromptTokens)
			assert.Equal(t, 500, usage.CompletionTokens)
		})
	}
}

func TestUpstreamDrainIsOffForOtherChannelsAndNonStreams(t *testing.T) {
	enabled := true
	for _, tt := range []struct {
		name    string
		match   bool
		arrange func(info *relaycommon.RelayInfo)
		want    bool
	}{
		{name: "sub2api stream", arrange: func(info *relaycommon.RelayInfo) {}, want: true},
		{name: "sub2api non-stream", arrange: func(info *relaycommon.RelayInfo) { info.IsStream = false }},
		{name: "sub2api non-stream when matching upstream", match: true, arrange: func(info *relaycommon.RelayInfo) { info.IsStream = false }, want: true},
		{name: "new api non-stream when matching upstream", match: true, arrange: func(info *relaycommon.RelayInfo) {
			info.IsStream = false
			info.ChannelType = constant.ChannelTypeNewAPI
		}, want: true},
		{name: "new api stream when matching upstream", match: true, arrange: func(info *relaycommon.RelayInfo) {
			info.ChannelType = constant.ChannelTypeNewAPI
		}},
		{name: "another channel type", arrange: func(info *relaycommon.RelayInfo) { info.ChannelType = constant.ChannelTypeOpenAI }},
		{name: "another channel type non-stream when matching upstream", match: true, arrange: func(info *relaycommon.RelayInfo) {
			info.IsStream = false
			info.ChannelType = constant.ChannelTypeOpenAI
		}},
		{name: "another channel type turned on", arrange: func(info *relaycommon.RelayInfo) {
			info.ChannelType = constant.ChannelTypeOpenAI
			info.ChannelSetting.UpstreamDrainOnDisconnect = &enabled
		}, want: true},
		{name: "sub2api turned off", arrange: func(info *relaycommon.RelayInfo) {
			disabled := false
			info.ChannelSetting.UpstreamDrainOnDisconnect = &disabled
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.match {
				setMatchUpstreamCharge(t)
			}
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
			drainRelayInfo(info)
			tt.arrange(info)
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Request = req
			_, drain := PrepareUpstreamDrain(c, info, req)
			assert.Equal(t, tt.want, drain != nil)
		})
	}
}
