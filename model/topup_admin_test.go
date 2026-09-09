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
		0,
	)

	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, user.Id, orders[0].UserId)
	assert.Equal(t, user.Username, orders[0].Username)
	assert.Equal(t, user.DisplayName, orders[0].DisplayName)
	assert.Equal(t, user.Email, orders[0].Email)
}

func TestSearchAllTopUpsFiltersByUserAndOrderWithPagination(t *testing.T) {
	truncateTables(t)
	orders := []TopUp{
		{UserId: 282, TradeNo: "MATCH-older", CreateTime: 1, Status: common.TopUpStatusSuccess},
		{UserId: 282, TradeNo: "OTHER-order", CreateTime: 2, Status: common.TopUpStatusPending},
		{UserId: 256, TradeNo: "MATCH-other-user", CreateTime: 3, Status: common.TopUpStatusSuccess},
		{UserId: 282, TradeNo: "MATCH-newer", CreateTime: 4, Status: common.TopUpStatusSuccess},
	}
	require.NoError(t, DB.Create(&orders).Error)

	tests := []struct {
		name      string
		keyword   string
		userId    int
		page      int
		pageSize  int
		wantTotal int64
		wantTrade []string
	}{
		{"user alone includes historical orders", "", 282, 1, 20, 3, []string{"MATCH-newer", "OTHER-order", "MATCH-older"}},
		{"combined filters exclude other users", "MATCH%", 282, 1, 1, 2, []string{"MATCH-newer"}},
		{"second page retains both filters and total", "MATCH%", 282, 2, 1, 2, []string{"MATCH-older"}},
		{"missing user returns no orders", "", 999, 1, 20, 0, []string{}},
		{"order search without user remains supported", "MATCH%", 0, 1, 20, 3, []string{"MATCH-newer", "MATCH-other-user", "MATCH-older"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, total, err := SearchAllTopUps(tt.keyword, &common.PageInfo{Page: tt.page, PageSize: tt.pageSize}, tt.userId)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTotal, total)
			tradeNumbers := make([]string, 0, len(results))
			for _, result := range results {
				tradeNumbers = append(tradeNumbers, result.TradeNo)
			}
			assert.Equal(t, tt.wantTrade, tradeNumbers)
		})
	}
}
