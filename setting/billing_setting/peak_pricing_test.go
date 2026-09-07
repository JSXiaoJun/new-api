package billing_setting

import (
	"math"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func peakTestSchedule(t *testing.T) PeakPricing {
	t.Helper()
	prices, err := ParsePeakPricing(`{"test":{"timezone":"Asia/Shanghai","default":{"mode":"per_request","price":1},"periods":[{"start":"22:00","end":"06:00","tariff":{"mode":"per_second","price":0.1}},{"start":"09:00","end":"18:00","tariff":{"mode":"per_token","input_price":2,"output_price":6}}]}}`)
	require.NoError(t, err)
	return prices["test"]
}

func TestPeakPricingDailyBoundaries(t *testing.T) {
	schedule := peakTestSchedule(t)
	for _, tc := range []struct{ utc, period, mode string }{
		{"2026-09-07T13:59:59Z", "default", BillingModePerRequest},
		{"2026-09-07T14:00:00Z", "22:00-06:00", BillingModePerSecond},
		{"2026-09-07T16:00:00Z", "22:00-06:00", BillingModePerSecond},
		{"2026-09-07T21:59:59Z", "22:00-06:00", BillingModePerSecond},
		{"2026-09-07T22:00:00Z", "default", BillingModePerRequest},
		{"2026-09-08T01:00:00Z", "09:00-18:00", BillingModePerToken},
		{"2026-09-08T10:00:00Z", "default", BillingModePerRequest},
	} {
		t.Run(tc.utc, func(t *testing.T) {
			at, err := time.Parse(time.RFC3339, tc.utc)
			require.NoError(t, err)
			snapshot, err := schedule.Resolve(at)
			require.NoError(t, err)
			assert.Equal(t, tc.period, snapshot.Period)
			assert.Equal(t, tc.mode, snapshot.Tariff.Mode)
		})
	}
}

func TestPeakPricingRejectsInvalidSchedules(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*PeakPricing)
	}{
		{"overlap across midnight", func(p *PeakPricing) { p.Periods[1].Start = "05:30" }},
		{"empty period", func(p *PeakPricing) { p.Periods[0].End = "22:00" }},
		{"invalid timezone", func(p *PeakPricing) { p.Timezone = "Invalid/Zone" }},
		{"implicit timezone", func(p *PeakPricing) { p.Timezone = "Local" }},
		{"invalid time", func(p *PeakPricing) { p.Periods[0].Start = "24:00" }},
		{"no periods", func(p *PeakPricing) { p.Periods = nil }},
		{"missing price", func(p *PeakPricing) { p.Default.Price = nil }},
		{"negative price", func(p *PeakPricing) { p.Default.Price = common.GetPointer(-1.0) }},
		{"infinite price", func(p *PeakPricing) { p.Default.Price = common.GetPointer(math.Inf(1)) }},
		{"NaN price", func(p *PeakPricing) { p.Default.Price = common.GetPointer(math.NaN()) }},
		{"invalid expression", func(p *PeakPricing) { p.Default = PeakTariff{Mode: BillingModeTieredExpr, Expression: "p ***"} }},
		{"negative expression", func(p *PeakPricing) { p.Default = PeakTariff{Mode: BillingModeTieredExpr, Expression: "-1.0"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schedule := peakTestSchedule(t)
			tc.change(&schedule)
			assert.Error(t, schedule.Validate())
		})
	}
}

func TestPeakTokenTariffUsesIndependentPrices(t *testing.T) {
	tariff := PeakTariff{Mode: BillingModePerToken, InputPrice: common.GetPointer(2.0), OutputPrice: common.GetPointer(6.0), CachePrice: common.GetPointer(0.0), AudioPrice: common.GetPointer(4.0), AudioOutputPrice: common.GetPointer(12.0)}
	require.NoError(t, tariff.Validate())
	price := tariff.PriceData()
	assert.Equal(t, 1.0, price.ModelRatio)
	assert.Equal(t, 3.0, price.CompletionRatio)
	assert.Zero(t, price.CacheRatio)
	assert.Equal(t, 2.0, price.AudioRatio)
	assert.Equal(t, 3.0, price.AudioCompletionRatio)
	tariff.InputPrice = common.GetPointer(math.SmallestNonzeroFloat64)
	assert.Error(t, tariff.Validate())
}
