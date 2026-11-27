package usdt

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// Network status as users see it: whether deposits on a network are
// credited promptly right now. It reveals nothing about providers.
const (
	NetworkStatusNormal    = "normal"
	NetworkStatusCongested = "congested"
)

const (
	// strainWindow is how long a round that could not reach two providers
	// keeps a network marked congested, unless a later round succeeds.
	strainWindow = 10 * time.Minute
	// scannerStaleAfter: the scanner reports every minute; past this, new
	// deposits are not being found and wait for a user claim or its return.
	scannerStaleAfter = 5 * time.Minute
)

var strainedAt sync.Map // chain id -> time.Time of the last provider shortage

func markStrained(chainID string) {
	strainedAt.Store(chainID, time.Now())
}

// NetworkStatus tells whether deposits on chainID are likely delayed now:
// fewer than two providers can verify, a recent round could not reach two,
// or the scanner stopped reporting.
func NetworkStatus(chainID string, cfg operation_setting.UsdtChainSetting) string {
	now := time.Now()
	if len(availableProviders(chainID, groupProviders(chainNodes(cfg)))) < 2 {
		return NetworkStatusCongested
	}
	if at, ok := strainedAt.Load(chainID); ok && now.Sub(at.(time.Time)) < strainWindow {
		return NetworkStatusCongested
	}
	hb := currentHeartbeat()
	if hb.at > 0 && now.Sub(time.Unix(hb.at, 0)) > scannerStaleAfter {
		return NetworkStatusCongested
	}
	return NetworkStatusNormal
}
