package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const epayNotifyTestKey = "epay-notify-test-key"

func setupEpayNotifyTest(t *testing.T) {
	t.Helper()
	db := setupManageUserTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.TopUp{}))
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	confirmPaymentComplianceForTest(t)

	oldAddress, oldID, oldKey := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey
	oldMethods, oldQuotaPerUnit := operation_setting.PayMethods, common.QuotaPerUnit
	operation_setting.PayAddress = "https://pay.example.com"
	operation_setting.EpayId = "1000"
	operation_setting.EpayKey = epayNotifyTestKey
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}, {"type": "wxpay"}}
	common.QuotaPerUnit = 500000
	t.Cleanup(func() {
		operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = oldAddress, oldID, oldKey
		operation_setting.PayMethods, common.QuotaPerUnit = oldMethods, oldQuotaPerUnit
	})
}

func signedEpayNotifyParams(tradeNo, payType, money, key string) url.Values {
	params := epay.GenerateParams(map[string]string{
		"pid":          operation_setting.EpayId,
		"trade_no":     "GW" + tradeNo,
		"out_trade_no": tradeNo,
		"type":         payType,
		"name":         "TUC10",
		"money":        money,
		"trade_status": epay.StatusTradeSuccess,
	}, key)
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	return values
}

func postEpayNotify(t *testing.T, form url.Values) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/epay/notify", strings.NewReader(form.Encode()))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	EpayNotify(c)
	return recorder.Body.String()
}

func userQuotaForEpayNotifyTest(t *testing.T, userID int) int {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("quota").First(&user, userID).Error)
	return user.Quota
}

func TestEpayNotifySimulatedPaymentCreditsWalletOnce(t *testing.T) {
	setupEpayNotifyTest(t)

	user := model.User{Username: "epay_buyer", Password: "password123", Quota: 100, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&user).Error)
	order := model.TopUp{
		UserId: user.Id, Amount: 10, Money: 10, TradeNo: "USR1NOEPAYSIM1",
		PaymentMethod: "alipay", PaymentProvider: model.PaymentProviderEpay,
		CreateTime: common.GetTimestamp(), Status: common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&order).Error)

	forged := signedEpayNotifyParams(order.TradeNo, "alipay", "10.00", "wrong-key")
	require.Equal(t, "fail", postEpayNotify(t, forged))
	require.Equal(t, 100, userQuotaForEpayNotifyTest(t, user.Id))

	tampered := signedEpayNotifyParams(order.TradeNo, "alipay", "10.00", epayNotifyTestKey)
	tampered.Set("money", "1000.00")
	require.Equal(t, "fail", postEpayNotify(t, tampered))
	require.Equal(t, 100, userQuotaForEpayNotifyTest(t, user.Id))

	valid := signedEpayNotifyParams(order.TradeNo, "alipay", "10.00", epayNotifyTestKey)
	require.Equal(t, "success", postEpayNotify(t, valid))
	require.Equal(t, 100+10*500000, userQuotaForEpayNotifyTest(t, user.Id))

	settled := model.GetTopUpByTradeNo(order.TradeNo)
	require.NotNil(t, settled)
	require.Equal(t, common.TopUpStatusSuccess, settled.Status)
	require.NotZero(t, settled.CompleteTime)

	var credits int64
	require.NoError(t, model.DB.Model(&model.QuotaCredit{}).
		Where("user_id = ? AND source = ? AND reference = ?", user.Id, "topup", order.TradeNo).
		Count(&credits).Error)
	require.EqualValues(t, 1, credits)

	require.Equal(t, "success", postEpayNotify(t, valid), "gateway retries must be acknowledged")
	require.Equal(t, 100+10*500000, userQuotaForEpayNotifyTest(t, user.Id))
	require.NoError(t, model.DB.Model(&model.QuotaCredit{}).
		Where("user_id = ? AND reference = ?", user.Id, order.TradeNo).Count(&credits).Error)
	require.EqualValues(t, 1, credits)

	unknown := signedEpayNotifyParams("USR1NOEPAYMISSING", "alipay", "10.00", epayNotifyTestKey)
	require.Equal(t, "fail", postEpayNotify(t, unknown))
}

func setTopUpBonusTiersForTest(t *testing.T, tiers map[int]float64) {
	t.Helper()
	paymentSetting := operation_setting.GetPaymentSetting()
	original := paymentSetting.AmountBonus
	paymentSetting.AmountBonus = tiers
	t.Cleanup(func() { paymentSetting.AmountBonus = original })
}

func TestTopUpBonusQuotaPicksHighestReachedTier(t *testing.T) {
	setTopUpBonusTiersForTest(t, map[int]float64{100: 10, 500: 20, 50: 5})

	testCases := []struct {
		name      string
		amount    int64
		paidQuota int
		wantBonus int64
	}{
		{name: "below every tier", amount: 49, paidQuota: 49_000, wantBonus: 0},
		{name: "exactly the lowest tier", amount: 50, paidQuota: 50_000, wantBonus: 2_500},
		{name: "between tiers uses the lower one", amount: 499, paidQuota: 499_000, wantBonus: 49_900},
		{name: "highest tier", amount: 1000, paidQuota: 1_000_000, wantBonus: 200_000},
		{name: "fractional bonus rounds down", amount: 50, paidQuota: 99, wantBonus: 4},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			bonus, err := topUpBonusQuota(tc.amount, tc.paidQuota)
			require.NoError(t, err)
			assert.Equal(t, tc.wantBonus, bonus)
		})
	}
}

func TestTopUpBonusIgnoresInvalidStoredTiers(t *testing.T) {
	setTopUpBonusTiersForTest(t, map[int]float64{0: 50, -10: 50, 100: -5, 200: 5000})
	bonus, err := topUpBonusQuota(1000, 1_000_000)
	require.NoError(t, err)
	assert.Zero(t, bonus)
}

func TestValidateAmountBonusJSON(t *testing.T) {
	testCases := []struct {
		raw     string
		wantErr bool
	}{
		{raw: `{}`},
		{raw: `{"100":10,"500":20.5}`},
		{raw: `{"100":0}`, wantErr: true},
		{raw: `{"100":-5}`, wantErr: true},
		{raw: `{"0":10}`, wantErr: true},
		{raw: `{"100":1001}`, wantErr: true},
		{raw: `[10]`, wantErr: true},
		{raw: `{"abc":10}`, wantErr: true},
	}
	for _, tc := range testCases {
		t.Run(tc.raw, func(t *testing.T) {
			err := operation_setting.ValidateAmountBonusJSON(tc.raw)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestEpayNotifyCreditsOrderBonusOnceAndIgnoresLaterTierChanges(t *testing.T) {
	setupEpayNotifyTest(t)
	setTopUpBonusTiersForTest(t, map[int]float64{10: 50})

	user := model.User{Username: "epay_bonus_buyer", Password: "password123", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&user).Error)
	paidQuota := 10 * 500000
	bonus, err := topUpBonusQuota(10, paidQuota)
	require.NoError(t, err)
	require.EqualValues(t, paidQuota/2, bonus)

	order := model.TopUp{
		UserId: user.Id, Amount: 10, Money: 10, TradeNo: "USR3NOEPAYBONUS1",
		PaymentMethod: "alipay", PaymentProvider: model.PaymentProviderEpay,
		CreateTime: common.GetTimestamp(), Status: common.TopUpStatusPending, BonusQuota: bonus,
	}
	require.NoError(t, model.DB.Create(&order).Error)

	setTopUpBonusTiersForTest(t, map[int]float64{10: 200})

	valid := signedEpayNotifyParams(order.TradeNo, "alipay", "10.00", epayNotifyTestKey)
	require.Equal(t, "success", postEpayNotify(t, valid))
	require.Equal(t, paidQuota+paidQuota/2, userQuotaForEpayNotifyTest(t, user.Id))

	require.Equal(t, "success", postEpayNotify(t, valid))
	require.Equal(t, paidQuota+paidQuota/2, userQuotaForEpayNotifyTest(t, user.Id))

	var credit model.QuotaCredit
	require.NoError(t, model.DB.Where("user_id = ? AND reference = ?", user.Id, order.TradeNo).First(&credit).Error)
	assert.EqualValues(t, paidQuota+paidQuota/2, credit.Delta)
}

func TestRequestEpayStoresBonusThenNotifyCreditsPaidPlusBonus(t *testing.T) {
	setupEpayNotifyTest(t)
	setTopUpBonusTiersForTest(t, map[int]float64{100: 10, 500: 20})
	oldPrice, oldMinTopUp := operation_setting.Price, operation_setting.MinTopUp
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	operation_setting.Price, operation_setting.MinTopUp = 1, 1
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	t.Cleanup(func() {
		operation_setting.Price, operation_setting.MinTopUp = oldPrice, oldMinTopUp
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
	})

	testCases := []struct {
		name      string
		amount    int64
		wantBonus int64
	}{
		{name: "below threshold gets no bonus", amount: 99, wantBonus: 0},
		{name: "first tier", amount: 100, wantBonus: 100 * 500000 / 10},
		{name: "second tier", amount: 600, wantBonus: 600 * 500000 / 5},
	}
	for i, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			user := model.User{Username: "epay_flow_" + strconv.Itoa(i), AffCode: "epay_flow_" + strconv.Itoa(i), Password: "password123", Group: "default", Status: common.UserStatusEnabled}
			require.NoError(t, model.DB.Create(&user).Error)

			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/user/pay",
				strings.NewReader(`{"amount":`+strconv.FormatInt(tc.amount, 10)+`,"payment_method":"alipay"}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("id", user.Id)
			RequestEpay(c)
			require.Contains(t, recorder.Body.String(), `"message":"success"`, recorder.Body.String())

			var order model.TopUp
			require.NoError(t, model.DB.Where("user_id = ?", user.Id).First(&order).Error)
			assert.Equal(t, tc.amount, order.Amount)
			assert.Equal(t, tc.wantBonus, order.BonusQuota)

			paid := signedEpayNotifyParams(order.TradeNo, "alipay", strconv.FormatFloat(order.Money, 'f', 2, 64), epayNotifyTestKey)
			require.Equal(t, "success", postEpayNotify(t, paid))
			assert.EqualValues(t, tc.amount*500000+tc.wantBonus, userQuotaForEpayNotifyTest(t, user.Id))
		})
	}
}

func TestEpayNotifyRejectsNegativeStoredBonus(t *testing.T) {
	setupEpayNotifyTest(t)

	user := model.User{Username: "epay_negative_bonus", Password: "password123", Quota: 7, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&user).Error)
	order := model.TopUp{
		UserId: user.Id, Amount: 10, Money: 10, TradeNo: "USR4NOEPAYNEG1",
		PaymentMethod: "alipay", PaymentProvider: model.PaymentProviderEpay,
		CreateTime: common.GetTimestamp(), Status: common.TopUpStatusPending, BonusQuota: -1_000_000,
	}
	require.NoError(t, model.DB.Create(&order).Error)

	require.Equal(t, "fail", postEpayNotify(t, signedEpayNotifyParams(order.TradeNo, "alipay", "10.00", epayNotifyTestKey)))
	require.Equal(t, 7, userQuotaForEpayNotifyTest(t, user.Id))
	require.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(order.TradeNo).Status)
}

func TestEpayNotifyRejectsWxPayOrderSettledAsAlipay(t *testing.T) {
	setupEpayNotifyTest(t)

	user := model.User{Username: "epay_wx_buyer", Password: "password123", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&user).Error)
	order := model.TopUp{
		UserId: user.Id, Amount: 5, Money: 5, TradeNo: "USR2NOEPAYWX1",
		PaymentMethod: "wxpay", PaymentProvider: model.PaymentProviderEpay,
		CreateTime: common.GetTimestamp(), Status: common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&order).Error)

	require.Equal(t, "fail", postEpayNotify(t, signedEpayNotifyParams(order.TradeNo, "alipay", "5.00", epayNotifyTestKey)))
	require.Equal(t, 0, userQuotaForEpayNotifyTest(t, user.Id))

	require.Equal(t, "success", postEpayNotify(t, signedEpayNotifyParams(order.TradeNo, "wxpay", "5.00", epayNotifyTestKey)))
	require.Equal(t, 5*500000, userQuotaForEpayNotifyTest(t, user.Id))
}
