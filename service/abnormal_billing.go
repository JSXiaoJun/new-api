package service

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/gin-gonic/gin"
)

// abnormalChargeCase is the billing situation of a request that did not end
// normally. A failure before the response started is always refunded.
type abnormalChargeCase int

const (
	abnormalChargeNone abnormalChargeCase = iota
	abnormalChargeClientBeforeResponse
	abnormalChargeClientDuringResponse
	abnormalChargeUpstreamDuringResponse
	abnormalChargeUpstreamBeforeResponse
)

const (
	abnormalChargeCaseKey = "abnormal_charge_case"
	// abnormalRequestFailedKey marks a request settled only because it failed
	// after the client left; official new-api refunds such a request in full.
	abnormalRequestFailedKey = "abnormal_request_failed"
	// upstreamBilledFailureKey marks a failed request charged the usage
	// upstream reported (charge_upstream_billed_failure).
	upstreamBilledFailureKey = "upstream_billed_failure"
)

var abnormalChargeCaseNames = map[abnormalChargeCase]string{
	abnormalChargeClientBeforeResponse:   "client_before_response",
	abnormalChargeClientDuringResponse:   "client_during_response",
	abnormalChargeUpstreamDuringResponse: "upstream_during_response",
	abnormalChargeUpstreamBeforeResponse: "upstream_before_response",
}

func (chargeCase abnormalChargeCase) userCaused() bool {
	return chargeCase == abnormalChargeClientBeforeResponse || chargeCase == abnormalChargeClientDuringResponse
}

// classifyAbnormalCharge reads the tracked upstream attempt. A request whose
// attempt is not tracked is treated as normal so it keeps today's charge.
func classifyAbnormalCharge(c *gin.Context, info *relaycommon.RelayInfo) abnormalChargeCase {
	if c != nil {
		if value, ok := c.Get(abnormalChargeCaseKey); ok {
			if chargeCase, ok := value.(abnormalChargeCase); ok {
				return chargeCase
			}
		}
	}
	attempt := info.UpstreamAttempt()
	if attempt == nil {
		return abnormalChargeNone
	}
	// A Cloudflare-truncated relay keeps running, so the attempt itself ends
	// normally; the moment the client left decides the case.
	if goneAt, gone := proxiedClientGoneAt(c); gone {
		if attempt.RespondedBefore(goneAt) {
			return abnormalChargeClientDuringResponse
		}
		return abnormalChargeClientBeforeResponse
	}
	termination := info.Termination()
	if termination == nil {
		return abnormalChargeNone
	}
	receiving := termination.Phase == relaycommon.TerminationPhaseReceiving
	switch {
	case termination.EndedBy == relaycommon.TerminationEndedByClient && receiving:
		return abnormalChargeClientDuringResponse
	case termination.EndedBy == relaycommon.TerminationEndedByClient:
		return abnormalChargeClientBeforeResponse
	case receiving:
		return abnormalChargeUpstreamDuringResponse
	default:
		return abnormalChargeUpstreamBeforeResponse
	}
}

// policyBlockIsFree reports whether upstream refused the request by a safety
// policy while charge_policy_block is off, so nothing may be charged for it.
func policyBlockIsFree(info *relaycommon.RelayInfo) bool {
	return info != nil && !billing_setting.ChargePolicyBlock() && info.StreamStatus.PolicyBlockCode() != ""
}

// abnormalChargeWaiver reports whether settlement must drop the charge and
// the log line explaining why. A request upstream refused by a safety policy
// is free unless charge_policy_block is on. A strict billing user is charged
// whenever the user caused the abnormal end. Otherwise the official alignment
// waives only a request that failed after the client left, which official
// new-api refunds, and charges the usage of every other interrupted response.
func abnormalChargeWaiver(c *gin.Context, info *relaycommon.RelayInfo, chargeCase abnormalChargeCase) (bool, *common.Message) {
	if policyBlockIsFree(info) {
		return true, common.NewMessage("Not charged: upstream blocked the request by a safety policy")
	}
	if chargeCase == abnormalChargeNone || c != nil && c.GetBool(upstreamBilledFailureKey) {
		return false, nil
	}
	if chargeCase.userCaused() && info != nil && billing_setting.IsStrictBillingUser(info.UserId) {
		return false, nil
	}
	settings := billing_setting.GetAbnormalBillingSetting()
	// Upstream bills whatever it produced before the request ended, so a user
	// is never let off a charge upstream made. Only a failure before upstream
	// answered stays free: nothing was produced or billed.
	if settings.MatchUpstreamCharge && chargeCase != abnormalChargeUpstreamBeforeResponse {
		return false, nil
	}
	if settings.AlignWithOfficial {
		if c == nil || !c.GetBool(abnormalRequestFailedKey) {
			return false, nil
		}
		switch chargeCase {
		case abnormalChargeClientDuringResponse:
			return true, common.NewMessage("Not charged: the client disconnected while receiving the response")
		case abnormalChargeUpstreamDuringResponse:
			return true, common.NewMessage("Not charged: upstream or the gateway interrupted the response")
		default:
			return true, common.NewMessage("Not charged: the client disconnected before upstream responded")
		}
	}
	if chargeCase == abnormalChargeUpstreamBeforeResponse {
		return true, common.NewMessage("Not charged: upstream or the gateway failed before responding")
	}
	return false, nil
}

// recordAbnormalCharge keeps the uncollected charge of an abnormal request for
// review and later recovery. A failed write is only logged: it must never
// change the settlement. When the gateway is still reading upstream after the
// client left, the record waits for the usage upstream charged.
func recordAbnormalCharge(c *gin.Context, info *relaycommon.RelayInfo, chargeCase abnormalChargeCase, outcome string, forgoneQuota int) {
	if info == nil || forgoneQuota <= 0 || chargeCase == abnormalChargeNone {
		return
	}
	record := newAbnormalChargeRecord(c, info, chargeCase, outcome, forgoneQuota)
	drain := upstreamDrainFromContext(c)
	if drain.active() && drain.started.Load() && chargeCase.userCaused() {
		drain.setFallback(record)
		return
	}
	if UpstreamDrainEnabled(info) && chargeCase.userCaused() {
		// The channel's upstream bills abandoned requests, but this reply could
		// not be read to learn how much.
		record.UpstreamBilling = model.UpstreamBillingUnknown
	}
	// The request has its record; a drain that starts only now must not
	// write a second one that could be recovered again.
	drain.markRecorded()
	writeAbnormalCharge(c, record)
}

func writeAbnormalCharge(c *gin.Context, record *model.AbnormalCharge) {
	if err := model.RecordAbnormalCharge(record); err != nil {
		logger.LogError(c, common.LogText("failed to record abnormal charge: %s", err.Error()))
	}
}

// newAbnormalChargeRecord captures the request facts while the request is
// still being handled.
func newAbnormalChargeRecord(c *gin.Context, info *relaycommon.RelayInfo, chargeCase abnormalChargeCase, outcome string, forgoneQuota int) *model.AbnormalCharge {
	record := &model.AbnormalCharge{
		UserId:       info.UserId,
		RequestId:    info.RequestId,
		TokenId:      info.TokenId,
		ModelName:    info.OriginModelName,
		IsStream:     info.IsStream,
		Case:         abnormalChargeCaseNames[chargeCase],
		UserCaused:   chargeCase.userCaused(),
		Outcome:      outcome,
		ForgoneQuota: forgoneQuota,
	}
	if info.ChannelMeta != nil {
		record.ChannelId = info.ChannelId
	}
	if termination := info.Termination(); termination != nil {
		record.EndedBy, record.Phase, record.Reason, record.ElapsedMs =
			termination.EndedBy, termination.Phase, termination.Reason, termination.ElapsedMs
	} else if goneAt, gone := proxiedClientGoneAt(c); gone {
		record.EndedBy, record.Reason = relaycommon.TerminationEndedByClient, "cloudflare_truncated"
		if !info.UpstreamStartTime.IsZero() {
			record.ElapsedMs = goneAt.Sub(info.UpstreamStartTime).Milliseconds()
		}
	}
	if record.ElapsedMs == 0 && !info.UpstreamStartTime.IsZero() {
		record.ElapsedMs = time.Since(info.UpstreamStartTime).Milliseconds()
	}
	if c != nil {
		record.TokenName = c.GetString("token_name")
		record.Ip = c.ClientIP()
		if c.Writer != nil {
			record.ClientReceivedData = c.Writer.Size() > 0
		}
	}
	return record
}

// RecordAbnormalTaskRefund records a task submit refunded because its caller
// disconnected after upstream had already accepted it.
func RecordAbnormalTaskRefund(c *gin.Context, info *relaycommon.RelayInfo, refundedQuota int) {
	if info == nil || refundedQuota <= 0 {
		return
	}
	record := &model.AbnormalCharge{
		UserId:       info.UserId,
		RequestId:    info.RequestId,
		TokenId:      info.TokenId,
		ModelName:    info.OriginModelName,
		IsTask:       true,
		Case:         abnormalChargeCaseNames[abnormalChargeClientDuringResponse],
		UserCaused:   true,
		EndedBy:      relaycommon.TerminationEndedByClient,
		Reason:       "task_caller_left",
		Outcome:      model.AbnormalChargeOutcomeRefunded,
		ForgoneQuota: refundedQuota,
	}
	if info.ChannelMeta != nil {
		record.ChannelId = info.ChannelId
	}
	if !info.StartTime.IsZero() {
		record.ElapsedMs = time.Since(info.StartTime).Milliseconds()
	}
	if c != nil {
		record.TokenName = c.GetString("token_name")
		record.Ip = c.ClientIP()
	}
	if err := model.RecordAbnormalCharge(record); err != nil {
		logger.LogError(c, common.LogText("failed to record abnormal charge: %s", err.Error()))
	}
}
