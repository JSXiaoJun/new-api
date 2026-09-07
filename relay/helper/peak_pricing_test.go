package helper

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPeakPriceHelperFreezesTariffAcrossTimeAndConfigChanges(t *testing.T) {
	originalGroups := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"peak-test":1}`))
	require.NoError(t, billing_setting.LoadPeakPricing(`{"peak-model":{"timezone":"UTC","default":{"mode":"per_request","price":1},"periods":[{"start":"09:00","end":"18:00","tariff":{"mode":"per_request","price":2}}]}}`))
	t.Cleanup(func() {
		require.NoError(t, billing_setting.LoadPeakPricing(`{}`))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroups))
	})
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{OriginModelName: "peak-model", UsingGroup: "peak-test", UserGroup: "peak-test", StartTime: time.Date(2026, 9, 7, 17, 59, 0, 0, time.UTC)}
	price, err := ModelPriceHelper(ctx, info, 0, &relaytypes.TokenCountMeta{})
	require.NoError(t, err)
	assert.Equal(t, common.QuotaFromFloat(2*common.QuotaPerUnit), price.QuotaToPreConsume)
	require.NoError(t, billing_setting.LoadPeakPricing(`{}`))
	info.StartTime = info.StartTime.Add(time.Hour)
	retryPrice, err := ModelPriceHelper(ctx, info, 0, &relaytypes.TokenCountMeta{})
	require.NoError(t, err)
	assert.Equal(t, price, retryPrice)
	assert.Equal(t, "09:00-18:00", info.PeakPricing.Period)
}

func TestPeakPriceHelperModesAndOverflow(t *testing.T) {
	originalGroups := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"peak-test":1}`))
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroups)) })
	for _, tc := range []struct {
		name      string
		tariff    billing_setting.PeakTariff
		task      bool
		wantPrice float64
		wantErr   bool
	}{
		{"token", billing_setting.PeakTariff{Mode: "per_token", InputPrice: common.GetPointer(4.0), OutputPrice: common.GetPointer(12.0)}, false, 0, false},
		{"request", billing_setting.PeakTariff{Mode: "per_request", Price: common.GetPointer(0.2)}, false, 0.2, false},
		{"free", billing_setting.PeakTariff{Mode: "per_request", Price: common.GetPointer(0.0)}, false, 0, false},
		{"seconds task", billing_setting.PeakTariff{Mode: "per_second", Price: common.GetPointer(0.3)}, true, 0.3, false},
		{"seconds synchronous rejected", billing_setting.PeakTariff{Mode: "per_second", Price: common.GetPointer(0.3)}, false, 0, true},
		{"oversized request rejected", billing_setting.PeakTariff{Mode: "per_request", Price: common.GetPointer(1e30)}, false, 0, true},
		{"expression", billing_setting.PeakTariff{Mode: "tiered_expr", Expression: `hour("UTC") == 9 ? p * 2 + c * 4 : p * 10`}, false, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{OriginModelName: "peak-only-model", UsingGroup: "peak-test", UserGroup: "peak-test", StartTime: time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC), PeakPricingResolved: true, PeakPricing: &billing_setting.PeakSnapshot{Timezone: "UTC", Period: "default", Tariff: tc.tariff}}
			price, err := ModelPriceHelper(ctx, info, 1000, &relaytypes.TokenCountMeta{MaxTokens: 100})
			if tc.task {
				price, err = ModelPriceHelperPerCall(ctx, info)
			}
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantPrice, price.ModelPrice)
			if tc.tariff.Mode == "per_token" {
				assert.Equal(t, 2.0, price.ModelRatio)
				assert.Equal(t, 3.0, price.CompletionRatio)
			}
			if tc.tariff.Mode == "tiered_expr" {
				require.NotNil(t, info.BillingRequestInput)
				cost, _, err := billingexpr.RunExprWithRequest(info.TieredBillingSnapshot.ExprString, billingexpr.TokenParams{P: 2000, C: 200}, *info.BillingRequestInput)
				require.NoError(t, err)
				assert.Equal(t, 4800.0, cost)
			}
		})
	}
}
