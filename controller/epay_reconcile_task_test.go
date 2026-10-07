package controller

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupEpayReconcileTest(t *testing.T) {
	t.Helper()
	setupEpayNotifyTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}))
}

// countEpayReconcileQueries wraps the gateway query so tests can assert how
// often the task asked the gateway about each order.
func countEpayReconcileQueries(t *testing.T) *sync.Map {
	t.Helper()
	counts := &sync.Map{}
	original := epayReconcileQuery
	epayReconcileQuery = func(ctx context.Context, tradeNo string, callbackTradeNo string, orderMoney float64) error {
		value, _ := counts.LoadOrStore(tradeNo, new(atomic.Int32))
		value.(*atomic.Int32).Add(1)
		return original(ctx, tradeNo, callbackTradeNo, orderMoney)
	}
	t.Cleanup(func() { epayReconcileQuery = original })
	return counts
}

func reconcileQueryCount(counts *sync.Map, tradeNo string) int32 {
	value, ok := counts.Load(tradeNo)
	if !ok {
		return 0
	}
	return value.(*atomic.Int32).Load()
}

func createEpayReconcileTopUp(t *testing.T, name string, method string, status string, createdAt time.Time) (model.User, model.TopUp) {
	t.Helper()
	user := model.User{Username: name, AffCode: name, Password: "password123", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&user).Error)
	topUp := model.TopUp{
		UserId: user.Id, Amount: 10, Money: 10, TradeNo: "USR" + strconv.Itoa(user.Id) + "NO" + name,
		PaymentMethod: method, PaymentProvider: model.PaymentProviderEpay,
		CreateTime: createdAt.Unix(), Status: status,
	}
	require.NoError(t, model.DB.Create(&topUp).Error)
	return user, topUp
}

func unpaidGatewayOrder(tradeNo string, money string) map[string]any {
	return map[string]any{
		"code": 1, "status": 0, "pid": operation_setting.EpayId,
		"trade_no": "GW" + tradeNo, "out_trade_no": tradeNo, "money": money,
	}
}

func TestEpayReconcileCreditsPaidOrdersWhoseCallbackNeverArrived(t *testing.T) {
	setupEpayReconcileTest(t)
	now := time.Now()

	testCases := []struct {
		name   string
		method string
		status string
	}{
		{name: "recon_alipay_pending", method: "alipay", status: common.TopUpStatusPending},
		{name: "recon_alipay_expired", method: "alipay", status: common.TopUpStatusExpired},
		{name: "recon_wxpay_pending", method: "wxpay", status: common.TopUpStatusPending},
		{name: "recon_wxpay_cancelled", method: "wxpay", status: common.TopUpStatusCancelled},
	}
	users := make([]model.User, len(testCases))
	topUps := make([]model.TopUp, len(testCases))
	for i, tc := range testCases {
		users[i], topUps[i] = createEpayReconcileTopUp(t, tc.name, tc.method, tc.status, now.Add(-2*time.Minute))
	}

	runEpayReconcileOnce(now)

	for i, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, 10*500000, userQuotaForEpayNotifyTest(t, users[i].Id))
			assert.Equal(t, common.TopUpStatusSuccess, model.GetTopUpByTradeNo(topUps[i].TradeNo).Status)
		})
	}

	// A later tick, and the gateway's late callback, must not credit again.
	runEpayReconcileOnce(now.Add(time.Minute))
	signed := signedEpayNotifyParams(topUps[0].TradeNo, "alipay", "10.00", epayNotifyTestKey)
	require.Equal(t, "success", postEpayNotify(t, signed))
	assert.Equal(t, 10*500000, userQuotaForEpayNotifyTest(t, users[0].Id))
}

func TestEpayReconcileLeavesUnconfirmedOrdersUntouched(t *testing.T) {
	setupEpayReconcileTest(t)
	now := time.Now()

	unpaidUser, unpaid := createEpayReconcileTopUp(t, "recon_unpaid", "alipay", common.TopUpStatusPending, now.Add(-2*time.Minute))
	setFakeEpayGatewayOrder(t, unpaid.TradeNo, unpaidGatewayOrder(unpaid.TradeNo, "10.00"))
	wrongUser, wrongMoney := createEpayReconcileTopUp(t, "recon_wrong_money", "alipay", common.TopUpStatusPending, now.Add(-2*time.Minute))
	setFakeEpayGatewayOrder(t, wrongMoney.TradeNo, map[string]any{
		"code": 1, "status": 1, "pid": operation_setting.EpayId,
		"trade_no": "GW" + wrongMoney.TradeNo, "out_trade_no": wrongMoney.TradeNo, "money": "0.01",
	})
	failedUser, failedTopUp := createEpayReconcileTopUp(t, "recon_failed", "alipay", common.TopUpStatusFailed, now.Add(-2*time.Minute))

	runEpayReconcileOnce(now)

	for _, check := range []struct {
		user   model.User
		topUp  model.TopUp
		status string
	}{
		{unpaidUser, unpaid, common.TopUpStatusPending},
		{wrongUser, wrongMoney, common.TopUpStatusPending},
		{failedUser, failedTopUp, common.TopUpStatusFailed},
	} {
		assert.Zero(t, userQuotaForEpayNotifyTest(t, check.user.Id), check.topUp.TradeNo)
		assert.Equal(t, check.status, model.GetTopUpByTradeNo(check.topUp.TradeNo).Status, check.topUp.TradeNo)
	}
}

func TestEpayReconcileAndCallbackRacingCreditOnce(t *testing.T) {
	setupEpayReconcileTest(t)
	now := time.Now()
	user, topUp := createEpayReconcileTopUp(t, "recon_race", "alipay", common.TopUpStatusPending, now.Add(-2*time.Minute))
	signed := signedEpayNotifyParams(topUp.TradeNo, "alipay", "10.00", epayNotifyTestKey)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			runEpayReconcileOnce(now)
		}()
		go func() {
			defer wg.Done()
			_ = postEpayNotify(t, signed)
		}()
	}
	wg.Wait()

	assert.Equal(t, 10*500000, userQuotaForEpayNotifyTest(t, user.Id))
	var credits int64
	require.NoError(t, model.DB.Model(&model.QuotaCredit{}).Where("reference = ?", topUp.TradeNo).Count(&credits).Error)
	assert.EqualValues(t, 1, credits)
}

func TestEpayReconcileQueriesOnlyWithinCheckoutLifetime(t *testing.T) {
	setupEpayReconcileTest(t)
	counts := countEpayReconcileQueries(t)
	now := time.Now()

	_, fresh := createEpayReconcileTopUp(t, "recon_fresh", "alipay", common.TopUpStatusPending, now)
	_, nearEnd := createEpayReconcileTopUp(t, "recon_near_end", "alipay", common.TopUpStatusPending, now.Add(-14*time.Minute))
	_, tooOld := createEpayReconcileTopUp(t, "recon_too_old", "alipay", common.TopUpStatusExpired, now.Add(-16*time.Minute))
	for _, order := range []model.TopUp{fresh, nearEnd, tooOld} {
		setFakeEpayGatewayOrder(t, order.TradeNo, unpaidGatewayOrder(order.TradeNo, "10.00"))
	}

	for tick := 0; tick < 4; tick++ {
		runEpayReconcileOnce(now.Add(time.Duration(tick) * epayReconcileTickInterval))
	}

	assert.EqualValues(t, 4, reconcileQueryCount(counts, fresh.TradeNo), "new orders are queried from the first tick")
	assert.EqualValues(t, 3, reconcileQueryCount(counts, nearEnd.TradeNo), "queries stop once the order is 15 minutes old")
	assert.Zero(t, reconcileQueryCount(counts, tooOld.TradeNo))
}

func TestEpayReconcileSkipsWhenEpayDisabled(t *testing.T) {
	setupEpayReconcileTest(t)
	counts := countEpayReconcileQueries(t)
	now := time.Now()
	_, topUp := createEpayReconcileTopUp(t, "recon_disabled", "alipay", common.TopUpStatusPending, now.Add(-2*time.Minute))

	operation_setting.PayMethods = nil
	runEpayReconcileOnce(now)

	assert.Zero(t, reconcileQueryCount(counts, topUp.TradeNo))
	assert.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(topUp.TradeNo).Status)
}

func TestEpayReconcileCompletesPaidSubscriptionOrders(t *testing.T) {
	setupEpayReconcileTest(t)
	now := time.Now()

	user := model.User{Username: "recon_sub_buyer", Password: "password123", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&user).Error)
	plan := model.SubscriptionPlan{
		Title: "Recon Sub", PriceAmount: 9.99, Currency: "CNY",
		DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, Enabled: true, TotalAmount: 1000,
	}
	require.NoError(t, model.DB.Create(&plan).Error)
	paid := model.SubscriptionOrder{
		UserId: user.Id, PlanId: plan.Id, Money: 9.99, TradeNo: "SUBUSR1NORECONPAID",
		PaymentMethod: "alipay", PaymentProvider: model.PaymentProviderEpay,
		CreateTime: now.Add(-2 * time.Minute).Unix(), Status: common.TopUpStatusPending,
	}
	unpaid := model.SubscriptionOrder{
		UserId: user.Id, PlanId: plan.Id, Money: 9.99, TradeNo: "SUBUSR1NORECONUNPAID",
		PaymentMethod: "alipay", PaymentProvider: model.PaymentProviderEpay,
		CreateTime: now.Add(-2 * time.Minute).Unix(), Status: common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&paid).Error)
	require.NoError(t, model.DB.Create(&unpaid).Error)
	setFakeEpayGatewayOrder(t, unpaid.TradeNo, unpaidGatewayOrder(unpaid.TradeNo, "9.99"))

	runEpayReconcileOnce(now)
	runEpayReconcileOnce(now.Add(time.Minute))

	assert.Equal(t, common.TopUpStatusSuccess, model.GetSubscriptionOrderByTradeNo(paid.TradeNo).Status)
	assert.Equal(t, common.TopUpStatusPending, model.GetSubscriptionOrderByTradeNo(unpaid.TradeNo).Status)
	var subscriptions int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptions).Error)
	assert.EqualValues(t, 1, subscriptions)
}
