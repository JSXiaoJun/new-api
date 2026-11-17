package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestEpayQRCodeUsesPcDeviceAndReturnsGatewayQRCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/mapi.php", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		require.NoError(t, r.ParseForm())
		require.Equal(t, "pc", r.PostForm.Get("device"))
		require.Equal(t, "wxpay", r.PostForm.Get("type"))
		require.Equal(t, "WX-QR-1", r.PostForm.Get("out_trade_no"))
		require.NotEmpty(t, r.PostForm.Get("sign"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"qrcode":"weixin://wxpay/bizpayurl?pr=WX-QR-1"}`))
	}))
	defer server.Close()

	oldAddress := operation_setting.PayAddress
	oldID := operation_setting.EpayId
	oldKey := operation_setting.EpayKey
	operation_setting.PayAddress = server.URL
	operation_setting.EpayId = "test-pid"
	operation_setting.EpayKey = "test-key"
	t.Cleanup(func() {
		operation_setting.PayAddress = oldAddress
		operation_setting.EpayId = oldID
		operation_setting.EpayKey = oldKey
	})

	qrCode, err := requestEpayQRCode(
		context.Background(),
		"wxpay",
		"WX-QR-1",
		"TUC100",
		"1.00",
		"https://merchant.example/notify",
		"https://merchant.example/return",
		"127.0.0.1",
	)
	require.NoError(t, err)
	require.Equal(t, "weixin://wxpay/bizpayurl?pr=WX-QR-1", qrCode)
}

func TestRequestEpayQRCodeAcceptsStringCodeAndPayURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"1","data":{"payurl":"weixin://wxpay/bizpayurl?pr=WX-QR-2"}}`))
	}))
	defer server.Close()

	oldAddress := operation_setting.PayAddress
	oldID := operation_setting.EpayId
	oldKey := operation_setting.EpayKey
	operation_setting.PayAddress = server.URL
	operation_setting.EpayId = "test-pid"
	operation_setting.EpayKey = "test-key"
	t.Cleanup(func() {
		operation_setting.PayAddress = oldAddress
		operation_setting.EpayId = oldID
		operation_setting.EpayKey = oldKey
	})

	qrCode, err := requestEpayQRCode(
		context.Background(),
		"wxpay",
		"WX-QR-2",
		"TUC100",
		"1.00",
		"https://merchant.example/notify",
		"https://merchant.example/return",
		"127.0.0.1",
	)
	require.NoError(t, err)
	require.Equal(t, "weixin://wxpay/bizpayurl?pr=WX-QR-2", qrCode)
}

func TestRequestEpayQRCodeRejectsSuccessfulResponseWithoutQRCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"msg":"二维码服务未配置"}`))
	}))
	defer server.Close()

	oldAddress := operation_setting.PayAddress
	oldID := operation_setting.EpayId
	oldKey := operation_setting.EpayKey
	operation_setting.PayAddress = server.URL
	operation_setting.EpayId = "test-pid"
	operation_setting.EpayKey = "test-key"
	t.Cleanup(func() {
		operation_setting.PayAddress = oldAddress
		operation_setting.EpayId = oldID
		operation_setting.EpayKey = oldKey
	})

	_, err := requestEpayQRCode(
		context.Background(),
		"wxpay",
		"WX-QR-3",
		"TUC100",
		"1.00",
		"https://merchant.example/notify",
		"https://merchant.example/return",
		"127.0.0.1",
	)
	var rejectedErr *epayQRCodeRejectedError
	require.ErrorAs(t, err, &rejectedErr)
	require.Equal(t, "二维码服务未配置", rejectedErr.Error())
}

func TestQueryEpayOrderUsesActiveOrderEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api.php", r.URL.Path)
		require.Equal(t, "order", r.URL.Query().Get("act"))
		require.Equal(t, "test-pid", r.URL.Query().Get("pid"))
		require.Equal(t, "test-key", r.URL.Query().Get("key"))
		require.Equal(t, "WX-QUERY-1", r.URL.Query().Get("out_trade_no"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"msg":"查询订单号成功！","status":1,"pid":"test-pid","trade_no":"GW1","out_trade_no":"WX-QUERY-1","money":"1.00"}`))
	}))
	defer server.Close()

	oldAddress := operation_setting.PayAddress
	oldID := operation_setting.EpayId
	oldKey := operation_setting.EpayKey
	operation_setting.PayAddress = server.URL
	operation_setting.EpayId = "test-pid"
	operation_setting.EpayKey = "test-key"
	t.Cleanup(func() {
		operation_setting.PayAddress = oldAddress
		operation_setting.EpayId = oldID
		operation_setting.EpayKey = oldKey
	})

	order, err := queryEpayOrder(context.Background(), "WX-QUERY-1")
	require.NoError(t, err)
	assert.Equal(t, &epayGatewayOrder{Paid: true, TradeNo: "GW1", OutTradeNo: "WX-QUERY-1", Pid: "test-pid", Money: "1.00"}, order)
}

func TestQueryEpayOrderReportsUnpaidOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"msg":"查询订单号成功！","status":0}`))
	}))
	defer server.Close()

	oldAddress := operation_setting.PayAddress
	oldID := operation_setting.EpayId
	oldKey := operation_setting.EpayKey
	operation_setting.PayAddress = server.URL
	operation_setting.EpayId = "test-pid"
	operation_setting.EpayKey = "test-key"
	t.Cleanup(func() {
		operation_setting.PayAddress = oldAddress
		operation_setting.EpayId = oldID
		operation_setting.EpayKey = oldKey
	})

	order, err := queryEpayOrder(context.Background(), "WX-QUERY-0")
	require.NoError(t, err)
	assert.False(t, order.Paid)
}

func TestSubscriptionEpayOrderNameSignsPlanTitlesWithAmpersand(t *testing.T) {
	name := subscriptionEpayOrderName("GPT & Claude")
	assert.Equal(t, "SUB:GPT + Claude", name)

	client := &epay.Client{Config: &epay.Config{PartnerID: "test-pid", Key: "test-key"}, BaseUrl: &url.URL{Scheme: "https", Host: "pay.example"}}
	_, params, err := client.Purchase(&epay.PurchaseArgs{
		Type:           "alipay",
		ServiceTradeNo: "SUBUSR1NOAMP",
		Name:           name,
		Money:          "1.00",
		Device:         epay.PC,
		NotifyUrl:      &url.URL{Scheme: "https", Host: "merchant.example", Path: "/notify"},
		ReturnUrl:      &url.URL{Scheme: "https", Host: "merchant.example", Path: "/return"},
	})
	require.NoError(t, err)
	assert.Equal(t, name, params["name"])
}

func TestGetPayMoneyAddsEpayFeeOnlyBelowThreshold(t *testing.T) {
	paymentSetting := operation_setting.GetPaymentSetting()
	oldPrice := operation_setting.Price
	oldEnabled, oldThreshold, oldRate := paymentSetting.EpayFeeEnabled, paymentSetting.EpayFeeThreshold, paymentSetting.EpayFeeRate
	oldDiscount := paymentSetting.AmountDiscount
	t.Cleanup(func() {
		operation_setting.Price = oldPrice
		paymentSetting.EpayFeeEnabled, paymentSetting.EpayFeeThreshold, paymentSetting.EpayFeeRate = oldEnabled, oldThreshold, oldRate
		paymentSetting.AmountDiscount = oldDiscount
	})
	operation_setting.Price = 1
	paymentSetting.AmountDiscount = map[int]float64{}
	require.NotEqual(t, operation_setting.QuotaDisplayTypeTokens, operation_setting.GetQuotaDisplayType())

	cases := []struct {
		name      string
		enabled   bool
		threshold float64
		rate      float64
		amount    int64
		wantMoney float64
		wantFee   float64
	}{
		{name: "disabled keeps the amount", enabled: false, threshold: 50, rate: 0.6, amount: 10, wantMoney: 10, wantFee: 0},
		{name: "below threshold adds the fee", enabled: true, threshold: 50, rate: 0.6, amount: 10, wantMoney: 10.06, wantFee: 0.06},
		{name: "fee rounds up to the cent", enabled: true, threshold: 50, rate: 0.6, amount: 49, wantMoney: 49.30, wantFee: 0.30},
		{name: "amount equal to threshold is merchant paid", enabled: true, threshold: 50, rate: 0.6, amount: 50, wantMoney: 50, wantFee: 0},
		{name: "amount above threshold is merchant paid", enabled: true, threshold: 50, rate: 0.6, amount: 100, wantMoney: 100, wantFee: 0},
		{name: "zero rate adds nothing", enabled: true, threshold: 50, rate: 0, amount: 10, wantMoney: 10, wantFee: 0},
		{name: "stored rate above the cap adds nothing", enabled: true, threshold: 50, rate: 60, amount: 10, wantMoney: 10, wantFee: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			paymentSetting.EpayFeeEnabled = tc.enabled
			paymentSetting.EpayFeeThreshold = tc.threshold
			paymentSetting.EpayFeeRate = tc.rate

			money, fee := getPayMoney(tc.amount, "default")
			assert.InDelta(t, tc.wantMoney, money, 1e-9)
			assert.InDelta(t, tc.wantFee, fee, 1e-9)
		})
	}
}

func TestValidateEpayFeeOptions(t *testing.T) {
	for _, raw := range []string{"0", "0.6", "20"} {
		assert.NoError(t, operation_setting.ValidateEpayFeeRate(raw), raw)
	}
	for _, raw := range []string{"-0.1", "20.01", "NaN", "abc", ""} {
		assert.Error(t, operation_setting.ValidateEpayFeeRate(raw), raw)
	}
	for _, raw := range []string{"0", "100", "99.5"} {
		assert.NoError(t, operation_setting.ValidateEpayFeeThreshold(raw), raw)
	}
	for _, raw := range []string{"-1", "Inf", "abc", ""} {
		assert.Error(t, operation_setting.ValidateEpayFeeThreshold(raw), raw)
	}
}
