package controller

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/usdt"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// usdtVisibleTo reports whether the requesting user may see and use USDT
// deposits. In administrator-only mode the server refuses everyone else, so
// hiding the card in the browser is not the only barrier.
func usdtVisibleTo(c *gin.Context) bool {
	if !isPaymentComplianceConfirmed() || len(usdt.UserNetworks()) == 0 {
		return false
	}
	return !operation_setting.GetUsdtSetting().AdminOnly || c.GetInt("role") >= common.RoleAdminUser
}

type usdtDepositView struct {
	model.UsdtDeposit
	AddressDisplay string `json:"address_display"`
	TxURL          string `json:"tx_url"`
}

func toUsdtDepositViews(rows []model.UsdtDeposit) []usdtDepositView {
	out := make([]usdtDepositView, 0, len(rows))
	for _, row := range rows {
		view := usdtDepositView{UsdtDeposit: row, AddressDisplay: usdt.FormatAddress(row.Chain, row.Address)}
		if chain, ok := usdt.LookupChain(row.Chain); ok {
			view.TxURL = chain.TxURL + strings.TrimPrefix(row.TxHash, "0x")
			if chain.Kind == usdt.KindEVM {
				view.TxURL = chain.TxURL + row.TxHash
			}
		}
		out = append(out, view)
	}
	return out
}

// usdtError maps verification errors to messages the web console translates.
// Users learn what happens next; provider names, endpoints and RPC errors
// stay in the server log and the administrator's view.
func usdtError(c *gin.Context, err error) {
	var verifyErr *usdt.VerifyError
	if errors.As(err, &verifyErr) {
		logger.LogWarn(c.Request.Context(), common.LogText("USDT verification incomplete error=%q", err.Error()))
	}
	switch {
	case errors.As(err, &verifyErr) && verifyErr.Reason == usdt.ReasonFailedOnChain:
		common.ApiErrorT(c, "This transaction failed on chain and cannot be credited")
	case errors.As(err, &verifyErr) && verifyErr.Reason != usdt.ReasonAwaitingConfirmation:
		common.ApiErrorT(c, "Your deposit is being verified and will be retried automatically. Do not send it again.")
	case errors.Is(err, usdt.ErrNotReady):
		common.ApiErrorT(c, "Waiting for on-chain confirmation. Try again in a few minutes.")
	case errors.Is(err, usdt.ErrRejected):
		common.ApiErrorT(c, "This transaction cannot be credited")
	case errors.Is(err, usdt.ErrChainUnavailable):
		common.ApiErrorT(c, "This USDT network is not available")
	default:
		var message *common.Message
		if errors.As(err, &message) {
			common.ApiError(c, err)
			return
		}
		logger.LogError(c.Request.Context(), common.LogText("USDT request failed error=%q", err.Error()))
		common.ApiErrorT(c, "USDT request failed. Try again later.")
	}
}

// GetUsdtInfo returns the networks, the user's existing addresses and the rate.
func GetUsdtInfo(c *gin.Context) {
	userID := c.GetInt("id")
	enabled := usdtVisibleTo(c)
	var networks []usdt.NetworkInfo
	addresses := map[string]string{}
	if enabled {
		networks = usdt.UserNetworks()
		rows, err := model.ListUserUsdtDepositAddresses(userID)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		for _, row := range rows {
			addresses[row.Chain] = usdt.FormatAddress(row.Chain, row.Address)
		}
	}
	data := gin.H{
		"enabled":   enabled,
		"networks":  networks,
		"addresses": addresses,
		"rate_mode": operation_setting.GetUsdtSetting().RateMode,
	}
	if enabled {
		if rate, source, err := usdt.CurrentRate(c.Request.Context()); err == nil {
			data["rate"] = rate.String()
			data["rate_source"] = source
		}
	}
	common.ApiSuccess(c, data)
}

type usdtChainRequest struct {
	Chain  string `json:"chain"`
	TxHash string `json:"tx_hash"`
}

// CreateUsdtAddress returns (and on first use, records) the user's permanent
// deposit address on one network.
func CreateUsdtAddress(c *gin.Context) {
	var req usdtChainRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if !usdtVisibleTo(c) {
		common.ApiErrorT(c, "USDT top-up is not available")
		return
	}
	address, err := usdt.EnsureUserAddress(req.Chain, c.GetInt("id"))
	if err != nil {
		usdtError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"chain": req.Chain, "address": usdt.FormatAddress(req.Chain, address)})
}

// usdtRecentDepositLimit is how many of a user's deposits the wallet lists.
const usdtRecentDepositLimit = 10

// GetUsdtDeposits returns the caller's latest USDT deposits, newest first.
func GetUsdtDeposits(c *gin.Context) {
	rows, err := model.ListRecentUserUsdtDeposits(c.GetInt("id"), usdtRecentDepositLimit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, toUsdtDepositViews(rows))
}

// ClaimUsdtDeposit lets a user submit a transaction hash instead of waiting
// for the scanner. The transaction is verified on chain exactly like a scanner
// report, and only transfers to the caller's own addresses are credited.
func ClaimUsdtDeposit(c *gin.Context) {
	var req usdtChainRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if !usdtVisibleTo(c) {
		common.ApiErrorT(c, "USDT top-up is not available")
		return
	}
	if _, err := usdt.NormalizeTxHash(req.TxHash); err != nil {
		common.ApiErrorT(c, "Invalid transaction hash")
		return
	}
	userID := c.GetInt("id")
	outcomes, err := usdt.ProcessTransaction(c.Request.Context(), req.Chain, req.TxHash, -1, userID, model.UsdtDepositSourceClaim, c.ClientIP(), false)
	if err != nil {
		usdtError(c, err)
		return
	}
	var mine []usdt.Outcome
	for _, o := range outcomes {
		if o.Status != "not_ours" {
			mine = append(mine, o)
		}
	}
	if len(mine) == 0 {
		common.ApiErrorT(c, "No USDT transfer to your deposit address was found in this transaction")
		return
	}
	common.ApiSuccess(c, mine)
}

// ---- scanner endpoints (HMAC authenticated) ----

func readScannerRequest(c *gin.Context) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return nil, false
	}
	err = usdt.VerifyScannerRequest(c.ClientIP(), c.GetHeader("X-Usdt-Timestamp"), c.GetHeader("X-Usdt-Signature"),
		c.Request.Method, c.Request.URL.RequestURI(), body)
	if err != nil {
		logger.LogWarn(c.Request.Context(), common.LogText("USDT scanner request rejected client_ip=%s path=%q", c.ClientIP(), c.Request.URL.Path))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return nil, false
	}
	return body, true
}

func UsdtScannerState(c *gin.Context) {
	if _, ok := readScannerRequest(c); !ok {
		return
	}
	since, _ := strconv.Atoi(c.Query("since"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	limit = min(max(limit, 1), 1000)
	state, err := usdt.BuildScannerState(max(since, 0), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "state unavailable"})
		return
	}
	c.JSON(http.StatusOK, state)
}

type usdtScannerDepositRequest struct {
	Chain    string `json:"chain"`
	TxHash   string `json:"tx_hash"`
	LogIndex int    `json:"log_index"`
}

// UsdtScannerDeposit treats the scanner's report as a hint: new-api verifies
// the transfer through its own RPC before crediting anything. 409 asks the
// scanner to retry later; 200 means the report is settled.
func UsdtScannerDeposit(c *gin.Context) {
	body, ok := readScannerRequest(c)
	if !ok {
		return
	}
	var req usdtScannerDepositRequest
	if err := common.Unmarshal(body, &req); err != nil || req.LogIndex < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return
	}
	outcomes, err := usdt.ProcessTransaction(c.Request.Context(), req.Chain, req.TxHash, req.LogIndex, 0, model.UsdtDepositSourceScan, c.ClientIP(), false)
	switch {
	case errors.Is(err, usdt.ErrNotReady):
		c.JSON(http.StatusConflict, gin.H{"status": "retry", "error": err.Error()})
	case errors.Is(err, usdt.ErrRejected), errors.Is(err, usdt.ErrChainUnavailable):
		logger.LogWarn(c.Request.Context(), common.LogText("USDT scanner report rejected chain=%s tx=%s log=%d error=%q", req.Chain, req.TxHash, req.LogIndex, err.Error()))
		c.JSON(http.StatusOK, gin.H{"status": "rejected", "error": err.Error()})
	case err != nil:
		logger.LogError(c.Request.Context(), common.LogText("USDT scanner report failed chain=%s tx=%s log=%d error=%q", req.Chain, req.TxHash, req.LogIndex, err.Error()))
		c.JSON(http.StatusInternalServerError, gin.H{"status": "retry", "error": "processing failed"})
	default:
		c.JSON(http.StatusOK, gin.H{"status": "ok", "outcomes": outcomes})
	}
}

type usdtHeartbeatRequest struct {
	Version string                         `json:"version"`
	Chains  map[string]usdt.ChainHeartbeat `json:"chains"`
}

func UsdtScannerHeartbeat(c *gin.Context) {
	body, ok := readScannerRequest(c)
	if !ok {
		return
	}
	var req usdtHeartbeatRequest
	if err := common.Unmarshal(body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return
	}
	usdt.RecordHeartbeat(req.Version, req.Chains)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ---- administrator endpoints (root) ----

func GetUsdtSettings(c *gin.Context) {
	view, err := usdt.SettingsForAdmin()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

func UpdateUsdtSettings(c *gin.Context) {
	var patch usdt.SettingsPatch
	if err := common.DecodeJson(c.Request.Body, &patch); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if err := usdt.UpdateSettings(patch); err != nil {
		usdtError(c, err)
		return
	}
	GetUsdtSettings(c)
}

func VerifyUsdtFactory(c *gin.Context) {
	var req usdtChainRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	chain, ok := usdt.LookupChain(req.Chain)
	if !ok {
		common.ApiErrorT(c, "This USDT network is not available")
		return
	}
	cfg := operation_setting.GetUsdtSetting().Chains[chain.ID]
	if cfg.FactoryAddress == "" {
		common.ApiErrorT(c, "Save the factory address first")
		return
	}
	check, err := usdt.VerifyFactory(c.Request.Context(), chain.ID, cfg.FactoryAddress)
	if err != nil {
		common.ApiErrorT(c, "Factory verification failed: {{reason}}", map[string]any{"reason": err.Error()})
		return
	}
	if err := usdt.MarkFactoryVerified(chain.ID, cfg.FactoryAddress, check); err != nil {
		usdtError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"implementation": usdt.DisplayAddress(chain, check.Implementation),
		"destination":    usdt.DisplayAddress(chain, check.Destination),
	})
}

func RegenerateUsdtScannerSecret(c *gin.Context) {
	secret, err := usdt.RegenerateScannerSecret()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"secret": secret})
}

func ListUsdtDeposits(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	userID, _ := strconv.Atoi(c.Query("user_id"))
	rows, total, err := model.ListUsdtDeposits(model.UsdtDepositFilter{
		UserId: userID, Chain: c.Query("chain"), Status: c.Query("status"), TxHash: strings.TrimSpace(c.Query("tx_hash")),
	}, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(toUsdtDepositViews(rows))
	common.ApiSuccess(c, pageInfo)
}

// RecheckUsdtTransaction verifies and credits every transfer in a transaction
// that reached one of our deposit wallets, for disputes and scanner outages.
func RecheckUsdtTransaction(c *gin.Context) {
	var req usdtChainRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	outcomes, err := usdt.ProcessTransaction(c.Request.Context(), req.Chain, req.TxHash, -1, 0, model.UsdtDepositSourceAdmin, c.ClientIP(), false)
	if err != nil {
		usdtAdminError(c, err)
		return
	}
	common.ApiSuccess(c, outcomes)
}

// usdtAdminError shows an administrator which providers were asked and why
// the round ended. Endpoints are masked; no URL or key is included.
func usdtAdminError(c *gin.Context, err error) {
	var verifyErr *usdt.VerifyError
	if !errors.As(err, &verifyErr) {
		usdtError(c, err)
		return
	}
	var attempts strings.Builder
	for _, a := range verifyErr.Attempts {
		if attempts.Len() > 0 {
			attempts.WriteString("; ")
		}
		fmt.Fprintf(&attempts, "%s %s: %s", a.Provider, a.Endpoint, a.Outcome)
		if a.Detail != "" {
			attempts.WriteString(" (" + a.Detail + ")")
		}
	}
	common.ApiErrorT(c, "Verification incomplete ({{reason}}): {{attempts}}", map[string]any{
		"reason": verifyErr.Reason, "attempts": attempts.String(),
	})
}

type usdtNodeTestRequest struct {
	Chain  string `json:"chain"`
	NodeID string `json:"node_id"`
}

// PreviewUsdtNodes recognizes the providers of pasted endpoint URLs.
func PreviewUsdtNodes(c *gin.Context) {
	var req struct {
		URLs []string `json:"urls"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || len(req.URLs) > 500 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	common.ApiSuccess(c, usdt.PreviewNodes(req.URLs))
}

// TestUsdtNodes tests saved verification endpoints from this new-api process,
// method by method.
func TestUsdtNodes(c *gin.Context) {
	var req usdtNodeTestRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	results, err := usdt.TestNodes(c.Request.Context(), req.Chain, req.NodeID)
	if err != nil {
		usdtError(c, err)
		return
	}
	common.ApiSuccess(c, results)
}

// ReleaseUsdtConflict lifts the hold on a transaction whose providers
// disagreed, after the administrator dealt with the faulty provider. The next
// verification asks two providers again from scratch.
func ReleaseUsdtConflict(c *gin.Context) {
	var req usdtChainRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if !usdt.ReleaseConflict(req.Chain, req.TxHash, fmt.Sprintf("user %d from %s", c.GetInt("id"), c.ClientIP())) {
		common.ApiErrorT(c, "This transaction is not on hold")
		return
	}
	common.ApiSuccess(c, nil)
}

// CreditUsdtDepositBelowMin credits a recorded deposit that was below the
// network minimum, after verifying it on chain again.
func CreditUsdtDepositBelowMin(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	deposit, err := model.GetUsdtDepositById(id)
	if err != nil {
		common.ApiErrorT(c, "Deposit not found")
		return
	}
	if deposit.Status != model.UsdtDepositBelowMin {
		common.ApiErrorT(c, "Only deposits below the minimum amount can be credited manually")
		return
	}
	outcomes, err := usdt.ProcessTransaction(c.Request.Context(), deposit.Chain, deposit.TxHash, deposit.LogIndex, deposit.UserId, model.UsdtDepositSourceAdmin, c.ClientIP(), true)
	if err != nil {
		usdtAdminError(c, err)
		return
	}
	common.ApiSuccess(c, outcomes)
}
