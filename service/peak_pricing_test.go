package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPeakAudioSettlementUsesCapturedTokenPrices(t *testing.T) {
	price := types.PriceData{ModelRatio: 2, CompletionRatio: 3, AudioRatio: 4, AudioCompletionRatio: 5}
	quota, clamp := calculateAudioQuota(QuotaInfo{
		ModelName:  "peak-audio-no-global-pricing",
		ModelRatio: price.ModelRatio, GroupRatio: 1.5,
		InputDetails:  TokenDetails{TextTokens: 100, AudioTokens: 20},
		OutputDetails: TokenDetails{TextTokens: 10, AudioTokens: 5},
		PeakPriceData: &price,
	})
	assert.Equal(t, 930, quota)
	assert.Nil(t, clamp)
}

func TestPeakAudioSettlementUsesCapturedFixedPrice(t *testing.T) {
	price := types.PriceData{UsePrice: true, ModelPrice: 0.8}
	quota, clamp := calculateAudioQuota(QuotaInfo{
		ModelName: "peak-audio-fixed", UsePrice: true,
		ModelPrice: 0, GroupRatio: 1.5, PeakPriceData: &price,
	})
	assert.Equal(t, int(common.QuotaPerUnit*1.2), quota)
	assert.Nil(t, clamp)
}

func TestPeakTaskModeOverridesLegacySoraModeAndSurvivesPersistence(t *testing.T) {
	info := &relaycommon.RelayInfo{OriginModelName: "sora-2", ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeSora}}
	for _, mode := range []string{billing_setting.BillingModePerSecond, billing_setting.BillingModePerRequest, billing_setting.BillingModePerToken} {
		t.Run(mode, func(t *testing.T) {
			info.PeakPricing = &billing_setting.PeakSnapshot{Timezone: "UTC", Period: "09:00-18:00", Tariff: billing_setting.PeakTariff{Mode: mode}}
			assert.Equal(t, mode == billing_setting.BillingModePerRequest, IsTaskPerCallBilling(info))
			assert.Equal(t, mode == billing_setting.BillingModePerSecond, IsTaskPerSecondBilling(info))
			data, err := common.Marshal(model.TaskBillingContext{BillingMode: info.EffectiveBillingMode(), PeakPricing: info.PeakPricing})
			require.NoError(t, err)
			var context model.TaskBillingContext
			require.NoError(t, common.Unmarshal(data, &context))
			other := taskBillingOther(&model.Task{PrivateData: model.TaskPrivateData{BillingContext: &context}})
			assert.Equal(t, info.PeakPricing, other["peak_pricing"])
		})
	}
}
