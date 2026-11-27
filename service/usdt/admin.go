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

// ChainView is one network as the settings page shows it. Endpoint URLs and
// the legacy URL fields are never sent: they can carry API keys.
type ChainView struct {
	operation_setting.UsdtChainSetting
	ID           string `json:"id"`
	Name         string `json:"name"`
	Contract     string `json:"contract"`
	Factory      string `json:"factory_display"`
	AddressCount int64  `json:"address_count"`
	// FactoryLocked is true once addresses exist: changing the factory would
	// strand every deposit sent to them.
	FactoryLocked bool       `json:"factory_locked"`
	Nodes         []NodeView `json:"nodes"`
	// EnabledNodes and Providers are "N enabled endpoints / M independent
	// providers"; AvailableProviders counts those not cooling down now.
	EnabledNodes       int `json:"enabled_nodes"`
	Providers          int `json:"providers"`
	AvailableProviders int `json:"available_providers"`
	// Status is what users see on this network: "normal" or "congested".
	Status string `json:"status"`
}

// NodeView is a verification endpoint without its credentials.
type NodeView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	// ProviderKey is the independent source the endpoint counts as ("" when
	// its operator is unknown and it is not used); ProviderName displays it.
	ProviderKey      string             `json:"provider_key"`
	ProviderName     string             `json:"provider_name"`
	ProviderKnown    bool               `json:"provider_known"`
	ProviderInferred bool               `json:"provider_inferred"`
	Endpoint         string             `json:"endpoint"`
	Priority         int                `json:"priority"`
	Enabled          bool               `json:"enabled"`
	Health           EndpointHealthView `json:"health"`
}

func nodeViews(chainID string, nodes []operation_setting.UsdtRPCNode) []NodeView {
	now := time.Now()
	views := make([]NodeView, 0, len(nodes))
	for _, n := range nodes {
		key := nodeProvider(n)
		views = append(views, NodeView{
			ID: n.ID, Name: n.Name, Provider: n.Provider, ProviderKey: key, ProviderName: providerDisplayName(key),
			ProviderKnown: knownProvider(rpcHost(n.URL)) != "", ProviderInferred: n.ProviderInferred,
			Endpoint: maskedRPCURL(n.URL), Priority: n.Priority, Enabled: n.Enabled,
			Health: healthFor(chainID, n.URL).view(now),
		})
	}
	return views
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
	Alerts           []AlertView               `json:"alerts"`
	HeartbeatAt      int64                     `json:"heartbeat_at"`
	ScannerVersion   string                    `json:"scanner_version"`
}

// AlertView is a disagreement and whether its transaction is still held.
type AlertView struct {
	VerificationAlert
	Held bool `json:"held"`
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
		Heartbeat: hb.chains, HeartbeatAt: hb.at, ScannerVersion: hb.version, Alerts: []AlertView{},
	}
	for _, alert := range RecentVerificationAlerts() {
		_, held := conflictHolds.Load(alert.Chain + "|" + alert.TxHash)
		view.Alerts = append(view.Alerts, AlertView{VerificationAlert: alert, Held: held})
	}
	for _, id := range operation_setting.UsdtChainOrder {
		chain, _ := LookupChain(id)
		cfg := setting.Chains[id]
		nodes := chainNodes(cfg)
		groups := groupProviders(nodes)
		public := cfg
		public.RPCNodes, public.RPCURL, public.SecondaryRPCURL = nil, "", ""
		v := ChainView{
			UsdtChainSetting: public, ID: id, Name: chain.Name, Contract: DisplayAddress(chain, chain.USDT),
			AddressCount: counts[id], FactoryLocked: counts[id] > 0, Nodes: nodeViews(id, nodes),
			Providers: len(groups), AvailableProviders: len(availableProviders(id, groups)), Status: NetworkStatus(id, cfg),
		}
		for _, n := range nodes {
			if n.Enabled {
				v.EnabledNodes++
			}
		}
		if f, err := ParseHex20(cfg.FactoryAddress); err == nil {
			v.Factory = DisplayAddress(chain, f)
		}
		view.Chains = append(view.Chains, v)
	}
	return view, nil
}

// NodePatch is one endpoint as the settings page sends it. An existing node
// (ID set) with an empty URL keeps its saved URL, so editing a name or the
// order never needs the API key again.
type NodePatch struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	URL      string `json:"url"`
	Enabled  bool   `json:"enabled"`
}

type ChainPatch struct {
	Enabled        *bool    `json:"enabled"`
	FactoryAddress *string  `json:"factory_address"`
	Finality       *string  `json:"finality"`
	Confirmations  *int     `json:"confirmations"`
	MinAmount      *float64 `json:"min_amount"`
	SweepThreshold *float64 `json:"sweep_threshold"`
	// RPCNodes replaces the node list, in priority order.
	RPCNodes *[]NodePatch `json:"rpc_nodes"`
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

// applyNodePatch validates a new node list against the saved one. The list
// order is the priority. A URL appears once; one registrable domain belongs
// to one provider, so renaming cannot split an operator into two votes.
func applyNodePatch(chain Chain, saved []operation_setting.UsdtRPCNode, patch []NodePatch) ([]operation_setting.UsdtRPCNode, error) {
	byID := map[string]operation_setting.UsdtRPCNode{}
	for _, n := range saved {
		byID[n.ID] = n
	}
	nodes := make([]operation_setting.UsdtRPCNode, 0, len(patch))
	seenURL := map[string]int{}
	domainProvider := map[string]string{}
	for i, p := range patch {
		position := map[string]any{"chain": chain.ID, "position": i + 1}
		node := operation_setting.UsdtRPCNode{
			ID: strings.TrimSpace(p.ID), Name: strings.TrimSpace(p.Name), Provider: strings.Join(strings.Fields(p.Provider), " "),
			URL: strings.TrimSpace(p.URL), Priority: (i + 1) * 10, Enabled: p.Enabled,
		}
		old, existing := byID[node.ID]
		if node.ID == "" || !existing {
			node.ID = newNodeID()
		}
		if node.URL == "" && existing {
			node.URL = old.URL
		}
		if existing && old.ProviderInferred && node.Provider == old.Provider {
			node.ProviderInferred = true
		}
		if len(node.Name) > 64 || len(node.Provider) > 64 || len(node.URL) > 2048 {
			return nil, common.NewMessage("Node {{position}} of {{chain}} has a field that is too long", position)
		}
		host := rpcHost(node.URL)
		if host == "" {
			return nil, common.NewMessage("Node {{position}} of {{chain}} needs an http or https URL", position)
		}
		normalized := normalizedRPCURL(node.URL)
		if first, dup := seenURL[normalized]; dup {
			position["first"] = first
			return nil, common.NewMessage("Node {{position}} of {{chain}} repeats the URL of node {{first}}", position)
		}
		seenURL[normalized] = i + 1
		domain, provider := registrableDomain(host), nodeProvider(node)
		if known, ok := domainProvider[domain]; ok && known != provider {
			position["domain"] = domain
			return nil, common.NewMessage("Endpoints on {{domain}} belong to one provider; give node {{position}} of {{chain}} the same provider as the others", position)
		}
		domainProvider[domain] = provider
		if node.Name == "" {
			node.Name = host
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
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
		providersBefore := len(groupProviders(chainNodes(cfg)))
		if p.RPCNodes != nil {
			nodes, err := applyNodePatch(chain, chainNodes(cfg), *p.RPCNodes)
			if err != nil {
				return err
			}
			cfg.RPCNodes = nodes
		}
		if cfg.RPCNodes == nil {
			// First save after an upgrade: keep the migrated legacy list.
			cfg.RPCNodes = chainNodes(cfg)
		}
		cfg.RPCURL, cfg.SecondaryRPCURL = legacySlots(cfg.RPCNodes)
		providers := len(groupProviders(cfg.RPCNodes))
		if cfg.Verified && providers < 2 && providers < providersBefore {
			// A verified network verifies deposits now, shown to users or not:
			// keep the working configuration rather than stall its deposits.
			return common.NewMessage("{{chain}} verifies deposits and needs at least two enabled independent providers; this change leaves {{count}}", map[string]any{"chain": chain.ID, "count": providers})
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
			if *p.Enabled && len(groupProviders(cfg.RPCNodes)) < 2 {
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
