package controller

import (
	"errors"
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

func isUsdtTopUpEnabled() bool {
	return isPaymentComplianceConfirmed() && len(usdt.UserNetworks()) > 0
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
func usdtError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, usdt.ErrNotReady):
		common.ApiErrorT(c, "The transaction is not confirmed yet. Try again in a few minutes.")
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
	networks := usdt.UserNetworks()
	enabled := isPaymentComplianceConfirmed() && len(networks) > 0
	addresses := map[string]string{}
	if enabled {
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
	if !isUsdtTopUpEnabled() {
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

func GetUsdtDeposits(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	rows, total, err := model.ListUserUsdtDeposits(c.GetInt("id"), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(toUsdtDepositViews(rows))
	common.ApiSuccess(c, pageInfo)
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
	if cfg.FactoryAddress == "" || strings.TrimSpace(cfg.RPCURL) == "" || strings.TrimSpace(cfg.SecondaryRPCURL) == "" {
		common.ApiErrorT(c, "Save the factory address and both verification RPC URLs first")
		return
	}
	check, err := usdt.VerifyFactory(c.Request.Context(), chain.ID, cfg.FactoryAddress, cfg.RPCURL, cfg.SecondaryRPCURL)
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
		usdtError(c, err)
		return
	}
	common.ApiSuccess(c, outcomes)
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
		usdtError(c, err)
		return
	}
	common.ApiSuccess(c, outcomes)
}
