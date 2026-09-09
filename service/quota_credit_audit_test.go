package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalletSettlementRefundAudit(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	const userID = 201
	seedUser(t, userID, 100)
	info := &relaycommon.RelayInfo{
		UserId: userID, RequestId: "wallet-settlement-audit", IsPlayground: true,
		ForcePreConsume: true,
		UserSetting:     dto.UserSetting{BillingPreference: "wallet_only"},
	}
	session, apiErr := NewBillingSession(ctx, info, 40)
	require.Nil(t, apiErr)
	require.NoError(t, session.Settle(10))
	require.NoError(t, session.Settle(10))
	assert.Equal(t, 90, getUserQuota(t, userID))

	var credits []model.QuotaCredit
	require.NoError(t, model.DB.Where("user_id = ?", userID).Find(&credits).Error)
	require.Len(t, credits, 1)
	assert.EqualValues(t, 30, credits[0].Delta)
	assert.Equal(t, "wallet_settlement_refund", credits[0].Source)
	assert.Equal(t, info.RequestId, credits[0].RequestId)
}

func TestWalletFundingRefundAudit(t *testing.T) {
	truncate(t)
	const userID = 202
	seedUser(t, userID, 100)
	funding := &WalletFunding{userId: userID, requestId: "wallet-failure-audit"}
	require.NoError(t, funding.PreConsume(40))
	require.NoError(t, funding.Refund())
	assert.Equal(t, 100, getUserQuota(t, userID))

	var credits []model.QuotaCredit
	require.NoError(t, model.DB.Where("user_id = ?", userID).Find(&credits).Error)
	require.Len(t, credits, 1)
	assert.EqualValues(t, 40, credits[0].Delta)
	assert.Equal(t, "wallet_refund", credits[0].Source)
	assert.Equal(t, funding.requestId, credits[0].RequestId)
}

func TestWalletReserveRollbackAudit(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	const userID, tokenID = 203, 203
	seedUser(t, userID, 100)
	seedToken(t, tokenID, userID, "wallet-reserve-token", 40)
	info := &relaycommon.RelayInfo{
		UserId: userID, TokenId: tokenID, TokenKey: "wallet-reserve-token",
		RequestId: "wallet-rollback-audit", ForcePreConsume: true,
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
	}
	session, apiErr := NewBillingSession(ctx, info, 40)
	require.Nil(t, apiErr)
	require.Error(t, session.Reserve(60))
	assert.Equal(t, 60, getUserQuota(t, userID))
	assert.Zero(t, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 40, session.GetPreConsumedQuota())

	var credits []model.QuotaCredit
	require.NoError(t, model.DB.Where("user_id = ?", userID).Find(&credits).Error)
	require.Len(t, credits, 1)
	assert.EqualValues(t, 20, credits[0].Delta)
	assert.Equal(t, "preconsume_rollback", credits[0].Source)
	assert.Equal(t, info.RequestId, credits[0].RequestId)
}

func TestLegacyWalletRefundAudit(t *testing.T) {
	truncate(t)
	const userID = 204
	seedUser(t, userID, 100)
	info := &relaycommon.RelayInfo{
		UserId: userID, RequestId: "legacy-refund-audit", IsPlayground: true,
	}
	require.NoError(t, PostConsumeQuota(info, -20, 0, false))
	assert.Equal(t, 120, getUserQuota(t, userID))

	var credits []model.QuotaCredit
	require.NoError(t, model.DB.Where("user_id = ?", userID).Find(&credits).Error)
	require.Len(t, credits, 1)
	assert.EqualValues(t, 20, credits[0].Delta)
	assert.Equal(t, "wallet_settlement_refund", credits[0].Source)
	assert.Equal(t, info.RequestId, credits[0].RequestId)
}
