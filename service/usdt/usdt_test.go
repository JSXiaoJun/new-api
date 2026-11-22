package usdt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		panic(err)
	}
	sqlDB.SetMaxOpenConns(1)
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.OptionMap = map[string]string{}
	if err := db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.QuotaCredit{}, &model.Log{}, &model.Option{},
		&model.UsdtDepositAddress{}, &model.UsdtDeposit{}); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestAddressDerivationMatchesTheScannerAndContract(t *testing.T) {
	// Vectors produced by new-api-usdt's derivation, which its simulated-chain
	// tests check against DepositFactory.walletAddress and real deployments.
	salt := Salt([32]byte{1, 2, 3}, 100)
	assert.Equal(t, "0xe2850d02bb38d9aa6c49e80db7287ee9f9f7dc9d09ae6deca874573ce14979fa", Hex32(salt))
	factory := mustHex20("0x1111111111111111111111111111111111111111")
	impl := mustHex20("0x2222222222222222222222222222222222222222")
	assert.Equal(t, "0xbcf8efa52713ceda3d694aa787c7f01cd82e8875", Hex20(WalletAddress(0xff, factory, impl, salt)))
	assert.Equal(t, "0xb68e0e64162a0697ee8aa5fec4eb79d8a5fcaa16", Hex20(WalletAddress(0x41, factory, impl, salt)))

	tron, _ := LookupChain(operation_setting.UsdtChainTron)
	assert.Equal(t, "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", DisplayAddress(tron, tron.USDT))
	back, err := ParseTronAddress("TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t")
	require.NoError(t, err)
	assert.Equal(t, tron.USDT, back)
	_, err = ParseTronAddress("TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6u")
	assert.Error(t, err)
}

// fakeChain is a scripted JSON-RPC node.
type fakeChain struct {
	mu        sync.Mutex
	chainID   uint64
	head      uint64
	finalized uint64
	hashes    map[uint64]string
	receipts  map[string]map[string]any
}

func (f *fakeChain) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	defer f.mu.Unlock()
	var result any
	switch req.Method {
	case "eth_chainId":
		result = fmt.Sprintf("0x%x", f.chainID)
	case "eth_blockNumber":
		result = fmt.Sprintf("0x%x", f.head)
	case "eth_getBlockByNumber":
		var tag string
		_ = json.Unmarshal(req.Params[0], &tag)
		number := f.finalized
		if tag != "finalized" && tag != "safe" {
			n, _ := strconv.ParseUint(tag[2:], 16, 64)
			number = n
		}
		result = map[string]any{"number": fmt.Sprintf("0x%x", number), "hash": f.hashes[number]}
	case "eth_getTransactionReceipt":
		var hash string
		_ = json.Unmarshal(req.Params[0], &hash)
		if rec, ok := f.receipts[hash]; ok {
			result = rec
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
}

func topicFor(a [20]byte) string { return fmt.Sprintf("0x%024x%x", 0, a[:]) }

func transferLog(contract, from, to [20]byte, amount *big.Int, index int) map[string]any {
	return map[string]any{
		"address":  Hex20(contract),
		"topics":   []string{Hex32(transferTopic), topicFor(from), topicFor(to)},
		"data":     fmt.Sprintf("0x%064x", amount),
		"logIndex": fmt.Sprintf("0x%x", index),
		"removed":  false,
	}
}

func receipt(status string, block uint64, hash string, logs ...map[string]any) map[string]any {
	return map[string]any{"status": status, "blockNumber": fmt.Sprintf("0x%x", block), "blockHash": hash, "logs": logs}
}

const (
	testFactory = "0x1111111111111111111111111111111111111111"
	testImpl    = "0x2222222222222222222222222222222222222222"
)

// configureChain points Polygon's two verification nodes at fake nodes and
// marks its factory verified.
func configureChain(t *testing.T, primary, secondary string, rate float64) {
	t.Helper()
	chains := map[string]operation_setting.UsdtChainSetting{
		operation_setting.UsdtChainPolygon: {
			Enabled: true, Verified: true, FactoryAddress: testFactory, ImplementationAddress: testImpl,
			RPCURL: primary, SecondaryRPCURL: secondary, Finality: operation_setting.UsdtFinalityFinalized, MinAmount: 1,
		},
	}
	raw, err := common.Marshal(chains)
	require.NoError(t, err)
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("usdt_setting"), map[string]string{
		"enabled": "true", "chains": string(raw), "rate_mode": "fixed", "fixed_rate": strconv.FormatFloat(rate, 'f', -1, 64),
		"salt_secret": "0x0101010101010101010101010101010101010101010101010101010101010101",
	}))
	chainIDCache.Clear()
}

func newFakeNode(t *testing.T, fake *fakeChain) string {
	t.Helper()
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return server.URL
}

// newFakePolygon returns one honest chain served by two separate nodes.
func newFakePolygon(t *testing.T) (*fakeChain, string, string) {
	t.Helper()
	fake := &fakeChain{chainID: 137, head: 120, finalized: 110, hashes: map[uint64]string{}, receipts: map[string]map[string]any{}}
	return fake, newFakeNode(t, fake), newFakeNode(t, fake)
}

func txHash(n int) string { return fmt.Sprintf("0x%064x", n) }

func TestFinalTransfersTrustsOnlyFinalCanonicalOfficialTransfers(t *testing.T) {
	fake, primary, secondary := newFakePolygon(t)
	configureChain(t, primary, secondary, 1)
	polygon, _ := LookupChain(operation_setting.UsdtChainPolygon)
	from, to := mustHex20("0x00000000000000000000000000000000000000aa"), mustHex20("0x00000000000000000000000000000000000000bb")
	fakeToken := mustHex20("0x00000000000000000000000000000000000000cc")
	fake.hashes[100] = "0xblock100"
	fake.hashes[115] = "0xblock115"
	fake.receipts[txHash(1)] = receipt("0x1", 100, "0xblock100",
		transferLog(fakeToken, from, to, big.NewInt(999_000_000), 0), // a token that only calls itself USDT
		transferLog(polygon.USDT, from, to, big.NewInt(12_500_000), 1))
	fake.receipts[txHash(2)] = receipt("0x0", 100, "0xblock100", transferLog(polygon.USDT, from, to, big.NewInt(1), 0))
	fake.receipts[txHash(3)] = receipt("0x1", 115, "0xblock115", transferLog(polygon.USDT, from, to, big.NewInt(1), 0))
	fake.receipts[txHash(4)] = receipt("0x1", 100, "0xorphaned", transferLog(polygon.USDT, from, to, big.NewInt(1), 0))

	transfers, err := FinalTransfers(context.Background(), "polygon", txHash(1))
	require.NoError(t, err)
	require.Len(t, transfers, 1, "only the official USDT contract counts")
	assert.Equal(t, 1, transfers[0].LogIndex)
	assert.Equal(t, to, transfers[0].To)
	assert.Equal(t, big.NewInt(12_500_000), transfers[0].Amount)

	_, err = FinalTransfers(context.Background(), "polygon", txHash(2))
	assert.True(t, errors.Is(err, ErrRejected), "failed transaction: %v", err)
	_, err = FinalTransfers(context.Background(), "polygon", txHash(3))
	assert.True(t, errors.Is(err, ErrNotReady), "block above finalized: %v", err)
	_, err = FinalTransfers(context.Background(), "polygon", txHash(4))
	assert.True(t, errors.Is(err, ErrNotReady), "reorganized block: %v", err)
	_, err = FinalTransfers(context.Background(), "polygon", txHash(9))
	assert.True(t, errors.Is(err, ErrNotReady), "unknown transaction: %v", err)

	fake.mu.Lock()
	fake.chainID = 80002 // an RPC that serves a testnet must never verify mainnet deposits
	fake.mu.Unlock()
	chainIDCache.Clear()
	_, err = FinalTransfers(context.Background(), "polygon", txHash(1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chain id")
}

func userQuota(t *testing.T, id int) int {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("quota").First(&user, id).Error)
	return user.Quota
}

func TestDepositIsCreditedOnceToTheAddressOwner(t *testing.T) {
	fake, primary, secondary := newFakePolygon(t)
	configureChain(t, primary, secondary, 1)
	polygon, _ := LookupChain(operation_setting.UsdtChainPolygon)
	require.NoError(t, model.DB.Create(&model.User{Id: 501, Username: "usdt-owner", AffCode: "aff501", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: 502, Username: "usdt-other", AffCode: "aff502", Status: common.UserStatusEnabled}).Error)

	addr, err := EnsureUserAddress("polygon", 501)
	require.NoError(t, err)
	again, err := EnsureUserAddress("polygon", 501)
	require.NoError(t, err)
	assert.Equal(t, addr, again, "an address is permanent")
	wallet := mustHex20(addr)
	from := mustHex20("0x00000000000000000000000000000000000000aa")
	fake.hashes[100] = "0xblock100"
	fake.receipts[txHash(10)] = receipt("0x1", 100, "0xblock100",
		transferLog(polygon.USDT, from, wallet, big.NewInt(12_500_000), 0), // 12.5 USDT
		transferLog(polygon.USDT, from, wallet, big.NewInt(500_000), 1))    // 0.5 USDT, below the minimum of 1

	// Another user claiming the transaction credits nothing.
	outcomes, err := ProcessTransaction(context.Background(), "polygon", txHash(10), -1, 502, model.UsdtDepositSourceClaim, "", false)
	require.NoError(t, err)
	for _, o := range outcomes {
		assert.Equal(t, "not_ours", o.Status)
	}
	assert.Zero(t, userQuota(t, 502))

	outcomes, err = ProcessTransaction(context.Background(), "polygon", txHash(10), -1, 0, model.UsdtDepositSourceScan, "", false)
	require.NoError(t, err)
	require.Len(t, outcomes, 2)
	assert.Equal(t, model.UsdtDepositCredited, outcomes[0].Status)
	assert.Equal(t, model.UsdtDepositBelowMin, outcomes[1].Status)
	expected := int(12.5 * common.QuotaPerUnit)
	assert.Equal(t, expected, userQuota(t, 501))

	// The scanner, the user and an administrator may all resubmit it.
	for _, source := range []string{model.UsdtDepositSourceScan, model.UsdtDepositSourceClaim, model.UsdtDepositSourceAdmin} {
		outcomes, err = ProcessTransaction(context.Background(), "polygon", txHash(10), 0, 0, source, "", false)
		require.NoError(t, err)
		require.Len(t, outcomes, 1)
		assert.Equal(t, "already", outcomes[0].Status)
	}
	assert.Equal(t, expected, userQuota(t, 501), "a transfer is credited at most once")

	// Resubmitting the below-minimum transfer still reports it as not credited.
	outcomes, err = ProcessTransaction(context.Background(), "polygon", txHash(10), 1, 501, model.UsdtDepositSourceClaim, "", false)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.Equal(t, model.UsdtDepositBelowMin, outcomes[0].Status)
	assert.Equal(t, expected, userQuota(t, 501))

	// An administrator can credit the below-minimum transfer explicitly.
	outcomes, err = ProcessTransaction(context.Background(), "polygon", txHash(10), 1, 501, model.UsdtDepositSourceAdmin, "", true)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.Equal(t, model.UsdtDepositCredited, outcomes[0].Status)
	assert.Equal(t, expected+int(0.5*common.QuotaPerUnit), userQuota(t, 501))

	var topups []model.TopUp
	require.NoError(t, model.DB.Where("user_id = ? AND payment_provider = ?", 501, model.PaymentProviderUSDT).Find(&topups).Error)
	require.Len(t, topups, 2)
	for _, topup := range topups {
		assert.Equal(t, common.TopUpStatusSuccess, topup.Status)
	}
}

func TestScannerRequestsMustBeSignedRecentAndFromAllowedIPs(t *testing.T) {
	// The same vector is asserted by the scanner service's client.
	assert.Equal(t, "d2109300514e5e27d648b9f9b2d357a664b836ae1c485ebe683be48906667b6e",
		ScannerSignature("scanner-secret", "1700000000", "POST", "/api/usdt/scanner/deposit", []byte(`{"chain":"bsc"}`)))

	secret := "scanner-secret"
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("usdt_setting"), map[string]string{
		"scanner_secret": secret, "scanner_ips": "",
	}))
	body := []byte(`{"chain":"bsc"}`)
	now := strconv.FormatInt(time.Now().Unix(), 10)
	sig := ScannerSignature(secret, now, "POST", "/api/usdt/scanner/deposit", body)

	assert.NoError(t, VerifyScannerRequest("1.2.3.4", now, sig, "POST", "/api/usdt/scanner/deposit", body))
	assert.Error(t, VerifyScannerRequest("1.2.3.4", now, sig, "POST", "/api/usdt/scanner/deposit", []byte(`{"chain":"tron"}`)), "tampered body")
	assert.Error(t, VerifyScannerRequest("1.2.3.4", now, sig, "POST", "/api/usdt/scanner/state", body), "other path")
	old := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	assert.Error(t, VerifyScannerRequest("1.2.3.4", old, ScannerSignature(secret, old, "POST", "/api/usdt/scanner/deposit", body), "POST", "/api/usdt/scanner/deposit", body), "stale timestamp")

	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("usdt_setting"), map[string]string{"scanner_ips": "5.6.7.8"}))
	assert.Error(t, VerifyScannerRequest("1.2.3.4", now, sig, "POST", "/api/usdt/scanner/deposit", body), "IP outside the allowlist")
	assert.NoError(t, VerifyScannerRequest("5.6.7.8", now, sig, "POST", "/api/usdt/scanner/deposit", body))
}

func TestFactoryFreezesOnceAddressesExistAndEnablingNeedsVerification(t *testing.T) {
	_, primary, secondary := newFakePolygon(t)
	configureChain(t, primary, secondary, 1)
	require.NoError(t, model.DB.Create(&model.User{Id: 601, Username: "usdt-freeze", AffCode: "aff601", Status: common.UserStatusEnabled}).Error)
	_, err := EnsureUserAddress("polygon", 601)
	require.NoError(t, err)

	other := "0x3333333333333333333333333333333333333333"
	err = UpdateSettings(SettingsPatch{Chains: map[string]ChainPatch{"polygon": {FactoryAddress: &other}}})
	require.Error(t, err)
	assert.Equal(t, testFactory, operation_setting.GetUsdtSetting().Chains["polygon"].FactoryAddress)

	enable := true
	err = UpdateSettings(SettingsPatch{Chains: map[string]ChainPatch{"bsc": {Enabled: &enable}}})
	require.Error(t, err, "an unverified network cannot be shown to users")

	// A network without addresses may change its factory, which clears verification.
	bscFactory := "0x4444444444444444444444444444444444444444"
	require.NoError(t, UpdateSettings(SettingsPatch{Chains: map[string]ChainPatch{"bsc": {FactoryAddress: &bscFactory}}}))
	bsc := operation_setting.GetUsdtSetting().Chains["bsc"]
	assert.Equal(t, bscFactory, bsc.FactoryAddress)
	assert.False(t, bsc.Verified)
}

func TestOneLyingVerificationNodeCannotCreditADeposit(t *testing.T) {
	honest, _, _ := newFakePolygon(t)
	liar := &fakeChain{chainID: 137, head: 120, finalized: 110, hashes: map[uint64]string{}, receipts: map[string]map[string]any{}}
	honestURL, liarURL := newFakeNode(t, honest), newFakeNode(t, liar)
	polygon, _ := LookupChain(operation_setting.UsdtChainPolygon)
	require.NoError(t, model.DB.Create(&model.User{Id: 801, Username: "usdt-liar-target", AffCode: "aff801", Status: common.UserStatusEnabled}).Error)
	configureChain(t, honestURL, liarURL, 1)
	addr, err := EnsureUserAddress("polygon", 801)
	require.NoError(t, err)
	wallet, from := mustHex20(addr), mustHex20("0x00000000000000000000000000000000000000aa")
	other := mustHex20("0x00000000000000000000000000000000000000ee")
	for _, f := range []*fakeChain{honest, liar} {
		f.hashes[100] = "0xblock100"
	}
	real := receipt("0x1", 100, "0xblock100", transferLog(polygon.USDT, from, wallet, big.NewInt(2_000_000), 0))
	cases := []struct {
		name         string
		honest, liar map[string]any
		wantAlert    bool
	}{
		{"invented transaction", nil, receipt("0x1", 100, "0xblock100", transferLog(polygon.USDT, from, wallet, big.NewInt(9_000_000_000), 0)), false},
		{"inflated amount", real, receipt("0x1", 100, "0xblock100", transferLog(polygon.USDT, from, wallet, big.NewInt(9_000_000_000), 0)), true},
		{"redirected recipient", receipt("0x1", 100, "0xblock100", transferLog(polygon.USDT, from, other, big.NewInt(2_000_000), 0)), real, true},
		{"failed transaction reported as success", receipt("0x0", 100, "0xblock100", transferLog(polygon.USDT, from, wallet, big.NewInt(2_000_000), 0)), real, true},
		{"extra transfer", real, receipt("0x1", 100, "0xblock100",
			transferLog(polygon.USDT, from, wallet, big.NewInt(2_000_000), 0), transferLog(polygon.USDT, from, wallet, big.NewInt(2_000_000), 1)), true},
	}
	for i, tc := range cases {
		hash := txHash(200 + i)
		if tc.honest != nil {
			honest.receipts[hash] = tc.honest
		}
		liar.receipts[hash] = tc.liar
		before := len(RecentVerificationAlerts())
		for _, order := range [][2]string{{honestURL, liarURL}, {liarURL, honestURL}} {
			configureChain(t, order[0], order[1], 1)
			_, err := ProcessTransaction(context.Background(), "polygon", hash, -1, 0, model.UsdtDepositSourceScan, "", false)
			assert.True(t, errors.Is(err, ErrNotReady), "%s: %v", tc.name, err)
		}
		assert.Equal(t, tc.wantAlert, len(RecentVerificationAlerts()) > before, tc.name)
		assert.Zero(t, userQuota(t, 801), tc.name)
	}

	// Once both nodes agree, the real deposit is credited.
	honest.receipts[txHash(300)], liar.receipts[txHash(300)] = real, real
	outcomes, err := ProcessTransaction(context.Background(), "polygon", txHash(300), -1, 0, model.UsdtDepositSourceScan, "", false)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.Equal(t, 2*int(common.QuotaPerUnit), userQuota(t, 801))

	// Without a second node nothing is credited at all.
	configureChain(t, honestURL, "", 1)
	honest.receipts[txHash(301)] = real
	_, err = FinalTransfers(context.Background(), "polygon", txHash(301))
	assert.True(t, errors.Is(err, ErrNotReady), "%v", err)
}

func TestVerificationNodesMustBeDifferentProviders(t *testing.T) {
	assert.NoError(t, DistinctProviders("https://polygon-mainnet.g.alchemy.com/v2/k", "https://rpc.ankr.com/polygon/k"))
	assert.Error(t, DistinctProviders("https://polygon-mainnet.g.alchemy.com/v2/a", "https://polygon-mainnet.g.alchemy.com/v2/b"), "same provider, two keys")
	assert.Error(t, DistinctProviders("https://a.example,https://b.example", "https://c.example,https://B.example"), "shared host in a failover list")
	assert.Error(t, DistinctProviders("https://a.example", ""))

	primary, secondary := "https://polygon-mainnet.g.alchemy.com/v2/a", "https://polygon-mainnet.g.alchemy.com/v2/b"
	err := UpdateSettings(SettingsPatch{Chains: map[string]ChainPatch{"arbitrum": {RPCURL: &primary, RPCURLSecondary: &secondary}}})
	require.Error(t, err)
}

func TestC2CRateIsTheMedianCNYPriceOverTheTopUpPriceAndRefreshesEveryHalfHour(t *testing.T) {
	okxAds := `{"code":0,"data":{"sell":[{"price":"6.70"},{"price":"6.72"},{"price":"9.90"},{"price":"6.71"},{"price":"5.10"}]}}`
	binanceAds := `{"code":"000000","data":[{"adv":{"price":"6.80"}},{"adv":{"price":"6.82"}}]}`
	var okxUp, binanceUp bool
	var okxHits int
	okx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		okxHits++
		if !okxUp {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(okxAds))
	}))
	t.Cleanup(okx.Close)
	binance := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !binanceUp || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(binanceAds))
	}))
	t.Cleanup(binance.Close)

	oldOkx, oldBinance, oldPrice := okxC2CURL, binanceC2CURL, operation_setting.Price
	okxC2CURL, binanceC2CURL, operation_setting.Price = okx.URL, binance.URL, 2
	t.Cleanup(func() {
		okxC2CURL, binanceC2CURL, operation_setting.Price = oldOkx, oldBinance, oldPrice
		c2cCache.price, c2cCache.source, c2cCache.at, c2cCache.tried, c2cCache.lastErr = decimal.Zero, "", time.Time{}, time.Time{}, nil
		require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("usdt_setting"), map[string]string{"rate_mode": "market"}))
	})
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("usdt_setting"), map[string]string{"rate_mode": "c2c"}))
	reset := func(at time.Time) {
		c2cCache.price, c2cCache.source, c2cCache.at, c2cCache.tried, c2cCache.lastErr = decimal.RequireFromString("6.5"), "okx_c2c", at, time.Time{}, nil
	}

	// Both feeds down and no price yet: deposits wait.
	reset(time.Time{})
	_, _, err := CurrentRate(context.Background())
	assert.True(t, errors.Is(err, ErrNotReady), "%v", err)

	// OKX answers: median of its ads (6.71) over a top-up price of 2.
	okxUp = true
	reset(time.Time{})
	rate, source, err := CurrentRate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "3.355", rate.String())
	assert.Equal(t, "okx_c2c", source)

	// Within 30 minutes the cached price is used without asking again.
	hits := okxHits
	_, _, err = CurrentRate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, hits, okxHits)

	// After 30 minutes it refreshes; OKX failing falls back to Binance.
	okxUp, binanceUp = false, true
	reset(time.Now().Add(-31 * time.Minute))
	rate, source, err = CurrentRate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "3.405", rate.String())
	assert.Equal(t, "binance_c2c", source)

	// Both down: the last price is kept for up to 2 hours, then deposits wait.
	binanceUp = false
	reset(time.Now().Add(-90 * time.Minute))
	rate, _, err = CurrentRate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "3.25", rate.String())
	reset(time.Now().Add(-3 * time.Hour))
	_, _, err = CurrentRate(context.Background())
	assert.True(t, errors.Is(err, ErrNotReady), "%v", err)

	// A price that cannot be USDT/CNY is refused.
	okxUp, okxAds = true, `{"code":0,"data":{"sell":[{"price":"0.99"}]}}`
	reset(time.Time{})
	_, _, err = CurrentRate(context.Background())
	assert.True(t, errors.Is(err, ErrNotReady), "%v", err)
}
