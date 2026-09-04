package service

import (
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateTextOtherInfoRecordsUpstreamTimingSeparately(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)

	start := time.Unix(1_700_000_000, 0)
	info := &relaycommon.RelayInfo{
		StartTime:                 start,
		FirstResponseTime:         start.Add(2 * time.Second),
		UpstreamStartTime:         start.Add(1500 * time.Millisecond),
		UpstreamFirstResponseTime: start.Add(2 * time.Second),
		UpstreamEndTime:           start.Add(5250 * time.Millisecond),
		ChannelMeta:               &relaycommon.ChannelMeta{},
	}

	other := GenerateTextOtherInfo(ctx, info, 1, 1, 1, 0, 0, 0, 1)

	assert.Equal(t, float64(2000), other["frt"])
	assert.Equal(t, float64(500), other["upstream_frt"])
	upstreamDuration, ok := other["upstream_duration"].(float64)
	require.True(t, ok)
	assert.InDelta(t, 3.75, upstreamDuration, 0.000001)
	assert.Equal(t, float64(1500), other["gateway_overhead"])
}

func TestGenerateTextOtherInfoOmitsInvalidUpstreamTiming(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)

	start := time.Unix(1_700_000_000, 0)
	info := &relaycommon.RelayInfo{
		StartTime:                 start,
		FirstResponseTime:         start.Add(time.Second),
		UpstreamStartTime:         start.Add(2 * time.Second),
		UpstreamFirstResponseTime: start.Add(time.Second),
		UpstreamEndTime:           start.Add(1500 * time.Millisecond),
		ChannelMeta:               &relaycommon.ChannelMeta{},
	}

	other := GenerateTextOtherInfo(ctx, info, 1, 1, 1, 0, 0, 0, 1)

	assert.NotContains(t, other, "upstream_frt")
	assert.NotContains(t, other, "upstream_duration")
	assert.Equal(t, float64(2000), other["gateway_overhead"])
}
