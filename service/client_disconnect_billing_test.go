package service

import (
	"context"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	disconnectUserId     = 701
	disconnectTokenId    = 702
	disconnectTokenKey   = "client-disconnect"
	disconnectStartQuota = 10_000
	disconnectPreConsume = 5_000
)

// newDisconnectBillingCase pre-consumes like a real relay request and returns
// a cancel func that simulates the client closing the connection.
func newDisconnectBillingCase(t *testing.T, priceData types.PriceData) (*gin.Context, *relaycommon.RelayInfo, context.CancelFunc) {
	t.Helper()
	truncate(t)
	// Below the trust quota, so the request really pre-consumes.
	seedUser(t, disconnectUserId, disconnectStartQuota)
	seedToken(t, disconnectTokenId, disconnectUserId, disconnectTokenKey, disconnectStartQuota)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	reqCtx, cancel := context.WithCancel(req.Context())
	t.Cleanup(cancel)
	ctx.Request = req.WithContext(reqCtx)

	info := &relaycommon.RelayInfo{
		UserId:          disconnectUserId,
		TokenId:         disconnectTokenId,
		TokenKey:        disconnectTokenKey,
		RequestId:       t.Name(),
		OriginModelName: "gpt-disconnect-test",
		StartTime:       time.Now(),
		UserSetting:     dto.UserSetting{BillingPreference: "wallet_only"},
		PriceData:       priceData,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 703},
	}
	info.SetEstimatePromptTokens(1_000)

	require.Nil(t, PreConsumeBilling(ctx, disconnectPreConsume, info))
	require.Equal(t, disconnectStartQuota-disconnectPreConsume, getUserQuota(t, disconnectUserId))
	return ctx, info, cancel
}

func tokenPriced() types.PriceData {
	return types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 1,
		GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
	}
}

func TestClientDisconnectAfterUpstreamSendChargesPromptTokens(t *testing.T) {
	ctx, info, cancel := newDisconnectBillingCase(t, tokenPriced())
	info.MarkUpstreamStart()
	cancel()

	settled := SettleClientDisconnectBilling(ctx, info)

	require.True(t, settled)
	session := info.Billing.(*BillingSession)
	assert.False(t, session.NeedsRefund(), "settled session must not be refunded")
	charged := disconnectStartQuota - getUserQuota(t, disconnectUserId)
	assert.Positive(t, charged, "prompt tokens must be charged")
	assert.Less(t, charged, disconnectPreConsume, "only the input side is charged, not the full reservation")
}

func TestClientDisconnectBeforeUpstreamSendKeepsRefund(t *testing.T) {
	ctx, info, cancel := newDisconnectBillingCase(t, tokenPriced())
	cancel()

	assert.False(t, SettleClientDisconnectBilling(ctx, info))
	assert.True(t, info.Billing.(*BillingSession).NeedsRefund())
}

func TestUpstreamFailureWithoutClientDisconnectKeepsRefund(t *testing.T) {
	ctx, info, _ := newDisconnectBillingCase(t, tokenPriced())
	info.MarkUpstreamStart()

	assert.False(t, SettleClientDisconnectBilling(ctx, info))
	assert.True(t, info.Billing.(*BillingSession).NeedsRefund())
}

func newProxiedRequestContext(t *testing.T, cfRay string, minIdleSeconds int) (*gin.Context, *httptest.ResponseRecorder, context.CancelFunc) {
	t.Helper()
	prevEnabled, prevMax, prevIdle := common.CloudflareDisconnectBillingEnabled, common.CloudflareDisconnectBillingMaxSeconds, common.CloudflareDisconnectBillingMinIdleSeconds
	common.CloudflareDisconnectBillingEnabled, common.CloudflareDisconnectBillingMaxSeconds, common.CloudflareDisconnectBillingMinIdleSeconds = true, 1800, minIdleSeconds
	t.Cleanup(func() {
		common.CloudflareDisconnectBillingEnabled, common.CloudflareDisconnectBillingMaxSeconds, common.CloudflareDisconnectBillingMinIdleSeconds = prevEnabled, prevMax, prevIdle
	})
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest("POST", "/v1beta/models/gemini:generateContent", nil)
	if cfRay != "" {
		req.Header.Set("Cf-Ray", cfRay)
	}
	reqCtx, cancel := context.WithCancel(req.Context())
	t.Cleanup(cancel)
	ctx.Request = req.WithContext(reqCtx)
	return ctx, recorder, cancel
}

func TestCloudflareTruncationKeepsRelayRunning(t *testing.T) {
	ctx, recorder, cancel := newProxiedRequestContext(t, "8f0c1d2e3a4b5c6d-SJC", 0)
	release := DetachProxiedClientCancellation(ctx)
	defer release()

	_, err := ctx.Writer.WriteString("before")
	require.NoError(t, err)
	assert.False(t, ClientRequestCanceled(ctx))

	cancel()

	require.Eventually(t, func() bool { return ProxiedClientGone(ctx) }, time.Second, time.Millisecond)
	assert.NoError(t, ctx.Request.Context().Err(), "upstream context must survive the Cloudflare truncation")
	assert.True(t, ClientRequestCanceled(ctx), "retry and refund paths must still see the client as gone")
	n, err := ctx.Writer.WriteString("after")
	require.NoError(t, err)
	assert.Equal(t, len("after"), n)
	assert.Equal(t, "before", recorder.Body.String(), "writes after the disconnect are discarded")

	streamSteps := 0
	clientGone := ctx.Stream(func(w io.Writer) bool {
		streamSteps++
		return streamSteps < 3
	})
	assert.False(t, clientGone, "c.Stream handlers must keep reading upstream after a truncation")
	assert.Equal(t, 3, streamSteps)
	closeNotify := ctx.Writer.CloseNotify()

	release()
	assert.Error(t, ctx.Request.Context().Err(), "release must cancel the detached relay")
	select {
	case <-closeNotify:
	case <-time.After(time.Second):
		t.Fatal("CloseNotify must fire once the relay ends")
	}
}

func TestDisconnectWithoutCloudflareTruncationCancelsRelay(t *testing.T) {
	tests := []struct {
		name  string
		cfRay string
	}{
		{name: "direct connection", cfRay: ""},
		{name: "user abort through Cloudflare before idle timeout", cfRay: "8f0c1d2e3a4b5c6d-SJC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _, cancel := newProxiedRequestContext(t, tt.cfRay, 90)
			release := DetachProxiedClientCancellation(ctx)
			defer release()

			cancel()

			select {
			case <-ctx.Request.Context().Done():
			case <-time.After(time.Second):
				t.Fatal("relay context was not cancelled")
			}
			assert.True(t, ClientRequestCanceled(ctx))
			assert.False(t, ProxiedClientGone(ctx))
		})
	}
}

func TestClientDisconnectOnPerCallModelKeepsRefund(t *testing.T) {
	priceData := tokenPriced()
	priceData.UsePrice = true
	priceData.ModelPrice = 0.01
	ctx, info, cancel := newDisconnectBillingCase(t, priceData)
	info.MarkUpstreamStart()
	cancel()

	assert.False(t, SettleClientDisconnectBilling(ctx, info))
	assert.True(t, info.Billing.(*BillingSession).NeedsRefund())
}
