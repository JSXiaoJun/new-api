package usdt

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
)

// ChainView is one network as the settings page shows it.
type ChainView struct {
	operation_setting.UsdtChainSetting
	ID           string `json:"id"`
	Name         string `json:"name"`
	Contract     string `json:"contract"`
	Factory      string `json:"factory_display"`
	AddressCount int64  `json:"address_count"`
	// FactoryLocked is true once addresses exist: changing the factory would
	// strand every deposit sent to them.
	FactoryLocked bool `json:"factory_locked"`
}

type SettingsView struct {
	Enabled          bool                      `json:"enabled"`
	AdminOnly        bool                      `json:"admin_only"`
	RateMode         string                    `json:"rate_mode"`
	FixedRate        float64                   `json:"fixed_rate"`
	ScannerIPs       string                    `json:"scanner_ips"`
	HasScannerSecret bool                      `json:"has_scanner_secret"`
	Chains           []ChainView               `json:"chains"`
	Heartbeat        map[string]ChainHeartbeat `json:"heartbeat"`
	Alerts           []VerificationAlert       `json:"alerts"`
	HeartbeatAt      int64                     `json:"heartbeat_at"`
	ScannerVersion   string                    `json:"scanner_version"`
}

func SettingsForAdmin() (SettingsView, error) {
	setting := operation_setting.GetUsdtSetting()
	counts, err := model.CountUsdtDepositAddresses()
	if err != nil {
		return SettingsView{}, err
	}
	hb := currentHeartbeat()
	view := SettingsView{
		Enabled: setting.Enabled, AdminOnly: setting.AdminOnly, RateMode: setting.RateMode, FixedRate: setting.FixedRate,
		ScannerIPs: setting.ScannerIPs, HasScannerSecret: strings.TrimSpace(setting.ScannerSecret) != "",
		Heartbeat: hb.chains, HeartbeatAt: hb.at, ScannerVersion: hb.version, Alerts: RecentVerificationAlerts(),
	}
	for _, id := range operation_setting.UsdtChainOrder {
		chain, _ := LookupChain(id)
		cfg := setting.Chains[id]
		v := ChainView{
			UsdtChainSetting: cfg, ID: id, Name: chain.Name, Contract: DisplayAddress(chain, chain.USDT),
			AddressCount: counts[id], FactoryLocked: counts[id] > 0,
		}
		if f, err := ParseHex20(cfg.FactoryAddress); err == nil {
			v.Factory = DisplayAddress(chain, f)
		}
		view.Chains = append(view.Chains, v)
	}
	return view, nil
}

type ChainPatch struct {
	Enabled         *bool    `json:"enabled"`
	FactoryAddress  *string  `json:"factory_address"`
	RPCURL          *string  `json:"rpc_url"`
	RPCURLSecondary *string  `json:"rpc_url_secondary"`
	Finality        *string  `json:"finality"`
	Confirmations   *int     `json:"confirmations"`
	MinAmount       *float64 `json:"min_amount"`
	SweepThreshold  *float64 `json:"sweep_threshold"`
}

type SettingsPatch struct {
	Enabled    *bool                 `json:"enabled"`
	AdminOnly  *bool                 `json:"admin_only"`
	RateMode   *string               `json:"rate_mode"`
	FixedRate  *float64              `json:"fixed_rate"`
	ScannerIPs *string               `json:"scanner_ips"`
	Chains     map[string]ChainPatch `json:"chains"`
}

var settingsMu sync.Mutex

func randomHex32() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(b[:]), nil
}

func validAmountSetting(v float64) bool {
	return !math.IsNaN(v) && v >= 0 && v <= 1_000_000
}

// UpdateSettings validates and persists a partial settings change. The salt
// secret is generated once and never changed; a chain's factory is frozen
// once that chain has issued an address.
func UpdateSettings(patch SettingsPatch) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	setting := operation_setting.GetUsdtSetting()
	counts, err := model.CountUsdtDepositAddresses()
	if err != nil {
		return err
	}

	for id, p := range patch.Chains {
		chain, ok := LookupChain(id)
		if !ok {
			return common.NewMessage("Unknown USDT network {{chain}}", map[string]any{"chain": id})
		}
		cfg := setting.Chains[chain.ID]
		if p.FactoryAddress != nil {
			normalized := ""
			if trimmed := strings.TrimSpace(*p.FactoryAddress); trimmed != "" {
				raw, err := ParseAddress(chain, trimmed)
				if err != nil {
					return common.NewMessage("Invalid factory address for {{chain}}", map[string]any{"chain": chain.ID})
				}
				normalized = Hex20(raw)
			}
			if normalized != cfg.FactoryAddress {
				if counts[chain.ID] > 0 {
					return common.NewMessage("{{chain}} has already issued deposit addresses, so its factory cannot change", map[string]any{"chain": chain.ID})
				}
				cfg.FactoryAddress = normalized
				cfg.ImplementationAddress = ""
				cfg.Verified = false
				cfg.Enabled = false
			}
		}
		if p.RPCURL != nil {
			cfg.RPCURL = strings.TrimSpace(*p.RPCURL)
		}
		if p.RPCURLSecondary != nil {
			cfg.SecondaryRPCURL = strings.TrimSpace(*p.RPCURLSecondary)
		}
		if cfg.RPCURL != "" && cfg.SecondaryRPCURL != "" {
			if err := DistinctProviders(cfg.RPCURL, cfg.SecondaryRPCURL); err != nil {
				return common.NewMessage("The two verification nodes of {{chain}} must use different providers", map[string]any{"chain": chain.ID})
			}
		}
		if p.Finality != nil {
			mode := strings.TrimSpace(*p.Finality)
			valid := []string{operation_setting.UsdtFinalityFinalized, operation_setting.UsdtFinalitySafe, operation_setting.UsdtFinalityConfirmations}
			if !slices.Contains(valid, mode) || (chain.Kind == KindTron && mode == operation_setting.UsdtFinalitySafe) {
				return common.NewMessage("Finality mode {{mode}} is not supported on {{chain}}", map[string]any{"chain": chain.ID, "mode": mode})
			}
			cfg.Finality = mode
		}
		if p.Confirmations != nil {
			if *p.Confirmations < 0 || *p.Confirmations > 10000 {
				return common.NewMessage("Confirmations for {{chain}} must be between 0 and 10000", map[string]any{"chain": chain.ID})
			}
			cfg.Confirmations = *p.Confirmations
		}
		if cfg.Finality == operation_setting.UsdtFinalityConfirmations && cfg.Confirmations < 1 {
			return common.NewMessage("{{chain}} needs at least 1 confirmation", map[string]any{"chain": chain.ID})
		}
		if p.MinAmount != nil {
			if !validAmountSetting(*p.MinAmount) {
				return common.NewMessage("Minimum deposit amount for {{chain}} is invalid", map[string]any{"chain": chain.ID})
			}
			cfg.MinAmount = *p.MinAmount
		}
		if p.SweepThreshold != nil {
			if !validAmountSetting(*p.SweepThreshold) {
				return common.NewMessage("Sweep threshold for {{chain}} is invalid", map[string]any{"chain": chain.ID})
			}
			cfg.SweepThreshold = *p.SweepThreshold
		}
		if p.Enabled != nil {
			if *p.Enabled && !cfg.Verified {
				return common.NewMessage("{{chain}} cannot be enabled before its factory is verified", map[string]any{"chain": chain.ID})
			}
			if *p.Enabled && DistinctProviders(cfg.RPCURL, cfg.SecondaryRPCURL) != nil {
				return common.NewMessage("{{chain}} needs two verification nodes from different providers", map[string]any{"chain": chain.ID})
			}
			cfg.Enabled = *p.Enabled
		}
		setting.Chains[chain.ID] = cfg
	}

	if patch.RateMode != nil {
		mode := strings.TrimSpace(*patch.RateMode)
		if !slices.Contains([]string{operation_setting.UsdtRateMarket, operation_setting.UsdtRateFixed, operation_setting.UsdtRateC2C}, mode) {
			return common.NewMessage("USDT rate mode {{mode}} is not supported", map[string]any{"mode": mode})
		}
		setting.RateMode = mode
	}
	if patch.FixedRate != nil {
		if *patch.FixedRate <= 0 || *patch.FixedRate > 100 {
			return common.NewMessage("Fixed USDT rate must be greater than 0 and at most 100")
		}
		setting.FixedRate = *patch.FixedRate
	}
	if patch.ScannerIPs != nil {
		setting.ScannerIPs = strings.TrimSpace(*patch.ScannerIPs)
	}
	if patch.Enabled != nil {
		setting.Enabled = *patch.Enabled
	}
	if patch.AdminOnly != nil {
		setting.AdminOnly = *patch.AdminOnly
	}

	values := map[string]string{}
	if setting.Enabled && strings.TrimSpace(setting.SaltSecret) == "" {
		secret, err := randomHex32()
		if err != nil {
			return err
		}
		values["usdt_setting.salt_secret"] = secret
	}
	chainsJSON, err := common.Marshal(setting.Chains)
	if err != nil {
		return err
	}
	values["usdt_setting.chains"] = string(chainsJSON)
	values["usdt_setting.enabled"] = strconv.FormatBool(setting.Enabled)
	values["usdt_setting.admin_only"] = strconv.FormatBool(setting.AdminOnly)
	values["usdt_setting.rate_mode"] = setting.RateMode
	values["usdt_setting.fixed_rate"] = strconv.FormatFloat(setting.FixedRate, 'f', -1, 64)
	values["usdt_setting.scanner_ips"] = setting.ScannerIPs
	return model.UpdateOptionsBulk(values)
}

// MarkFactoryVerified stores a successful factory check for chainID, but only
// if the factory being checked is still the configured one.
func MarkFactoryVerified(chainID, factory string, check FactoryCheck) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	setting := operation_setting.GetUsdtSetting()
	cfg := setting.Chains[chainID]
	if cfg.FactoryAddress != factory {
		return common.NewMessage("The factory address changed during verification. Verify again.")
	}
	cfg.ImplementationAddress = Hex20(check.Implementation)
	cfg.Verified = true
	setting.Chains[chainID] = cfg
	chainsJSON, err := common.Marshal(setting.Chains)
	if err != nil {
		return err
	}
	return model.UpdateOptionsBulk(map[string]string{"usdt_setting.chains": string(chainsJSON)})
}

// RegenerateScannerSecret replaces the HMAC secret and returns it once.
func RegenerateScannerSecret() (string, error) {
	secret, err := randomHex32()
	if err != nil {
		return "", err
	}
	secret = strings.TrimPrefix(secret, "0x")
	if err := model.UpdateOptionsBulk(map[string]string{"usdt_setting.scanner_secret": secret}); err != nil {
		return "", err
	}
	return secret, nil
}

// ---- scanner protocol ----

const scannerClockSkew = 5 * time.Minute

var ErrScannerUnauthorized = errors.New("scanner request is not authorized")

// ScannerSignature is hex(HMAC-SHA256(secret, timestamp \n method \n path \n body)).
func ScannerSignature(secret, timestamp, method, path string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "\n" + strings.ToUpper(method) + "\n" + path + "\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyScannerRequest authenticates a request from the scanner service.
func VerifyScannerRequest(clientIP, timestamp, signature, method, path string, body []byte) error {
	setting := operation_setting.GetUsdtSetting()
	secret := strings.TrimSpace(setting.ScannerSecret)
	if secret == "" {
		return ErrScannerUnauthorized
	}
	if allow := strings.TrimSpace(setting.ScannerIPs); allow != "" {
		allowed := false
		for ip := range strings.SplitSeq(allow, ",") {
			if strings.TrimSpace(ip) == clientIP {
				allowed = true
				break
			}
		}
		if !allowed {
			return ErrScannerUnauthorized
		}
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ErrScannerUnauthorized
	}
	if d := time.Since(time.Unix(ts, 0)); d > scannerClockSkew || d < -scannerClockSkew {
		return ErrScannerUnauthorized
	}
	expected := ScannerSignature(secret, timestamp, method, path, body)
	if !hmac.Equal([]byte(expected), []byte(strings.ToLower(strings.TrimSpace(signature)))) {
		return ErrScannerUnauthorized
	}
	return nil
}

type ScannerChain struct {
	ID             string `json:"id"`
	ChainID        uint64 `json:"chain_id"`
	Factory        string `json:"factory"`
	Implementation string `json:"implementation"`
	USDT           string `json:"usdt"`
	Decimals       int32  `json:"decimals"`
	Finality       string `json:"finality"`
	Confirmations  int    `json:"confirmations"`
	SweepThreshold string `json:"sweep_threshold"`
}

type ScannerAddress struct {
	ID      int    `json:"id"`
	Chain   string `json:"chain"`
	Address string `json:"address"`
	Salt    string `json:"salt"`
}

type ScannerState struct {
	Chains    []ScannerChain   `json:"chains"`
	Addresses []ScannerAddress `json:"addresses"`
	NextSince int              `json:"next_since"`
	ServerNow int64            `json:"server_now"`
}

// BuildScannerState returns the networks to watch and a page of addresses.
// Every verified network is watched, even when hidden from users, so a late
// deposit to an address issued earlier is still found and swept.
func BuildScannerState(sinceID, limit int) (ScannerState, error) {
	setting := operation_setting.GetUsdtSetting()
	state := ScannerState{NextSince: sinceID, ServerNow: time.Now().Unix()}
	for _, id := range operation_setting.UsdtChainOrder {
		cfg := setting.Chains[id]
		chain, _ := LookupChain(id)
		if !cfg.Verified {
			continue
		}
		state.Chains = append(state.Chains, ScannerChain{
			ID: id, ChainID: chain.ChainID, Factory: cfg.FactoryAddress, Implementation: cfg.ImplementationAddress,
			USDT: Hex20(chain.USDT), Decimals: chain.Decimals, Finality: cfg.Finality, Confirmations: cfg.Confirmations,
			SweepThreshold: decimal.NewFromFloat(cfg.SweepThreshold).String(),
		})
	}
	rows, err := model.ListUsdtDepositAddressesSince(sinceID, limit)
	if err != nil {
		return state, err
	}
	for _, r := range rows {
		state.Addresses = append(state.Addresses, ScannerAddress{ID: r.Id, Chain: r.Chain, Address: r.Address, Salt: r.Salt})
		state.NextSince = r.Id
	}
	return state, nil
}

type ChainHeartbeat struct {
	Head          uint64 `json:"head"`
	Cursor        uint64 `json:"cursor"`
	GasBalance    string `json:"gas_balance"`
	PendingSweeps int    `json:"pending_sweeps"`
	LastError     string `json:"last_error"`
}

var heartbeat struct {
	mu      sync.Mutex
	at      int64
	version string
	chains  map[string]ChainHeartbeat
}

func RecordHeartbeat(version string, chains map[string]ChainHeartbeat) {
	heartbeat.mu.Lock()
	defer heartbeat.mu.Unlock()
	heartbeat.at = time.Now().Unix()
	heartbeat.version = version
	heartbeat.chains = chains
}

type heartbeatSnapshot struct {
	at      int64
	version string
	chains  map[string]ChainHeartbeat
}

func currentHeartbeat() heartbeatSnapshot {
	heartbeat.mu.Lock()
	defer heartbeat.mu.Unlock()
	return heartbeatSnapshot{at: heartbeat.at, version: heartbeat.version, chains: heartbeat.chains}
}
