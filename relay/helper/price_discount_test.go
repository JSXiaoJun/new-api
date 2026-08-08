package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleGroupRatioKeepsDiscountSeparateFromBaseRatio(t *testing.T) {
	original := ratio_setting.GroupRatio2JSONString()
	originalSpecial := ratio_setting.GroupGroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1.5}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{"vip":{"default":1.25}}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(original))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(originalSpecial))
	})

	ctx, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		UsingGroup:              "default",
		UserGroup:               "vip",
		BillingDiscountRatio:    0.8,
		BillingDiscountResolved: true,
	}

	groupRatio := HandleGroupRatio(ctx, info)
	assert.True(t, groupRatio.HasSpecialRatio)
	assert.InDelta(t, 1.25, info.BillingBaseGroupRatio, 0.000001)
	assert.InDelta(t, 1.25, groupRatio.GroupRatio, 0.000001)
	assert.InDelta(t, 1.25, groupRatio.GroupSpecialRatio, 0.000001)

	info.BillingDiscountRatio = 0.6
	groupRatio = HandleGroupRatio(ctx, info)
	assert.InDelta(t, 1.25, info.BillingBaseGroupRatio, 0.000001)
	assert.InDelta(t, 1.25, groupRatio.GroupRatio, 0.000001)
	assert.InDelta(t, 1.25, groupRatio.GroupSpecialRatio, 0.000001)
}

func TestModelPriceHelperKeepsBasePricingSeparateFromDiscount(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	originalModelRatios := ratio_setting.ModelRatio2JSONString()
	originalModelPrices := ratio_setting.ModelPrice2JSONString()
	originalBillingModes := billing_setting.GetBillingModeCopy()
	originalBillingExprs := billing_setting.GetBillingExprCopy()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalModelPrices))
		billingConfig := config.GlobalConfig.Get("billing_setting")
		modeJSON, err := common.Marshal(originalBillingModes)
		require.NoError(t, err)
		exprJSON, err := common.Marshal(originalBillingExprs)
		require.NoError(t, err)
		require.NoError(t, config.UpdateConfigFromMap(billingConfig, map[string]string{
			"billing_mode": string(modeJSON),
			"billing_expr": string(exprJSON),
		}))
	})

	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1.5}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"discount-ratio-model":2}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"discount-fixed-model":0.01}`))
	billingConfig := config.GlobalConfig.Get("billing_setting")
	require.NotNil(t, billingConfig)
	require.NoError(t, config.UpdateConfigFromMap(billingConfig, map[string]string{
		"billing_mode": `{"discount-tiered-model":"tiered_expr"}`,
		"billing_expr": `{"discount-tiered-model":"tier(\"base\", p * 2)"}`,
	}))

	newRequest := func(model string) (*gin.Context, *relaycommon.RelayInfo) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		return ctx, &relaycommon.RelayInfo{
			OriginModelName:         model,
			UsingGroup:              "default",
			UserGroup:               "vip",
			BillingDiscountRatio:    0.8,
			BillingDiscountResolved: true,
			BillingRequestInput: &billingexpr.RequestInput{
				Body: []byte(`{}`),
			},
		}
	}

	tests := []struct {
		name      string
		model     string
		meta      *relaytypes.TokenCountMeta
		wantQuota int
	}{
		{name: "ratio billing", model: "discount-ratio-model", meta: &relaytypes.TokenCountMeta{}, wantQuota: 3000},
		{name: "fixed price billing", model: "discount-fixed-model", meta: &relaytypes.TokenCountMeta{}, wantQuota: 7500},
		{name: "tiered expression billing", model: "discount-tiered-model", meta: &relaytypes.TokenCountMeta{MaxTokens: 1}, wantQuota: 1500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, info := newRequest(tt.model)
			priceData, err := ModelPriceHelper(ctx, info, 1000, tt.meta)
			require.NoError(t, err)
			assert.InDelta(t, 1.5, priceData.GroupRatioInfo.GroupRatio, 0.000001)
			assert.Equal(t, tt.wantQuota, priceData.QuotaToPreConsume)
		})
	}
}

func TestModelPriceHelperPreservesPositiveBasePricing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	originalModelRatios := ratio_setting.ModelRatio2JSONString()
	originalModelPrices := ratio_setting.ModelPrice2JSONString()
	originalBillingModes := billing_setting.GetBillingModeCopy()
	originalBillingExprs := billing_setting.GetBillingExprCopy()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalModelPrices))
		billingConfig := config.GlobalConfig.Get("billing_setting")
		modeJSON, err := common.Marshal(originalBillingModes)
		require.NoError(t, err)
		exprJSON, err := common.Marshal(originalBillingExprs)
		require.NoError(t, err)
		require.NoError(t, config.UpdateConfigFromMap(billingConfig, map[string]string{
			"billing_mode": string(modeJSON),
			"billing_expr": string(exprJSON),
		}))
	})

	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"paid":0.01,"free":0}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"tiny-ratio-model":0.000001}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"tiny-fixed-model":0.000000001}`))
	billingConfig := config.GlobalConfig.Get("billing_setting")
	require.NotNil(t, billingConfig)
	require.NoError(t, config.UpdateConfigFromMap(billingConfig, map[string]string{
		"billing_mode": `{"tiny-tiered-model":"tiered_expr"}`,
		"billing_expr": `{"tiny-tiered-model":"tier(\"base\", p * 0.000001)"}`,
	}))

	tests := []struct {
		name  string
		model string
		group string
		want  int
	}{
		{name: "paid ratio", model: "tiny-ratio-model", group: "paid", want: 1},
		{name: "free ratio", model: "tiny-ratio-model", group: "free", want: 0},
		{name: "paid fixed", model: "tiny-fixed-model", group: "paid", want: 1},
		{name: "free fixed", model: "tiny-fixed-model", group: "free", want: 0},
		{name: "paid tiered", model: "tiny-tiered-model", group: "paid", want: 1},
		{name: "free tiered", model: "tiny-tiered-model", group: "free", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{
				OriginModelName:         tt.model,
				UsingGroup:              tt.group,
				UserGroup:               "vip",
				BillingDiscountRatio:    1,
				BillingDiscountResolved: true,
			}
			priceData, err := ModelPriceHelper(ctx, info, 1, &relaytypes.TokenCountMeta{MaxTokens: 1})
			require.NoError(t, err)
			assert.Equal(t, tt.want, priceData.QuotaToPreConsume)
		})
	}
}
