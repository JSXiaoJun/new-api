package model

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
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
	adjustment, err := AdjustUserQuota(user.Id, common.RoleRootUser, "override", 50, QuotaCreditMeta{OperatorId: 9})
	require.NoError(t, err)
	assert.Equal(t, 20, adjustment.Before)
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

// A delayed copy of an operation that fell back to SQL must find it recorded,
// even when only the acknowledgment (which lifts the flush barrier) ran.
func TestWalletFallbackRecordsOperationBeforeLiftingBarrier(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	require.NoError(t, DecreaseUserQuota(user.Id, 10, false))
	var captured []any
	common.RDB.AddHook(walletCommandHook{before: func(cmd redis.Cmder) error {
		if cmd.Name() == "eval" && strings.Contains(cmd.Args()[1].(string), "local previous") && captured == nil {
			captured = append([]any(nil), cmd.Args()...)
			server.HSet(walletKeys(user.Id)[0], "busy", "interrupted-flush")
		}
		if cmd.Name() == "set" {
			return errors.New("plain SET unavailable")
		}
		return nil
	}})
	require.NoError(t, DecreaseUserQuota(user.Id, 20, false, QuotaCreditMeta{OperationId: "fallback-op"}))
	require.NotNil(t, captured)
	assert.True(t, server.Exists("wallet:v1:op:fallback-op"), "recorded by the acknowledgment")

	result, err := common.RDB.Do(context.Background(), captured...).Int()
	require.NoError(t, err)
	assert.Equal(t, 1, result, "the delayed copy reports the applied operation")
	FlushWalletJournals()
	assert.Equal(t, 70, getUserQuotaFromDB(t, user.Id), "and does not apply it again")
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
			// A wallet already at the 64-bit ceiling rejects further credits.
			full := createReserveTestUser(t, common.MaxWalletQuota)
			require.ErrorIs(t, IncreaseUserQuota(full.Id, 1, false), ErrWalletQuotaLimitExceeded)
			assert.Equal(t, common.MaxWalletQuota, getUserQuotaFromDB(t, full.Id))

			user := createReserveTestUser(t, common.MaxQuota)
			ok, err := TryReserveUserQuota(user.Id, common.MaxQuota+1)
			require.Error(t, err)
			assert.False(t, ok)
			ok, err = TryReserveUserQuota(user.Id, common.MaxQuota)
			require.NoError(t, err)
			require.True(t, ok)
			require.NoError(t, DecreaseUserQuota(user.Id, common.MaxQuota, false))
			FlushWalletJournals()
			// The reservation and the overdraft each took MaxQuota.
			assert.Equal(t, -common.MaxQuota, getUserQuotaFromDB(t, user.Id))
			_, total, err := GetUserQuotaCredits(user.Id, 0, 10)
			require.NoError(t, err)
			assert.Zero(t, total)

			// An overdraft may not cross the negative wallet floor.
			floor := createReserveTestUser(t, -common.MaxWalletQuota+1)
			require.Error(t, DecreaseUserQuota(floor.Id, 2, false))
			FlushWalletJournals()
			assert.Equal(t, -common.MaxWalletQuota+1, getUserQuotaFromDB(t, floor.Id))
			_, total, err = GetUserQuotaCredits(floor.Id, 0, 10)
			require.NoError(t, err)
			assert.Zero(t, total)
		})
	}
}

func TestWalletJournalKeepsLargeBalancesExact(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	// Lua number formatting would write this balance in exponent notation.
	start := common.MaxWalletQuota - 1_000_000
	user := createReserveTestUser(t, start)

	require.NoError(t, DecreaseUserQuota(user.Id, 7, false))
	require.NoError(t, IncreaseUserQuota(user.Id, 1_000_007, false))
	assert.Equal(t, "9007199254740991", server.HGet(walletKeys(user.Id)[0], "quota"))
	quota, err := getWalletQuota(user.Id)
	require.NoError(t, err)
	assert.Equal(t, common.MaxWalletQuota, quota)

	FlushWalletJournals()
	assert.Equal(t, common.MaxWalletQuota, getUserQuotaFromDB(t, user.Id))
	require.ErrorIs(t, IncreaseUserQuota(user.Id, 1, false), ErrWalletQuotaLimitExceeded)
	FlushWalletJournals()
	assert.Equal(t, common.MaxWalletQuota, getUserQuotaFromDB(t, user.Id))
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

// A rolled-back report lets billing retry a settlement, so a failed commit,
// which may have written the change, must never be reported as rolled back.
// MySQL has no deferred constraints, so its commit cannot be failed on demand;
// the body cases still run there.
func TestWalletTransactionReportsRollbackOnlyWhenNothingCommitted(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(filepath.Join(t.TempDir(), "wallet-tx.db") + "?_pragma=foreign_keys(1)")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				driver = postgres.Open(dsn)
			}
			// The prefix keeps the wallet tables apart from real ones in a shared database.
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("wallet_tx_%d_", time.Now().UnixNano())}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			previous := DB
			DB = db
			t.Cleanup(func() {
				DB = previous
				db.Exec("DROP TABLE IF EXISTS wallet_commit_child")
				db.Exec("DROP TABLE IF EXISTS wallet_commit_parent")
				require.NoError(t, sqlDB.Close())
			})
			db.Exec("DROP TABLE IF EXISTS wallet_commit_child")
			db.Exec("DROP TABLE IF EXISTS wallet_commit_parent")
			require.NoError(t, db.Exec("CREATE TABLE wallet_commit_parent (id INTEGER PRIMARY KEY)").Error)
			childTable := "CREATE TABLE wallet_commit_child (id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES wallet_commit_parent(id) DEFERRABLE INITIALLY DEFERRED)"
			if dialect == "mysql" {
				childTable = "CREATE TABLE wallet_commit_child (id INTEGER PRIMARY KEY, parent_id INTEGER)"
			}
			require.NoError(t, db.Exec(childTable).Error)

			rolledBack, err := walletSQLTransaction(func(tx *gorm.DB) error {
				require.NoError(t, tx.Exec("INSERT INTO wallet_commit_child (id, parent_id) VALUES (1, NULL)").Error)
				return errors.New("body failed")
			})
			require.Error(t, err)
			assert.True(t, rolledBack, "a failed body rolls back")

			if dialect != "mysql" {
				rolledBack, err = walletSQLTransaction(func(tx *gorm.DB) error {
					return tx.Exec("INSERT INTO wallet_commit_child (id, parent_id) VALUES (2, 99)").Error
				})
				require.Error(t, err, "the deferred foreign key fails the commit")
				assert.False(t, rolledBack, "a failed commit is not reported as rolled back")
				if dialect == "sqlite" {
					// SQLite keeps a transaction open after a failed COMMIT; close it here.
					require.NoError(t, db.Exec("ROLLBACK").Error)
				}
			}

			rolledBack, err = walletSQLTransaction(func(tx *gorm.DB) error {
				return tx.Exec("INSERT INTO wallet_commit_parent (id) VALUES (1)").Error
			})
			require.NoError(t, err)
			assert.False(t, rolledBack)
			var children, parents int64
			require.NoError(t, db.Raw("SELECT COUNT(*) FROM wallet_commit_child").Scan(&children).Error)
			require.NoError(t, db.Raw("SELECT COUNT(*) FROM wallet_commit_parent").Scan(&parents).Error)
			assert.Zero(t, children, "neither failed transaction left a row")
			assert.EqualValues(t, 1, parents)

			// Wallet mutations rejected inside the transaction are not applied.
			require.NoError(t, db.AutoMigrate(&User{}, &QuotaCredit{}))
			t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(&QuotaCredit{}, &User{})) })
			full := User{Username: "wallet-full-" + dialect, Password: "unused", AffCode: "wallet-full-" + dialect, Quota: walletQuotaLimit}
			require.NoError(t, db.Create(&full).Error)
			require.ErrorIs(t, IncreaseUserQuota(full.Id, 10, true), ErrWalletNotApplied)
			require.ErrorIs(t, IncreaseUserQuota(full.Id, 10, true), ErrWalletQuotaLimitExceeded)
			require.ErrorIs(t, DecreaseUserQuota(full.Id+1000, 10, true), ErrWalletNotApplied)
			require.NoError(t, DecreaseUserQuota(full.Id, 10, true))
			require.NoError(t, IncreaseUserQuota(full.Id, 10, true))
			var stored User
			require.NoError(t, db.Select("quota").First(&stored, full.Id).Error)
			assert.Equal(t, walletQuotaLimit, stored.Quota)
			var credits int64
			require.NoError(t, db.Model(&QuotaCredit{}).Where("user_id = ?", full.Id).Count(&credits).Error)
			assert.EqualValues(t, 1, credits, "only the applied increase is recorded")
		})
	}
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
