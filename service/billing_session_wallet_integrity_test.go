package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
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
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
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
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
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
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
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

// walletReplyHook fails the first wallet apply script after it is armed, either
// before Redis runs it or after it ran, the way a lost reply looks to the caller.
type walletReplyHook struct {
	afterApply bool
	failed     *atomic.Bool
}

func isWalletApply(cmd redis.Cmder) bool {
	return cmd.Name() == "eval" && strings.Contains(fmt.Sprint(cmd.Args()[1]), "local previous")
}

func (h walletReplyHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	if !h.afterApply && isWalletApply(cmd) && h.failed.CompareAndSwap(false, true) {
		return ctx, errors.New("i/o timeout")
	}
	return ctx, nil
}

func (h walletReplyHook) AfterProcess(_ context.Context, cmd redis.Cmder) error {
	if h.afterApply && isWalletApply(cmd) && h.failed.CompareAndSwap(false, true) {
		return errors.New("i/o timeout")
	}
	return nil
}

func (walletReplyHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (walletReplyHook) AfterProcessPipeline(context.Context, []redis.Cmder) error { return nil }

func TestWalletSettlementFailureRefundsExactlyOnce(t *testing.T) {
	cases := []struct {
		name        string
		redis       bool
		afterApply  bool
		actual      int
		wantUnknown bool
		want        int
	}{
		// A definite failure keeps the full refund, as before.
		{name: "database_rejects_refund_difference", actual: 10, want: 100},
		// An unknown outcome retries the same operation instead of refunding all.
		{name: "redis_reply_lost_after_refund_applied", redis: true, afterApply: true, actual: 10, wantUnknown: true, want: 90},
		{name: "redis_reply_lost_before_refund_applied", redis: true, actual: 10, wantUnknown: true, want: 90},
		// A charge with an unknown outcome is neither refunded nor charged again.
		{name: "redis_reply_lost_after_charge_applied", redis: true, afterApply: true, actual: 60, wantUnknown: true, want: 40},
		{name: "redis_reply_lost_before_charge_applied", redis: true, actual: 60, wantUnknown: true, want: 60},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			truncate(t)
			seedUser(t, 610, 100)
			failed := &atomic.Bool{}
			failed.Store(true) // armed right before settlement
			if tc.redis {
				server := miniredis.RunT(t)
				previousEnabled, previousRDB := common.RedisEnabled, common.RDB
				common.RedisEnabled = true
				common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
				common.RDB.AddHook(walletReplyHook{afterApply: tc.afterApply, failed: failed})
				t.Cleanup(func() {
					_ = common.RDB.Close()
					common.RedisEnabled, common.RDB = previousEnabled, previousRDB
				})
			}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{UserId: 610, RequestId: tc.name, IsPlayground: true, ForcePreConsume: true,
				UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
			session, apiErr := NewBillingSession(ctx, info, 40)
			require.Nil(t, apiErr)
			if !tc.redis {
				const callback = "test:wallet_settlement_rejected"
				require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
					if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "User" {
						tx.AddError(errors.New("wallet storage unavailable"))
					}
				}))
				t.Cleanup(func() { _ = model.DB.Callback().Update().Remove(callback) })
			}

			failed.Store(false)
			err := session.Settle(tc.actual)
			require.Error(t, err)
			assert.Equal(t, tc.wantUnknown, errors.Is(err, model.ErrWalletOutcomeUnknown))
			if tc.wantUnknown {
				// A later settlement must not reuse an unresolved charge or amount.
				require.Error(t, session.Settle(tc.actual+1))
				require.Error(t, session.Settle(40))
			}
			if !tc.redis {
				require.NoError(t, model.DB.Callback().Update().Remove("test:wallet_settlement_rejected"))
			}
			session.Refund(ctx)

			balance := func() int {
				model.FlushWalletJournals()
				return getUserQuota(t, 610)
			}
			require.Eventually(t, func() bool { return balance() == tc.want }, 2*time.Second, 10*time.Millisecond)
			time.Sleep(50 * time.Millisecond)
			assert.Equal(t, tc.want, balance(), "no late second credit")
		})
	}
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
