package operation_setting

import (
	"maps"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

// USDT deposit chain identifiers. The on-chain facts for each (token contract,
// decimals, chain id) live in service/usdt, not in settings.
const (
	UsdtChainBSC      = "bsc"
	UsdtChainArbitrum = "arbitrum"
	UsdtChainPolygon  = "polygon"
	UsdtChainTron     = "tron"
)

// UsdtChainOrder is the display order of the supported networks.
var UsdtChainOrder = []string{UsdtChainBSC, UsdtChainArbitrum, UsdtChainPolygon, UsdtChainTron}

const (
	UsdtFinalityFinalized     = "finalized"
	UsdtFinalitySafe          = "safe"
	UsdtFinalityConfirmations = "confirmations"

	UsdtRateMarket = "market"
	UsdtRateFixed  = "fixed"
)

// UsdtChainSetting is one network's deposit configuration. FactoryAddress and
// ImplementationAddress are 0x-prefixed lowercase hex on every chain,
// including TRON. Verified is set only by the factory check endpoint and is
// cleared whenever the factory changes.
type UsdtChainSetting struct {
	Enabled               bool   `json:"enabled"`
	FactoryAddress        string `json:"factory_address"`
	ImplementationAddress string `json:"implementation_address"`
	Verified              bool   `json:"verified"`
	RPCURL                string `json:"rpc_url"`
	// SecondaryRPCURL must be a different provider: a deposit is credited only
	// when both providers report exactly the same transfer.
	SecondaryRPCURL string  `json:"rpc_url_secondary"`
	Finality        string  `json:"finality"`
	Confirmations   int     `json:"confirmations"`
	MinAmount       float64 `json:"min_amount"`
	SweepThreshold  float64 `json:"sweep_threshold"`
}

// UsdtSetting is managed through the dedicated USDT settings endpoints, never
// the generic option API: the salt secret and a chain's factory are frozen
// once addresses exist, and the generic API cannot enforce that.
type UsdtSetting struct {
	Enabled  bool                        `json:"enabled"`
	Chains   map[string]UsdtChainSetting `json:"chains"`
	RateMode string                      `json:"rate_mode"`
	// FixedRate is the USD quota credited per USDT in fixed mode.
	FixedRate float64 `json:"fixed_rate"`
	// SaltSecret derives every deposit address (32-byte hex). Frozen once any
	// address is issued; a change would orphan funds sent to old addresses.
	SaltSecret string `json:"salt_secret"`
	// ScannerSecret signs scanner requests (HMAC-SHA256).
	ScannerSecret string `json:"scanner_secret"`
	// ScannerIPs optionally restricts scanner requests to these client IPs.
	ScannerIPs string `json:"scanner_ips"`
}

func defaultUsdtChains() map[string]UsdtChainSetting {
	return map[string]UsdtChainSetting{
		UsdtChainBSC:      {Finality: UsdtFinalityFinalized, MinAmount: 1, SweepThreshold: 1},
		UsdtChainArbitrum: {Finality: UsdtFinalitySafe, MinAmount: 1, SweepThreshold: 1},
		UsdtChainPolygon:  {Finality: UsdtFinalityFinalized, MinAmount: 1, SweepThreshold: 1},
		UsdtChainTron:     {Finality: UsdtFinalityConfirmations, Confirmations: 20, MinAmount: 10, SweepThreshold: 50},
	}
}

var usdtSetting = UsdtSetting{
	Chains:    defaultUsdtChains(),
	RateMode:  UsdtRateMarket,
	FixedRate: 1,
}

func init() {
	config.GlobalConfig.Register("usdt_setting", &usdtSetting)
}

// GetUsdtSetting returns a copy that callers may read without racing a
// concurrent settings update. Chains missing from storage get their defaults.
func GetUsdtSetting() UsdtSetting {
	out := usdtSetting
	out.Chains = maps.Clone(defaultUsdtChains())
	maps.Copy(out.Chains, usdtSetting.Chains)
	if out.RateMode != UsdtRateFixed {
		out.RateMode = UsdtRateMarket
	}
	return out
}

// IsUsdtSettingOptionKey reports keys owned by the USDT settings endpoints.
func IsUsdtSettingOptionKey(key string) bool {
	return strings.HasPrefix(key, "usdt_setting.")
}
