package usdt

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

var (
	// ErrNotReady means the transaction may still become creditable: it is not
	// mined, not final yet, or the node could not be reached. Retry later.
	ErrNotReady = errors.New("transaction is not final yet")
	// ErrRejected means the transaction can never be credited.
	ErrRejected = errors.New("transaction cannot be credited")
	// ErrChainUnavailable means the chain is disabled or not configured.
	ErrChainUnavailable = errors.New("USDT network is not available")
)

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

// verificationNodes returns the two independent verification RPCs of a
// chain with a verified factory. A network switched off in the settings still
// verifies: users who saw its address before may keep sending to it, and that
// money has arrived.
func verificationNodes(ctx context.Context, chainID string) (Chain, operation_setting.UsdtChainSetting, [2]*rpcClient, error) {
	var nodes [2]*rpcClient
	chain, ok := LookupChain(chainID)
	if !ok {
		return Chain{}, operation_setting.UsdtChainSetting{}, nodes, ErrChainUnavailable
	}
	cfg := operation_setting.GetUsdtSetting().Chains[chain.ID]
	if !cfg.Verified {
		return chain, cfg, nodes, ErrChainUnavailable
	}
	for i, urls := range [2]string{cfg.RPCURL, cfg.SecondaryRPCURL} {
		client, err := newRPCClient(urls)
		if err != nil {
			// Without both providers nothing is credited; the deposit waits.
			return chain, cfg, nodes, fmt.Errorf("%w: verification node %d is not configured", ErrNotReady, i+1)
		}
		if err := checkChainID(ctx, client, urls, chain); err != nil {
			return chain, cfg, nodes, err
		}
		nodes[i] = client
	}
	return chain, cfg, nodes, nil
}

var chainIDCache sync.Map // rpc url list -> time.Time of last successful check

// checkChainID refuses an RPC that serves a different network, which would
// otherwise make a testnet or another chain's transfer look like a deposit.
func checkChainID(ctx context.Context, client *rpcClient, key string, chain Chain) error {
	if at, ok := chainIDCache.Load(key + "|" + chain.ID); ok && time.Since(at.(time.Time)) < 10*time.Minute {
		return nil
	}
	id, err := client.chainID(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotReady, err)
	}
	if id != chain.ChainID {
		return fmt.Errorf("RPC chain id %d does not match %s (%d)", id, chain.ID, chain.ChainID)
	}
	chainIDCache.Store(key+"|"+chain.ID, time.Now())
	return nil
}

// nodeReport is what one verification node says about a transaction.
type nodeReport struct {
	blockHash string
	transfers []Transfer
	err       error
}

// FinalTransfers returns every USDT Transfer log of a successful, final,
// canonical transaction. Two independent providers are asked, and the answer
// counts only when both agree exactly: a single lying or compromised provider
// can delay a deposit but never invent one. The scanner's report is never
// trusted for amounts, recipients or finality.
func FinalTransfers(ctx context.Context, chainID, txHash string) ([]Transfer, error) {
	hash, err := NormalizeTxHash(txHash)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRejected, err)
	}
	chain, cfg, nodes, err := verificationNodes(ctx, chainID)
	if err != nil {
		return nil, err
	}
	var reports [2]nodeReport
	var wg sync.WaitGroup
	for i, node := range nodes {
		wg.Go(func() { reports[i] = transfersFromNode(ctx, chain, cfg, node, hash) })
	}
	wg.Wait()

	primary, secondary := reports[0], reports[1]
	primaryRejected, secondaryRejected := errors.Is(primary.err, ErrRejected), errors.Is(secondary.err, ErrRejected)
	switch {
	case primary.err == nil && secondary.err == nil:
		if primary.blockHash != secondary.blockHash || !sameTransfers(primary.transfers, secondary.transfers) {
			recordMismatch(chain.ID, hash, "the two verification nodes report different transfers")
			return nil, fmt.Errorf("%w: verification nodes disagree", ErrNotReady)
		}
		return primary.transfers, nil
	case primaryRejected && secondaryRejected:
		return nil, primary.err
	case (primary.err == nil && secondaryRejected) || (secondary.err == nil && primaryRejected):
		recordMismatch(chain.ID, hash, "one verification node reports success, the other a failed transaction")
		return nil, fmt.Errorf("%w: verification nodes disagree", ErrNotReady)
	case primary.err != nil && !primaryRejected:
		// Not found or not final yet on this node: wait for it to catch up.
		return nil, primary.err
	default:
		return nil, secondary.err
	}
}

// transfersFromNode reads one transaction through one node: the receipt must
// show success, the block must be final on that node and still canonical.
func transfersFromNode(ctx context.Context, chain Chain, cfg operation_setting.UsdtChainSetting, client *rpcClient, hash string) nodeReport {
	receipt, err := client.receipt(ctx, hash)
	if err != nil {
		return nodeReport{err: fmt.Errorf("%w: %v", ErrNotReady, err)}
	}
	if receipt == nil {
		return nodeReport{err: fmt.Errorf("%w: transaction not found", ErrNotReady)}
	}
	if receipt.Status != "0x1" {
		return nodeReport{err: fmt.Errorf("%w: transaction failed on chain", ErrRejected)}
	}
	block, err := parseQuantity(receipt.BlockNumber)
	if err != nil {
		return nodeReport{err: fmt.Errorf("%w: %v", ErrNotReady, err)}
	}
	if err := checkFinal(ctx, client, cfg, block); err != nil {
		return nodeReport{err: err}
	}
	canonical, err := client.blockByNumber(ctx, fmt.Sprintf("0x%x", block))
	if err != nil {
		return nodeReport{err: fmt.Errorf("%w: %v", ErrNotReady, err)}
	}
	if !strings.EqualFold(canonical.Hash, receipt.BlockHash) {
		return nodeReport{err: fmt.Errorf("%w: block %d was reorganized", ErrNotReady, block)}
	}

	report := nodeReport{blockHash: strings.ToLower(receipt.BlockHash)}
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

// VerificationAlert records a disagreement between the two verification
// nodes: one provider is lying, compromised or broken, and the deposit waits.
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

func recordMismatch(chain, hash, detail string) {
	common.SysError(common.LogText("USDT verification nodes disagree chain=%s tx=%s detail=%s", chain, hash, detail))
	alerts.mu.Lock()
	defer alerts.mu.Unlock()
	alerts.list = append(alerts.list, VerificationAlert{At: time.Now().Unix(), Chain: chain, TxHash: hash, Detail: detail})
	if len(alerts.list) > 20 {
		alerts.list = alerts.list[len(alerts.list)-20:]
	}
}

// RecentVerificationAlerts returns the latest disagreements, oldest first.
func RecentVerificationAlerts() []VerificationAlert {
	alerts.mu.Lock()
	defer alerts.mu.Unlock()
	return append([]VerificationAlert(nil), alerts.list...)
}

func checkFinal(ctx context.Context, client *rpcClient, cfg operation_setting.UsdtChainSetting, block uint64) error {
	switch cfg.Finality {
	case operation_setting.UsdtFinalityFinalized, operation_setting.UsdtFinalitySafe:
		tagged, err := client.blockByNumber(ctx, cfg.Finality)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrNotReady, err)
		}
		height, err := parseQuantity(tagged.Number)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrNotReady, err)
		}
		if height < block {
			return fmt.Errorf("%w: %s block %d is below %d", ErrNotReady, cfg.Finality, height, block)
		}
		return nil
	default:
		head, err := client.blockNumber(ctx)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrNotReady, err)
		}
		required := uint64(max(cfg.Confirmations, 1))
		if head < block || head-block < required {
			return fmt.Errorf("%w: %d of %d confirmations", ErrNotReady, max(int64(head)-int64(block), 0), required)
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

// VerifyFactory proves that the configured factory is a DepositFactory for
// this chain and that new-api computes exactly the addresses it deploys:
// local CREATE2 == walletAddress() == the address deployWallet() creates.
func VerifyFactory(ctx context.Context, chainID, factoryAddr, primaryURL, secondaryURL string) (FactoryCheck, error) {
	if err := DistinctProviders(primaryURL, secondaryURL); err != nil {
		return FactoryCheck{}, err
	}
	first, err := verifyFactoryOn(ctx, chainID, factoryAddr, primaryURL)
	if err != nil {
		return first, fmt.Errorf("verification node 1: %w", err)
	}
	second, err := verifyFactoryOn(ctx, chainID, factoryAddr, secondaryURL)
	if err != nil {
		return second, fmt.Errorf("verification node 2: %w", err)
	}
	if first != second {
		return first, errors.New("the two verification nodes report a different factory")
	}
	return first, nil
}

// DistinctProviders requires the two verification nodes to share no host, so
// one provider cannot answer for both.
func DistinctProviders(primaryURL, secondaryURL string) error {
	hosts := map[string]bool{}
	for raw := range strings.SplitSeq(primaryURL, ",") {
		if u, err := url.Parse(strings.TrimSpace(raw)); err == nil && u.Hostname() != "" {
			hosts[strings.ToLower(u.Hostname())] = true
		}
	}
	if len(hosts) == 0 || strings.TrimSpace(secondaryURL) == "" {
		return errors.New("both verification nodes must be configured")
	}
	for raw := range strings.SplitSeq(secondaryURL, ",") {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || u.Hostname() == "" {
			return errors.New("invalid secondary verification node URL")
		}
		if hosts[strings.ToLower(u.Hostname())] {
			return errors.New("the two verification nodes must use different providers")
		}
	}
	return nil
}

func verifyFactoryOn(ctx context.Context, chainID, factoryAddr, rpcURL string) (FactoryCheck, error) {
	var result FactoryCheck
	chain, ok := LookupChain(chainID)
	if !ok {
		return result, ErrChainUnavailable
	}
	factory, err := ParseAddress(chain, factoryAddr)
	if err != nil {
		return result, err
	}
	client, err := newRPCClient(rpcURL)
	if err != nil {
		return result, err
	}
	if err := checkChainID(ctx, client, rpcURL, chain); err != nil {
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
