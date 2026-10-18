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
	useSettlementRetryDelays(t, 0)
	t.Run("database failure", func(t *testing.T) {
		const callback = "test:wallet_settlement_failure"
		require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "User" {
				tx.AddError(errors.New("wallet storage unavailable"))
			}
		}))
		t.Cleanup(func() { require.NoError(t, model.DB.Callback().Update().Remove(callback)) })
		require.Error(t, session.Settle(30))
		waitSettlementRetry(t, session)
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

func useSettlementRetryDelays(t *testing.T, delays ...time.Duration) {
	t.Helper()
	previous := walletSettlementRetryDelays
	walletSettlementRetryDelays = delays
	t.Cleanup(func() { walletSettlementRetryDelays = previous })
}

// waitSettlementRetry waits until no background retry owns the session.
func waitSettlementRetry(t *testing.T, session *BillingSession) {
	t.Helper()
	require.Eventually(t, func() bool {
		session.mu.Lock()
		defer session.mu.Unlock()
		return session.pending == nil || !session.pending.retrying
	}, 2*time.Second, time.Millisecond)
}

// useWalletReplyRedis serves the wallet from a fresh Redis whose next armed
// apply script loses its reply, before or after Redis ran it.
func useWalletReplyRedis(t *testing.T, afterApply bool, failed *atomic.Bool) {
	t.Helper()
	server := miniredis.RunT(t)
	previousEnabled, previousRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	common.RDB.AddHook(walletReplyHook{afterApply: afterApply, failed: failed})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RedisEnabled, common.RDB = previousEnabled, previousRDB
	})
}

// commitFailedFunding reports its first settlement with a plain error, the
// way a failed SQL commit looks; with applied it really wrote the change.
type commitFailedFunding struct {
	*WalletFunding
	applied bool
	called  *atomic.Bool
}

func (f commitFailedFunding) Settle(delta int) error {
	if f.called.CompareAndSwap(false, true) {
		if f.applied {
			if err := f.WalletFunding.Settle(delta); err != nil {
				return err
			}
		}
		return errors.New("commit failed")
	}
	return f.WalletFunding.Settle(delta)
}

func TestWalletSettlementFailureSettlesExactlyOnce(t *testing.T) {
	// Balance 100, reservation 40, so 60 is left before settlement. The correct
	// result is 90 for actual usage 10 and 40 for actual usage 60. Each case runs
	// once for a request that completed and once for one that ended abnormally
	// (the relay then calls Refund). Before these retries, an abnormal end
	// refunded the whole reservation (100) and a completed request kept it (60).
	cases := []struct {
		name          string
		actual        int
		redis         bool
		afterApply    bool
		dbFailures    int32
		commitFailed  bool
		commitApplied bool
		completed     int
		abnormal      int
	}{
		// The write was rolled back: it is retried in the background.
		{name: "database_rejects_refund_difference_once", actual: 10, dbFailures: 1, completed: 90, abnormal: 90},
		{name: "database_rejects_charge_difference_once", actual: 60, dbFailures: 1, completed: 40, abnormal: 40},
		// Storage never recovers: the reservation stays charged and is logged.
		{name: "database_always_rejects_refund_difference", actual: 10, dbFailures: 1000, completed: 60, abnormal: 60},
		{name: "database_always_rejects_charge_difference", actual: 60, dbFailures: 1000, completed: 60, abnormal: 60},
		// Redis lost the reply: the same operation is retried and applied once.
		{name: "redis_reply_lost_after_refund_applied", actual: 10, redis: true, afterApply: true, completed: 90, abnormal: 90},
		{name: "redis_reply_lost_before_refund_applied", actual: 10, redis: true, completed: 90, abnormal: 90},
		{name: "redis_reply_lost_after_charge_applied", actual: 60, redis: true, afterApply: true, completed: 40, abnormal: 40},
		{name: "redis_reply_lost_before_charge_applied", actual: 60, redis: true, completed: 40, abnormal: 40},
		// The commit failed, so the change may have landed with no idempotency
		// guard. A charge is never retried (the user could pay twice). A refund
		// gets one attempt when the request ended abnormally; if the commit had
		// landed this credits the difference twice (120), still less than the
		// full refund it replaces (130).
		{name: "commit_failed_refund_not_applied", actual: 10, commitFailed: true, completed: 60, abnormal: 90},
		{name: "commit_failed_refund_applied", actual: 10, commitFailed: true, commitApplied: true, completed: 90, abnormal: 120},
		{name: "commit_failed_charge_not_applied", actual: 60, commitFailed: true, completed: 60, abnormal: 60},
		{name: "commit_failed_charge_applied", actual: 60, commitFailed: true, commitApplied: true, completed: 40, abnormal: 40},
	}
	for _, tc := range cases {
		for _, abnormal := range []bool{false, true} {
			name, want := tc.name+"/completed", tc.completed
			if abnormal {
				name, want = tc.name+"/abnormal", tc.abnormal
			}
			t.Run(name, func(t *testing.T) {
				useSettlementRetryDelays(t, 0, time.Millisecond, time.Millisecond)
				truncate(t)
				seedUser(t, 610, 100)
				failed := &atomic.Bool{}
				failed.Store(true) // armed right before settlement
				if tc.redis {
					useWalletReplyRedis(t, tc.afterApply, failed)
				}
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
				info := &relaycommon.RelayInfo{UserId: 610, RequestId: name, IsPlayground: true, ForcePreConsume: true,
					UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
				session, apiErr := NewBillingSession(ctx, info, 40)
				require.Nil(t, apiErr)
				if tc.commitFailed {
					session.funding = commitFailedFunding{WalletFunding: session.funding.(*WalletFunding), applied: tc.commitApplied, called: &atomic.Bool{}}
				}
				if tc.dbFailures > 0 {
					var rejected atomic.Int32
					const callback = "test:wallet_settlement_rejected"
					require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
						if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "User" && rejected.Add(1) <= tc.dbFailures {
							tx.AddError(errors.New("wallet storage unavailable"))
						}
					}))
					t.Cleanup(func() { require.NoError(t, model.DB.Callback().Update().Remove(callback)) })
				}

				failed.Store(false)
				require.Error(t, session.Settle(tc.actual))
				if tc.commitFailed {
					// An unprotected difference must not be settled again by the caller.
					require.Error(t, session.Settle(tc.actual))
				}
				if abnormal {
					session.Refund(ctx)
				}
				waitSettlementRetry(t, session)

				balance := func() int {
					model.FlushWalletJournals()
					return getUserQuota(t, 610)
				}
				require.Eventually(t, func() bool { return balance() == want }, 2*time.Second, 5*time.Millisecond)
				time.Sleep(30 * time.Millisecond)
				assert.Equal(t, want, balance(), "no late second settlement")
			})
		}
	}
}

// blockingRetryFunding fails the first settlement as not applied and holds the
// retry inside the wallet write until release is closed.
type blockingRetryFunding struct {
	*WalletFunding
	calls   *atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (f blockingRetryFunding) Settle(delta int) error {
	if f.calls.Add(1) == 1 {
		return fmt.Errorf("%w: storage busy", model.ErrWalletNotApplied)
	}
	close(f.entered)
	<-f.release
	return f.WalletFunding.Settle(delta)
}

func TestWalletSettlementRetryDoesNotBlockRefundOrRunConcurrently(t *testing.T) {
	useSettlementRetryDelays(t, 0)
	truncate(t)
	seedUser(t, 612, 100)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{UserId: 612, RequestId: "retry-in-flight", IsPlayground: true, ForcePreConsume: true,
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
	session, apiErr := NewBillingSession(ctx, info, 40)
	require.Nil(t, apiErr)
	funding := blockingRetryFunding{WalletFunding: session.funding.(*WalletFunding), calls: &atomic.Int32{},
		entered: make(chan struct{}), release: make(chan struct{})}
	session.funding = funding

	require.Error(t, session.Settle(10))
	<-funding.entered
	refunded := make(chan struct{})
	go func() { session.Refund(ctx); close(refunded) }()
	select {
	case <-refunded:
	case <-time.After(time.Second):
		t.Fatal("Refund waited for the background wallet write")
	}
	require.Error(t, session.Settle(10), "no second write while the retry is in flight")
	close(funding.release)
	waitSettlementRetry(t, session)
	assert.EqualValues(t, 2, funding.calls.Load())
	assert.Equal(t, 90, getUserQuota(t, 612))
}

func TestUnknownWalletSettlementStopsRetryingAfterItsWindow(t *testing.T) {
	useSettlementRetryDelays(t, 0, 300*time.Millisecond)
	previousWindow := walletSettlementRetryWindow
	walletSettlementRetryWindow = 100 * time.Millisecond
	t.Cleanup(func() { walletSettlementRetryWindow = previousWindow })
	truncate(t)
	seedUser(t, 613, 100)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{UserId: 613, RequestId: "retry-window", IsPlayground: true, ForcePreConsume: true,
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
	session, apiErr := NewBillingSession(ctx, info, 40)
	require.Nil(t, apiErr)
	var calls atomic.Int32
	session.funding = countingUnknownFunding{WalletFunding: session.funding.(*WalletFunding), calls: &calls}

	require.ErrorIs(t, session.Settle(10), model.ErrWalletOutcomeUnknown)
	waitSettlementRetry(t, session)
	// The immediate retry is still inside the window; the next one is not.
	assert.EqualValues(t, 2, calls.Load())
	require.Error(t, session.Settle(10), "an expired unknown operation is not retried by the caller either")
	assert.EqualValues(t, 2, calls.Load())
	assert.Equal(t, 60, getUserQuota(t, 613))
}

// countingUnknownFunding reports every settlement as an unknown outcome
// without writing anything.
type countingUnknownFunding struct {
	*WalletFunding
	calls *atomic.Int32
}

func (f countingUnknownFunding) Settle(int) error {
	f.calls.Add(1)
	return fmt.Errorf("%w: i/o timeout", model.ErrWalletOutcomeUnknown)
}

func TestUnknownWalletSettlementOnlyRetriesTheSameAmount(t *testing.T) {
	useSettlementRetryDelays(t, 200*time.Millisecond)
	truncate(t)
	seedUser(t, 611, 100)
	failed := &atomic.Bool{}
	failed.Store(true)
	useWalletReplyRedis(t, true, failed)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{UserId: 611, RequestId: "unknown-same-amount", IsPlayground: true, ForcePreConsume: true,
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
	session, apiErr := NewBillingSession(ctx, info, 40)
	require.Nil(t, apiErr)

	failed.Store(false)
	err := session.Settle(10)
	require.ErrorIs(t, err, model.ErrWalletOutcomeUnknown)
	// The lost operation may have refunded 30: another amount, including the
	// reservation itself, would be recorded against the same operation.
	require.Error(t, session.Settle(11))
	require.Error(t, session.Settle(40))
	require.NoError(t, session.Settle(10))
	model.FlushWalletJournals()
	assert.Equal(t, 90, getUserQuota(t, 611))
	time.Sleep(300 * time.Millisecond) // let the scheduled retry find nothing to do
	model.FlushWalletJournals()
	assert.Equal(t, 90, getUserQuota(t, 611))
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
