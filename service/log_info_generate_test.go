package service

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
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

	assert.Equal(t, float64(2000), other.Snapshot()["frt"])
	assert.Equal(t, float64(500), other.Snapshot()["upstream_frt"])
	upstreamDuration, ok := other.Snapshot()["upstream_duration"].(float64)
	require.True(t, ok)
	assert.InDelta(t, 3.75, upstreamDuration, 0.000001)
	assert.Equal(t, float64(1500), other.Snapshot()["gateway_overhead"])
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

	assert.NotContains(t, other.Snapshot(), "upstream_frt")
	assert.NotContains(t, other.Snapshot(), "upstream_duration")
	assert.Equal(t, float64(2000), other.Snapshot()["gateway_overhead"])
}

func TestAppendRelayTerminationNamesTheEndingSide(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name         string
		failure      error
		canceled     bool
		clientWrote  bool
		wantEndedBy  string
		wantAdminErr bool
	}{
		{name: "client disconnect keeps no upstream error", failure: context.Canceled, canceled: true, wantEndedBy: relaycommon.TerminationEndedByClient},
		{name: "upstream drop keeps its error admin-only", failure: errors.New(`Post "https://10.0.0.8:8080/v1/responses": EOF`), wantEndedBy: relaycommon.TerminationEndedByUpstream, wantAdminErr: true},
		{name: "client data written before the drop is reported", failure: context.Canceled, canceled: true, clientWrote: true, wantEndedBy: relaycommon.TerminationEndedByClient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			if tt.clientWrote {
				_, err := ctx.Writer.Write([]byte(": PING\n\n"))
				require.NoError(t, err)
			}
			info := &relaycommon.RelayInfo{IsStream: true}
			info.MarkUpstreamStart()
			info.UpstreamAttempt().Fail(tt.failure, tt.canceled)

			other := model.NewLogOther()
			AppendRelayTermination(ctx, info, other)

			snapshot := other.Snapshot()
			termination, ok := snapshot["termination"].(map[string]any)
			require.True(t, ok, "termination must be public so users see who disconnected")
			assert.Equal(t, tt.wantEndedBy, termination["ended_by"])
			assert.Equal(t, relaycommon.TerminationPhaseSending, termination["phase"])
			assert.Equal(t, tt.clientWrote, termination["client_received_data"])
			adminInfo, _ := snapshot["admin_info"].(map[string]any)
			if !tt.wantAdminErr {
				assert.NotContains(t, adminInfo, "termination_error")
				return
			}
			require.Contains(t, adminInfo, "termination_error")
			assert.NotContains(t, adminInfo["termination_error"], "10.0.0.8")
			assert.NotContains(t, termination, "error")
		})
	}
}

func TestAppendRelayTerminationSkipsCompletedAttempts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	info := &relaycommon.RelayInfo{}
	info.MarkUpstreamStart()
	info.UpstreamAttempt().MarkResponse()

	other := model.NewLogOther()
	AppendRelayTermination(ctx, info, other)
	AppendRelayTermination(ctx, &relaycommon.RelayInfo{}, other)

	assert.NotContains(t, other.Snapshot(), "termination")
}
