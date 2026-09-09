package service

import (
	"errors"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTrustedWalletChargesUsageDespiteTokenExhaustionOrDeletion(t *testing.T) {
	for _, mode := range []string{"actual_exceeds_token", "exhausted_after_admission", "deleted_after_admission", "token_write_failure"} {
		t.Run(mode, func(t *testing.T) {
			truncate(t)
			oldQuotaPerUnit := common.QuotaPerUnit
			common.QuotaPerUnit = 500_000
			t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })
			seedUser(t, 601, 20_000_000)
			seedToken(t, 602, 601, "trusted-wallet-integrity", 6_000_000)
			ctx, _ := gin.CreateTestContext(nil)
			ctx.Set("token_quota", 6_000_000)
			info := &relaycommon.RelayInfo{UserId: 601, TokenId: 602, TokenKey: "trusted-wallet-integrity",
				RequestId: mode, UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
			session, apiErr := NewBillingSession(ctx, info, 100)
			require.Nil(t, apiErr)
			require.True(t, session.trusted)
			require.Zero(t, session.GetPreConsumedQuota())
			switch mode {
			case "exhausted_after_admission":
				require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 602).Update("remain_quota", 0).Error)
			case "deleted_after_admission":
				require.NoError(t, model.DB.Delete(&model.Token{}, 602).Error)
			case "token_write_failure":
				const callback = "test:token_settlement_failure"
				require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
					if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Token" {
						tx.AddError(errors.New("token storage unavailable"))
					}
				}))
				t.Cleanup(func() { require.NoError(t, model.DB.Callback().Update().Remove(callback)) })
			}
			err := session.Settle(7_000_000)
			if mode == "token_write_failure" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, 13_000_000, getUserQuota(t, 601))
			if mode == "actual_exceeds_token" {
				assert.Equal(t, -1_000_000, getTokenRemainQuota(t, 602))
			}
			if mode == "exhausted_after_admission" {
				assert.Equal(t, -7_000_000, getTokenRemainQuota(t, 602))
			}
			// Two callers retrying settlement cannot charge the same session twice.
			var wg sync.WaitGroup
			errs := make(chan error, 2)
			for range 2 {
				wg.Add(1)
				go func() { defer wg.Done(); errs <- session.Settle(7_000_000) }()
			}
			wg.Wait()
			for range 2 {
				require.NoError(t, <-errs)
			}
			session.Refund(ctx)
			assert.False(t, session.NeedsRefund())
			assert.Equal(t, 13_000_000, getUserQuota(t, 601))
		})
	}
}

func TestZeroEstimatedWalletStillChargesActualUsage(t *testing.T) {
	truncate(t)
	seedUser(t, 603, 100)
	seedToken(t, 604, 603, "empty-estimate-wallet", 0)
	ctx, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{UserId: 603, TokenId: 604, TokenKey: "empty-estimate-wallet",
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
	session, apiErr := NewBillingSession(ctx, info, 0)
	require.Nil(t, apiErr)
	require.NoError(t, session.Settle(30))
	assert.Equal(t, 70, getUserQuota(t, 603))
	assert.Equal(t, -30, getTokenRemainQuota(t, 604))
}

func TestWalletSettlementRetryAfterFundingFailureChargesOnlyOnce(t *testing.T) {
	truncate(t)
	seedUser(t, 605, 100)
	seedToken(t, 606, 605, "wallet-retry", 100)
	ctx, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{UserId: 605, TokenId: 606, TokenKey: "wallet-retry",
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
	session, apiErr := NewBillingSession(ctx, info, 20)
	require.Nil(t, apiErr)
	t.Run("database failure", func(t *testing.T) {
		const callback = "test:wallet_settlement_failure"
		require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "User" {
				tx.AddError(errors.New("wallet storage unavailable"))
			}
		}))
		t.Cleanup(func() { require.NoError(t, model.DB.Callback().Update().Remove(callback)) })
		require.Error(t, session.Settle(30))
		assert.Equal(t, 80, getUserQuota(t, 605))
		assert.Equal(t, 80, getTokenRemainQuota(t, 606))
	})
	require.NoError(t, session.Settle(30))
	require.NoError(t, session.Settle(30))
	assert.Equal(t, 70, getUserQuota(t, 605))
	assert.Equal(t, 70, getTokenRemainQuota(t, 606))
}

func TestRefundedWalletSessionRejectsLateSettlement(t *testing.T) {
	truncate(t)
	seedUser(t, 607, 100)
	seedToken(t, 608, 607, "refunded-wallet", 100)
	session := &BillingSession{relayInfo: &relaycommon.RelayInfo{UserId: 607, TokenId: 608},
		funding: &WalletFunding{userId: 607}, preConsumedQuota: 20, refunded: true}
	require.Error(t, session.Settle(10))
	assert.Equal(t, 100, getUserQuota(t, 607))
	assert.Equal(t, 100, getTokenRemainQuota(t, 608))
}
