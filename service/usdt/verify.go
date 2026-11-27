package usdt

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

var (
	// ErrNotReady means the transaction may still become creditable: it is not
	// mined, not final yet, or not enough providers could confirm it. Retry later.
	ErrNotReady = errors.New("transaction is not verified yet")
	// ErrRejected means the transaction can never be credited.
	ErrRejected = errors.New("transaction cannot be credited")
	// ErrChainUnavailable means the chain is disabled or not configured.
	ErrChainUnavailable = errors.New("USDT network is not available")
)

// Why a verification round ended without a result.
const (
	// ReasonAwaitingConfirmation: providers answered, and the transaction is not
	// found, not final or not canonical yet.
	ReasonAwaitingConfirmation = "awaiting_confirmation"
	// ReasonProvidersUnavailable: fewer than two independent providers could
	// answer this round (rate limits, refusals, timeouts, cooldowns).
	ReasonProvidersUnavailable = "providers_unavailable"
	// ReasonProvidersInsufficient: the network has fewer than two identified,
	// enabled providers configured.
	ReasonProvidersInsufficient = "providers_insufficient"
	// ReasonConflict: two providers returned contradicting on-chain data.
	ReasonConflict = "conflict"
	// ReasonFailedOnChain: two providers agree the transaction failed.
	ReasonFailedOnChain = "failed_on_chain"
)

const (
	// verifyParallel providers are asked at once; a failed one is replaced by
	// the next healthy provider until two agree.
	verifyParallel = 2
	// verifyMaxProviders bounds how many providers one round may ask, so an
	// outage costs the same however many nodes are configured. Providers left
	// out are reached in later rounds once the failed ones cool down.
	verifyMaxProviders = 6
	verifyRoundBudget  = 20 * time.Second
)

// VerifyAttempt is one provider's answer in a round. Endpoint is masked.
type VerifyAttempt struct {
	Provider string `json:"provider"`
	Endpoint string `json:"endpoint"`
	Outcome  string `json:"outcome"` // final, failed_tx, pending, fault
	Detail   string `json:"detail,omitempty"`
}

// VerifyError explains a round that credited nothing. It wraps ErrNotReady or
// ErrRejected and never contains an endpoint URL or key.
type VerifyError struct {
	Reason   string
	Attempts []VerifyAttempt
	base     error
}

func (e *VerifyError) Error() string {
	var b strings.Builder
	b.WriteString(e.base.Error())
	b.WriteString(" (")
	b.WriteString(e.Reason)
	b.WriteString(")")
	for _, a := range e.Attempts {
		fmt.Fprintf(&b, "; %s [%s] %s", a.Provider, a.Endpoint, a.Outcome)
		if a.Detail != "" {
			b.WriteString(": ")
			b.WriteString(a.Detail)
		}
	}
	return b.String()
}

func (e *VerifyError) Unwrap() error { return e.base }

// Transfer is one USDT Transfer log that new-api verified itself.
type Transfer struct {
	Chain       string
	TxHash      string
	LogIndex    int
	From        [20]byte
	To          [20]byte
	Amount      *big.Int
	BlockNumber uint64
}

// NormalizeTxHash accepts 0x-prefixed or bare 64-hex hashes (TRON txids).
func NormalizeTxHash(s string) (string, error) {
	raw := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "0x")
	if len(raw) != 64 {
		return "", errors.New("invalid transaction hash")
	}
	for _, r := range raw {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return "", errors.New("invalid transaction hash")
		}
	}
	return "0x" + raw, nil
}

var chainIDCache sync.Map // chain|url fingerprint -> time.Time of last successful check

// checkChainID refuses an RPC that serves a different network, which would
// otherwise make a testnet or another chain's transfer look like a deposit.
func checkChainID(ctx context.Context, client *rpcClient, chain Chain) error {
	key := chain.ID + "|" + urlFingerprint(client.url)
	if at, ok := chainIDCache.Load(key); ok && time.Since(at.(time.Time)) < 10*time.Minute {
		return nil
	}
	id, err := client.chainID(ctx)
	if err != nil {
		return asFault("eth_chainId", err)
	}
	if id != chain.ChainID {
		fault := &rpcFault{Kind: faultWrongNetwork, Method: "eth_chainId",
			Detail: fmt.Sprintf("RPC chain id %d does not match %s (%d)", id, chain.ID, chain.ChainID)}
		if client.health != nil {
			client.health.record("eth_chainId", fault)
		}
		return fault
	}
	chainIDCache.Store(key, time.Now())
	return nil
}

type evidenceKind string

const (
	evidenceFinal    evidenceKind = "final"     // success receipt in a final, canonical block
	evidenceFailedTx evidenceKind = "failed_tx" // failed receipt in a final, canonical block
	evidencePending  evidenceKind = "pending"   // not found, not final or not canonical on this node
	evidenceFault    evidenceKind = "fault"     // the endpoint could not answer
)

// nodeReport is one provider's complete answer about a transaction. Every
// field comes from a single endpoint, never stitched from two.
type nodeReport struct {
	kind   evidenceKind
	detail string
	// mined is set on a pending answer that found the receipt (the block is
	// not final or not canonical yet), as opposed to not finding it at all.
	mined     bool
	provider  string
	endpoint  string
	blockHash string
	transfers []Transfer
}

func (r nodeReport) attempt() VerifyAttempt {
	return VerifyAttempt{Provider: providerDisplayName(r.provider), Endpoint: r.endpoint, Outcome: string(r.kind), Detail: r.detail}
}

// answerFromError turns a lookup error into a report: endpoint faults and
// cancellation make the endpoint unusable this round, anything else is a
// valid "not yet" answer from the chain.
func answerFromError(err error) nodeReport {
	var fault *rpcFault
	if errors.As(err, &fault) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nodeReport{kind: evidenceFault, detail: err.Error()}
	}
	return nodeReport{kind: evidencePending, detail: err.Error()}
}

// evidenceFrom reads one transaction through one endpoint: the right network,
// the receipt, finality and the block still being canonical.
func evidenceFrom(ctx context.Context, chain Chain, cfg operation_setting.UsdtChainSetting, client *rpcClient, hash string) nodeReport {
	if err := checkChainID(ctx, client, chain); err != nil {
		return answerFromError(err)
	}
	receipt, err := client.receipt(ctx, hash)
	if err != nil {
		return answerFromError(err)
	}
	if receipt == nil {
		return nodeReport{kind: evidencePending, detail: "transaction not found"}
	}
	if receipt.TransactionHash != "" && strings.TrimPrefix(strings.ToLower(receipt.TransactionHash), "0x") != strings.TrimPrefix(hash, "0x") {
		return nodeReport{kind: evidenceFault, detail: "eth_getTransactionReceipt: receipt of another transaction"}
	}
	block, err := parseQuantity(receipt.BlockNumber)
	if err != nil {
		return nodeReport{kind: evidencePending, detail: "receipt without a block number"}
	}
	if err := checkFinal(ctx, client, cfg, block); err != nil {
		report := answerFromError(err)
		report.mined = report.kind == evidencePending
		return report
	}
	canonical, err := client.blockByNumber(ctx, fmt.Sprintf("0x%x", block))
	if err != nil {
		return answerFromError(err)
	}
	if !strings.EqualFold(canonical.Hash, receipt.BlockHash) {
		return nodeReport{kind: evidencePending, mined: true, detail: fmt.Sprintf("block %d was reorganized", block)}
	}
	report := nodeReport{kind: evidenceFinal, blockHash: strings.ToLower(receipt.BlockHash)}
	if receipt.Status != "0x1" {
		report.kind = evidenceFailedTx
		report.detail = "transaction failed on chain"
		return report
	}
	for _, log := range receipt.Logs {
		if log.Removed || len(log.Topics) != 3 {
			continue
		}
		contract, err := ParseHex20(log.Address)
		if err != nil || contract != chain.USDT {
			continue
		}
		topic0, err := ParseHex32(log.Topics[0])
		if err != nil || topic0 != transferTopic {
			continue
		}
		from, errFrom := ParseHex32(log.Topics[1])
		to, errTo := ParseHex32(log.Topics[2])
		data := strings.TrimPrefix(log.Data, "0x")
		amount, ok := new(big.Int).SetString(data, 16)
		index, errIndex := parseQuantity(log.LogIndex)
		if errFrom != nil || errTo != nil || len(data) != 64 || !ok || errIndex != nil {
			continue
		}
		t := Transfer{Chain: chain.ID, TxHash: hash, LogIndex: int(index), Amount: amount, BlockNumber: block}
		copy(t.From[:], from[12:])
		copy(t.To[:], to[12:])
		report.transfers = append(report.transfers, t)
	}
	return report
}

// providerEvidence asks one provider, trying its endpoints in priority order
// and skipping those cooling down. Another endpoint of the same provider is a
// connection backup, never a second vote.
func providerEvidence(ctx context.Context, chain Chain, cfg operation_setting.UsdtChainSetting, group providerGroup, hash string) nodeReport {
	last := nodeReport{kind: evidenceFault, detail: "every endpoint is cooling down"}
	now := time.Now()
	for _, node := range group.Nodes {
		health := healthFor(chain.ID, node.URL)
		if _, cooling := health.coolingUntil(now); cooling {
			continue
		}
		report := evidenceFrom(ctx, chain, cfg, &rpcClient{url: node.URL, health: health}, hash)
		report.provider, report.endpoint = group.Provider, maskedRPCURL(node.URL)
		if report.kind != evidenceFault {
			health.markHealthy()
			return report
		}
		if ctx.Err() != nil {
			// The round ended (answered or out of time); not the endpoint's fault.
			return report
		}
		common.SysLog(common.LogText("USDT verification endpoint failed chain=%s provider=%s endpoint=%s error=%s",
			chain.ID, providerDisplayName(group.Provider), report.endpoint, report.detail))
		last = report
	}
	last.provider = group.Provider
	return last
}

// availableProviders keeps providers with at least one endpoint not cooling
// down. Providers that answered last time come first, then untried ones,
// then those that failed recently, each in priority order: a round never
// restarts from the same failing head of a long list, so providers further
// down are reached within a few rounds.
func availableProviders(chainID string, groups []providerGroup) []providerGroup {
	now := time.Now()
	type ranked struct {
		group providerGroup
		rank  int
	}
	var out []ranked
	for _, g := range groups {
		best := -1
		for _, node := range g.Nodes {
			if rank, cooling := healthFor(chainID, node.URL).rank(now); !cooling && (best < 0 || rank < best) {
				best = rank
			}
		}
		if best >= 0 {
			out = append(out, ranked{g, best})
		}
	}
	slices.SortStableFunc(out, func(a, b ranked) int { return a.rank - b.rank })
	groupsOut := make([]providerGroup, 0, len(out))
	for _, r := range out {
		groupsOut = append(groupsOut, r.group)
	}
	return groupsOut
}

// conflictHolds keeps a transaction on hold once two providers contradicted
// each other, so neither a third provider, a retry nor a user's claim can
// settle it until an administrator releases it. Held in memory: a restart
// clears it, and the next round asks the providers again.
var conflictHolds sync.Map // chain|hash -> VerificationAlert

// ReleaseConflict lifts the hold on a transaction after the administrator
// removed or fixed the faulty provider.
func ReleaseConflict(chainID, txHash, operator string) bool {
	hash, err := NormalizeTxHash(txHash)
	if err != nil {
		return false
	}
	_, released := conflictHolds.LoadAndDelete(strings.ToLower(chainID) + "|" + hash)
	if released {
		common.SysLog(common.LogText("USDT verification conflict released chain=%s tx=%s by=%s", chainID, hash, operator))
	}
	return released
}

// verificationChain returns a chain with a verified factory. A network
// switched off in the settings still verifies: users who saw its address
// before may keep sending to it, and that money has arrived.
func verificationChain(chainID string) (Chain, operation_setting.UsdtChainSetting, error) {
	chain, ok := LookupChain(chainID)
	if !ok {
		return Chain{}, operation_setting.UsdtChainSetting{}, ErrChainUnavailable
	}
	cfg := operation_setting.GetUsdtSetting().Chains[chain.ID]
	if !cfg.Verified {
		return chain, cfg, ErrChainUnavailable
	}
	return chain, cfg, nil
}

// FinalTransfers returns every USDT Transfer log of a successful, final,
// canonical transaction. Independent providers are asked two at a time; a
// provider that cannot answer is replaced by the next healthy one, and the
// answer counts only when two providers agree exactly. A single lying or
// compromised provider can delay a deposit but never invent one, and a third
// provider never outvotes a contradiction. The scanner's report is never
// trusted for amounts, recipients or finality.
func FinalTransfers(ctx context.Context, chainID, txHash string) ([]Transfer, error) {
	hash, err := NormalizeTxHash(txHash)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRejected, err)
	}
	chain, cfg, err := verificationChain(chainID)
	if err != nil {
		return nil, err
	}
	holdKey := chain.ID + "|" + hash
	if held, ok := conflictHolds.Load(holdKey); ok {
		alert := held.(VerificationAlert)
		return nil, &VerifyError{Reason: ReasonConflict, base: ErrNotReady,
			Attempts: []VerifyAttempt{{Provider: "-", Endpoint: "-", Outcome: "held", Detail: alert.Detail}}}
	}
	groups := groupProviders(chainNodes(cfg))
	if len(groups) < 2 {
		markStrained(chain.ID)
		return nil, &VerifyError{Reason: ReasonProvidersInsufficient, base: ErrNotReady}
	}
	candidates := availableProviders(chain.ID, groups)
	if len(candidates) < 2 {
		markStrained(chain.ID)
		return nil, &VerifyError{Reason: ReasonProvidersUnavailable, base: ErrNotReady,
			Attempts: []VerifyAttempt{{Provider: "-", Endpoint: "-", Outcome: string(evidenceFault),
				Detail: fmt.Sprintf("%d of %d providers are cooling down", len(groups)-len(candidates), len(groups))}}}
	}

	ctx, cancel := context.WithTimeout(ctx, verifyRoundBudget)
	defer cancel()
	limit := min(len(candidates), verifyMaxProviders)
	results := make(chan nodeReport, limit)
	var final, failed []nodeReport
	var attempts []VerifyAttempt
	next, inFlight, pending, mined := 0, 0, 0, 0
	for {
		for inFlight < verifyParallel && next < limit && ctx.Err() == nil {
			group := candidates[next]
			next++
			inFlight++
			go func() {
				select {
				case rpcSlots <- struct{}{}:
					defer func() { <-rpcSlots }()
					results <- providerEvidence(ctx, chain, cfg, group, hash)
				case <-ctx.Done():
					results <- nodeReport{kind: evidenceFault, provider: group.Provider, endpoint: "-", detail: "verification budget exhausted"}
				}
			}()
		}
		if inFlight == 0 {
			break
		}
		report := <-results
		inFlight--
		attempts = append(attempts, report.attempt())
		switch report.kind {
		case evidenceFinal, evidenceFailedTx:
			for _, other := range append(append([]nodeReport{}, final...), failed...) {
				if fields := conflictingFields(other, report); fields != "" {
					detail := fmt.Sprintf("%s and %s disagree on %s", providerDisplayName(other.provider), providerDisplayName(report.provider), fields)
					conflictHolds.Store(holdKey, recordMismatch(chain.ID, hash, detail))
					return nil, &VerifyError{Reason: ReasonConflict, Attempts: attempts, base: ErrNotReady}
				}
			}
			if report.kind == evidenceFinal {
				final = append(final, report)
			} else {
				failed = append(failed, report)
			}
		case evidencePending:
			pending++
			if report.mined {
				mined++
			}
		}
		switch {
		case len(final) >= 2:
			strainedAt.Delete(chain.ID)
			return final[0].transfers, nil
		case len(failed) >= 2:
			return nil, &VerifyError{Reason: ReasonFailedOnChain, Attempts: attempts, base: ErrRejected}
		case mined >= 2 && len(final) == 0 && len(failed) == 0:
			// Two providers see the transaction mined but not final yet: that
			// is the chain, and asking more only costs requests. A provider
			// that does not find it at all may lag, so others are still asked.
			return nil, &VerifyError{Reason: ReasonAwaitingConfirmation, Attempts: attempts, base: ErrNotReady}
		}
	}
	// Out of providers or time. When two providers did answer, the chain is
	// what is missing; otherwise not enough providers could be reached.
	reason := ReasonProvidersUnavailable
	if len(final)+len(failed)+pending >= 2 {
		reason = ReasonAwaitingConfirmation
	} else {
		markStrained(chain.ID)
	}
	return nil, &VerifyError{Reason: reason, Attempts: attempts, base: ErrNotReady}
}

// conflictingFields names what two complete answers disagree on, or "".
func conflictingFields(a, b nodeReport) string {
	var fields []string
	if a.kind != b.kind {
		fields = append(fields, fmt.Sprintf("receipt status (%s / %s)", a.kind, b.kind))
	}
	if a.blockHash != b.blockHash {
		fields = append(fields, fmt.Sprintf("block hash (%s / %s)", a.blockHash, b.blockHash))
	}
	if !sameTransfers(a.transfers, b.transfers) {
		fields = append(fields, fmt.Sprintf("transfers (%s / %s)", describeTransfers(a.transfers), describeTransfers(b.transfers)))
	}
	return strings.Join(fields, ", ")
}

func describeTransfers(list []Transfer) string {
	if len(list) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(list))
	for _, t := range list {
		parts = append(parts, fmt.Sprintf("#%d %s->%s %s@%d", t.LogIndex, Hex20(t.From), Hex20(t.To), t.Amount, t.BlockNumber))
	}
	return strings.Join(parts, " ")
}

func sameTransfers(a, b []Transfer) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].LogIndex != b[i].LogIndex || a[i].From != b[i].From || a[i].To != b[i].To ||
			a[i].BlockNumber != b[i].BlockNumber || a[i].Amount.Cmp(b[i].Amount) != 0 {
			return false
		}
	}
	return true
}

// VerificationAlert records a disagreement between two verification
// providers: one is lying, compromised or broken, and the deposit waits.
type VerificationAlert struct {
	At     int64  `json:"at"`
	Chain  string `json:"chain"`
	TxHash string `json:"tx_hash"`
	Detail string `json:"detail"`
}

var alerts struct {
	mu   sync.Mutex
	list []VerificationAlert
}

func recordMismatch(chain, hash, detail string) VerificationAlert {
	common.SysError(common.LogText("USDT verification nodes disagree chain=%s tx=%s detail=%s", chain, hash, detail))
	alert := VerificationAlert{At: time.Now().Unix(), Chain: chain, TxHash: hash, Detail: detail}
	alerts.mu.Lock()
	defer alerts.mu.Unlock()
	alerts.list = append(alerts.list, alert)
	if len(alerts.list) > 20 {
		alerts.list = alerts.list[len(alerts.list)-20:]
	}
	return alert
}

// RecentVerificationAlerts returns the latest disagreements, oldest first.
func RecentVerificationAlerts() []VerificationAlert {
	alerts.mu.Lock()
	defer alerts.mu.Unlock()
	return append([]VerificationAlert(nil), alerts.list...)
}

// asFault keeps an endpoint fault as is and turns a malformed or missing
// answer to a call that must succeed (chain id, finality) into one, so a
// broken endpoint never counts as a "not final yet" answer.
func asFault(method string, err error) error {
	var fault *rpcFault
	if errors.As(err, &fault) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return &rpcFault{Kind: faultUnsupported, Method: method, Detail: err.Error()}
}

// checkFinal returns nil when block is final on this endpoint, an *rpcFault
// when the endpoint could not tell, and a plain error when it is not final.
// It never falls back to a weaker finality rule than the configured one.
func checkFinal(ctx context.Context, client *rpcClient, cfg operation_setting.UsdtChainSetting, block uint64) error {
	switch cfg.Finality {
	case operation_setting.UsdtFinalityFinalized, operation_setting.UsdtFinalitySafe:
		// A node without the configured tag cannot vouch for finality; it is
		// an endpoint fault, never a reason to fall back to a weaker rule.
		tagged, err := client.blockByNumber(ctx, cfg.Finality)
		if err != nil {
			return asFault("eth_getBlockByNumber", err)
		}
		height, err := parseQuantity(tagged.Number)
		if err != nil {
			return asFault("eth_getBlockByNumber", err)
		}
		if height < block {
			return fmt.Errorf("%s block %d is below %d", cfg.Finality, height, block)
		}
		return nil
	default:
		head, err := client.blockNumber(ctx)
		if err != nil {
			return asFault("eth_blockNumber", err)
		}
		required := uint64(max(cfg.Confirmations, 1))
		if head < block || head-block < required {
			return fmt.Errorf("%d of %d confirmations", max(int64(head)-int64(block), 0), required)
		}
		return nil
	}
}

var (
	selImplementation = keccak256([]byte("implementation()"))[:4]
	selCreate2Prefix  = keccak256([]byte("create2Prefix()"))[:4]
	selDestination    = keccak256([]byte("destination()"))[:4]
	selWalletAddress  = keccak256([]byte("walletAddress(bytes32)"))[:4]
	selDeployWallet   = keccak256([]byte("deployWallet(bytes32)"))[:4]
)

// ProbeSalt is the salt the deploy tool deploys for real right after the
// factory, so chains whose nodes cannot simulate CREATE2 still prove the
// address formula with an on-chain wallet.
var ProbeSalt = func() [32]byte {
	var s [32]byte
	copy(s[:], keccak256([]byte("new-api-usdt:probe")))
	return s
}()

func callAddress(ctx context.Context, client *rpcClient, to [20]byte, data []byte) ([20]byte, error) {
	var out [20]byte
	raw, err := client.ethCall(ctx, to, data)
	if err != nil {
		return out, err
	}
	if len(raw) != 32 {
		return out, errors.New("unexpected call result")
	}
	copy(out[:], raw[12:])
	return out, nil
}

// FactoryCheck is what the admin sees after verifying a factory.
type FactoryCheck struct {
	Implementation [20]byte
	Destination    [20]byte
}

// VerifyFactory proves, through two independent providers of the network's
// node list, that the configured factory is a DepositFactory for this chain
// and that new-api computes exactly the addresses it deploys: local CREATE2
// == walletAddress() == the address deployWallet() creates. A provider that
// fails is replaced by the next one.
func VerifyFactory(ctx context.Context, chainID, factoryAddr string) (FactoryCheck, error) {
	chain, ok := LookupChain(chainID)
	if !ok {
		return FactoryCheck{}, ErrChainUnavailable
	}
	groups := groupProviders(chainNodes(operation_setting.GetUsdtSetting().Chains[chain.ID]))
	if len(groups) < 2 {
		return FactoryCheck{}, errors.New("two independent verification providers are required")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	var checks []FactoryCheck
	var names, failures []string
	for _, group := range groups {
		if len(checks) == 2 || ctx.Err() != nil {
			break
		}
		var lastErr error
		for _, node := range group.Nodes {
			client := &rpcClient{url: node.URL, health: healthFor(chain.ID, node.URL)}
			check, err := verifyFactoryOn(ctx, chain.ID, factoryAddr, client)
			if err == nil {
				checks = append(checks, check)
				names = append(names, providerDisplayName(group.Provider))
				lastErr = nil
				break
			}
			lastErr = fmt.Errorf("%s [%s]: %w", providerDisplayName(group.Provider), maskedRPCURL(node.URL), err)
		}
		if lastErr != nil {
			failures = append(failures, lastErr.Error())
		}
	}
	if len(checks) < 2 {
		return FactoryCheck{}, fmt.Errorf("fewer than two providers could verify the factory: %s", strings.Join(failures, "; "))
	}
	if checks[0] != checks[1] {
		return checks[0], fmt.Errorf("%s and %s report a different factory", names[0], names[1])
	}
	return checks[0], nil
}

func verifyFactoryOn(ctx context.Context, chainID, factoryAddr string, client *rpcClient) (FactoryCheck, error) {
	var result FactoryCheck
	chain, ok := LookupChain(chainID)
	if !ok {
		return result, ErrChainUnavailable
	}
	factory, err := ParseAddress(chain, factoryAddr)
	if err != nil {
		return result, err
	}
	if err := checkChainID(ctx, client, chain); err != nil {
		return result, err
	}

	prefix, err := client.ethCall(ctx, factory, selCreate2Prefix)
	if err != nil {
		return result, fmt.Errorf("read create2Prefix: %w", err)
	}
	if len(prefix) != 32 || prefix[0] != chain.Create2Prefix {
		return result, fmt.Errorf("factory create2Prefix does not match %s", chain.ID)
	}
	impl, err := callAddress(ctx, client, factory, selImplementation)
	if err != nil {
		return result, fmt.Errorf("read implementation: %w", err)
	}
	dest, err := callAddress(ctx, client, factory, selDestination)
	if err != nil {
		return result, fmt.Errorf("read destination: %w", err)
	}
	if dest == ([20]byte{}) {
		return result, errors.New("factory destination is empty")
	}

	var randomSalt [32]byte
	if _, err := rand.Read(randomSalt[:]); err != nil {
		return result, err
	}
	predicted, err := callAddress(ctx, client, factory, append(append([]byte{}, selWalletAddress...), randomSalt[:]...))
	if err != nil {
		return result, fmt.Errorf("read walletAddress: %w", err)
	}
	if local := WalletAddress(chain.Create2Prefix, factory, impl, randomSalt); local != predicted {
		return result, errors.New("local address derivation differs from the factory")
	}
	deployed, simErr := callAddress(ctx, client, factory, append(append([]byte{}, selDeployWallet...), randomSalt[:]...))
	if simErr == nil && deployed != predicted {
		return result, errors.New("simulated deployment address differs from the prediction")
	}
	if simErr != nil {
		// Some nodes cannot simulate CREATE2. Fall back to the probe wallet the
		// deploy tool created for real: code at the predicted address proves the
		// contract's own create2 check passed on this chain.
		probe := WalletAddress(chain.Create2Prefix, factory, impl, ProbeSalt)
		code, err := client.code(ctx, probe)
		if err != nil {
			return result, fmt.Errorf("simulate deployWallet: %v; read probe wallet: %w", simErr, err)
		}
		if strings.TrimPrefix(code, "0x") == "" {
			return result, fmt.Errorf("simulate deployWallet failed (%v) and the probe wallet is not deployed; run the deploy tool's probe step", simErr)
		}
	}
	result.Implementation = impl
	result.Destination = dest
	return result, nil
}
