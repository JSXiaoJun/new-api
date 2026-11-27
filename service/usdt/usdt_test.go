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
	"slices"
	"strconv"
	"strings"
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

// fakeChain is a scripted JSON-RPC node. deny makes a method answer with an
// HTTP status instead, the way a provider refuses one method and serves others.
type fakeChain struct {
	mu        sync.Mutex
	chainID   uint64
	head      uint64
	finalized uint64
	hashes    map[uint64]string
	txs       map[uint64][]string
	receipts  map[string]map[string]any
	deny      map[string]int
	calls     map[string]int
}

func (f *fakeChain) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[req.Method]++
	if status := f.deny[req.Method]; status != 0 {
		w.WriteHeader(status)
		return
	}
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
		result = map[string]any{"number": fmt.Sprintf("0x%x", number), "hash": f.hashes[number], "transactions": f.txs[number]}
	case "eth_getTransactionReceipt":
		var hash string
		_ = json.Unmarshal(req.Params[0], &hash)
		if rec, ok := f.receipts[hash]; ok {
			result = rec
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
}

func (f *fakeChain) set(fn func(f *fakeChain)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeChain) callCount(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[method]
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

// configureChain points Polygon's verification nodes at fake nodes, each its
// own provider, in priority order, and marks its factory verified.
func configureChain(t *testing.T, primary, secondary string, rate float64) {
	t.Helper()
	var urls []string
	for _, u := range []string{primary, secondary} {
		if u != "" {
			urls = append(urls, u)
		}
	}
	configureChainNodes(t, operation_setting.UsdtChainPolygon, rate, urls...)
}

func configureChainNodes(t *testing.T, chainID string, rate float64, urls ...string) {
	t.Helper()
	nodes := []operation_setting.UsdtRPCNode{}
	for i, u := range urls {
		nodes = append(nodes, operation_setting.UsdtRPCNode{
			ID: fmt.Sprintf("n%d", i+1), Name: fmt.Sprintf("node %d", i+1), Provider: fmt.Sprintf("provider %d", i+1),
			URL: u, Priority: (i + 1) * 10, Enabled: true,
		})
	}
	configureChainSetting(t, chainID, rate, operation_setting.UsdtChainSetting{RPCNodes: nodes})
}

func configureChainSetting(t *testing.T, chainID string, rate float64, cfg operation_setting.UsdtChainSetting) {
	t.Helper()
	cfg.Enabled, cfg.Verified, cfg.FactoryAddress, cfg.ImplementationAddress = true, true, testFactory, testImpl
	cfg.Finality, cfg.MinAmount = operation_setting.UsdtFinalityFinalized, 1
	raw, err := common.Marshal(map[string]operation_setting.UsdtChainSetting{chainID: cfg})
	require.NoError(t, err)
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("usdt_setting"), map[string]string{
		"enabled": "true", "chains": string(raw), "rate_mode": "fixed", "fixed_rate": strconv.FormatFloat(rate, 'f', -1, 64),
		"salt_secret": "0x0101010101010101010101010101010101010101010101010101010101010101",
	}))
	chainIDCache.Clear()
	healthStore.Clear()
	conflictHolds.Clear()
	strainedAt.Clear()
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

func TestNodeListCountsOperatorsNotURLs(t *testing.T) {
	node := func(provider, url string) operation_setting.UsdtRPCNode {
		return operation_setting.UsdtRPCNode{ID: newNodeID(), Provider: provider, URL: url, Enabled: true}
	}
	// Ten URLs of one operator (keys, regions, product hosts) are one provider,
	// whatever the administrator calls them.
	var alchemy []operation_setting.UsdtRPCNode
	for i := range 10 {
		alchemy = append(alchemy, node(fmt.Sprintf("my provider %d", i), fmt.Sprintf("https://bnb-mainnet%d.g.alchemy.com/v2/key%d", i, i)))
	}
	alchemy = append(alchemy, node("other", "https://dashboard.alchemyapi.io/rpc/k"))
	require.Len(t, groupProviders(alchemy), 1)

	// An unknown host counts only once its operator is named.
	selfHosted := node("", "https://rpc.my-node.example/bsc")
	assert.Len(t, groupProviders(append(slices.Clone(alchemy), selfHosted)), 1)
	selfHosted.Provider = "Our own node"
	assert.Len(t, groupProviders(append(slices.Clone(alchemy), selfHosted)), 2)
	// Naming a known provider joins it instead of adding a vote.
	selfHosted.Provider = "Alchemy"
	assert.Len(t, groupProviders(append(slices.Clone(alchemy), selfHosted)), 1)
	// Disabled endpoints do not count.
	disabled := node("Our own node", "https://rpc.my-node.example/bsc")
	disabled.Enabled = false
	assert.Len(t, groupProviders(append(slices.Clone(alchemy), disabled)), 1)

	// A saved list never shows a URL, path or key.
	assert.Equal(t, "https://bnb-mainnet0.g.alchemy.com/…", maskedRPCURL(alchemy[0].URL))
	assert.Equal(t, "https://rpc.ankr.com", maskedRPCURL("https://rpc.ankr.com"))
}

func TestLegacyTwoSlotSettingsMigrateToANodeList(t *testing.T) {
	legacy := operation_setting.UsdtChainSetting{
		RPCURL:          "https://bsc-mainnet.nodereal.io/v1/KEY1, https://lb.drpc.org/ogrpc?network=bsc&dkey=KEY2",
		SecondaryRPCURL: "https://rpc.my-node.example/bsc,https://bsc-mainnet.nodereal.io/v1/KEY1",
	}
	nodes := chainNodes(legacy)
	require.Len(t, nodes, 3, "a URL listed in both slots is kept once")
	assert.Equal(t, "https://bsc-mainnet.nodereal.io/v1/KEY1", nodes[0].URL, "slot order is the priority")
	assert.Equal(t, "Secondary", nodes[2].Name)
	assert.True(t, nodes[2].ProviderInferred, "an unknown host from the old slots is flagged for review")
	assert.Equal(t, "my-node.example", nodes[2].Provider)
	assert.Len(t, groupProviders(nodes), 3)

	// Every save rewrites the legacy fields, so a rolled-back build still
	// finds two different providers.
	primary, secondary := legacySlots(nodes)
	assert.Equal(t, "https://bsc-mainnet.nodereal.io/v1/KEY1", primary)
	assert.Equal(t, "https://lb.drpc.org/ogrpc?network=bsc&dkey=KEY2,https://rpc.my-node.example/bsc", secondary)
}

func TestSavingNodeListsKeepsKeysAndRefusesUnsafeLists(t *testing.T) {
	configureChainSetting(t, operation_setting.UsdtChainArbitrum, 1, operation_setting.UsdtChainSetting{
		RPCURL: "https://arb-mainnet.g.alchemy.com/v2/SECRET1", SecondaryRPCURL: "https://rpc.ankr.com/arbitrum/SECRET2",
	})
	save := func(nodes ...NodePatch) error {
		return UpdateSettings(SettingsPatch{Chains: map[string]ChainPatch{"arbitrum": {RPCNodes: &nodes}}})
	}
	view, err := SettingsForAdmin()
	require.NoError(t, err)
	var arbitrum ChainView
	for _, c := range view.Chains {
		if c.ID == "arbitrum" {
			arbitrum = c
		}
	}
	require.Len(t, arbitrum.Nodes, 2, "the legacy slots appear as a list before the first save")
	raw, err := common.Marshal(view)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "SECRET", "the settings view carries no endpoint key")

	// Reorder and rename without resending the keys: the saved URLs stay.
	first, second := arbitrum.Nodes[0], arbitrum.Nodes[1]
	require.NoError(t, save(
		NodePatch{ID: second.ID, Name: "Ankr", Enabled: true},
		NodePatch{ID: first.ID, Name: "Alchemy", Enabled: true},
		NodePatch{Name: "PublicNode", URL: "https://arbitrum-one-rpc.publicnode.com", Enabled: true},
	))
	saved := operation_setting.GetUsdtSetting().Chains["arbitrum"]
	require.Len(t, saved.RPCNodes, 3)
	assert.Equal(t, "https://rpc.ankr.com/arbitrum/SECRET2", saved.RPCNodes[0].URL)
	assert.Equal(t, "https://arb-mainnet.g.alchemy.com/v2/SECRET1", saved.RPCNodes[1].URL)
	assert.Less(t, saved.RPCNodes[0].Priority, saved.RPCNodes[1].Priority)
	assert.Equal(t, "https://rpc.ankr.com/arbitrum/SECRET2", saved.RPCURL)

	// A verified network may not drop below two providers; the saved list stays.
	assert.Error(t, save(NodePatch{ID: second.ID, Enabled: true}, NodePatch{ID: first.ID, Enabled: false}))
	assert.Error(t, save(NodePatch{ID: second.ID, Enabled: true},
		NodePatch{Provider: "Ankr", URL: "https://rpc.ankr.com/arbitrum/OTHERKEY", Enabled: true}), "a second Ankr key is not a second provider")
	assert.Len(t, operation_setting.GetUsdtSetting().Chains["arbitrum"].RPCNodes, 3)

	// Duplicates and one domain under two operator names are refused.
	assert.Error(t, save(NodePatch{ID: second.ID, Enabled: true}, NodePatch{ID: first.ID, Enabled: true},
		NodePatch{URL: "HTTPS://RPC.ANKR.COM/arbitrum/SECRET2", Enabled: true}))
	assert.Error(t, save(NodePatch{ID: second.ID, Enabled: true}, NodePatch{ID: first.ID, Enabled: true},
		NodePatch{Provider: "Node A", URL: "https://a.my-node.example", Enabled: true},
		NodePatch{Provider: "Node B", URL: "https://b.my-node.example", Enabled: true}))

	// There is no fixed cap: 101 endpoints save and come back in order.
	patch := []NodePatch{{ID: second.ID, Enabled: true}, {ID: first.ID, Enabled: true}}
	for i := range 99 {
		patch = append(patch, NodePatch{Provider: "Own nodes", URL: fmt.Sprintf("https://n%d.my-node.example/rpc", i), Enabled: true})
	}
	require.NoError(t, save(patch...))
	saved = operation_setting.GetUsdtSetting().Chains["arbitrum"]
	require.Len(t, saved.RPCNodes, 101)
	assert.Equal(t, "https://n98.my-node.example/rpc", saved.RPCNodes[100].URL)
	assert.Len(t, groupProviders(saved.RPCNodes), 3)
}

// newFakeProviders starts n honest nodes of one Polygon chain, each its own
// provider with an API key in its URL path, with a 3 USDT deposit to wallet
// in tx 500 (block 100).
func newFakeProviders(t *testing.T, n int, wallet [20]byte) ([]*fakeChain, []string) {
	t.Helper()
	polygon, _ := LookupChain(operation_setting.UsdtChainPolygon)
	from := mustHex20("0x00000000000000000000000000000000000000aa")
	var fakes []*fakeChain
	var urls []string
	for i := range n {
		f := &fakeChain{chainID: 137, head: 120, finalized: 110, hashes: map[uint64]string{100: "0xblock100", 110: "0xblock110"},
			txs: map[uint64][]string{110: {txHash(1)}}, receipts: map[string]map[string]any{
				txHash(500): receipt("0x1", 100, "0xblock100", transferLog(polygon.USDT, from, wallet, big.NewInt(3_000_000), 0)),
				txHash(1):   receipt("0x1", 110, "0xblock110"),
			}}
		fakes = append(fakes, f)
		urls = append(urls, fmt.Sprintf("%s/v2/SECRETKEY%d", newFakeNode(t, f), i))
	}
	return fakes, urls
}

func TestAFailingProviderIsReplacedByTheNextHealthyOne(t *testing.T) {
	require.NoError(t, model.DB.Create(&model.User{Id: 901, Username: "usdt-failover", AffCode: "aff901", Status: common.UserStatusEnabled}).Error)
	configureChainNodes(t, "polygon", 1)
	addr, err := EnsureUserAddress("polygon", 901)
	require.NoError(t, err)
	fakes, urls := newFakeProviders(t, 5, mustHex20(addr))
	configureChainNodes(t, "polygon", 1, urls...)

	// The first provider answers eth_chainId but refuses receipts (yesterday's
	// dRPC case); the second is rate limited. The next two settle it.
	fakes[0].set(func(f *fakeChain) { f.deny = map[string]int{"eth_getTransactionReceipt": http.StatusForbidden} })
	fakes[1].set(func(f *fakeChain) { f.deny = map[string]int{"eth_chainId": http.StatusTooManyRequests} })
	outcomes, err := ProcessTransaction(context.Background(), "polygon", txHash(500), 0, 0, model.UsdtDepositSourceScan, "", false)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.Equal(t, model.UsdtDepositCredited, outcomes[0].Status)
	assert.Equal(t, 3*int(common.QuotaPerUnit), userQuota(t, 901))
	assert.Zero(t, fakes[4].callCount("eth_getTransactionReceipt"), "two agreeing providers end the round")

	// The refusing and rate-limited endpoints rest; a retry does not ask them again.
	before := fakes[0].callCount("eth_getTransactionReceipt")
	outcomes, err = ProcessTransaction(context.Background(), "polygon", txHash(500), 0, 0, model.UsdtDepositSourceClaim, "", false)
	require.NoError(t, err)
	assert.Equal(t, "already", outcomes[0].Status)
	assert.Equal(t, before, fakes[0].callCount("eth_getTransactionReceipt"))
	assert.Equal(t, 3*int(common.QuotaPerUnit), userQuota(t, 901), "a retry never credits twice")

	// The administrator sees which method failed where, without the URL.
	view, err := SettingsForAdmin()
	require.NoError(t, err)
	for _, c := range view.Chains {
		if c.ID != "polygon" {
			continue
		}
		assert.Equal(t, 5, c.Providers)
		assert.Equal(t, 3, c.AvailableProviders)
		assert.Equal(t, "cooling", c.Nodes[0].Health.State)
		assert.Equal(t, "denied", c.Nodes[0].Health.LastKind)
		methods := map[string]bool{}
		for _, m := range c.Nodes[0].Health.Methods {
			methods[m.Method] = m.OK
		}
		assert.Equal(t, map[string]bool{"eth_chainId": true, "eth_getTransactionReceipt": false}, methods)
		assert.Equal(t, "rate_limited", c.Nodes[1].Health.LastKind)
	}
}

func TestProvidersAtTheEndOfALongListAreReachedInLaterRounds(t *testing.T) {
	fakes, urls := newFakeProviders(t, 10, mustHex20("0x00000000000000000000000000000000000000bb"))
	configureChainNodes(t, "polygon", 1, urls...)
	for _, f := range fakes[:8] {
		f.set(func(f *fakeChain) { f.deny = map[string]int{"eth_chainId": http.StatusServiceUnavailable} })
	}
	// One round asks a bounded number of providers, however long the list.
	_, err := FinalTransfers(context.Background(), "polygon", txHash(500))
	var verifyErr *VerifyError
	require.ErrorAs(t, err, &verifyErr)
	assert.Equal(t, ReasonProvidersUnavailable, verifyErr.Reason)
	assert.Len(t, verifyErr.Attempts, verifyMaxProviders)
	assert.NotContains(t, err.Error(), "SECRETKEY", "errors never carry an endpoint key")
	cfg := operation_setting.GetUsdtSetting().Chains["polygon"]
	assert.Equal(t, NetworkStatusCongested, NetworkStatus("polygon", cfg), "users see the network as congested")

	// The next round skips the resting providers and reaches the 9th and 10th.
	transfers, err := FinalTransfers(context.Background(), "polygon", txHash(500))
	require.NoError(t, err)
	require.Len(t, transfers, 1)
	assert.Equal(t, NetworkStatusNormal, NetworkStatus("polygon", cfg), "a verified deposit clears the congestion mark")
	assert.Equal(t, 1, fakes[8].callCount("eth_getTransactionReceipt"))
	assert.Equal(t, 1, fakes[9].callCount("eth_getTransactionReceipt"))
	for _, f := range fakes[:6] {
		assert.Equal(t, 1, f.callCount("eth_chainId"), "a resting provider is not asked again")
	}

	// With one live provider nothing is credited; once another recovers, it is.
	configureChainNodes(t, "polygon", 1, urls[7], urls[8])
	_, err = FinalTransfers(context.Background(), "polygon", txHash(500))
	require.ErrorAs(t, err, &verifyErr)
	assert.Equal(t, ReasonProvidersUnavailable, verifyErr.Reason)
	fakes[7].set(func(f *fakeChain) { f.deny = nil })
	healthStore.Clear()
	_, err = FinalTransfers(context.Background(), "polygon", txHash(500))
	require.NoError(t, err)
}

func TestProvidersMayDifferInHeightButNotInTheBlock(t *testing.T) {
	fakes, urls := newFakeProviders(t, 3, mustHex20("0x00000000000000000000000000000000000000bb"))
	configureChainNodes(t, "polygon", 1, urls...)
	fakes[1].set(func(f *fakeChain) { f.finalized, f.hashes[140] = 140, "0xblock140" })
	_, err := FinalTransfers(context.Background(), "polygon", txHash(500))
	require.NoError(t, err, "both finalized heights cover block 100")

	// One provider not final yet: the round keeps asking until two agree.
	fakes[0].set(func(f *fakeChain) { f.finalized = 90 })
	_, err = FinalTransfers(context.Background(), "polygon", txHash(500))
	require.NoError(t, err)
	assert.Equal(t, 1, fakes[2].callCount("eth_getTransactionReceipt"))

	// Two providers with the block mined but not final: wait for the chain.
	fakes[1].set(func(f *fakeChain) { f.finalized = 90 })
	_, err = FinalTransfers(context.Background(), "polygon", txHash(500))
	var verifyErr *VerifyError
	require.ErrorAs(t, err, &verifyErr)
	assert.Equal(t, ReasonAwaitingConfirmation, verifyErr.Reason)
}

func TestAContradictionIsHeldUntilAnAdministratorReleasesIt(t *testing.T) {
	polygon, _ := LookupChain(operation_setting.UsdtChainPolygon)
	wallet := mustHex20("0x00000000000000000000000000000000000000bb")
	fakes, urls := newFakeProviders(t, 3, wallet)
	configureChainNodes(t, "polygon", 1, urls...)
	from := mustHex20("0x00000000000000000000000000000000000000aa")
	honest := receipt("0x1", 100, "0xblock100", transferLog(polygon.USDT, from, wallet, big.NewInt(3_000_000), 0))
	fakes[0].set(func(f *fakeChain) {
		f.receipts[txHash(500)] = receipt("0x1", 100, "0xblock100", transferLog(polygon.USDT, from, wallet, big.NewInt(3_000_000_000), 0))
	})
	_, err := FinalTransfers(context.Background(), "polygon", txHash(500))
	var verifyErr *VerifyError
	require.ErrorAs(t, err, &verifyErr)
	assert.Equal(t, ReasonConflict, verifyErr.Reason)
	assert.Zero(t, fakes[2].callCount("eth_getTransactionReceipt"), "a third provider is not asked to outvote")

	// The liar is fixed, yet the hold stays until it is released on purpose.
	fakes[0].set(func(f *fakeChain) { f.receipts[txHash(500)] = honest })
	_, err = FinalTransfers(context.Background(), "polygon", txHash(500))
	require.ErrorAs(t, err, &verifyErr)
	assert.Equal(t, ReasonConflict, verifyErr.Reason)
	require.True(t, ReleaseConflict("polygon", strings.TrimPrefix(txHash(500), "0x"), "test"))
	_, err = FinalTransfers(context.Background(), "polygon", txHash(500))
	require.NoError(t, err)
}

func TestReplayOfTheBSCDepositWithDRPCRefusingReceipts(t *testing.T) {
	bsc, _ := LookupChain(operation_setting.UsdtChainBSC)
	const (
		tx    = "0x28fa66f587b16ed59e0fb6ec4db4637751b52283a31f2deb304ba4cd72e889f2"
		block = uint64(135818312)
	)
	wallet := mustHex20("0xdc2be7b8f33554f1f04dc9f94c0970420c603c37")
	from := mustHex20("0x00000000000000000000000000000000000000aa")
	amount, _ := new(big.Int).SetString("15000000000000000000", 10) // 15 USDT, 18 decimals
	var fakes []*fakeChain
	var nodes []operation_setting.UsdtRPCNode
	for i, provider := range []string{"dRPC", "NodeReal", "PublicNode"} {
		f := &fakeChain{chainID: 56, head: block + 30, finalized: block + 10, hashes: map[uint64]string{block: "0xbsc135818312", block + 10: "0xbscfinal"},
			receipts: map[string]map[string]any{tx: receipt("0x1", block, "0xbsc135818312", transferLog(bsc.USDT, from, wallet, amount, 7))}}
		fakes = append(fakes, f)
		nodes = append(nodes, operation_setting.UsdtRPCNode{ID: provider, Provider: provider, URL: newFakeNode(t, f), Priority: i, Enabled: true})
	}
	fakes[0].set(func(f *fakeChain) {
		f.deny = map[string]int{"eth_getTransactionReceipt": http.StatusForbidden, "eth_getBlockByNumber": http.StatusForbidden}
	})
	configureChainSetting(t, operation_setting.UsdtChainBSC, 1, operation_setting.UsdtChainSetting{RPCNodes: nodes})
	require.NoError(t, model.DB.Create(&model.User{Id: 902, Username: "usdt-bsc-replay", AffCode: "aff902", Status: common.UserStatusEnabled}).Error)
	_, err := model.CreateUsdtDepositAddress(&model.UsdtDepositAddress{UserId: 902, Chain: "bsc", Address: Hex20(wallet), Salt: Hex32([32]byte{9})})
	require.NoError(t, err)

	outcomes, err := ProcessTransaction(context.Background(), "bsc", tx, 7, 0, model.UsdtDepositSourceScan, "", false)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.Equal(t, model.UsdtDepositCredited, outcomes[0].Status)
	assert.Equal(t, "15", outcomes[0].Amount)
	credited := userQuota(t, 902)
	assert.Equal(t, 15*int(common.QuotaPerUnit), credited)
	var deposit model.UsdtDeposit
	require.NoError(t, model.DB.Where("chain = ? AND tx_hash = ?", "bsc", tx).Take(&deposit).Error)
	assert.Equal(t, int64(block), deposit.BlockNumber)

	// Replaying a settled deposit returns the record and changes no balance.
	outcomes, err = ProcessTransaction(context.Background(), "bsc", tx, -1, 0, model.UsdtDepositSourceAdmin, "", false)
	require.NoError(t, err)
	assert.Equal(t, "already", outcomes[0].Status)
	assert.Equal(t, credited, userQuota(t, 902))

	// The node test, run from this process, names the refused method.
	results, err := TestNodes(context.Background(), "bsc", "dRPC")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].OK)
	require.GreaterOrEqual(t, len(results[0].Checks), 2)
	assert.True(t, results[0].Checks[0].OK, "eth_chainId still works")
	assert.Equal(t, "eth_getBlockByNumber(finalized)", results[0].Checks[1].Method)
	assert.Contains(t, results[0].Checks[1].Detail, "HTTP 403")
	fakes[2].set(func(f *fakeChain) {
		// A final block holding a transaction, for the end-to-end receipt check.
		f.txs = map[uint64][]string{block + 10: {txHash(1)}}
		f.receipts[txHash(1)] = receipt("0x1", block+10, "0xbscfinal")
	})
	results, err = TestNodes(context.Background(), "bsc", "PublicNode")
	require.NoError(t, err)
	assert.True(t, results[0].OK, "%+v", results[0].Checks)
}

func TestRateLimitCooldownHonoursRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, 120*time.Second, parseRetryAfter("120", now))
	assert.Equal(t, 90*time.Second, parseRetryAfter(now.Add(90*time.Second).Format(http.TimeFormat), now))
	assert.Zero(t, parseRetryAfter("soon", now))
	assert.Zero(t, parseRetryAfter("-5", now))
	cooldown := cooldownFor(&rpcFault{Kind: faultRateLimited, RetryAfter: 7 * time.Minute}, 1)
	assert.Equal(t, 7*time.Minute, cooldown, "a valid Retry-After beyond the backoff wins")
	first, third := cooldownFor(&rpcFault{Kind: faultUnavailable}, 1), cooldownFor(&rpcFault{Kind: faultUnavailable}, 3)
	assert.Greater(t, third, first, "repeated failures rest longer")
	assert.LessOrEqual(t, cooldownFor(&rpcFault{Kind: faultUnavailable}, 50), 6*time.Minute, "backoff is capped")
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
