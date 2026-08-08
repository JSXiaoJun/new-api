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

func TestPaidTextQuotaDoesNotDisappearAtIntegerBoundary(t *testing.T) {
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

func TestPaidTextQuotaPreservesExistingNonzeroCharge(t *testing.T) {
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

func TestPaidAudioQuotaDoesNotDisappearAtIntegerBoundary(t *testing.T) {
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

func TestViolationFeeUsesSafePositiveAndSaturatingConversion(t *testing.T) {
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

func TestApplyBillingDiscountNeverTurnsPaidRequestFree(t *testing.T) {
	info := &relaycommon.RelayInfo{
		BillingDiscountResolved: true,
		BillingDiscountRatio:    0.9,
	}

	assert.Equal(t, 0, ApplyBillingDiscount(info, 0))
	assert.Equal(t, 1, ApplyBillingDiscount(info, 1))
	assert.Equal(t, 9, ApplyBillingDiscount(info, 10))

	// A positive integer charge whose discounted value is below 0.5 must
	// retain the original charge instead of rounding to a free request.
	info.BillingDiscountRatio = 0.49
	assert.Equal(t, 1, ApplyBillingDiscount(info, 1))
	assert.Equal(t, 1, ApplyBillingDiscount(info, 2))
	// Exactly 0.5 rounds to one quota unit, so the normal discount remains
	// active at this boundary.
	info.BillingDiscountRatio = 0.5
	assert.Equal(t, 1, ApplyBillingDiscount(info, 1))
	info.BillingDiscountRatio = 0.01
	assert.Equal(t, 1, ApplyBillingDiscount(info, 100))
}
