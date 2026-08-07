package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestDiscountedTextQuotaMinimumChargePreservesFreePricing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	usage := &dto.Usage{PromptTokens: 1, TotalTokens: 1}

	paid := &relaycommon.RelayInfo{
		OriginModelName: "tiny-fixed-price",
		PriceData: hosttypes.PriceData{
			UsePrice:       true,
			ModelPrice:     0.000000001,
			GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 0.01},
		},
	}
	assert.Equal(t, 1, calculateTextQuotaSummary(ctx, paid, usage).Quota)

	free := &relaycommon.RelayInfo{
		OriginModelName: "free-fixed-price",
		PriceData: hosttypes.PriceData{
			UsePrice:       true,
			ModelPrice:     0.000000001,
			GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 0},
		},
	}
	assert.Equal(t, 0, calculateTextQuotaSummary(ctx, free, usage).Quota)
}

func TestDiscountedTextQuotaChargesMinimumWhenBillableUsageRoundsToZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	usage := &dto.Usage{CompletionTokens: 1, TotalTokens: 1}

	paid := &relaycommon.RelayInfo{
		OriginModelName: "missing-token-breakdown",
		PriceData: hosttypes.PriceData{
			ModelRatio:      1,
			CompletionRatio: 0,
			GroupRatioInfo:  hosttypes.GroupRatioInfo{GroupRatio: 0.01},
		},
	}
	assert.Equal(t, 1, calculateTextQuotaSummary(ctx, paid, usage).Quota)

	paid.PriceData.GroupRatioInfo.GroupRatio = 0
	assert.Equal(t, 0, calculateTextQuotaSummary(ctx, paid, usage).Quota)
}

func TestDiscountedAudioQuotaMinimumChargePreservesFreePricing(t *testing.T) {
	paid, paidClamp := calculateAudioQuota(QuotaInfo{
		InputDetails: TokenDetails{TextTokens: 1},
		ModelName:    "tiny-audio-price",
		UsePrice:     true,
		ModelPrice:   0.000000001,
		GroupRatio:   0.01,
	})
	assert.Nil(t, paidClamp)
	assert.Equal(t, 1, paid)

	free, freeClamp := calculateAudioQuota(QuotaInfo{
		InputDetails: TokenDetails{TextTokens: 1},
		ModelName:    "free-audio-price",
		UsePrice:     true,
		ModelPrice:   0.000000001,
		GroupRatio:   0,
	})
	assert.Nil(t, freeClamp)
	assert.Equal(t, 0, free)
}

func TestViolationFeeUsesSafeMinimumAndSaturatingConversion(t *testing.T) {
	quota, clamp := calcViolationFeeQuota(0.000000001, 0.01)
	assert.Nil(t, clamp)
	assert.Equal(t, 1, quota)

	quota, clamp = calcViolationFeeQuota(1, 0)
	assert.Nil(t, clamp)
	assert.Equal(t, 0, quota)

	quota, clamp = calcViolationFeeQuota(1e20, 1)
	assert.Equal(t, common.MaxQuota, quota)
	assert.NotNil(t, clamp)
	assert.Equal(t, common.QuotaClampOverflow, clamp.Kind)
}
