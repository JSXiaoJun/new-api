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
		TopUpFilter{Keyword: "USR4574NOe3Sey51787124215"},
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

func TestSearchAllTopUpsFiltersByUserOrderAndPaymentMethodWithPagination(t *testing.T) {
	truncateTables(t)
	orders := []TopUp{
		{UserId: 282, TradeNo: "MATCH-older", PaymentMethod: "alipay", CreateTime: 1, Status: common.TopUpStatusSuccess},
		{UserId: 282, TradeNo: "OTHER-order", PaymentMethod: "wxpay", CreateTime: 2, Status: common.TopUpStatusPending},
		{UserId: 256, TradeNo: "MATCH-other-user", PaymentMethod: PaymentMethodUSDT, CreateTime: 3, Status: common.TopUpStatusSuccess},
		{UserId: 282, TradeNo: "USDT-polygon-abc123def456-0", PaymentMethod: PaymentMethodUSDT, CreateTime: 4, Status: common.TopUpStatusSuccess},
	}
	require.NoError(t, DB.Create(&orders).Error)

	tests := []struct {
		name      string
		filter    TopUpFilter
		page      int
		pageSize  int
		wantTotal int64
		wantTrade []string
	}{
		{"user alone includes historical orders", TopUpFilter{UserId: 282}, 1, 20, 3, []string{"USDT-polygon-abc123def456-0", "OTHER-order", "MATCH-older"}},
		{"combined filters exclude other users", TopUpFilter{Keyword: "MATCH%", UserId: 282}, 1, 1, 1, []string{"MATCH-older"}},
		{"second page retains both filters and total", TopUpFilter{Keyword: "MATCH%", UserId: 282}, 2, 1, 1, []string{}},
		{"missing user returns no orders", TopUpFilter{UserId: 999}, 1, 20, 0, []string{}},
		{"order search without user remains supported", TopUpFilter{Keyword: "MATCH%"}, 1, 20, 2, []string{"MATCH-other-user", "MATCH-older"}},
		{"payment method alone lists that method for every user", TopUpFilter{PaymentMethod: PaymentMethodUSDT}, 1, 20, 2, []string{"USDT-polygon-abc123def456-0", "MATCH-other-user"}},
		{"payment method combines with user", TopUpFilter{UserId: 282, PaymentMethod: "alipay"}, 1, 20, 1, []string{"MATCH-older"}},
		{"tx_hash filters by partial trade_no match", TopUpFilter{TxHash: "abc123"}, 1, 20, 1, []string{"USDT-polygon-abc123def456-0"}},
		{"tx_hash with 0x prefix is stripped before matching", TopUpFilter{TxHash: "0xabc123"}, 1, 20, 1, []string{"USDT-polygon-abc123def456-0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, total, err := SearchAllTopUps(tt.filter, &common.PageInfo{Page: tt.page, PageSize: tt.pageSize})
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
