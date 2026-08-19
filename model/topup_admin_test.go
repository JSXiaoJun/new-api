package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchAllTopUpsIdentifiesTheUserForAnOrder(t *testing.T) {
	truncateTables(t)

	user := &User{
		Username:    "order-owner",
		Password:    "password123",
		DisplayName: "Order Owner",
		Email:       "owner@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AffCode:     "owner-aff",
	}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          user.Id,
		Amount:          10,
		Money:           10,
		TradeNo:         "USR4574NOe3Sey51787124215",
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderEpay,
		CreateTime:      1787124215,
		Status:          common.TopUpStatusSuccess,
	}).Error)

	orders, total, err := SearchAllTopUps(
		"USR4574NOe3Sey51787124215",
		&common.PageInfo{Page: 1, PageSize: 20},
	)

	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, user.Id, orders[0].UserId)
	assert.Equal(t, user.Username, orders[0].Username)
	assert.Equal(t, user.DisplayName, orders[0].DisplayName)
	assert.Equal(t, user.Email, orders[0].Email)
}
