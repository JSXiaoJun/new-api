package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
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

func TestQueryEpayOrderUsesActiveOrderEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api.php", r.URL.Path)
		require.Equal(t, "order", r.URL.Query().Get("act"))
		require.Equal(t, "test-pid", r.URL.Query().Get("pid"))
		require.Equal(t, "test-key", r.URL.Query().Get("key"))
		require.Equal(t, "WX-QUERY-1", r.URL.Query().Get("out_trade_no"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"msg":"查询订单号成功！","status":1}`))
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

	paid, err := queryEpayOrder(context.Background(), "WX-QUERY-1")
	require.NoError(t, err)
	require.True(t, paid)
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

	paid, err := queryEpayOrder(context.Background(), "WX-QUERY-0")
	require.NoError(t, err)
	require.False(t, paid)
}
