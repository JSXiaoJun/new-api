package model

import (
	"errors"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestQuotaCreditsSurviveNetZeroWalletChangeWithBatching(t *testing.T) {
	setupUserUpdateTestState(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, common.MaxQuota)
	reserved, err := TryReserveUserQuota(user.Id, 40)
	require.NoError(t, err)
	require.True(t, reserved)
	assert.Equal(t, common.MaxQuota, getUserQuotaFromDB(t, user.Id))
	require.NoError(t, IncreaseUserQuota(user.Id, 40, false, QuotaCreditMeta{
		Source: "wallet_refund", RequestId: "failed-request",
	}))
	assert.Equal(t, common.MaxQuota, getUserQuotaFromDB(t, user.Id))
	_, pendingTotal, err := GetUserQuotaCredits(user.Id, 0, 10)
	require.NoError(t, err)
	assert.Zero(t, pendingTotal, "credits become visible only with the balance batch")
	t.Run("mysql unchanged row count", func(t *testing.T) {
		emulateMySQLZeroCreditRows(t)
		batchUpdate()
	})
	credits, total, err := GetUserQuotaCredits(user.Id, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, credits, 1)
	assert.EqualValues(t, 40, credits[0].Delta)
	assert.Equal(t, "failed-request", credits[0].RequestId)

	batchUpdate()
	assert.Equal(t, common.MaxQuota, getUserQuotaFromDB(t, user.Id))
	cachedUser, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, common.MaxQuota, cachedUser.Quota)
}

func TestBatchCreditFailureRetainsBalanceAndRecordsForRetry(t *testing.T) {
	setupUserUpdateTestState(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 100)
	require.NoError(t, populateUserCache(user))
	require.NoError(t, DecreaseUserQuota(user.Id, 50, false))
	require.NoError(t, IncreaseUserQuota(user.Id, 10, false, QuotaCreditMeta{Source: "wallet_refund", RequestId: "first"}))
	require.NoError(t, IncreaseUserQuota(user.Id, 20, false, QuotaCreditMeta{Source: "wallet_refund", RequestId: "second"}))
	UpdateUserUsedQuotaAndRequestCount(user.Id, 20)
	assert.Equal(t, 100, getUserQuotaFromDB(t, user.Id))

	t.Run("failed flush", func(t *testing.T) {
		rejectDirectQuotaCreditWrites(t)
		batchUpdate()
		assert.Equal(t, 100, getUserQuotaFromDB(t, user.Id))
		_, total, err := GetUserQuotaCredits(user.Id, 0, 10)
		require.NoError(t, err)
		assert.Zero(t, total)
	})
	require.NoError(t, IncreaseUserQuota(user.Id, 5, false, QuotaCreditMeta{Source: "wallet_refund", RequestId: "after-failure"}))
	batchUpdate()
	batchUpdate()
	var saved User
	require.NoError(t, DB.First(&saved, user.Id).Error)
	assert.Equal(t, 85, saved.Quota)
	assert.Equal(t, 20, saved.UsedQuota)
	assert.Equal(t, 1, saved.RequestCount)
	credits, total, err := GetUserQuotaCredits(user.Id, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Len(t, credits, 3)
	assert.Equal(t, "after-failure", credits[0].RequestId)
	assert.EqualValues(t, 5, credits[0].Delta)
	assert.EqualValues(t, 20, credits[1].Delta)
	assert.EqualValues(t, 10, credits[2].Delta)
	cached, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, saved.Quota, cached.Quota, "retry must not apply cache deltas twice")
}

func TestExplicitImmediateCreditStaysImmediateWithBatching(t *testing.T) {
	setupUserUpdateTestState(t)
	resetBatchUpdateTestState(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 100)
	require.NoError(t, IncreaseUserQuota(user.Id, 10, true, QuotaCreditMeta{Source: "admin_add"}))
	assert.Equal(t, 110, getUserQuotaFromDB(t, user.Id))
	_, total, err := GetUserQuotaCredits(user.Id, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	batchUpdate()
	assert.Equal(t, 110, getUserQuotaFromDB(t, user.Id))
}

func TestMultiInsertCreditBatchRollsBackEarlierRowsBeforeRetry(t *testing.T) {
	setupUserUpdateTestState(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 100)
	// Cross the SQLite-safe insert chunk boundary to exercise partial writes.
	for i := 0; i < 81; i++ {
		require.NoError(t, IncreaseUserQuota(user.Id, 1, false, QuotaCreditMeta{RequestId: strconv.Itoa(i)}))
	}
	t.Run("later insert fails", func(t *testing.T) {
		const callback = "test:fail_second_credit_insert"
		inserts := 0
		require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "QuotaCredit" {
				inserts++
				if inserts == 2 {
					tx.AddError(errors.New("second credit insert failed"))
				}
			}
		}))
		t.Cleanup(func() { require.NoError(t, DB.Callback().Create().Remove(callback)) })
		batchUpdate()
		assert.Equal(t, 100, getUserQuotaFromDB(t, user.Id))
		_, total, err := GetUserQuotaCredits(user.Id, 0, 100)
		require.NoError(t, err)
		assert.Zero(t, total)
	})
	batchUpdate()
	batchUpdate()
	assert.Equal(t, 181, getUserQuotaFromDB(t, user.Id))
	rows, total, err := GetUserQuotaCredits(user.Id, 0, 100)
	require.NoError(t, err)
	assert.EqualValues(t, 81, total)
	requests := make(map[string]bool)
	for _, row := range rows {
		assert.False(t, requests[row.RequestId], "duplicate credit after rollback")
		requests[row.RequestId] = true
		assert.EqualValues(t, 1, row.Delta)
	}
	assert.Len(t, requests, 81)
}

func TestCreditAuditFailureDoesNotIncreaseDatabaseOrCache(t *testing.T) {
	setupUserUpdateTestState(t)
	useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	require.NoError(t, populateUserCache(user))
	rejectDirectQuotaCreditWrites(t)
	require.Error(t, IncreaseUserQuota(user.Id, 10, true))
	assert.Equal(t, 100, getUserQuotaFromDB(t, user.Id))
	cachedUser, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 100, cachedUser.Quota)
	_, total, err := GetUserQuotaCredits(user.Id, 0, 10)
	require.NoError(t, err)
	assert.Zero(t, total)
}

func TestInvalidCreditsDoNotCreateEvidence(t *testing.T) {
	setupUserUpdateTestState(t)
	user := createReserveTestUser(t, common.MaxQuota)
	require.Error(t, IncreaseUserQuota(user.Id, -1, true))
	require.Error(t, IncreaseUserQuota(user.Id, 1, true))
	require.Error(t, IncreaseUserQuota(user.Id+1, 10, true))
	require.NoError(t, IncreaseUserQuota(user.Id, 0, true))
	_, total, err := GetUserQuotaCredits(user.Id, 0, 10)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Equal(t, common.MaxQuota, getUserQuotaFromDB(t, user.Id))
}

func TestOverrideCreditAuditFailureRollsBackActualBalance(t *testing.T) {
	setupUserUpdateTestState(t)
	user := createReserveTestUser(t, 100)
	rejectDirectQuotaCreditWrites(t)
	_, err := OverrideUserQuota(user.Id, 150, QuotaCreditMeta{OperatorId: 9})
	require.Error(t, err)
	assert.Equal(t, 100, getUserQuotaFromDB(t, user.Id))
}

func TestOverrideAuditsOnlyActualPositiveDelta(t *testing.T) {
	setupUserUpdateTestState(t)
	useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 100)
	require.NoError(t, populateUserCache(user))
	require.NoError(t, DecreaseUserQuota(user.Id, 20, true))
	previous, err := OverrideUserQuota(user.Id, 110, QuotaCreditMeta{OperatorId: 9, RequestId: "override-request"})
	require.NoError(t, err)
	assert.Equal(t, 80, previous)
	for _, value := range []int{110, 50} {
		_, err := OverrideUserQuota(user.Id, value, QuotaCreditMeta{OperatorId: 9})
		require.NoError(t, err)
	}
	credits, total, err := GetUserQuotaCredits(user.Id, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, credits, 1)
	assert.EqualValues(t, 30, credits[0].Delta)
	assert.Equal(t, "admin_override", credits[0].Source)
	assert.Equal(t, 9, credits[0].OperatorId)
	assert.Equal(t, "override-request", credits[0].RequestId)
	cachedUser, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 50, cachedUser.Quota)
}

func TestRegistrationCreditSharesUserTransaction(t *testing.T) {
	setupUserUpdateTestState(t)
	oldQuota := common.QuotaForNewUser
	common.QuotaForNewUser = 25
	t.Cleanup(func() { common.QuotaForNewUser = oldQuota })
	user := User{Username: "registered-credit-user", Role: common.RoleCommonUser}
	require.NoError(t, user.Insert(0))
	credits, total, err := GetUserQuotaCredits(user.Id, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, credits, 1)
	assert.EqualValues(t, 25, credits[0].Delta)
	assert.Equal(t, "registration", credits[0].Source)

	rollbackUser := User{Username: "rollback-credit-user", Role: common.RoleCommonUser}
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := rollbackUser.InsertWithTx(tx, 0); err != nil {
			return err
		}
		return errors.New("oauth binding failed")
	})
	require.Error(t, err)
	var count int64
	require.NoError(t, DB.Model(&User{}).Where("username = ?", rollbackUser.Username).Count(&count).Error)
	assert.Zero(t, count)
	_, total, err = GetUserQuotaCredits(rollbackUser.Id, 0, 10)
	require.NoError(t, err)
	assert.Zero(t, total)
}

func TestCreditPaginationIsIndependentOfBusinessLogDatabase(t *testing.T) {
	setupUserUpdateTestState(t)
	previousLogDB := LOG_DB
	LOG_DB = nil
	t.Cleanup(func() { LOG_DB = previousLogDB })
	for _, credit := range []QuotaCredit{
		{UserId: 42, Delta: 10, Source: "topup", CreatedAt: 100},
		{UserId: 42, Delta: 20, Source: "admin_add", CreatedAt: 100},
		{UserId: 42, Delta: 30, Source: "wallet_refund", CreatedAt: 101},
		{UserId: 420, Delta: 40, Source: "topup", CreatedAt: 102},
	} {
		require.NoError(t, DB.Create(&credit).Error)
	}
	first, total, err := GetUserQuotaCredits(42, 0, 2)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Len(t, first, 2)
	assert.EqualValues(t, 30, first[0].Delta)
	assert.EqualValues(t, 20, first[1].Delta)
	second, total, err := GetUserQuotaCredits(42, 2, 2)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Len(t, second, 1)
	assert.EqualValues(t, 10, second[0].Delta)
}
