package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func rejectDirectQuotaCreditWrites(t *testing.T) {
	t.Helper()
	const callbackName = "test:reject_direct_quota_credit"
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "QuotaCredit" {
			tx.AddError(errors.New("quota credit storage unavailable"))
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, DB.Callback().Create().Remove(callbackName))
	})
}

func TestTopUpCreditAuditIsAtomicAndRecordedOnce(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 10
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	tests := []struct {
		name        string
		provider    string
		quota       int
		operatorId  int
		repeatError bool
		complete    func(string) error
	}{
		{"epay", PaymentProviderEpay, 20, 0, false, func(tradeNo string) error {
			_, err := RechargeEpay(tradeNo, "alipay", "127.0.0.1")
			return err
		}},
		{"stripe", PaymentProviderStripe, 30, 0, true, func(tradeNo string) error {
			return Recharge(tradeNo, "customer", "127.0.0.1")
		}},
		{"creem", PaymentProviderCreem, 2, 0, true, func(tradeNo string) error {
			return RechargeCreem(tradeNo, "", "", "127.0.0.1")
		}},
		{"waffo", PaymentProviderWaffo, 20, 0, false, func(tradeNo string) error {
			return RechargeWaffo(tradeNo, "127.0.0.1")
		}},
		{"waffo_pancake", PaymentProviderWaffoPancake, 20, 0, false, RechargeWaffoPancake},
		{"manual", PaymentProviderEpay, 20, 97, false, func(tradeNo string) error {
			return ManualCompleteTopUp(tradeNo, "127.0.0.1", QuotaCreditMeta{OperatorId: 97})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, failAudit := range []bool{false, true} {
				name := "success"
				if failAudit {
					name = "audit failure rolls back wallet and order"
				}
				t.Run(name, func(t *testing.T) {
					truncateTables(t)
					user := insertUserForPaymentGuardTest(t, 701, 7)
					order := &TopUp{
						UserId: user.Id, TradeNo: "credit-audit-" + tt.name,
						Amount: 2, Money: 3, PaymentProvider: tt.provider,
						PaymentMethod: tt.provider, Status: common.TopUpStatusPending,
					}
					require.NoError(t, DB.Create(order).Error)
					if failAudit {
						rejectDirectQuotaCreditWrites(t)
						require.Error(t, tt.complete(order.TradeNo))
						assert.Equal(t, 7, getUserQuotaForPaymentGuardTest(t, user.Id))
						assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
						var count int64
						require.NoError(t, DB.Model(&QuotaCredit{}).Where("user_id = ?", user.Id).Count(&count).Error)
						assert.Zero(t, count)
						return
					}

					require.NoError(t, tt.complete(order.TradeNo))
					err := tt.complete(order.TradeNo)
					if tt.repeatError {
						require.Error(t, err)
					} else {
						require.NoError(t, err)
					}
					assert.Equal(t, 7+tt.quota, getUserQuotaForPaymentGuardTest(t, user.Id))
					var credits []QuotaCredit
					require.NoError(t, DB.Where("user_id = ?", user.Id).Find(&credits).Error)
					require.Len(t, credits, 1)
					assert.Equal(t, int64(tt.quota), credits[0].Delta)
					assert.Equal(t, "topup", credits[0].Source)
					assert.Equal(t, order.TradeNo, credits[0].Reference)
					assert.Equal(t, tt.operatorId, credits[0].OperatorId)
					assert.NotZero(t, credits[0].CreatedAt)
				})
			}
		})
	}
}

func TestRedemptionAuditFailureRollsBackRedemptionAndWallet(t *testing.T) {
	userId, key := setupRedeemFixture(t, 500)
	rejectDirectQuotaCreditWrites(t)

	_, err := Redeem(key, userId)
	require.Error(t, err)
	assert.Zero(t, getUserQuotaForPaymentGuardTest(t, userId))
	var redemption Redemption
	require.NoError(t, DB.Where("name = ?", "redeem-test").First(&redemption).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, redemption.Status)
	assert.Zero(t, redemption.UsedUserId)
	var count int64
	require.NoError(t, DB.Model(&QuotaCredit{}).Where("user_id = ?", userId).Count(&count).Error)
	assert.Zero(t, count)
}

// MySQL reports changed rows, not matched rows, for a zero-delta UPDATE.
func emulateMySQLZeroCreditRows(t *testing.T) {
	t.Helper()
	const name = "test:mysql_zero_credit_rows"
	require.NoError(t, DB.Callback().Update().After("gorm:update").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "User" {
			updates, ok := tx.Statement.Dest.(map[string]interface{})
			if !ok {
				return
			}
			expr, ok := updates["quota"].(clause.Expr)
			if ok && len(expr.Vars) == 1 && expr.Vars[0] == 0 {
				tx.RowsAffected = 0
			}
		}
	}))
	t.Cleanup(func() { require.NoError(t, DB.Callback().Update().Remove(name)) })
}

func TestZeroRedemptionSucceedsWithoutCreditRecord(t *testing.T) {
	userId, key := setupRedeemFixture(t, 0)
	// Inserting zero uses the model's default, while an administrator can
	// explicitly update a code to zero through the existing edit endpoint.
	require.NoError(t, DB.Model(&Redemption{}).Where("name = ?", "redeem-test").Update("quota", 0).Error)
	emulateMySQLZeroCreditRows(t)
	quota, err := Redeem(key, userId)
	require.NoError(t, err)
	assert.Zero(t, quota)
	assert.Zero(t, getUserQuotaForPaymentGuardTest(t, userId))
	var redemption Redemption
	require.NoError(t, DB.Where("name = ?", "redeem-test").First(&redemption).Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, redemption.Status)
	assert.Equal(t, userId, redemption.UsedUserId)
	_, total, err := GetUserQuotaCredits(userId, 0, 10)
	require.NoError(t, err)
	assert.Zero(t, total)
}

func TestZeroCheckinSucceedsWithoutCreditRecord(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&Checkin{}))
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&Checkin{}).Error)
	})
	user := insertUserForPaymentGuardTest(t, 703, 7)
	emulateMySQLZeroCreditRows(t)
	checkin := &Checkin{UserId: user.Id, CheckinDate: "2026-09-09"}
	_, err := userCheckinWithTransaction(checkin, user.Id, 0)
	require.NoError(t, err)
	assert.Equal(t, 7, getUserQuotaForPaymentGuardTest(t, user.Id))
	var saved Checkin
	require.NoError(t, DB.First(&saved, checkin.Id).Error)
	assert.Zero(t, saved.QuotaAwarded)
	_, total, err := GetUserQuotaCredits(user.Id, 0, 10)
	require.NoError(t, err)
	assert.Zero(t, total)
}

func TestCheckinCreditAudit(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Checkin{}))
	for name, checkinFn := range map[string]func(*Checkin, int, int) (*Checkin, error){
		"transaction": userCheckinWithTransaction,
		"sqlite":      userCheckinWithoutTransaction,
	} {
		t.Run(name, func(t *testing.T) {
			for _, failAudit := range []bool{false, true} {
				name := "success"
				if failAudit {
					name = "audit failure rolls back wallet and checkin"
				}
				t.Run(name, func(t *testing.T) {
					truncateTables(t)
					t.Cleanup(func() {
						require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&Checkin{}).Error)
					})
					user := insertUserForPaymentGuardTest(t, 702, 7)
					checkin := &Checkin{UserId: user.Id, CheckinDate: "2026-09-09", QuotaAwarded: 100}
					if failAudit {
						rejectDirectQuotaCreditWrites(t)
						_, err := checkinFn(checkin, user.Id, checkin.QuotaAwarded)
						require.Error(t, err)
						assert.Equal(t, 7, getUserQuotaForPaymentGuardTest(t, user.Id))
						var count int64
						require.NoError(t, DB.Model(&Checkin{}).Where("user_id = ?", user.Id).Count(&count).Error)
						assert.Zero(t, count)
						require.NoError(t, DB.Model(&QuotaCredit{}).Where("user_id = ?", user.Id).Count(&count).Error)
						assert.Zero(t, count)
						return
					}
					_, err := checkinFn(checkin, user.Id, checkin.QuotaAwarded)
					require.NoError(t, err)
					_, err = checkinFn(&Checkin{UserId: user.Id, CheckinDate: checkin.CheckinDate, QuotaAwarded: 100}, user.Id, 100)
					require.Error(t, err)
					assert.Equal(t, 107, getUserQuotaForPaymentGuardTest(t, user.Id))
					var credits []QuotaCredit
					require.NoError(t, DB.Where("user_id = ?", user.Id).Find(&credits).Error)
					require.Len(t, credits, 1)
					assert.Equal(t, int64(100), credits[0].Delta)
					assert.Equal(t, "checkin", credits[0].Source)
					assert.Equal(t, checkin.CheckinDate, credits[0].Reference)
				})
			}
		})
	}
}
