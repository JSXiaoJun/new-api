package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/gin-gonic/gin"
)

const (
	upstreamDrainKey = "upstream_drain"
	// maxDrainLine bounds one buffered SSE line while draining.
	maxDrainLine = 4 << 20
)

// UpstreamDrainEnabled reports whether a channel keeps reading a request's
// reply after the client left. Sub2API bills every abandoned stream, so its
// streams default to on. Non-streams are read only when the user is charged
// what upstream billed: Sub2API (OpenAI platform) and official New API both
// finish and bill an abandoned non-stream request, while reading one that
// upstream would have cancelled must be paid by someone.
func UpstreamDrainEnabled(info *relaycommon.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil {
		return false
	}
	if !info.IsStream && !billing_setting.MatchUpstreamCharge() {
		return false
	}
	if setting := info.ChannelSetting.UpstreamDrainOnDisconnect; setting != nil {
		return *setting
	}
	if info.ChannelType == constant.ChannelTypeSub2API {
		return true
	}
	return !info.IsStream && info.ChannelType == constant.ChannelTypeNewAPI
}

// upstreamDrain follows one upstream attempt that the client may abandon. The
// handler settles the request as before; the drain only adds the usage
// upstream reported to the abnormal request record. Whichever of the two
// finishes last writes the record.
type upstreamDrain struct {
	clientCtx context.Context
	started   atomic.Bool

	mu       sync.Mutex
	settled  bool
	drained  bool
	recorded bool
	parser   *drainUsageParser
	record   *model.AbnormalCharge
	fallback *model.AbnormalCharge
	charged  int
	ctx      *gin.Context
	info     relaycommon.RelayInfo
}

func upstreamDrainFromContext(c *gin.Context) *upstreamDrain {
	if c == nil {
		return nil
	}
	value, ok := c.Get(upstreamDrainKey)
	if !ok {
		return nil
	}
	drain, _ := value.(*upstreamDrain)
	return drain
}

// active reports whether the client left this attempt, so its usage is being
// drained.
func (d *upstreamDrain) active() bool {
	return d != nil && d.clientCtx.Err() != nil
}

// UpstreamDrainRequest is the client side of a drainable upstream request.
type UpstreamDrainRequest struct {
	drain     *upstreamDrain
	clientCtx context.Context
	cancel    context.CancelFunc
}

// PrepareUpstreamDrain detaches a streaming request from the client so its
// upstream reply can be read after the client leaves. It returns nil, and
// leaves the request untouched, when the channel does not drain.
func PrepareUpstreamDrain(c *gin.Context, info *relaycommon.RelayInfo, req *http.Request) (*http.Request, *UpstreamDrainRequest) {
	if c == nil {
		return req, nil
	}
	if req == nil || !UpstreamDrainEnabled(info) {
		if upstreamDrainFromContext(c) != nil {
			c.Set(upstreamDrainKey, (*upstreamDrain)(nil))
		}
		return req, nil
	}
	clientCtx := req.Context()
	maxRun := time.Duration(common.CloudflareDisconnectBillingMaxSeconds) * time.Second
	if maxRun <= 0 {
		maxRun = 30 * time.Minute
	}
	upstreamCtx, cancel := context.WithCancel(context.WithoutCancel(clientCtx))
	// The bound starts only when the client leaves, so a stream the client
	// still reads is never cut short.
	context.AfterFunc(clientCtx, func() {
		timer := time.NewTimer(maxRun)
		defer timer.Stop()
		select {
		case <-timer.C:
			cancel()
		case <-upstreamCtx.Done():
		}
	})
	drain := &upstreamDrain{clientCtx: clientCtx}
	c.Set(upstreamDrainKey, drain)
	return req.WithContext(upstreamCtx), &UpstreamDrainRequest{drain: drain, clientCtx: clientCtx, cancel: cancel}
}

// ClientContext is the context whose cancellation means the client left.
func (r *UpstreamDrainRequest) ClientContext() context.Context { return r.clientCtx }

// Do sends the request. A client that leaves before upstream answers still
// gets the cancellation error it got before; when the request had already
// reached upstream, the reply is drained in the background.
func (r *UpstreamDrainRequest) Do(req *http.Request, send func(*http.Request) (*http.Response, error), requestSent func() bool) (*http.Response, error) {
	type result struct {
		resp *http.Response
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := send(req)
		done <- result{resp, err}
	}()
	select {
	case got := <-done:
		if got.err != nil {
			r.cancel()
		}
		return got.resp, got.err
	case <-r.clientCtx.Done():
	}
	if !requestSent() {
		r.cancel()
	} else {
		r.drain.started.Store(true)
	}
	go func() {
		got := <-done
		if got.err != nil || got.resp == nil {
			r.cancel()
			r.drain.finishDrain(nil)
			return
		}
		r.drainBody(got.resp)
	}()
	return nil, &url.Error{Op: req.Method, URL: req.URL.String(), Err: context.Canceled}
}

// WrapBody lets the handler read the upstream body as before while the drain
// keeps reading it once the client leaves.
func (r *UpstreamDrainRequest) WrapBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		r.cancel()
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: r.cancel}
		return
	}
	reader, writer := io.Pipe()
	body := &drainBody{
		raw:     resp.Body,
		reader:  reader,
		request: r,
		parser:  &drainUsageParser{},
	}
	body.stopWatch = context.AfterFunc(r.clientCtx, func() {
		r.drain.started.Store(true)
		_ = reader.CloseWithError(context.Canceled)
	})
	go body.pump(writer)
	resp.Body = body
}

func (r *UpstreamDrainRequest) drainBody(resp *http.Response) {
	defer r.cancel()
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		r.drain.finishDrain(nil)
		return
	}
	parser := &drainUsageParser{}
	buf := make([]byte, 32<<10)
	for {
		n, err := resp.Body.Read(buf)
		parser.feed(buf[:n])
		if err != nil {
			break
		}
	}
	r.drain.finishDrain(parser)
}

type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

// drainBody forwards the upstream body to the handler through a pipe. A pump
// owns the real body: closing the handler side while the client is still
// there closes upstream as before; once the client is gone the pump keeps
// reading upstream for its usage.
type drainBody struct {
	raw       io.ReadCloser
	reader    *io.PipeReader
	request   *UpstreamDrainRequest
	parser    *drainUsageParser
	stopWatch func() bool
	closeOnce sync.Once
	abandoned atomic.Bool
}

// Read reports the client cancellation the handler saw before draining
// existed, rather than the closed pipe that now carries it.
func (b *drainBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		if ctxErr := b.request.clientCtx.Err(); ctxErr != nil {
			return n, ctxErr
		}
	}
	return n, err
}

func (b *drainBody) Close() error {
	b.closeOnce.Do(func() {
		b.stopWatch()
		if b.request.clientCtx.Err() != nil {
			b.request.drain.started.Store(true)
			_ = b.reader.CloseWithError(context.Canceled)
			return
		}
		b.abandoned.Store(true)
		_ = b.reader.Close()
		_ = b.raw.Close()
	})
	return nil
}

func (b *drainBody) pump(writer *io.PipeWriter) {
	defer b.request.cancel()
	defer b.raw.Close()
	buf := make([]byte, 32<<10)
	forwarding := true
	for {
		n, err := b.raw.Read(buf)
		if n > 0 {
			b.parser.feed(buf[:n])
			if forwarding {
				if _, werr := writer.Write(buf[:n]); werr != nil {
					forwarding = false
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				_ = writer.Close()
			} else {
				_ = writer.CloseWithError(err)
			}
			break
		}
		if !forwarding && b.request.clientCtx.Err() == nil {
			// The handler stopped reading while the client is still there.
			return
		}
	}
	// A reply read to its end while the client was still there is finished
	// too: the client may still leave before settlement waives the charge.
	if b.abandoned.Load() {
		return
	}
	b.request.drain.finishDrain(b.parser)
}

// setFallback keeps the record settlement would have written; it is written
// as is when the drain cannot learn the usage upstream charged.
func (d *upstreamDrain) setFallback(record *model.AbnormalCharge) {
	d.mu.Lock()
	d.fallback = record
	d.mu.Unlock()
}

// markRecorded notes that settlement already wrote the request's record.
func (d *upstreamDrain) markRecorded() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.recorded = true
	d.mu.Unlock()
}

// settle hands the drain what the request was charged.
func (d *upstreamDrain) settle(c *gin.Context, info *relaycommon.RelayInfo, charged int) {
	if d == nil {
		return
	}
	chargeCase := classifyAbnormalCharge(c, info)
	if chargeCase == abnormalChargeNone {
		chargeCase = abnormalChargeClientBeforeResponse
		if info.UpstreamAttempt().Responded() {
			chargeCase = abnormalChargeClientDuringResponse
		}
	}
	outcome := model.AbnormalChargeOutcomeRefunded
	if charged > 0 {
		outcome = model.AbnormalChargeOutcomePartial
	}
	record := newAbnormalChargeRecord(c, info, chargeCase, outcome, 0)
	d.mu.Lock()
	if d.settled || d.recorded {
		d.mu.Unlock()
		return
	}
	d.settled = true
	d.record = record
	d.charged = charged
	d.ctx = c.Copy()
	d.info = *info
	ready := d.drained
	d.mu.Unlock()
	if ready {
		d.finish()
	}
}

// finishDrain takes the parser of a reply read to its end, or nil when no
// reply could be read. The usage is parsed only if settlement needs it.
func (d *upstreamDrain) finishDrain(parser *drainUsageParser) {
	d.mu.Lock()
	if d.drained {
		d.mu.Unlock()
		return
	}
	d.drained = true
	d.parser = parser
	ready := d.settled
	d.mu.Unlock()
	if ready {
		d.finish()
	}
}

// finish writes the record once both the settlement and the drain are done.
func (d *upstreamDrain) finish() {
	d.mu.Lock()
	parser, record, fallback, charged, ctx := d.parser, d.record, d.fallback, d.charged, d.ctx
	info := d.info
	d.mu.Unlock()

	var usage *dto.Usage
	if parser != nil {
		usage = parser.result()
	}

	billingUsage := normalizeUsageForBilling(effectiveBillingUsage(usage), info.GetEstimatePromptTokens())
	upstreamQuota := 0
	if billingUsage != nil {
		priced := priceTextUsage(ctx, &info, billingUsage)
		if priced.summary.hasBillableUsage() {
			upstreamQuota = priced.summary.Quota
			record.UpstreamPromptTokens = priced.summary.PromptTokens
			record.UpstreamCompletionTokens = priced.summary.CompletionTokens
		}
	}
	if upstreamQuota <= 0 {
		if fallback != nil {
			fallback.UpstreamBilling = model.UpstreamBillingUnknown
			writeAbnormalCharge(ctx, fallback)
		}
		return
	}
	if upstreamQuota <= charged {
		return
	}
	if fallback != nil {
		record.Outcome = fallback.Outcome
	}
	record.UpstreamBilling = model.UpstreamBillingBilled
	record.UpstreamQuota = upstreamQuota
	record.ChargedQuota = charged
	record.ForgoneQuota = upstreamQuota - charged
	writeAbnormalCharge(ctx, record)
	if billing_setting.MatchUpstreamCharge() && record.Id > 0 {
		chargeUpstreamDifference(ctx, &info, record)
	}
}

// chargeUpstreamDifference charges the user what upstream billed beyond the
// request's settled charge and marks the record recovered. The user sees an
// ordinary consume entry; why it was charged stays admin-only.
func chargeUpstreamDifference(ctx *gin.Context, info *relaycommon.RelayInfo, record *model.AbnormalCharge) {
	recovery, err := model.RecoverAbnormalCharges(record.UserId, 0, common.RoleRootUser, []int64{record.Id}, 0, 0)
	if err != nil {
		logger.LogError(ctx, common.LogText("failed to charge the upstream billed difference (userId=%d, quota=%d): %s",
			record.UserId, record.ForgoneQuota, err.Error()))
		return
	}
	quota := int(recovery.Quota)
	if !info.IsPlayground {
		if err := model.DecreaseTokenQuota(info.TokenId, info.TokenKey, quota); err != nil {
			common.SysLog(common.LogText("error adjusting token quota after the upstream billed difference (userId=%d, tokenId=%d, quota=%d): %s",
				info.UserId, info.TokenId, quota, err.Error()))
		}
	}
	model.UpdateUserUsedQuota(info.UserId, quota)
	model.UpdateChannelUsedQuota(record.ChannelId, quota)

	other := model.NewLogOther()
	other.SetPublic("upstream_billed_difference", true)
	other.SetPublic("upstream_quota", record.UpstreamQuota)
	other.SetPublic("charged_quota", record.ChargedQuota)
	other.SetAdmin("abnormal_charge_id", record.Id)
	other.SetAdmin("abnormal_charge_recovery_id", recovery.RecoveryId)
	model.RecordConsumeLog(ctx, info.UserId, model.RecordConsumeLogParams{
		ChannelId:        record.ChannelId,
		PromptTokens:     record.UpstreamPromptTokens,
		CompletionTokens: record.UpstreamCompletionTokens,
		ModelName:        info.OriginModelName,
		TokenName:        record.TokenName,
		Quota:            quota,
		Content: []*common.Message{common.NewMessage("Charged the usage upstream billed for a request that ended early: {{amount}}",
			map[string]any{"amount": logger.FormatQuota(quota)})},
		TokenId:  info.TokenId,
		IsStream: info.IsStream,
		Group:    info.UsingGroup,
		Other:    other,
	})
}

// FinishUpstreamDrain closes the drain of a request that ended without text
// settlement: a failed request is refunded in full, and a waived one kept its
// fallback record. Any other request settled through a path that does not
// report its charge, so nothing is recorded for it.
func FinishUpstreamDrain(c *gin.Context, info *relaycommon.RelayInfo, failed bool) {
	drain := upstreamDrainFromContext(c)
	if !drain.active() || !drain.started.Load() {
		return
	}
	drain.mu.Lock()
	settled, hasFallback := drain.settled, drain.fallback != nil
	drain.mu.Unlock()
	if settled || (!failed && !hasFallback) {
		return
	}
	drain.settle(c, info, 0)
}

// noteUpstreamDrainSettlement gives an active drain the charge text settlement
// made for a request the user ended.
func noteUpstreamDrainSettlement(c *gin.Context, info *relaycommon.RelayInfo, charged int) {
	drain := upstreamDrainFromContext(c)
	if !drain.active() || !drain.started.Load() || !classifyAbnormalCharge(c, info).userCaused() {
		return
	}
	drain.settle(c, info, charged)
}

// drainUsageParser collects the usage events of an SSE stream in any of the
// relayed formats: OpenAI chat, OpenAI Responses, Claude Messages and Gemini.
type drainUsageParser struct {
	line []byte
	// whole keeps the start of the body for a non-stream JSON reply.
	whole  []byte
	sse    bool
	openAI *dto.Usage
	resp   *dto.Usage
	claude *dto.ClaudeUsage
	gemini *dto.GeminiUsageMetadata
}

type drainUsageEvent struct {
	Type    string         `json:"type"`
	Usage   *drainRawUsage `json:"usage"`
	Message *struct {
		Usage *dto.ClaudeUsage `json:"usage"`
	} `json:"message"`
	Response *struct {
		Usage *dto.Usage `json:"usage"`
	} `json:"response"`
	UsageMetadata *dto.GeminiUsageMetadata `json:"usageMetadata"`
}

// drainRawUsage keeps a top-level usage object undecoded until the event type
// tells which format it is.
type drainRawUsage struct{ raw []byte }

func (u *drainRawUsage) UnmarshalJSON(data []byte) error {
	u.raw = append(u.raw[:0], data...)
	return nil
}

func (p *drainUsageParser) feed(data []byte) {
	if !p.sse && len(p.whole)+len(data) <= maxDrainLine {
		p.whole = append(p.whole, data...)
	}
	for len(data) > 0 {
		index := bytes.IndexByte(data, '\n')
		if index < 0 {
			if len(p.line)+len(data) <= maxDrainLine {
				p.line = append(p.line, data...)
			}
			return
		}
		if len(p.line)+index <= maxDrainLine {
			p.line = append(p.line, data[:index]...)
		}
		p.observe(bytes.TrimSpace(p.line))
		p.line = p.line[:0]
		data = data[index+1:]
	}
}

func (p *drainUsageParser) observe(line []byte) {
	payload, ok := bytes.CutPrefix(line, []byte("data:"))
	if !ok {
		return
	}
	p.sse = true
	payload = bytes.TrimSpace(payload)
	// Only events that carry usage are decoded; this also matches Gemini's
	// usageMetadata.
	if len(payload) == 0 || payload[0] != '{' || !bytes.Contains(payload, []byte(`"usage`)) {
		return
	}
	var event drainUsageEvent
	if common.Unmarshal(payload, &event) != nil {
		return
	}
	p.observeEvent(&event)
}

func (p *drainUsageParser) observeEvent(event *drainUsageEvent) {
	switch {
	case event.Type == "message_start" && event.Message != nil && event.Message.Usage != nil:
		p.mergeClaude(event.Message.Usage)
	case (event.Type == "message_delta" || event.Type == "message") && event.Usage != nil:
		var usage dto.ClaudeUsage
		if common.Unmarshal(event.Usage.raw, &usage) == nil {
			p.mergeClaude(&usage)
		}
	case event.Response != nil && event.Response.Usage != nil:
		p.resp = event.Response.Usage
	case event.UsageMetadata != nil:
		p.gemini = event.UsageMetadata
	case event.Usage != nil:
		var usage dto.Usage
		if common.Unmarshal(event.Usage.raw, &usage) == nil && dto.HasOpenAIUsageTokens(&usage) {
			p.openAI = &usage
		}
	}
}

func (p *drainUsageParser) mergeClaude(usage *dto.ClaudeUsage) {
	if p.claude == nil {
		p.claude = &dto.ClaudeUsage{}
	}
	p.claude.InputTokens = max(p.claude.InputTokens, usage.InputTokens)
	p.claude.OutputTokens = max(p.claude.OutputTokens, usage.OutputTokens)
	p.claude.CacheReadInputTokens = max(p.claude.CacheReadInputTokens, usage.CacheReadInputTokens)
	p.claude.CacheCreationInputTokens = max(p.claude.CacheCreationInputTokens, usage.CacheCreationInputTokens)
	if usage.CacheCreation != nil {
		if p.claude.CacheCreation == nil {
			p.claude.CacheCreation = &dto.ClaudeCacheCreationUsage{}
		}
		p.claude.CacheCreation.Ephemeral5mInputTokens = max(p.claude.CacheCreation.Ephemeral5mInputTokens, usage.CacheCreation.Ephemeral5mInputTokens)
		p.claude.CacheCreation.Ephemeral1hInputTokens = max(p.claude.CacheCreation.Ephemeral1hInputTokens, usage.CacheCreation.Ephemeral1hInputTokens)
	}
}

// result returns the usage upstream reported, as a billing usage settlement
// prices the same way, or nil when the stream carried none.
func (p *drainUsageParser) result() *dto.Usage {
	if p.line != nil {
		p.observe(bytes.TrimSpace(p.line))
		p.line = nil
	}
	if !p.sse && len(p.whole) > 0 {
		// A non-stream reply is one JSON body in any of the relayed formats.
		var event drainUsageEvent
		if common.Unmarshal(bytes.TrimSpace(p.whole), &event) == nil {
			p.observeEvent(&event)
		}
		p.whole = nil
	}
	var billing *dto.BillingUsage
	switch {
	case p.claude != nil:
		billing = dto.NewClaudeMessagesBillingUsage(p.claude)
	case p.resp != nil:
		billing = dto.NewOpenAIResponsesBillingUsage(p.resp)
	case p.gemini != nil:
		billing = dto.NewGeminiChatBillingUsage(p.gemini)
	case p.openAI != nil:
		billing = dto.NewOpenAIChatBillingUsage(p.openAI)
	}
	if billing == nil {
		return nil
	}
	return &dto.Usage{BillingUsage: billing}
}
