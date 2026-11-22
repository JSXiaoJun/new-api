package controller

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/usdt"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests play a malicious user or an attacker against the HTTP handlers:
// whatever they send, a balance only grows by a real, final, official USDT
// transfer to the owner's own deposit address, and only once.

const (
	usdtTestFactory = "0x1111111111111111111111111111111111111111"
	usdtTestImpl    = "0x2222222222222222222222222222222222222222"
	usdtTestSecret  = "usdt-scanner-secret"
	usdtAttackerID  = 701
	usdtVictimID    = 702
)

type usdtFakeChain struct {
	mu       sync.Mutex
	receipts map[string]map[string]any
}

// ServeHTTP answers as a Polygon node: chain id 137, head 120, finalized 110,
// and the scripted receipts; every block hash is "0xblock<number>".
func (f *usdtFakeChain) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		result = "0x89"
	case "eth_blockNumber":
		result = "0x78"
	case "eth_getBlockByNumber":
		var tag string
		_ = json.Unmarshal(req.Params[0], &tag)
		number := uint64(110)
		if tag != "finalized" {
			number, _ = strconv.ParseUint(strings.TrimPrefix(tag, "0x"), 16, 64)
		}
		result = map[string]any{"number": fmt.Sprintf("0x%x", number), "hash": fmt.Sprintf("0xblock%d", number)}
	case "eth_getTransactionReceipt":
		var hash string
		_ = json.Unmarshal(req.Params[0], &hash)
		if rec, ok := f.receipts[strings.ToLower(hash)]; ok {
			result = rec
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
}

func usdtTxHash(n int) string { return fmt.Sprintf("0x%064x", n) }

func usdtTransferLog(contract string, to string, amount int64, index int) map[string]any {
	return map[string]any{
		"address":  contract,
		"topics":   []string{"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef", fmt.Sprintf("0x%064x", 0xaa), "0x000000000000000000000000" + strings.TrimPrefix(to, "0x")},
		"data":     fmt.Sprintf("0x%064x", big.NewInt(amount)),
		"logIndex": fmt.Sprintf("0x%x", index),
		"removed":  false,
	}
}

func usdtReceipt(status string, block uint64, logs ...map[string]any) map[string]any {
	return map[string]any{"status": status, "blockNumber": fmt.Sprintf("0x%x", block), "blockHash": fmt.Sprintf("0xblock%d", block), "logs": logs}
}

type usdtAttackFixture struct {
	chain    *usdtFakeChain
	honest   string // URL of the second honest node
	attacker string // the attacker's own Polygon deposit address
	victim   string // the victim's Polygon deposit address
}

const (
	txVictimDeposit  = 1 // 10 USDT to the victim
	txFakeToken      = 2 // 1000 "USDT" from a look-alike token to the attacker
	txReverted       = 3 // a failed transaction to the attacker
	txNotFinal       = 4 // 1000 USDT to the attacker above the finalized block
	txAttackerPays   = 5 // 5 USDT the attacker really paid
	txUnknown        = 9 // never mined
	usdtPolygonToken = "0xc2132d05d31c914a87c6611c10748aeb04b58e8f"
)

func setupUsdtAttackTest(t *testing.T) usdtAttackFixture {
	t.Helper()
	db := setupManageUserTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.TopUp{}, &model.Option{}, &model.UsdtDepositAddress{}, &model.UsdtDeposit{}))
	t.Setenv("LOG_SQL_DSN", "")
	confirmPaymentComplianceForTest(t)
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })
	for _, id := range []int{usdtAttackerID, usdtVictimID} {
		require.NoError(t, db.Create(&model.User{Id: id, Username: fmt.Sprintf("usdt-%d", id), AffCode: fmt.Sprintf("usdt%d", id), Status: common.UserStatusEnabled}).Error)
	}

	// Two honest providers serving the same chain.
	fake := &usdtFakeChain{receipts: map[string]map[string]any{}}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	second := httptest.NewServer(fake)
	t.Cleanup(second.Close)
	chains, err := common.Marshal(map[string]operation_setting.UsdtChainSetting{
		operation_setting.UsdtChainPolygon: {
			Enabled: true, Verified: true, FactoryAddress: usdtTestFactory, ImplementationAddress: usdtTestImpl,
			RPCURL: server.URL, SecondaryRPCURL: second.URL, Finality: operation_setting.UsdtFinalityFinalized, MinAmount: 1,
		},
	})
	require.NoError(t, err)
	cfg := config.GlobalConfig.Get("usdt_setting")
	require.NoError(t, config.UpdateConfigFromMap(cfg, map[string]string{
		"enabled": "true", "admin_only": "false", "chains": string(chains), "rate_mode": "fixed", "fixed_rate": "1", "scanner_ips": "",
		"salt_secret":    "0x0303030303030303030303030303030303030303030303030303030303030303",
		"scanner_secret": usdtTestSecret,
	}))
	t.Cleanup(func() {
		defaults, _ := common.Marshal(map[string]operation_setting.UsdtChainSetting{})
		_ = config.UpdateConfigFromMap(cfg, map[string]string{"enabled": "false", "chains": string(defaults), "salt_secret": "", "scanner_secret": ""})
	})

	attacker, err := usdt.EnsureUserAddress("polygon", usdtAttackerID)
	require.NoError(t, err)
	victim, err := usdt.EnsureUserAddress("polygon", usdtVictimID)
	require.NoError(t, err)
	fake.receipts[usdtTxHash(txVictimDeposit)] = usdtReceipt("0x1", 100, usdtTransferLog(usdtPolygonToken, victim, 10_000_000, 0))
	fake.receipts[usdtTxHash(txFakeToken)] = usdtReceipt("0x1", 100, usdtTransferLog("0x00000000000000000000000000000000000000cc", attacker, 1_000_000_000, 0))
	fake.receipts[usdtTxHash(txReverted)] = usdtReceipt("0x0", 100, usdtTransferLog(usdtPolygonToken, attacker, 1_000_000_000, 0))
	fake.receipts[usdtTxHash(txNotFinal)] = usdtReceipt("0x1", 115, usdtTransferLog(usdtPolygonToken, attacker, 1_000_000_000, 0))
	fake.receipts[usdtTxHash(txAttackerPays)] = usdtReceipt("0x1", 100, usdtTransferLog(usdtPolygonToken, attacker, 5_000_000, 0))
	return usdtAttackFixture{chain: fake, honest: second.URL, attacker: attacker, victim: victim}
}

// usdtQuota reads the wallet the way the product does, so it also sees
// credits held in the Redis wallet journal.
func usdtQuota(t *testing.T, userID int) int {
	t.Helper()
	quota, err := model.GetUserQuota(userID, true)
	require.NoError(t, err)
	return quota
}

// scannerCall drives a scanner endpoint with explicit auth headers.
func scannerCall(handler gin.HandlerFunc, method, path, body, timestamp, signature string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	if timestamp != "" {
		c.Request.Header.Set("X-Usdt-Timestamp", timestamp)
	}
	if signature != "" {
		c.Request.Header.Set("X-Usdt-Signature", signature)
	}
	handler(c)
	return recorder
}

func signedDeposit(body string) *httptest.ResponseRecorder {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	return scannerCall(UsdtScannerDeposit, http.MethodPost, "/api/usdt/scanner/deposit", body, ts,
		usdt.ScannerSignature(usdtTestSecret, ts, http.MethodPost, "/api/usdt/scanner/deposit", []byte(body)))
}

func depositBody(chain string, tx int, logIndex int) string {
	return fmt.Sprintf(`{"chain":%q,"tx_hash":%q,"log_index":%d}`, chain, usdtTxHash(tx), logIndex)
}

// userCall drives a logged-in regular user's endpoint.
func userCall(handler gin.HandlerFunc, userID int, path, body string) (bool, string) {
	return userCallAs(handler, userID, common.RoleCommonUser, path, body)
}

func userCallAs(handler gin.HandlerFunc, userID, role int, path, body string) (bool, string) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", userID)
	c.Set("role", role)
	handler(c)
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(recorder.Body.Bytes(), &resp)
	return resp.Success, resp.Message
}

func TestUsdtScannerCallbackRejectsForgedRequests(t *testing.T) {
	setupUsdtAttackTest(t)
	body := depositBody("polygon", txVictimDeposit, 0)
	now := strconv.FormatInt(time.Now().Unix(), 10)
	old := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	path := "/api/usdt/scanner/deposit"
	sign := func(secret, ts, p, b string) string {
		return usdt.ScannerSignature(secret, ts, http.MethodPost, p, []byte(b))
	}

	cases := []struct {
		name, timestamp, signature, body string
	}{
		{"no signature", "", "", body},
		{"guessed secret", now, sign("guess", now, path, body), body},
		{"signature from another request body", now, sign(usdtTestSecret, now, path, depositBody("polygon", txAttackerPays, 0)), body},
		{"signature for another endpoint", now, sign(usdtTestSecret, now, "/api/usdt/scanner/heartbeat", body), body},
		{"replayed after five minutes", old, sign(usdtTestSecret, old, path, body), body},
		{"garbage timestamp", "tomorrow", sign(usdtTestSecret, "tomorrow", path, body), body},
	}
	for _, tc := range cases {
		recorder := scannerCall(UsdtScannerDeposit, http.MethodPost, path, tc.body, tc.timestamp, tc.signature)
		assert.Equal(t, http.StatusUnauthorized, recorder.Code, tc.name)
	}
	assert.Zero(t, usdtQuota(t, usdtVictimID), "nothing was credited by a forged request")

	// With no scanner secret configured, a signature made with an empty key
	// (computable by anyone) must not be accepted either.
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("usdt_setting"), map[string]string{"scanner_secret": ""}))
	recorder := scannerCall(UsdtScannerDeposit, http.MethodPost, path, body, now, sign("", now, path, body))
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	state := scannerCall(UsdtScannerState, http.MethodGet, "/api/usdt/scanner/state", "", now,
		usdt.ScannerSignature("", now, http.MethodGet, "/api/usdt/scanner/state", nil))
	assert.Equal(t, http.StatusUnauthorized, state.Code, "the address list is not readable without the secret")
}

func TestUsdtLeakedScannerSecretCannotMintBalance(t *testing.T) {
	setupUsdtAttackTest(t)

	cases := []struct {
		name     string
		body     string
		wantCode int
	}{
		{"look-alike token named USDT", depositBody("polygon", txFakeToken, 0), http.StatusOK},
		{"reverted transaction", depositBody("polygon", txReverted, 0), http.StatusOK},
		{"transfer not final yet", depositBody("polygon", txNotFinal, 0), http.StatusConflict},
		{"transaction that does not exist", depositBody("polygon", txUnknown, 0), http.StatusConflict},
		{"chain that is not configured", depositBody("bsc", txAttackerPays, 0), http.StatusOK},
		{"made-up chain", depositBody("ethereum", txAttackerPays, 0), http.StatusOK},
		{"negative log index", depositBody("polygon", txAttackerPays, -1), http.StatusBadRequest},
		{"malformed hash", `{"chain":"polygon","tx_hash":"' OR 1=1 --","log_index":0}`, http.StatusOK},
	}
	for _, tc := range cases {
		recorder := signedDeposit(tc.body)
		assert.Equal(t, tc.wantCode, recorder.Code, "%s: %s", tc.name, recorder.Body.String())
	}
	assert.Zero(t, usdtQuota(t, usdtAttackerID), "no fake, failed, pending or foreign transfer credits the attacker")

	// A real deposit reported again and again is credited once, to its owner.
	for range 5 {
		recorder := signedDeposit(depositBody("polygon", txVictimDeposit, 0))
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	}
	assert.Equal(t, 10*500000, usdtQuota(t, usdtVictimID))
	assert.Zero(t, usdtQuota(t, usdtAttackerID))
	var count int64
	require.NoError(t, model.DB.Model(&model.TopUp{}).Where("user_id = ?", usdtVictimID).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestUsdtUserClaimCannotStealForgeOrDoubleCredit(t *testing.T) {
	fx := setupUsdtAttackTest(t)
	claim := func(userID int, chain, hash string) (bool, string) {
		return userCall(ClaimUsdtDeposit, userID, "/api/user/usdt/claim", fmt.Sprintf(`{"chain":%q,"tx_hash":%q}`, chain, hash))
	}

	ok, msg := claim(usdtAttackerID, "polygon", usdtTxHash(txVictimDeposit))
	assert.False(t, ok)
	assert.Equal(t, "No USDT transfer to your deposit address was found in this transaction", msg, "claiming the victim's deposit")
	ok, _ = claim(usdtAttackerID, "polygon", usdtTxHash(txFakeToken))
	assert.False(t, ok, "look-alike token")
	ok, msg = claim(usdtAttackerID, "polygon", usdtTxHash(txReverted))
	assert.False(t, ok)
	assert.Equal(t, "This transaction cannot be credited", msg)
	ok, msg = claim(usdtAttackerID, "polygon", usdtTxHash(txNotFinal))
	assert.False(t, ok)
	assert.Equal(t, "The transaction is not confirmed yet. Try again in a few minutes.", msg)
	ok, msg = claim(usdtAttackerID, "polygon", "' OR 1=1 --")
	assert.False(t, ok)
	assert.Equal(t, "Invalid transaction hash", msg)
	ok, msg = claim(usdtAttackerID, "bsc", usdtTxHash(txAttackerPays))
	assert.False(t, ok)
	assert.Equal(t, "This USDT network is not available", msg)
	assert.Zero(t, usdtQuota(t, usdtAttackerID))
	assert.Zero(t, usdtQuota(t, usdtVictimID), "the attacker's claim did not touch the victim either")

	// The attacker races the same real payment in many requests and hash
	// spellings (0x / bare / upper case): it is credited exactly once.
	real := usdtTxHash(txAttackerPays)
	spellings := []string{real, strings.TrimPrefix(real, "0x"), "0x" + strings.ToUpper(strings.TrimPrefix(real, "0x"))}
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Go(func() { claim(usdtAttackerID, "polygon", spellings[i%len(spellings)]) })
	}
	wg.Wait()
	assert.Equal(t, 5*500000, usdtQuota(t, usdtAttackerID))
	var deposits int64
	require.NoError(t, model.DB.Model(&model.UsdtDeposit{}).Where("user_id = ?", usdtAttackerID).Count(&deposits).Error)
	assert.Equal(t, int64(1), deposits)

	// The deposit address is always derived by the server.
	ok, msg = userCall(CreateUsdtAddress, usdtAttackerID, "/api/user/usdt/address", `{"chain":"bsc"}`)
	assert.False(t, ok)
	assert.Equal(t, "This USDT network is not available", msg, "unverified network")
	ok, _ = userCall(CreateUsdtAddress, usdtAttackerID, "/api/user/usdt/address", `{"chain":"polygon","address":"0xdeadbeef00000000000000000000000000000000"}`)
	assert.True(t, ok)
	row, err := model.GetUsdtDepositAddress(usdtAttackerID, "polygon")
	require.NoError(t, err)
	assert.Equal(t, fx.attacker, row.Address, "a client-supplied address is ignored")
}

func TestUsdtConcurrentClaimsCreditOnceInRedisWalletMode(t *testing.T) {
	setupUsdtAttackTest(t)
	server := miniredis.RunT(t)
	oldRDB := common.RDB
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	common.RedisEnabled = true
	t.Cleanup(func() { _ = common.RDB.Close(); common.RDB = oldRDB })

	body := fmt.Sprintf(`{"chain":"polygon","tx_hash":%q}`, usdtTxHash(txAttackerPays))
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() { userCall(ClaimUsdtDeposit, usdtAttackerID, "/api/user/usdt/claim", body) })
	}
	wg.Wait()
	assert.Equal(t, 5*500000, usdtQuota(t, usdtAttackerID))
	var topups int64
	require.NoError(t, model.DB.Model(&model.TopUp{}).Where("user_id = ?", usdtAttackerID).Count(&topups).Error)
	assert.Equal(t, int64(1), topups)
}

func TestUsdtCompromisedVerificationNodeCannotMintBalance(t *testing.T) {
	fx := setupUsdtAttackTest(t)
	// The first provider is compromised and fabricates a 1,000,000 USDT
	// deposit to the attacker; the second provider has never seen it.
	liar := &usdtFakeChain{receipts: map[string]map[string]any{
		usdtTxHash(txUnknown): usdtReceipt("0x1", 100, usdtTransferLog(usdtPolygonToken, fx.attacker, 1_000_000_000_000, 0)),
		// and inflates the attacker's real 5 USDT payment
		usdtTxHash(txAttackerPays): usdtReceipt("0x1", 100, usdtTransferLog(usdtPolygonToken, fx.attacker, 5_000_000_000, 0)),
	}}
	liarServer := httptest.NewServer(liar)
	t.Cleanup(liarServer.Close)
	chains, err := common.Marshal(map[string]operation_setting.UsdtChainSetting{
		operation_setting.UsdtChainPolygon: {
			Enabled: true, Verified: true, FactoryAddress: usdtTestFactory, ImplementationAddress: usdtTestImpl,
			RPCURL: liarServer.URL, SecondaryRPCURL: fx.honest, Finality: operation_setting.UsdtFinalityFinalized, MinAmount: 1,
		},
	})
	require.NoError(t, err)
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("usdt_setting"), map[string]string{"chains": string(chains)}))

	for _, tx := range []int{txUnknown, txAttackerPays} {
		ok, _ := userCall(ClaimUsdtDeposit, usdtAttackerID, "/api/user/usdt/claim", fmt.Sprintf(`{"chain":"polygon","tx_hash":%q}`, usdtTxHash(tx)))
		assert.False(t, ok, "claim of tx %d", tx)
		recorder := signedDeposit(depositBody("polygon", tx, 0))
		assert.Equal(t, http.StatusConflict, recorder.Code, "scanner report of tx %d is held, not settled", tx)
	}
	assert.Zero(t, usdtQuota(t, usdtAttackerID), "one lying provider cannot create or inflate a deposit")
}

func TestUsdtAdminOnlyModeRefusesRegularUsersOnTheServer(t *testing.T) {
	setupUsdtAttackTest(t)
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("usdt_setting"), map[string]string{"admin_only": "true"}))
	claimBody := fmt.Sprintf(`{"chain":"polygon","tx_hash":%q}`, usdtTxHash(txAttackerPays))

	infoEnabled := func(role int) bool {
		gin.SetMode(gin.TestMode)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/user/usdt/info", nil)
		c.Set("id", usdtAttackerID)
		c.Set("role", role)
		GetUsdtInfo(c)
		var resp struct {
			Data struct {
				Enabled bool `json:"enabled"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
		return resp.Data.Enabled
	}

	// A regular user calling the API directly, bypassing the hidden card.
	assert.False(t, infoEnabled(common.RoleCommonUser))
	ok, msg := userCallAs(CreateUsdtAddress, usdtAttackerID, common.RoleCommonUser, "/api/user/usdt/address", `{"chain":"polygon"}`)
	assert.False(t, ok)
	assert.Equal(t, "USDT top-up is not available", msg)
	ok, msg = userCallAs(ClaimUsdtDeposit, usdtAttackerID, common.RoleCommonUser, "/api/user/usdt/claim", claimBody)
	assert.False(t, ok)
	assert.Equal(t, "USDT top-up is not available", msg)
	assert.Zero(t, usdtQuota(t, usdtAttackerID))

	// An administrator can test the whole flow.
	assert.True(t, infoEnabled(common.RoleAdminUser))
	ok, _ = userCallAs(CreateUsdtAddress, usdtAttackerID, common.RoleAdminUser, "/api/user/usdt/address", `{"chain":"polygon"}`)
	assert.True(t, ok)
	ok, msg = userCallAs(ClaimUsdtDeposit, usdtAttackerID, common.RoleAdminUser, "/api/user/usdt/claim", claimBody)
	assert.True(t, ok, msg)
	assert.Equal(t, 5*500000, usdtQuota(t, usdtAttackerID))
}

func TestUsdtDepositHistoryListsOnlyTheCallersLatestTen(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.UsdtDeposit{}))
	for i := range 12 {
		require.NoError(t, db.Create(&model.UsdtDeposit{Chain: "polygon", TxHash: usdtTxHash(i), UserId: usdtAttackerID, Status: model.UsdtDepositCredited}).Error)
	}
	require.NoError(t, db.Create(&model.UsdtDeposit{Chain: "polygon", TxHash: usdtTxHash(99), UserId: usdtVictimID, Status: model.UsdtDepositCredited}).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/usdt/deposits?p=1&page_size=100", nil)
	c.Set("id", usdtAttackerID)
	GetUsdtDeposits(c)

	var resp struct {
		Success bool                `json:"success"`
		Data    []model.UsdtDeposit `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	require.Len(t, resp.Data, 10, "paging parameters cannot widen the list")
	assert.Equal(t, usdtTxHash(11), resp.Data[0].TxHash)
	assert.Equal(t, usdtTxHash(2), resp.Data[9].TxHash)
	for _, deposit := range resp.Data {
		assert.Equal(t, usdtAttackerID, deposit.UserId)
	}
}
