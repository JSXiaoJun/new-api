package service

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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
