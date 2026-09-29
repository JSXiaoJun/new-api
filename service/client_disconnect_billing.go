package service

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

// ClientRequestCanceled reports whether the inbound client connection is gone.
func ClientRequestCanceled(c *gin.Context) bool {
	return c != nil && c.Request != nil && c.Request.Context().Err() != nil
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
	usage := &dto.Usage{
		PromptTokens: promptTokens,
		InputTokens:  promptTokens,
		TotalTokens:  promptTokens,
	}
	PostTextConsumeQuota(c, info, usage, []string{"客户端断开连接，但已发送到上游，按使用计费"})
	return true
}
