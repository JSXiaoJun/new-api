package usdt

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/url"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"golang.org/x/net/publicsuffix"
)

// knownProviderDomains maps the registrable domains of RPC providers to one
// operator, so several hosts, regions or API keys of one company count as a
// single provider however the administrator names them.
var knownProviderDomains = map[string]string{
	"alchemy.com":        "alchemy",
	"alchemyapi.io":      "alchemy",
	"infura.io":          "infura",
	"quiknode.pro":       "quicknode",
	"quiknode.io":        "quicknode",
	"quicknode.com":      "quicknode",
	"ankr.com":           "ankr",
	"drpc.org":           "drpc",
	"drpc.live":          "drpc",
	"publicnode.com":     "allnodes",
	"allnodes.me":        "allnodes",
	"llamarpc.com":       "llamanodes",
	"blastapi.io":        "blast",
	"bwarelabs.com":      "blast",
	"chainstack.com":     "chainstack",
	"p2pify.com":         "chainstack",
	"getblock.io":        "getblock",
	"getblock.us":        "getblock",
	"nodereal.io":        "nodereal",
	"binance.org":        "bnbchain",
	"bnbchain.org":       "bnbchain",
	"defibit.io":         "bnbchain",
	"ninicoin.io":        "bnbchain",
	"1rpc.io":            "1rpc",
	"onfinality.io":      "onfinality",
	"tatum.io":           "tatum",
	"nownodes.io":        "nownodes",
	"trongrid.io":        "trongrid",
	"arbitrum.io":        "offchainlabs",
	"omniatech.io":       "omnia",
	"lavanet.xyz":        "lava",
	"unifra.io":          "unifra",
	"moralis.io":         "moralis",
	"moralis-nodes.com":  "moralis",
	"blockpi.network":    "blockpi",
	"4everland.org":      "4everland",
	"meowrpc.com":        "meowrpc",
	"tenderly.co":        "tenderly",
	"zan.top":            "zan",
	"nodies.app":         "nodies",
	"grove.city":         "grove",
	"pokt.network":       "grove",
	"subquery.network":   "subquery",
	"blastapi.com":       "blast",
	"rpcfast.com":        "rpcfast",
	"merkle.io":          "merkle",
	"stackup.sh":         "stackup",
	"thirdweb.com":       "thirdweb",
	"polygon-rpc.com":    "polygon-rpc",
	"polygon.technology": "polygon-labs",
}

// knownProviderNames are the operators' display names.
var knownProviderNames = map[string]string{
	"alchemy": "Alchemy", "infura": "Infura", "quicknode": "QuickNode", "ankr": "Ankr", "drpc": "dRPC",
	"allnodes": "PublicNode (Allnodes)", "llamanodes": "LlamaNodes", "blast": "Blast", "chainstack": "Chainstack",
	"getblock": "GetBlock", "nodereal": "NodeReal", "bnbchain": "BNB Chain", "1rpc": "1RPC", "onfinality": "OnFinality",
	"tatum": "Tatum", "nownodes": "NOWNodes", "trongrid": "TronGrid", "offchainlabs": "Offchain Labs", "omnia": "Omnia",
	"lava": "Lava", "unifra": "Unifra", "moralis": "Moralis", "blockpi": "BlockPI", "4everland": "4EVERLAND",
	"meowrpc": "MeowRPC", "tenderly": "Tenderly", "zan": "ZAN", "nodies": "Nodies", "grove": "Grove (Pocket)",
	"subquery": "SubQuery", "rpcfast": "RPC Fast", "merkle": "Merkle", "stackup": "Stackup", "thirdweb": "thirdweb",
	"polygon-rpc": "polygon-rpc.com", "polygon-labs": "Polygon Labs",
}

// rpcHost returns the lowercase host of an RPC URL, or "" when it is not an
// http(s) URL.
func rpcHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
}

// knownProvider returns the operator of a host new-api recognizes.
func knownProvider(host string) string {
	if host == "" || net.ParseIP(host) != nil {
		return ""
	}
	return knownProviderDomains[registrableDomain(host)]
}

// NormalizeProviderLabel turns an administrator's operator label into the
// key that groups endpoints. A label naming a known provider joins it.
func NormalizeProviderLabel(label string) string {
	key := strings.ToLower(strings.Join(strings.Fields(label), " "))
	for id, name := range knownProviderNames {
		if key == id || key == strings.ToLower(name) {
			return id
		}
	}
	return key
}

// nodeProvider is the independent source an endpoint belongs to. A known
// host always belongs to its operator, whatever label it carries; any other
// host needs the administrator to name its operator, and until then it is
// not used for verification ("" result).
func nodeProvider(node operation_setting.UsdtRPCNode) string {
	if known := knownProvider(rpcHost(node.URL)); known != "" {
		return known
	}
	return NormalizeProviderLabel(node.Provider)
}

func providerDisplayName(key string) string {
	if name, ok := knownProviderNames[key]; ok {
		return name
	}
	return key
}

// normalizedRPCURL is the duplicate-detection form of a URL: scheme and host
// are case-insensitive, the rest (paths and keys) is not.
func normalizedRPCURL(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	return strings.TrimSuffix(u.String(), "/")
}

// urlFingerprint identifies a URL in logs and health keys without revealing it.
func urlFingerprint(raw string) string {
	sum := sha256.Sum256([]byte(normalizedRPCURL(raw)))
	return hex.EncodeToString(sum[:])[:12]
}

// maskedRPCURL shows an endpoint without its credentials: scheme and host,
// and a marker when a path or query (where providers put API keys) exists.
func maskedRPCURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "invalid URL"
	}
	out := u.Scheme + "://" + u.Host
	if strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.User != nil {
		out += "/…"
	}
	return out
}

func newNodeID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// chainNodes returns a network's verification nodes. A network saved before
// node lists existed gets them from its two legacy fields, primary first, so
// the order survives; the list is written on the next save. The old rule
// already required the two slots to use different hosts, so an unrecognized
// host keeps counting as its own provider, flagged for the administrator to
// confirm.
func chainNodes(cfg operation_setting.UsdtChainSetting) []operation_setting.UsdtRPCNode {
	if cfg.RPCNodes != nil {
		return cfg.RPCNodes
	}
	var nodes []operation_setting.UsdtRPCNode
	seen := map[string]bool{}
	for slot, urls := range []string{cfg.RPCURL, cfg.SecondaryRPCURL} {
		for raw := range strings.SplitSeq(urls, ",") {
			raw = strings.TrimSpace(raw)
			if raw == "" || seen[normalizedRPCURL(raw)] {
				continue
			}
			seen[normalizedRPCURL(raw)] = true
			name := "Primary"
			if slot == 1 {
				name = "Secondary"
			}
			node := operation_setting.UsdtRPCNode{
				ID: "legacy-" + urlFingerprint(raw), Name: name, URL: raw,
				Priority: (len(nodes) + 1) * 10, Enabled: true,
			}
			if host := rpcHost(raw); host != "" && knownProvider(host) == "" {
				node.Provider, node.ProviderInferred = registrableDomain(host), true
			}
			nodes = append(nodes, node)
		}
	}
	return nodes
}

// registrableDomain is the part of a host one operator controls
// (rpc.eu.example.co.uk -> example.co.uk); an IP address is its own.
func registrableDomain(host string) string {
	if net.ParseIP(host) != nil {
		return host
	}
	if domain, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil {
		return domain
	}
	return host
}

// providerGroup is one independent source with its endpoints, best first.
type providerGroup struct {
	Provider string
	Nodes    []operation_setting.UsdtRPCNode
}

// groupProviders returns the enabled, identified endpoints grouped by
// provider, ordered by each provider's best priority (then list order).
func groupProviders(nodes []operation_setting.UsdtRPCNode) []providerGroup {
	ordered := slices.Clone(nodes)
	slices.SortStableFunc(ordered, func(a, b operation_setting.UsdtRPCNode) int { return a.Priority - b.Priority })
	var groups []providerGroup
	index := map[string]int{}
	for _, node := range ordered {
		provider := nodeProvider(node)
		if !node.Enabled || provider == "" || rpcHost(node.URL) == "" {
			continue
		}
		i, ok := index[provider]
		if !ok {
			i = len(groups)
			index[provider] = i
			groups = append(groups, providerGroup{Provider: provider})
		}
		groups[i].Nodes = append(groups[i].Nodes, node)
	}
	return groups
}

// NodePreview is what new-api recognizes in a pasted URL, before saving.
type NodePreview struct {
	URL          string `json:"url"`
	Endpoint     string `json:"endpoint"`
	Valid        bool   `json:"valid"`
	ProviderKey  string `json:"provider_key"`
	ProviderName string `json:"provider_name"`
	Known        bool   `json:"known"`
	// Domain groups unknown hosts: endpoints on one domain share an operator.
	Domain string `json:"domain"`
}

// PreviewNodes recognizes pasted URLs, one per entry. The URLs are echoed
// back to the administrator who pasted them and are not stored or logged.
func PreviewNodes(urls []string) []NodePreview {
	out := make([]NodePreview, 0, len(urls))
	for _, raw := range urls {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		host := rpcHost(raw)
		p := NodePreview{URL: raw, Endpoint: maskedRPCURL(raw), Valid: host != ""}
		if p.Valid {
			p.Domain = registrableDomain(host)
			p.ProviderKey = knownProvider(host)
			p.Known = p.ProviderKey != ""
			p.ProviderName = providerDisplayName(p.ProviderKey)
		}
		out = append(out, p)
	}
	return out
}

// legacySlots renders a node list into the two legacy fields: the first
// provider's endpoints, then every other provider's. A build from before
// node lists reads these and still sees two different providers.
func legacySlots(nodes []operation_setting.UsdtRPCNode) (string, string) {
	groups := groupProviders(nodes)
	if len(groups) == 0 {
		return "", ""
	}
	join := func(groups []providerGroup) string {
		var urls []string
		for _, g := range groups {
			for _, n := range g.Nodes {
				urls = append(urls, strings.TrimSpace(n.URL))
			}
		}
		return strings.Join(urls, ",")
	}
	return join(groups[:1]), join(groups[1:])
}
