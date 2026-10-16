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
// DB keys: abnormal_billing_setting.<json field>
type AbnormalBillingSetting struct {
	ChargeClientDisconnectBeforeResponse  bool `json:"charge_client_disconnect_before_response"`
	ChargeClientDisconnectDuringResponse  bool `json:"charge_client_disconnect_during_response"`
	ChargeUpstreamInterruptDuringResponse bool `json:"charge_upstream_interrupt_during_response"`
}

var abnormalBillingSetting = AbnormalBillingSetting{
	ChargeClientDisconnectBeforeResponse:  true,
	ChargeClientDisconnectDuringResponse:  true,
	ChargeUpstreamInterruptDuringResponse: true,
}

func init() {
	config.GlobalConfig.Register("abnormal_billing_setting", &abnormalBillingSetting)
}

func GetAbnormalBillingSetting() AbnormalBillingSetting {
	return abnormalBillingSetting
}

// ValidateAbnormalBillingOption accepts only the declared switches with an
// explicit true/false value, so a typo can never silently keep the old policy.
func ValidateAbnormalBillingOption(key, value string) error {
	field, ok := strings.CutPrefix(key, AbnormalBillingOptionPrefix)
	if !ok {
		return nil
	}
	switch field {
	case "charge_client_disconnect_before_response",
		"charge_client_disconnect_during_response",
		"charge_upstream_interrupt_during_response":
	default:
		return fmt.Errorf("unknown abnormal billing option %q", key)
	}
	if value != "true" && value != "false" {
		return fmt.Errorf("%s must be true or false", key)
	}
	return nil
}
