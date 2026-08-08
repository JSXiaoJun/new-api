package relay

import (
	"math"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsResponsesEventStreamContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        bool
	}{
		{name: "plain", contentType: "text/event-stream", want: true},
		{name: "mixed case with charset", contentType: "Text/Event-Stream; charset=utf-8", want: true},
		{name: "json", contentType: "application/json", want: false},
		{name: "empty", contentType: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isResponsesEventStreamContentType(tt.contentType))
		})
	}
}

func TestRecalcQuotaFromRatiosIgnoresInvalidMultipliers(t *testing.T) {
	info := &relaycommon.RelayInfo{
		PriceData: types.PriceData{
			QuotaBeforeGroup: 50,
			GroupRatioInfo:   types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	info.PriceData.AddOtherRatio("duration", 2)

	quota, ok := recalcQuotaFromRatios(info, map[string]float64{
		"duration": 3,
		"zero":     0,
		"negative": -1,
		"nan":      math.NaN(),
		"inf":      math.Inf(1),
	})

	require.True(t, ok)
	assert.Equal(t, 150, quota)
	assert.True(t, info.PriceData.HasOtherRatio("duration"))
}

func TestRecalcQuotaFromRatiosRejectsAllInvalidAdjustedRatios(t *testing.T) {
	info := &relaycommon.RelayInfo{
		PriceData: types.PriceData{
			QuotaBeforeGroup: 50,
			GroupRatioInfo:   types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	info.PriceData.AddOtherRatio("duration", 2)

	quota, ok := recalcQuotaFromRatios(info, map[string]float64{
		"zero":     0,
		"negative": -1,
		"nan":      math.NaN(),
		"inf":      math.Inf(1),
	})

	require.False(t, ok)
	assert.Equal(t, 0, quota)
	assert.True(t, info.PriceData.HasOtherRatio("duration"))
}

func TestCalculateTaskQuotaRoundsOnlyAfterAllRatios(t *testing.T) {
	tests := []struct {
		name       string
		baseQuota  float64
		groupRatio float64
		want       int
	}{
		{name: "positive fractional charge remains billable", baseQuota: 0.5, groupRatio: 0.6, want: 1},
		{name: "preserves fractional base before large ratio", baseQuota: 0.5, groupRatio: 60, want: 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quota, clamp := calculateTaskQuota(types.PriceData{
				QuotaBeforeGroup: tt.baseQuota,
				GroupRatioInfo:   types.GroupRatioInfo{GroupRatio: tt.groupRatio},
			})

			assert.Nil(t, clamp)
			assert.Equal(t, tt.want, quota)
		})
	}
}
