package service

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsTaskPerCallBillingUsesSoraChannelType(t *testing.T) {
	previous := constant.TaskPricePatches
	t.Cleanup(func() { constant.TaskPricePatches = previous })
	constant.TaskPricePatches = nil

	require.True(t, IsTaskPerCallBilling(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeSora},
	}))
	assert.False(t, IsTaskPerCallBilling(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI},
	}))
	require.True(t, IsTaskPerCallBilling(&relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI},
		OriginModelName: "sora-2-custom",
	}))
}

func TestIsTaskPerCallBillingKeepsLegacyModelPatch(t *testing.T) {
	previous := constant.TaskPricePatches
	t.Cleanup(func() { constant.TaskPricePatches = previous })
	constant.TaskPricePatches = []string{"legacy-video-model"}

	require.True(t, IsTaskPerCallBilling(&relaycommon.RelayInfo{OriginModelName: "legacy-video-model"}))
	assert.False(t, IsTaskPerCallBilling(&relaycommon.RelayInfo{OriginModelName: "other-model"}))
}

func TestPerSecondBillingOverridesSoraPerCallDetection(t *testing.T) {
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"sora-per-second":"per_second"}`,
	}))

	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeSora},
		OriginModelName: "sora-per-second",
	}

	require.Equal(t, billing_setting.BillingModePerSecond, billing_setting.GetBillingMode(info.OriginModelName))
	assert.True(t, IsTaskPerSecondBilling(info))
	assert.False(t, IsTaskPerCallBilling(info))
}
