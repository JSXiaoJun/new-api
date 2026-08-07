package service

import (
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppendBillingFormulaRecordsFrozenDiscountInputs(t *testing.T) {
	other := map[string]interface{}{}
	info := &relaycommon.RelayInfo{
		BillingBaseGroupRatio:   1.25,
		BillingDiscountRatio:    0.8,
		BillingDiscountResolved: true,
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}

	appendBillingFormula(other, info, "per_token", 1200, 1.5, 25, 1825)

	formula, ok := other["billing_formula"].(billingFormulaLog)
	require.True(t, ok)
	assert.Equal(t, "per_token", formula.Mode)
	assert.InDelta(t, 1200, formula.BaseQuota, 0.000001)
	assert.InDelta(t, 1.25, formula.BaseGroupRatio, 0.000001)
	assert.InDelta(t, 0.8, formula.DiscountRatio, 0.000001)
	assert.InDelta(t, 1, formula.EffectiveGroupRatio, 0.000001)
	assert.InDelta(t, 1.5, formula.OtherRatio, 0.000001)
	assert.InDelta(t, 25, formula.SurchargeQuota, 0.000001)
	assert.Equal(t, 1825, formula.FinalQuota)
}

func TestAppendBillingFormulaRejectsNegativeAuditValues(t *testing.T) {
	other := map[string]interface{}{}
	info := &relaycommon.RelayInfo{
		BillingDiscountRatio:    1,
		BillingDiscountResolved: true,
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}

	appendBillingFormula(other, info, "per_token", -1, 1, 0, 10)

	assert.NotContains(t, other, "billing_formula")
}

func TestAppendBillingFormulaMarksMinimumCharge(t *testing.T) {
	other := map[string]interface{}{}
	info := &relaycommon.RelayInfo{
		BillingBaseGroupRatio:   1,
		BillingDiscountRatio:    0.01,
		BillingDiscountResolved: true,
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.01},
		},
	}

	appendBillingFormula(other, info, "per_call", 0.1, 1, 0, 1)

	formula, ok := other["billing_formula"].(billingFormulaLog)
	require.True(t, ok)
	assert.True(t, formula.MinimumChargeApplied)
}

func TestTextBillingFormulaRecomposesDiscountedQuota(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName:         "discount-formula-model",
		StartTime:               time.Now(),
		BillingBaseGroupRatio:   1,
		BillingDiscountRatio:    0.8,
		BillingDiscountResolved: true,
		PriceData: types.PriceData{
			ModelRatio:      2,
			CompletionRatio: 2,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 0.8},
		},
	}
	info.PriceData.AddOtherRatio("request", 1.5)

	summary := calculateTextQuotaSummary(ctx, info, &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	})
	other := map[string]interface{}{}
	appendBillingFormula(other, info, "per_token", summary.BaseQuotaBeforeGroup.InexactFloat64(), summary.OtherRatioMultiplier, summary.ToolCallSurchargeQuota.InexactFloat64(), summary.Quota)

	formula, ok := other["billing_formula"].(billingFormulaLog)
	require.True(t, ok)
	assert.Equal(t, 480, summary.Quota)
	assert.Equal(t, summary.Quota, formula.FinalQuota)
	assert.InDelta(t, 400, formula.BaseQuota, 0.000001)
	assert.InDelta(t, 480, formula.BaseQuota*formula.BaseGroupRatio*formula.DiscountRatio*formula.OtherRatio+formula.SurchargeQuota, 0.000001)
}
