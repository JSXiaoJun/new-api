package billing_setting

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

const AbnormalBillingOptionPrefix = "abnormal_billing_setting."

// AbnormalBillingSetting decides whether requests that ended abnormally are
// still charged. Without the official alignment, an interrupted response is
// charged the usage produced so far, a client that leaves after the request
// reached upstream is charged the estimated input, and an upstream or gateway
// failure before the response started is always refunded.
//
// AlignWithOfficial settles abnormal requests exactly like upstream new-api:
// no prompt charge for a client that leaves before the response, no task
// submit kept alive after its caller leaves, and no waiver of usage produced
// before an interruption.
//
// ContinueAfterCloudflareTruncation keeps a relay running after Cloudflare
// cut an idle connection so the real usage is settled. Upstream new-api has
// no such behavior; it is independent of AlignWithOfficial.
// DB keys: abnormal_billing_setting.<json field>
type AbnormalBillingSetting struct {
	AlignWithOfficial                 bool `json:"align_with_official"`
	ContinueAfterCloudflareTruncation bool `json:"continue_after_cloudflare_truncation"`
	// MatchUpstreamCharge charges a user, after the fact, whatever the
	// channel's upstream billed for a request the user abandoned, whatever
	// the switches above waived. It needs channels that keep reading upstream
	// after the client leaves; it also makes non-stream requests on Sub2API
	// and New API channels keep reading, since those upstreams bill them.
	MatchUpstreamCharge bool `json:"match_upstream_charge"`
	// ChargeUpstreamBilledFailure charges a request that failed after
	// upstream had already reported usage, such as a stream upstream broke
	// off, what upstream reported, instead of refunding it. Only the final
	// attempt counts; earlier failed retries stay unbilled.
	ChargeUpstreamBilledFailure bool `json:"charge_upstream_billed_failure"`
	// ChargePolicyBlock charges a request upstream refused by a safety policy
	// (cyber_policy and the like) by the rules above. Off, such a request is
	// never charged, whatever the other switches say, as Sub2API does.
	ChargePolicyBlock bool `json:"charge_policy_block"`
}

var abnormalBillingSetting = AbnormalBillingSetting{
	ContinueAfterCloudflareTruncation: true,
}

func init() {
	config.GlobalConfig.Register("abnormal_billing_setting", &abnormalBillingSetting)
}

func GetAbnormalBillingSetting() AbnormalBillingSetting {
	return abnormalBillingSetting
}

// AbnormalBillingAlignsWithOfficial reports whether abnormal requests settle
// exactly like upstream new-api.
func AbnormalBillingAlignsWithOfficial() bool {
	return abnormalBillingSetting.AlignWithOfficial
}

// MatchUpstreamCharge reports whether abandoned requests are charged what
// upstream billed.
func MatchUpstreamCharge() bool {
	return abnormalBillingSetting.MatchUpstreamCharge
}

// ChargeUpstreamBilledFailure reports whether a failed request is charged
// the usage upstream reported before it failed.
func ChargeUpstreamBilledFailure() bool {
	return abnormalBillingSetting.ChargeUpstreamBilledFailure
}

// ChargePolicyBlock reports whether a request upstream refused by a safety
// policy is charged.
func ChargePolicyBlock() bool {
	return abnormalBillingSetting.ChargePolicyBlock
}

// ContinueAfterCloudflareTruncation reports whether a relay keeps reading
// upstream after Cloudflare truncated the client connection.
func ContinueAfterCloudflareTruncation() bool {
	return abnormalBillingSetting.ContinueAfterCloudflareTruncation
}

// ValidateAbnormalBillingOption accepts only the declared switches with an
// explicit true/false value, so a typo can never silently keep the old policy.
func ValidateAbnormalBillingOption(key, value string) error {
	field, ok := strings.CutPrefix(key, AbnormalBillingOptionPrefix)
	if !ok {
		return nil
	}
	switch field {
	case "align_with_official",
		"continue_after_cloudflare_truncation",
		"match_upstream_charge",
		"charge_upstream_billed_failure",
		"charge_policy_block":
	default:
		return fmt.Errorf("unknown abnormal billing option %q", key)
	}
	if value != "true" && value != "false" {
		return fmt.Errorf("%s must be true or false", key)
	}
	return nil
}
