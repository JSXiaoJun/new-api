package service

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/gin-gonic/gin"
)

const detachedClientWriterKey = "detached_client_writer"

// detachedClientWriter keeps a relay running after the client disconnected
// once the request reached the upstream: writes are then discarded and
// reported as successful so handlers keep reading upstream until the real
// usage arrives.
type detachedClientWriter struct {
	gin.ResponseWriter
	clientCtx         context.Context
	relayCtx          context.Context
	viaCloudflare     bool
	upstreamContacted atomic.Bool
	writeFailed       atomic.Bool
	truncated         atomic.Bool
	truncatedAtUnixNs atomic.Int64
}

func (w *detachedClientWriter) clientGone() bool {
	return w.writeFailed.Load() || w.clientCtx.Err() != nil
}

func (w *detachedClientWriter) Write(data []byte) (int, error) {
	if w.clientGone() {
		return len(data), nil
	}
	if _, err := w.ResponseWriter.Write(data); err != nil {
		w.writeFailed.Store(true)
	}
	return len(data), nil
}

func (w *detachedClientWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func (w *detachedClientWriter) Flush() {
	if w.clientGone() {
		return
	}
	w.ResponseWriter.Flush()
}

// CloseNotify follows the relay context rather than the client connection so
// handlers built on gin's c.Stream keep reading upstream after a truncation.
func (w *detachedClientWriter) CloseNotify() <-chan bool {
	notify := make(chan bool, 1)
	context.AfterFunc(w.relayCtx, func() { notify <- true })
	return notify
}

func (w *detachedClientWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// DetachProxiedClientCancellation decouples the relay from the inbound
// connection, the way Sub2API detaches its upstream context: once the request
// has reached the upstream, a client disconnect no longer cancels the upstream
// call. The upstream bills a request it already received, whether the user
// stopped the reply, Cloudflare cut an idle connection after about 100
// seconds, or a downstream gateway hit its stream timeout. Detaching lets the
// relay read to the end and settle the real usage instead of the prompt
// estimate, which is zero when token counting is off and is all an
// OpenAI-format stream leaves before its final usage frame.
//
// A client that leaves before the upstream was contacted cancels the relay as
// before, so nothing is billed. A detached relay may run at most
// CloudflareDisconnectBillingMaxSeconds before it is cancelled.
//
// The returned release func must be called when the request ends.
func DetachProxiedClientCancellation(c *gin.Context) func() {
	if !common.CloudflareDisconnectBillingEnabled || c == nil || c.Request == nil || c.Writer == nil ||
		!billing_setting.ContinueAfterCloudflareTruncation() {
		return func() {}
	}
	clientCtx := c.Request.Context()
	detachedCtx, cancel := context.WithCancel(context.WithoutCancel(clientCtx))
	maxRun := time.Duration(common.CloudflareDisconnectBillingMaxSeconds) * time.Second
	if maxRun <= 0 {
		maxRun = 30 * time.Minute
	}
	writer := &detachedClientWriter{
		ResponseWriter: c.Writer,
		clientCtx:      clientCtx,
		relayCtx:       detachedCtx,
		viaCloudflare:  c.GetHeader("Cf-Ray") != "",
	}
	stopWatch := context.AfterFunc(clientCtx, func() {
		if !writer.upstreamContacted.Load() {
			cancel()
			return
		}
		writer.truncatedAtUnixNs.Store(time.Now().UnixNano())
		writer.truncated.Store(true)
		timer := time.NewTimer(maxRun)
		defer timer.Stop()
		select {
		case <-timer.C:
			cancel()
		case <-detachedCtx.Done():
		}
	})

	c.Writer = writer
	c.Set(detachedClientWriterKey, writer)
	c.Request = c.Request.WithContext(detachedCtx)
	return func() {
		stopWatch()
		cancel()
	}
}

// MarkUpstreamContacted records that the relay is about to send the request
// upstream, from which point a client disconnect keeps the relay running.
// Call it right before the upstream HTTP call, after checking that the client
// is still there.
func MarkUpstreamContacted(c *gin.Context) {
	if c == nil {
		return
	}
	if value, ok := c.Get(detachedClientWriterKey); ok {
		if writer, ok := value.(*detachedClientWriter); ok && writer.clientCtx.Err() == nil {
			writer.upstreamContacted.Store(true)
		}
	}
}

// ProxiedClientGone reports whether the client of a detached request
// disconnected after the request reached the upstream and the relay kept
// running.
func ProxiedClientGone(c *gin.Context) bool {
	if c == nil {
		return false
	}
	value, ok := c.Get(detachedClientWriterKey)
	if !ok {
		return false
	}
	writer, ok := value.(*detachedClientWriter)
	return ok && writer.truncated.Load()
}

// detachedClientBilledMessage is the log line of a detached request that was
// charged its actual usage after the client disconnected.
func detachedClientBilledMessage(c *gin.Context) *common.Message {
	if value, ok := c.Get(detachedClientWriterKey); ok {
		if writer, ok := value.(*detachedClientWriter); ok && writer.viaCloudflare {
			return common.NewMessage("Client disconnected through Cloudflare after the upstream finished, billed by actual usage")
		}
	}
	return common.NewMessage("Client disconnected after the request reached the upstream; the upstream finished and was billed by actual usage")
}

// proxiedClientGoneAt returns when the client connection of a detached
// request was cut.
func proxiedClientGoneAt(c *gin.Context) (time.Time, bool) {
	if c == nil {
		return time.Time{}, false
	}
	value, ok := c.Get(detachedClientWriterKey)
	if !ok {
		return time.Time{}, false
	}
	writer, ok := value.(*detachedClientWriter)
	if !ok || !writer.truncated.Load() {
		return time.Time{}, false
	}
	return time.Unix(0, writer.truncatedAtUnixNs.Load()), true
}

// ClientRequestCanceled reports whether the inbound client connection is gone.
func ClientRequestCanceled(c *gin.Context) bool {
	return c != nil && c.Request != nil && (c.Request.Context().Err() != nil || ProxiedClientGone(c))
}

// SettleClientDisconnectBilling handles a relay that failed because the client
// disconnected after the request had already been sent upstream (non-stream
// requests, or streams cancelled before the first byte). Upstream providers
// usually bill the processed input even when the call is cancelled, so instead
// of refunding the whole reservation this settles the estimated prompt tokens.
//
// It returns true when the session was settled. Callers should still invoke
// Billing.Refund afterwards; Refund is a no-op on a settled session and acts
// as a fallback if settlement did not complete.
//
// Under the official alignment the request is settled at zero, which leaves
// the same balance as the full refund upstream new-api makes, so the waived
// prompt charge is logged and kept for recovery.
//
// Per-call priced and free models keep the previous full-refund behavior,
// since there is no token count to charge proportionally.
func SettleClientDisconnectBilling(c *gin.Context, info *relaycommon.RelayInfo) bool {
	if info == nil || info.Billing == nil || info.ChannelMeta == nil || !ClientRequestCanceled(c) {
		return false
	}
	// MarkUpstreamStart is only called right before the upstream HTTP call,
	// so a zero start time means nothing reached the provider.
	if info.UpstreamStartTime.IsZero() {
		return false
	}
	if info.PriceData.UsePrice || info.PriceData.FreeModel {
		return false
	}
	promptTokens := info.GetEstimatePromptTokens()
	if promptTokens <= 0 {
		return false
	}
	chargeCase := classifyAbnormalCharge(c, info)
	if chargeCase == abnormalChargeNone {
		// The relay failed only because the client left, so the attempt still
		// tells which side of the response the client left from.
		chargeCase = abnormalChargeClientBeforeResponse
		if info.UpstreamAttempt().Responded() {
			chargeCase = abnormalChargeClientDuringResponse
		}
	}
	if chargeCase == abnormalChargeUpstreamBeforeResponse {
		// Upstream failed before answering; the client leaving afterwards does
		// not make that billable.
		return false
	}
	// Settlement reads the case back and decides the waiver itself.
	c.Set(abnormalChargeCaseKey, chargeCase)
	usage := &dto.Usage{
		PromptTokens: promptTokens,
		InputTokens:  promptTokens,
		TotalTokens:  promptTokens,
	}
	var content []*common.Message
	if waived, _ := abnormalChargeWaiver(info, chargeCase); !waived {
		content = append(content, common.NewMessage("Client disconnected after the request reached the upstream, billed by usage"))
	}
	PostTextConsumeQuota(c, info, usage, content)
	return true
}
