package usdt

import (
	"errors"
	"math/rand/v2"
	"sort"
	"sync"
	"time"
)

// Endpoint health is kept per network and endpoint, so a provider whose BSC
// receipts are refused keeps serving its other networks, and a provider's
// other endpoints keep working. It lives in memory: after a restart every
// endpoint is tried again, which costs at most one failed request each.

// methodResult is the last outcome of one JSON-RPC method on an endpoint.
type methodResult struct {
	OK     bool   `json:"ok"`
	Kind   string `json:"kind,omitempty"`
	Detail string `json:"detail,omitempty"`
	At     int64  `json:"at"`
}

type endpointHealth struct {
	mu        sync.Mutex
	failures  int
	until     time.Time
	lastFault *rpcFault
	lastAt    time.Time
	methods   map[string]methodResult
}

var healthStore sync.Map // chain|url fingerprint -> *endpointHealth

func healthFor(chainID, rawURL string) *endpointHealth {
	key := chainID + "|" + urlFingerprint(rawURL)
	if h, ok := healthStore.Load(key); ok {
		return h.(*endpointHealth)
	}
	h, _ := healthStore.LoadOrStore(key, &endpointHealth{methods: map[string]methodResult{}})
	return h.(*endpointHealth)
}

// cooldownFor grows with consecutive failures and is jittered so endpoints
// that failed together do not all come back at the same instant.
func cooldownFor(f *rpcFault, failures int) time.Duration {
	base, ceiling := 15*time.Second, 5*time.Minute
	switch f.Kind {
	case faultRateLimited:
		base, ceiling = 30*time.Second, 10*time.Minute
	case faultDenied:
		base, ceiling = 5*time.Minute, time.Hour
	case faultUnsupported:
		base, ceiling = 30*time.Minute, 6*time.Hour
	case faultWrongNetwork:
		base, ceiling = 24*time.Hour, 24*time.Hour
	}
	d := base << min(max(failures-1, 0), 8)
	d = min(d, ceiling)
	d += time.Duration(rand.Int64N(int64(d)/5 + 1))
	if f.RetryAfter > d {
		d = min(f.RetryAfter, time.Hour)
	}
	return d
}

// record notes one call's outcome. A fault starts or extends the cooldown;
// a success of one method does not lift a cooldown caused by another (a node
// that answers eth_chainId but refuses receipts is still unusable).
func (h *endpointHealth) record(method string, err error) {
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastAt = now
	var fault *rpcFault
	if !errors.As(err, &fault) {
		// Success, or an answer that is not an endpoint fault (block not found).
		h.methods[method] = methodResult{OK: true, At: now.Unix()}
		return
	}
	h.methods[method] = methodResult{Kind: string(fault.Kind), Detail: fault.Detail, At: now.Unix()}
	h.failures++
	h.lastFault = fault
	h.until = now.Add(cooldownFor(fault, h.failures))
}

// markHealthy clears the cooldown once the endpoint delivered a full piece of
// evidence or passed the administrator's test.
func (h *endpointHealth) markHealthy() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failures = 0
	h.until = time.Time{}
	h.lastFault = nil
}

func (h *endpointHealth) coolingUntil(now time.Time) (time.Time, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.until, now.Before(h.until)
}

// rank orders endpoints for the next round: 0 answered last time, 1 never
// asked, 2 failed recently (its cooldown has passed).
func (h *endpointHealth) rank(now time.Time) (int, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cooling := now.Before(h.until)
	switch {
	case h.failures > 0:
		return 2, cooling
	case h.lastAt.IsZero():
		return 1, cooling
	default:
		return 0, cooling
	}
}

// EndpointHealthView is what the settings page shows for one endpoint.
type EndpointHealthView struct {
	State        string                  `json:"state"` // healthy, cooling, unknown
	CoolingUntil int64                   `json:"cooling_until,omitempty"`
	Failures     int                     `json:"failures"`
	LastError    string                  `json:"last_error,omitempty"`
	LastKind     string                  `json:"last_kind,omitempty"`
	LastAt       int64                   `json:"last_at,omitempty"`
	Methods      []EndpointMethodOutcome `json:"methods"`
}

type EndpointMethodOutcome struct {
	Method string `json:"method"`
	methodResult
}

func (h *endpointHealth) view(now time.Time) EndpointHealthView {
	h.mu.Lock()
	defer h.mu.Unlock()
	v := EndpointHealthView{State: "unknown", Failures: h.failures, Methods: []EndpointMethodOutcome{}}
	if !h.lastAt.IsZero() {
		v.State = "healthy"
		v.LastAt = h.lastAt.Unix()
	}
	if now.Before(h.until) {
		v.State = "cooling"
		v.CoolingUntil = h.until.Unix()
	}
	if h.lastFault != nil {
		v.LastError = h.lastFault.Error()
		v.LastKind = string(h.lastFault.Kind)
	}
	for method, r := range h.methods {
		v.Methods = append(v.Methods, EndpointMethodOutcome{Method: method, methodResult: r})
	}
	sort.Slice(v.Methods, func(i, j int) bool { return v.Methods[i].Method < v.Methods[j].Method })
	return v
}

// rpcSlots bounds the verification requests in flight across the process, so
// a backlog of deposits during an outage cannot turn into a request storm.
var rpcSlots = make(chan struct{}, 32)
