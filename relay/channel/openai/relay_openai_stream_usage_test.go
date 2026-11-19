package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOaiStreamHandlerMergesUsageAcrossFramesAndKeepsItAfterEmptyTerminalUsage(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"id":"chatcmpl_1","model":"gpt-test","choices":[{"index":0,"delta":{"content":"hello"}}]}`,
		`data: {"id":"chatcmpl_1","model":"gpt-test","choices":[],"usage":{"input_tokens":12,"output_tokens":5,"total_tokens":17,"input_tokens_details":{"cached_tokens":3}}}`,
		`data: {"id":"chatcmpl_1","model":"gpt-test","choices":[],"usage":{}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "chat-stream-usage-test")
	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
		RelayMode:          relayconstant.RelayModeChatCompletions,
		RelayFormat:        types.RelayFormatOpenAI,
		ShouldIncludeUsage: true,
		DisablePing:        true,
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}

	usage, apiErr := OaiStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 12, usage.PromptTokens)
	assert.Equal(t, 5, usage.CompletionTokens)
	assert.Equal(t, 17, usage.TotalTokens)
	assert.Equal(t, 3, usage.PromptTokensDetails.CachedTokens)
}

func TestOaiStreamHandlerLabelsBareSafetyPolicyErrorChunkWithoutFailingTheStream(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	tests := []struct {
		name     string
		chunk    string
		wantCode string
	}{
		{name: "sub2api cyber policy chunk", chunk: `{"error":{"type":"invalid_request_error","code":"cyber_policy","message":"blocked"}}`, wantCode: "cyber_policy"},
		{name: "other upstream error chunk", chunk: `{"error":{"type":"server_error","code":"server_error","message":"boom"}}`, wantCode: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Set(common.RequestIdKey, "chat-stream-policy-test")
			info := &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
				RelayMode:   relayconstant.RelayModeChatCompletions,
				RelayFormat: types.RelayFormatOpenAI,
				DisablePing: true,
			}
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("data: " + tt.chunk + "\ndata: [DONE]\n")),
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			}

			_, apiErr := OaiStreamHandler(c, info, resp)

			require.Nil(t, apiErr)
			assert.Equal(t, tt.wantCode, info.StreamStatus.PolicyBlockCode())
			// The label must not change how the request is classified or settled.
			assert.Empty(t, info.StreamStatus.ResponseOutcome())
		})
	}
}

func TestExtractChatStreamUsageSupportsNestedResponseDataAndAliases(t *testing.T) {
	usage := extractChatStreamUsage(`{"data":{"response":{"usage":{"prompt_tokens":9,"completion_tokens":4,"total_tokens":13,"completion_tokens_details":{"reasoning_tokens":2}}}}}`)
	require.NotNil(t, usage)
	assert.Equal(t, 9, usage.PromptTokens)
	assert.Equal(t, 4, usage.CompletionTokens)
	assert.Equal(t, 13, usage.TotalTokens)
	assert.Equal(t, 2, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestOaiStreamHandlerPreservesTotalOnlyUsage(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"id":"chatcmpl_total","model":"gpt-test","choices":[{"index":0,"delta":{"content":"hello"}}]}`,
		`data: {"id":"chatcmpl_total","model":"gpt-test","choices":[],"usage":{"total_tokens":100}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "chat-stream-total-only-test")
	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
		RelayMode:          relayconstant.RelayModeChatCompletions,
		RelayFormat:        types.RelayFormatOpenAI,
		ShouldIncludeUsage: true,
		DisablePing:        true,
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}

	usage, apiErr := OaiStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 100, usage.TotalTokens)
}

// An upstream new-api relaying Claude returns the 5m/1h cache-write split only
// in billing_usage and the claude_cache_creation_* fields; the stream merge
// must keep them, or every 1h write settles at the 5m price.
func TestOaiStreamHandlerKeepsUpstreamClaudeCacheWriteSplit(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"id":"c1","model":"claude-opus-5-5","choices":[{"index":0,"delta":{"content":"OK"}}]}`,
		`data: {"id":"c1","model":"claude-opus-5-5","choices":[],"usage":{"prompt_tokens":20377,"completion_tokens":4,"total_tokens":20381,"usage_semantic":"openai","usage_source":"anthropic","billing_usage":{"source":"claude_messages","semantic":"anthropic","claude_usage":{"input_tokens":4,"cache_creation_input_tokens":20373,"cache_read_input_tokens":0,"output_tokens":4,"cache_creation":{"ephemeral_1h_input_tokens":20373},"claude_cache_creation_5_m_tokens":0,"claude_cache_creation_1_h_tokens":0}},"prompt_tokens_details":{"cached_tokens":0,"cached_creation_tokens":20373,"cache_write_tokens":20373},"input_tokens":20377,"output_tokens":0,"input_tokens_details":null,"claude_cache_creation_5_m_tokens":0,"claude_cache_creation_1_h_tokens":20373}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "chat-stream-claude-cache-split")
	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{UpstreamModelName: "claude-opus-5-5"},
		RelayMode:          relayconstant.RelayModeChatCompletions,
		RelayFormat:        types.RelayFormatOpenAI,
		ShouldIncludeUsage: true,
		DisablePing:        true,
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}

	usage, apiErr := OaiStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 20373, usage.ClaudeCacheCreation1hTokens)
	require.NotNil(t, usage.BillingUsage)

	canonical, ok := usage.BillingUsage.CanonicalUsage()
	require.True(t, ok)
	params := service.BuildTieredTokenParams(canonical, true, map[string]bool{"p": true, "c": true, "cr": true, "cc": true, "cc1h": true})
	assert.Equal(t, 4.0, params.P)
	assert.Equal(t, 0.0, params.CC)
	assert.Equal(t, 20373.0, params.CC1h)
}

// A user stopping an OpenAI-format stream must not cut off the final usage
// frame once the request reached the upstream: the detached relay keeps
// reading and settles what the upstream reports, as Sub2API does.
func TestOaiStreamHandlerKeepsReadingUsageAfterClientStops(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout, oldEnabled, oldMax := constant.StreamingTimeout, common.CloudflareDisconnectBillingEnabled, common.CloudflareDisconnectBillingMaxSeconds
	constant.StreamingTimeout, common.CloudflareDisconnectBillingEnabled, common.CloudflareDisconnectBillingMaxSeconds = 30, true, 1800
	t.Cleanup(func() {
		constant.StreamingTimeout, common.CloudflareDisconnectBillingEnabled, common.CloudflareDisconnectBillingMaxSeconds = oldTimeout, oldEnabled, oldMax
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	clientCtx, clientLeaves := context.WithCancel(context.Background())
	t.Cleanup(clientLeaves)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(clientCtx)
	c.Set(common.RequestIdKey, "chat-stream-client-stops")
	release := service.DetachProxiedClientCancellation(c)
	defer release()
	service.MarkUpstreamContacted(c)

	upstream, upstreamWriter := io.Pipe()
	go func() {
		_, _ = io.WriteString(upstreamWriter, `data: {"id":"c1","model":"claude-opus-5-5","choices":[{"index":0,"delta":{"content":"partial"}}]}`+"\n\n")
		clientLeaves()
		require.Eventually(t, func() bool { return service.ProxiedClientGone(c) }, time.Second, time.Millisecond)
		_, _ = io.WriteString(upstreamWriter, `data: {"id":"c1","model":"claude-opus-5-5","choices":[{"index":0,"delta":{"content":" rest of the answer"},"finish_reason":"stop"}]}`+"\n\n")
		_, _ = io.WriteString(upstreamWriter, `data: {"id":"c1","model":"claude-opus-5-5","choices":[],"usage":{"prompt_tokens":311000,"completion_tokens":57,"total_tokens":311057,"prompt_tokens_details":{"cached_tokens":310990}}}`+"\n\n")
		_, _ = io.WriteString(upstreamWriter, "data: [DONE]\n\n")
		_ = upstreamWriter.Close()
	}()

	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{UpstreamModelName: "claude-opus-5-5"},
		RelayMode:          relayconstant.RelayModeChatCompletions,
		RelayFormat:        types.RelayFormatOpenAI,
		ShouldIncludeUsage: true,
		DisablePing:        true,
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       upstream,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}

	usage, apiErr := OaiStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 311000, usage.PromptTokens)
	assert.Equal(t, 310990, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 57, usage.CompletionTokens)
	assert.NotContains(t, recorder.Body.String(), "rest of the answer", "nothing reaches a client that left")
}
