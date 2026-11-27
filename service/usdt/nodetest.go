package usdt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// NodeCheck is one step of an endpoint test.
type NodeCheck struct {
	Method string `json:"method"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// NodeTestResult reports whether an endpoint can verify deposits, method by
// method, as seen from this new-api process.
type NodeTestResult struct {
	NodeID   string      `json:"node_id"`
	Provider string      `json:"provider"`
	Endpoint string      `json:"endpoint"`
	OK       bool        `json:"ok"`
	Checks   []NodeCheck `json:"checks"`
	Millis   int64       `json:"millis"`
}

type rpcBlockWithTxs struct {
	Number       string   `json:"number"`
	Hash         string   `json:"hash"`
	Transactions []string `json:"transactions"`
}

// testEndpoint runs every call verification needs and checks that the answers
// fit together: the chain id, the configured finality, the final block by
// height, and a receipt of a transaction in that block pointing back to it.
// An HTTP 200, a right chain id or an empty result alone proves nothing.
func testEndpoint(ctx context.Context, chain Chain, cfg operation_setting.UsdtChainSetting, node operation_setting.UsdtRPCNode) (result NodeTestResult) {
	started := time.Now()
	health := healthFor(chain.ID, node.URL)
	client := &rpcClient{url: node.URL, health: health}
	result = NodeTestResult{NodeID: node.ID, Provider: providerDisplayName(nodeProvider(node)), Endpoint: maskedRPCURL(node.URL)}
	add := func(method string, err error, detail string) bool {
		check := NodeCheck{Method: method, OK: err == nil, Detail: detail}
		if err != nil {
			check.Detail = err.Error()
		}
		result.Checks = append(result.Checks, check)
		return err == nil
	}
	defer func() {
		result.Millis = time.Since(started).Milliseconds()
		result.OK = len(result.Checks) > 0
		for _, c := range result.Checks {
			result.OK = result.OK && c.OK
		}
		if result.OK {
			health.markHealthy()
		}
	}()

	id, err := client.chainID(ctx)
	if err == nil && id != chain.ChainID {
		fault := &rpcFault{Kind: faultWrongNetwork, Method: "eth_chainId", Detail: fmt.Sprintf("chain id %d, expected %d", id, chain.ChainID)}
		health.record("eth_chainId", fault)
		err = fault
	}
	if !add("eth_chainId", err, fmt.Sprintf("%d", id)) {
		return result
	}

	var height uint64
	switch cfg.Finality {
	case operation_setting.UsdtFinalityFinalized, operation_setting.UsdtFinalitySafe:
		tagged, err := client.blockByNumber(ctx, cfg.Finality)
		if err == nil {
			height, err = parseQuantity(tagged.Number)
		}
		if !add("eth_getBlockByNumber("+cfg.Finality+")", err, fmt.Sprintf("%d", height)) {
			return result
		}
	default:
		head, err := client.blockNumber(ctx)
		required := uint64(max(cfg.Confirmations, 1))
		if err == nil && head <= required {
			err = errors.New("head is below the confirmation depth")
		}
		if !add("eth_blockNumber", err, fmt.Sprintf("%d", head)) {
			return result
		}
		height = head - required
	}

	// Walk back from the final block to one with a transaction (bounded).
	var block *rpcBlockWithTxs
	for step := range 5 {
		var out *rpcBlockWithTxs
		err := client.call(ctx, "eth_getBlockByNumber", []any{fmt.Sprintf("0x%x", height-uint64(step)), false}, &out)
		if err == nil && out == nil {
			err = errors.New("block not found")
		}
		if err == nil {
			if n, perr := parseQuantity(out.Number); perr != nil || n != height-uint64(step) || out.Hash == "" {
				err = errors.New("returned another block")
			}
		}
		if err != nil {
			add("eth_getBlockByNumber(height)", err, "")
			return result
		}
		if len(out.Transactions) > 0 {
			block = out
			break
		}
	}
	if block == nil {
		add("eth_getBlockByNumber(height)", errors.New("no transaction in the last 5 final blocks"), "")
		return result
	}
	number, _ := parseQuantity(block.Number)
	add("eth_getBlockByNumber(height)", nil, fmt.Sprintf("%d", number))

	tx := block.Transactions[0]
	receipt, err := client.receipt(ctx, tx)
	if err == nil && receipt == nil {
		err = errors.New("no receipt for a final transaction")
	}
	if err == nil {
		receiptBlock, perr := parseQuantity(receipt.BlockNumber)
		if perr != nil || receiptBlock != number || !strings.EqualFold(receipt.BlockHash, block.Hash) {
			err = errors.New("receipt points to another block")
		}
	}
	add("eth_getTransactionReceipt", err, tx)
	return result
}

// TestNodes tests a network's saved endpoints (one, when nodeID is set) from
// this process, a few at a time.
func TestNodes(ctx context.Context, chainID, nodeID string) ([]NodeTestResult, error) {
	chain, ok := LookupChain(chainID)
	if !ok {
		return nil, ErrChainUnavailable
	}
	cfg := operation_setting.GetUsdtSetting().Chains[chain.ID]
	var nodes []operation_setting.UsdtRPCNode
	for _, n := range chainNodes(cfg) {
		if nodeID == "" || n.ID == nodeID {
			nodes = append(nodes, n)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	results := make([]NodeTestResult, len(nodes))
	slots := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, node := range nodes {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			results[i] = testEndpoint(ctx, chain, cfg, node)
		})
	}
	wg.Wait()
	return results, nil
}
