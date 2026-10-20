package billing_setting

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

const AbnormalBillingOptionPrefix = "abnormal_billing_setting."

// AbnormalBillingSetting decides whether requests that ended abnormally are
// still charged. Each switch only waives a charge the normal settlement would
// make; none of them adds a charge. An upstream or gateway failure before the
// response started is always refunded and has no switch.
//
// AlignWithOfficial overrides the three switches and settles abnormal requests
// exactly like upstream new-api: no prompt charge for a client that leaves
// before the response, no task submit kept alive after its caller leaves, and
// no waiver of usage produced before an interruption.
//
// ContinueAfterCloudflareTruncation keeps a relay running after Cloudflare
// cut an idle connection so the real usage is settled. Upstream new-api has
// no such behavior; it is independent of AlignWithOfficial.
// DB keys: abnormal_billing_setting.<json field>
type AbnormalBillingSetting struct {
	AlignWithOfficial                     bool `json:"align_with_official"`
	ChargeClientDisconnectBeforeResponse  bool `json:"charge_client_disconnect_before_response"`
	ChargeClientDisconnectDuringResponse  bool `json:"charge_client_disconnect_during_response"`
	ChargeUpstreamInterruptDuringResponse bool `json:"charge_upstream_interrupt_during_response"`
	ContinueAfterCloudflareTruncation     bool `json:"continue_after_cloudflare_truncation"`
	// MatchUpstreamCharge charges a user, after the fact, whatever the
	// channel's upstream billed for a request the user abandoned, whatever
	// the switches above waived. It needs channels that keep reading upstream
	// after the client leaves; it also makes non-stream requests on Sub2API
	// and New API channels keep reading, since those upstreams bill them.
	MatchUpstreamCharge bool `json:"match_upstream_charge"`
}

var abnormalBillingSetting = AbnormalBillingSetting{
	ChargeClientDisconnectBeforeResponse:  true,
	ChargeClientDisconnectDuringResponse:  true,
	ChargeUpstreamInterruptDuringResponse: true,
	ContinueAfterCloudflareTruncation:     true,
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
		"charge_client_disconnect_before_response",
		"charge_client_disconnect_during_response",
		"charge_upstream_interrupt_during_response",
		"continue_after_cloudflare_truncation",
		"match_upstream_charge":
	default:
		return fmt.Errorf("unknown abnormal billing option %q", key)
	}
	if value != "true" && value != "false" {
		return fmt.Errorf("%s must be true or false", key)
	}
	return nil
}
