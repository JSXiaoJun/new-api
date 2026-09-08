package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasSuccessfulTopUpGrantsPermanentAfterSalesAccess(t *testing.T) {
	truncateTables(t)

	user := &User{
		Username: "after-sales-user",
		Password: "password123",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "after-sales-aff",
	}
	require.NoError(t, DB.Create(user).Error)

	hasPaid, err := HasSuccessfulTopUp(user.Id)
	require.NoError(t, err)
	assert.False(t, hasPaid)

	require.NoError(t, DB.Create(&TopUp{
		UserId:     user.Id,
		TradeNo:    "AFTERSALESPENDING",
		Status:     common.TopUpStatusPending,
		CreateTime: common.GetTimestamp(),
	}).Error)
	hasPaid, err = HasSuccessfulTopUp(user.Id)
	require.NoError(t, err)
	assert.False(t, hasPaid)

	require.NoError(t, DB.Create(&TopUp{
		UserId:     user.Id,
		TradeNo:    "AFTERSALESSUCCESS",
		Status:     common.TopUpStatusSuccess,
		CreateTime: common.GetTimestamp() - topUpQueryWindowSeconds - 1,
	}).Error)
	hasPaid, err = HasSuccessfulTopUp(user.Id)
	require.NoError(t, err)
	assert.True(t, hasPaid)
}
