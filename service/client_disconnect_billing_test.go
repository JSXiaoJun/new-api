package service

import (
	"context"
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
