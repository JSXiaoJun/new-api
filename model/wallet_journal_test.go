package model

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type walletCommandHook struct{ before func(redis.Cmder) error }

func (h walletCommandHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	return ctx, h.before(cmd)
}
func (walletCommandHook) AfterProcess(context.Context, redis.Cmder) error { return nil }
func (walletCommandHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}
func (walletCommandHook) AfterProcessPipeline(context.Context, []redis.Cmder) error { return nil }

func isWalletAck(cmd redis.Cmder) bool {
	return cmd.Name() == "eval" && strings.Contains(cmd.Args()[1].(string), "'HDEL'")
}

func TestWalletExpiryAndProfileInvalidationCannotRestoreSpentQuota(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 100)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	ok, err := TryReserveUserQuota(user.Id, 80)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 100, getUserQuotaFromDB(t, user.Id))
	server.FastForward(time.Duration(userCacheTTLSeconds()+1) * time.Second)
	for _, invalidate := range []bool{false, true} {
		if invalidate {
			require.NoError(t, invalidateUserCache(user.Id))
		}
		base, err := GetUserCache(user.Id)
		require.NoError(t, err)
		assert.Equal(t, 20, base.Quota)
		ok, err := TryReserveUserQuota(user.Id, 80)
		require.NoError(t, err)
		assert.False(t, ok)
	}
	FlushWalletJournals()
	assert.Equal(t, 20, getUserQuotaFromDB(t, user.Id))
}

func TestWalletCreditHydrationBetweenCommitAndAckDoesNotDoubleCredit(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(map[bool]string{false: "immediate", true: "batch"}[batch], func(t *testing.T) {
			truncateTables(t)
			resetBatchUpdateTestState(t)
			useUserCacheMiniRedis(t)
			common.BatchUpdateEnabled = batch
			user := createReserveTestUser(t, 100)
			_, err := getWalletQuota(user.Id)
			require.NoError(t, err)
			var injected atomic.Bool
			common.RDB.AddHook(walletCommandHook{before: func(cmd redis.Cmder) error {
				if isWalletAck(cmd) && injected.CompareAndSwap(false, true) {
					assert.Equal(t, 110, getUserQuotaFromDB(t, user.Id))
					base, err := GetUserCache(user.Id)
					require.NoError(t, err)
					assert.Equal(t, 110, base.Quota)
				}
				return nil
			}})
			require.NoError(t, IncreaseUserQuota(user.Id, 10, true, QuotaCreditMeta{Source: "admin_add"}))
			require.True(t, injected.Load())
			ok, err := TryReserveUserQuota(user.Id, 120)
			require.NoError(t, err)
			assert.False(t, ok)
			FlushWalletJournals()
			assert.Equal(t, 110, getUserQuotaFromDB(t, user.Id))
			_, total, err := GetUserQuotaCredits(user.Id, 0, 10)
			require.NoError(t, err)
			assert.EqualValues(t, 1, total)
		})
	}
}

func TestWalletAckFailureRecoveryDoesNotReplayCredit(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 100)
	require.NoError(t, DecreaseUserQuota(user.Id, 30, false))
	require.NoError(t, IncreaseUserQuota(user.Id, 10, false, QuotaCreditMeta{RequestId: "refund"}))
	var fail atomic.Bool
	fail.Store(true)
	common.RDB.AddHook(walletCommandHook{before: func(cmd redis.Cmder) error {
		if isWalletAck(cmd) && fail.Load() {
			return errors.New("ack transport unavailable")
		}
		return nil
	}})
	FlushWalletJournals()
	assert.Equal(t, 80, getUserQuotaFromDB(t, user.Id))
	fail.Store(false)
	FlushWalletJournals()
	FlushWalletJournals()
	quota, err := GetUserQuota(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 80, quota)
	assert.Equal(t, 80, getUserQuotaFromDB(t, user.Id))
	_, total, err := GetUserQuotaCredits(user.Id, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
}

func TestWalletJournalSurvivesNewClientAndConcurrentFlush(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 100)
	require.NoError(t, DecreaseUserQuota(user.Id, 80, false))
	require.NoError(t, IncreaseUserQuota(user.Id, 5, false, QuotaCreditMeta{RequestId: "survives-process"}))
	require.NoError(t, common.RDB.Close())
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	// Only Redis and SQL are shared; no process-local wallet queue is needed.
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); FlushWalletJournals() }()
	}
	wg.Wait()
	assert.Equal(t, 25, getUserQuotaFromDB(t, user.Id))
	_, total, err := GetUserQuotaCredits(user.Id, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
}

func TestWalletLossAndRedisDisableFailClosed(t *testing.T) {
	for _, loss := range []string{"state", "events", "all", "disabled"} {
		t.Run(loss, func(t *testing.T) {
			truncateTables(t)
			resetBatchUpdateTestState(t)
			server := useUserCacheMiniRedis(t)
			common.BatchUpdateEnabled = true
			user := createReserveTestUser(t, 100)
			require.NoError(t, DecreaseUserQuota(user.Id, 80, false))
			switch loss {
			case "state":
				server.Del(walletKeys(user.Id)[0])
			case "events":
				server.Del(walletKeys(user.Id)[1])
			case "all":
				server.FlushAll()
			case "disabled":
				common.RedisEnabled = false
			}
			ok, err := TryReserveUserQuota(user.Id, 80)
			require.Error(t, err)
			assert.False(t, ok)
			assert.Equal(t, 100, getUserQuotaFromDB(t, user.Id))
			require.Error(t, IncreaseUserQuota(user.Id, 10, true))
		})
	}
}

func TestWalletConcurrentReservationsEnforceSingleBalance(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 100)
	_, err := getWalletQuota(user.Id)
	require.NoError(t, err)
	start := make(chan struct{})
	type result struct {
		ok  bool
		err error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() { <-start; ok, err := TryReserveUserQuota(user.Id, 80); results <- result{ok, err} }()
	}
	close(start)
	accepted := 0
	for range 2 {
		r := <-results
		require.NoError(t, r.err)
		if r.ok {
			accepted++
		}
	}
	assert.Equal(t, 1, accepted)
	FlushWalletJournals()
	assert.Equal(t, 20, getUserQuotaFromDB(t, user.Id))
}

func TestWalletNormalBatchDoesNotWriteSQLPerMutation(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 100)
	_, err := getWalletQuota(user.Id)
	require.NoError(t, err)
	const callback = "test:wallet_write_count"
	updates := 0
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "User" {
			updates++
		}
	}))
	t.Cleanup(func() { require.NoError(t, DB.Callback().Update().Remove(callback)) })
	for _, quota := range []int{10, 20, 30} {
		require.NoError(t, DecreaseUserQuota(user.Id, quota, false))
	}
	assert.Zero(t, updates, "hot-path wallet mutations must remain batched")
	FlushWalletJournals()
	assert.Equal(t, 40, getUserQuotaFromDB(t, user.Id))
}

func TestWalletOverrideDrainsPendingDebitsAndAuditsActualIncrease(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 100)
	require.NoError(t, DecreaseUserQuota(user.Id, 80, false))
	previous, err := OverrideUserQuota(user.Id, 50, QuotaCreditMeta{OperatorId: 9})
	require.NoError(t, err)
	assert.Equal(t, 20, previous)
	FlushWalletJournals()
	assert.Equal(t, 50, getUserQuotaFromDB(t, user.Id))
	rows, total, err := GetUserQuotaCredits(user.Id, 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	assert.EqualValues(t, 30, rows[0].Delta)
}

func TestWalletSettlementDuringFlushIsNotDropped(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 100)
	require.NoError(t, DecreaseUserQuota(user.Id, 10, false))
	var injected atomic.Bool
	common.RDB.AddHook(walletCommandHook{before: func(cmd redis.Cmder) error {
		if cmd.Name() == "eval" && strings.Contains(cmd.Args()[1].(string), "local previous") && injected.CompareAndSwap(false, true) {
			server.HSet(walletKeys(user.Id)[0], "busy", "interrupted-flush")
		}
		return nil
	}})
	require.NoError(t, DecreaseUserQuota(user.Id, 20, false))
	require.True(t, injected.Load())
	FlushWalletJournals()
	assert.Equal(t, 70, getUserQuotaFromDB(t, user.Id))
}

func TestWalletPaymentProvidersDrainPendingDebitsWithoutReplayingCredit(t *testing.T) {
	for _, provider := range []string{PaymentProviderEpay, PaymentProviderStripe, PaymentProviderCreem, PaymentProviderWaffo, PaymentProviderWaffoPancake, "manual"} {
		t.Run(provider, func(t *testing.T) {
			truncateTables(t)
			resetBatchUpdateTestState(t)
			useUserCacheMiniRedis(t)
			common.BatchUpdateEnabled = true
			oldQuotaPerUnit := common.QuotaPerUnit
			common.QuotaPerUnit = 1
			t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })
			user := createReserveTestUser(t, 100)
			require.NoError(t, DecreaseUserQuota(user.Id, 80, false))
			orderProvider := provider
			if provider == "manual" {
				orderProvider = PaymentProviderEpay
			}
			order := TopUp{UserId: user.Id, TradeNo: "pending-wallet-" + provider, Amount: 10, Money: 10,
				PaymentProvider: orderProvider, PaymentMethod: orderProvider, Status: common.TopUpStatusPending}
			require.NoError(t, DB.Create(&order).Error)
			for attempt := 0; attempt < 2; attempt++ {
				var err error
				switch provider {
				case PaymentProviderEpay:
					_, err = RechargeEpay(order.TradeNo, "alipay", "")
				case PaymentProviderStripe:
					err = Recharge(order.TradeNo, "customer", "")
				case PaymentProviderCreem:
					err = RechargeCreem(order.TradeNo, "", "", "")
				case PaymentProviderWaffo:
					err = RechargeWaffo(order.TradeNo, "")
				case PaymentProviderWaffoPancake:
					err = RechargeWaffoPancake(order.TradeNo)
				case "manual":
					err = ManualCompleteTopUp(order.TradeNo, "")
				}
				if attempt == 0 {
					require.NoError(t, err)
				}
			}
			FlushWalletJournals()
			quota, err := GetUserQuota(user.Id, false)
			require.NoError(t, err)
			assert.Equal(t, 30, quota)
			assert.Equal(t, 30, getUserQuotaFromDB(t, user.Id))
			_, total, err := GetUserQuotaCredits(user.Id, 0, 10)
			require.NoError(t, err)
			assert.EqualValues(t, 1, total)
		})
	}
}

func TestWalletSubscriptionPurchaseCannotSpendPendingDebits(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 1
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })
	user := createReserveTestUser(t, 100)
	plan := SubscriptionPlan{Title: "wallet-integrity-plan", PriceAmount: 30, Enabled: true,
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 100}
	require.NoError(t, DB.Create(&plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	t.Cleanup(func() { InvalidateSubscriptionPlanCache(plan.Id) })
	require.NoError(t, DecreaseUserQuota(user.Id, 80, false))
	require.Error(t, PurchaseSubscriptionWithBalance(user.Id, plan.Id))
	FlushWalletJournals()
	assert.Equal(t, 20, getUserQuotaFromDB(t, user.Id))
	require.NoError(t, IncreaseUserQuota(user.Id, 20, true))
	require.NoError(t, PurchaseSubscriptionWithBalance(user.Id, plan.Id))
	quota, err := GetUserQuota(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 10, quota)
}

func TestWalletStaleProfileCannotRollBackCheckpoint(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 100)
	require.NoError(t, DecreaseUserQuota(user.Id, 10, false))
	FlushWalletJournals()
	var stale User
	require.NoError(t, DB.First(&stale, user.Id).Error)
	require.NoError(t, DecreaseUserQuota(user.Id, 20, false))
	FlushWalletJournals()
	stale.DisplayName = "updated-profile"
	require.NoError(t, stale.Update(false))
	require.NoError(t, IncreaseUserQuota(user.Id, 5, false))
	FlushWalletJournals()
	assert.Equal(t, 75, getUserQuotaFromDB(t, user.Id))
	quota, err := GetUserQuota(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 75, quota)
}

func TestWalletIntegerBoundariesDoNotTurnChargesIntoCredits(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "sql", true: "redis"}[enabled], func(t *testing.T) {
			truncateTables(t)
			resetBatchUpdateTestState(t)
			if enabled {
				useUserCacheMiniRedis(t)
				common.BatchUpdateEnabled = true
			}
			user := createReserveTestUser(t, common.MaxQuota)
			require.Error(t, IncreaseUserQuota(user.Id, 1, false))
			ok, err := TryReserveUserQuota(user.Id, common.MaxQuota+1)
			require.Error(t, err)
			assert.False(t, ok)
			ok, err = TryReserveUserQuota(user.Id, common.MaxQuota)
			require.NoError(t, err)
			require.True(t, ok)
			require.NoError(t, DecreaseUserQuota(user.Id, common.MaxQuota, false))
			require.Error(t, DecreaseUserQuota(user.Id, 2, false))
			FlushWalletJournals()
			assert.Equal(t, -common.MaxQuota, getUserQuotaFromDB(t, user.Id))
			_, total, err := GetUserQuotaCredits(user.Id, 0, 10)
			require.NoError(t, err)
			assert.Zero(t, total)
		})
	}
}

func TestWalletEnrollmentRollbackRecoversWithoutApplyingFailedCredit(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	t.Run("credit fails during first enrollment", func(t *testing.T) {
		rejectDirectQuotaCreditWrites(t)
		require.Error(t, IncreaseUserQuota(user.Id, 10, true))
		assert.Equal(t, 100, getUserQuotaFromDB(t, user.Id))
	})
	quota, err := GetUserQuota(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 100, quota)
	require.NoError(t, IncreaseUserQuota(user.Id, 10, true))
	assert.Equal(t, 110, getUserQuotaFromDB(t, user.Id))
	_, total, err := GetUserQuotaCredits(user.Id, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
}

func TestWalletRedisCommandReplayIsIdempotentAcrossCheckpoint(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 100)
	var captured []interface{}
	common.RDB.AddHook(walletCommandHook{before: func(cmd redis.Cmder) error {
		if captured == nil && cmd.Name() == "eval" && strings.Contains(cmd.Args()[1].(string), "local previous") {
			captured = append([]interface{}(nil), cmd.Args()...)
		}
		return nil
	}})
	require.NoError(t, IncreaseUserQuota(user.Id, 10, false))
	require.NotEmpty(t, captured)
	for range 2 {
		result, err := common.RDB.Do(context.Background(), captured...).Int()
		require.NoError(t, err)
		assert.Equal(t, 1, result)
		FlushWalletJournals()
	}
	assert.Equal(t, 110, getUserQuotaFromDB(t, user.Id))
	_, total, err := GetUserQuotaCredits(user.Id, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
}

func TestWalletMalformedDirtyKeyCannotPartiallyApplyMutation(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 100)
	_, err := getWalletQuota(user.Id)
	require.NoError(t, err)
	require.NoError(t, server.Set(walletDirtyKey, "invalid-type"))
	require.Error(t, IncreaseUserQuota(user.Id, 10, false))
	server.Del(walletDirtyKey)
	quota, err := GetUserQuota(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 100, quota)
	FlushWalletJournals()
	assert.Equal(t, 100, getUserQuotaFromDB(t, user.Id))
}

func TestWalletConcurrentSQLConnectionsCommitJournalOnce(t *testing.T) {
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "wallet.db")+"?_pragma=busy_timeout(5000)"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB; require.NoError(t, sqlDB.Close()) })
	require.NoError(t, DB.AutoMigrate(&User{}, &QuotaCredit{}))
	user := createReserveTestUser(t, 100)
	require.NoError(t, DecreaseUserQuota(user.Id, 80, false))
	require.NoError(t, IncreaseUserQuota(user.Id, 10, false))
	start := make(chan struct{})
	errs := make(chan error, 2)
	for range 2 {
		go func() { <-start; errs <- withWalletTransaction(user.Id, nil) }()
	}
	close(start)
	for range 2 {
		require.NoError(t, <-errs)
	}
	assert.Equal(t, 30, getUserQuotaFromDB(t, user.Id))
	quota, err := GetUserQuota(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 30, quota)
	_, total, err := GetUserQuotaCredits(user.Id, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
}
