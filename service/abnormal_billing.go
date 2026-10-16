package service

import (
	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/gin-gonic/gin"
)

// abnormalChargeCase is the billing situation of a request that did not end
// normally. Each case maps to one administrator switch, except a failure
// before the response started, which is always refunded.
type abnormalChargeCase int

const (
	abnormalChargeNone abnormalChargeCase = iota
	abnormalChargeClientBeforeResponse
	abnormalChargeClientDuringResponse
	abnormalChargeUpstreamDuringResponse
	abnormalChargeUpstreamBeforeResponse
)

const abnormalChargeCaseKey = "abnormal_charge_case"

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

// abnormalChargeWaiver reports whether settlement must drop the charge and
// the log line explaining why.
func abnormalChargeWaiver(chargeCase abnormalChargeCase) (bool, *common.Message) {
	settings := billing_setting.GetAbnormalBillingSetting()
	switch chargeCase {
	case abnormalChargeClientBeforeResponse:
		if !settings.ChargeClientDisconnectBeforeResponse {
			return true, common.NewMessage("Not charged: the client disconnected before upstream responded")
		}
	case abnormalChargeClientDuringResponse:
		if !settings.ChargeClientDisconnectDuringResponse {
			return true, common.NewMessage("Not charged: the client disconnected while receiving the response")
		}
	case abnormalChargeUpstreamDuringResponse:
		if !settings.ChargeUpstreamInterruptDuringResponse {
			return true, common.NewMessage("Not charged: upstream or the gateway interrupted the response")
		}
	case abnormalChargeUpstreamBeforeResponse:
		return true, common.NewMessage("Not charged: upstream or the gateway failed before responding")
	}
	return false, nil
}
