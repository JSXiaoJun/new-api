package model

import (
	"errors"
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
	assert.Equal(t, common.MaxQuota-40, getUserQuotaFromDB(t, user.Id))
	require.NoError(t, IncreaseUserQuota(user.Id, 40, false, QuotaCreditMeta{
		Source: "wallet_refund", RequestId: "failed-request",
	}))
	assert.Equal(t, common.MaxQuota, getUserQuotaFromDB(t, user.Id))
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
