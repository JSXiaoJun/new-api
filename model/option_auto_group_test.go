package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateOptionValueRejectsInvalidMaxTokenAutoGroups(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "1.5", "invalid"} {
		t.Run(value, func(t *testing.T) {
			assert.Error(t, validateOptionValue("MaxTokenAutoGroups", value))
		})
	}
	require.NoError(t, validateOptionValue("MaxTokenAutoGroups", "999999"))
}

func TestValidateOptionValueAcceptsOnlyBooleanAbnormalBillingSwitches(t *testing.T) {
	for _, field := range []string{
		"align_with_official",
		"charge_client_disconnect_before_response",
		"charge_client_disconnect_during_response",
		"charge_upstream_interrupt_during_response",
		"continue_after_cloudflare_truncation",
	} {
		key := "abnormal_billing_setting." + field
		for _, value := range []string{"true", "false"} {
			assert.NoError(t, validateOptionValue(key, value), "%s=%s", key, value)
		}
		for _, value := range []string{"", "1", "0", "TRUE", "yes", " true"} {
			assert.Error(t, validateOptionValue(key, value), "%s=%q must be rejected", key, value)
		}
	}
	assert.Error(t, validateOptionValue("abnormal_billing_setting.charge_everything", "true"), "unknown switches must be rejected")
}
