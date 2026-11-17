package usdt

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

var rpcHTTPClient = &http.Client{Timeout: 15 * time.Second}

// rpcClient speaks Ethereum JSON-RPC. TRON nodes expose the same methods on
// /jsonrpc, with 20-byte hex addresses, so one client serves all four chains.
type rpcClient struct {
	endpoints []string
}

func newRPCClient(urls string) (*rpcClient, error) {
	var endpoints []string
	for raw := range strings.SplitSeq(urls, ",") {
		if u := strings.TrimSpace(raw); u != "" {
			endpoints = append(endpoints, u)
		}
	}
	if len(endpoints) == 0 {
		return nil, errors.New("no RPC endpoint configured")
	}
	return &rpcClient{endpoints: endpoints}, nil
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// call tries each endpoint in order and decodes the first successful result.
// Endpoint URLs can carry API keys, so errors never include them.
func (c *rpcClient) call(ctx context.Context, method string, params []any, out any) error {
	body, err := common.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return err
	}
	var lastErr error
	for i, endpoint := range c.endpoints {
		raw, err := c.post(ctx, endpoint, body)
		if err != nil {
			lastErr = fmt.Errorf("%s via endpoint %d: %w", method, i+1, err)
			continue
		}
		var resp rpcResponse
		if err := common.Unmarshal(raw, &resp); err != nil {
			lastErr = fmt.Errorf("%s via endpoint %d: invalid response", method, i+1)
			continue
		}
		if resp.Error != nil {
			lastErr = fmt.Errorf("%s via endpoint %d: rpc error %d: %s", method, i+1, resp.Error.Code, resp.Error.Message)
			continue
		}
		if out == nil {
			return nil
		}
		if err := common.Unmarshal(resp.Result, out); err != nil {
			lastErr = fmt.Errorf("%s via endpoint %d: decode result: %w", method, i+1, err)
			continue
		}
		return nil
	}
	return lastErr
}

func (c *rpcClient) post(ctx context.Context, endpoint string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("invalid endpoint URL")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := rpcHTTPClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("request failed")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, errors.New("read failed")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return raw, nil
}

func parseQuantity(s string) (uint64, error) {
	raw, ok := strings.CutPrefix(strings.TrimSpace(s), "0x")
	if !ok || raw == "" {
		return 0, fmt.Errorf("invalid quantity %q", s)
	}
	n, ok := new(big.Int).SetString(raw, 16)
	if !ok || !n.IsUint64() {
		return 0, fmt.Errorf("invalid quantity %q", s)
	}
	return n.Uint64(), nil
}

func (c *rpcClient) chainID(ctx context.Context) (uint64, error) {
	var out string
	if err := c.call(ctx, "eth_chainId", []any{}, &out); err != nil {
		return 0, err
	}
	return parseQuantity(out)
}

type rpcBlock struct {
	Number string `json:"number"`
	Hash   string `json:"hash"`
}

// blockByNumber accepts a hex height or a tag such as "finalized".
func (c *rpcClient) blockByNumber(ctx context.Context, tag string) (*rpcBlock, error) {
	var out *rpcBlock
	if err := c.call(ctx, "eth_getBlockByNumber", []any{tag, false}, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, errors.New("block not found")
	}
	return out, nil
}

func (c *rpcClient) blockNumber(ctx context.Context) (uint64, error) {
	var out string
	if err := c.call(ctx, "eth_blockNumber", []any{}, &out); err != nil {
		return 0, err
	}
	return parseQuantity(out)
}

type rpcLog struct {
	Address  string   `json:"address"`
	Topics   []string `json:"topics"`
	Data     string   `json:"data"`
	LogIndex string   `json:"logIndex"`
	Removed  bool     `json:"removed"`
}

type rpcReceipt struct {
	Status      string   `json:"status"`
	BlockNumber string   `json:"blockNumber"`
	BlockHash   string   `json:"blockHash"`
	Logs        []rpcLog `json:"logs"`
}

func (c *rpcClient) receipt(ctx context.Context, txHash string) (*rpcReceipt, error) {
	var out *rpcReceipt
	if err := c.call(ctx, "eth_getTransactionReceipt", []any{txHash}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *rpcClient) ethCall(ctx context.Context, to [20]byte, data []byte) ([]byte, error) {
	var out string
	err := c.call(ctx, "eth_call", []any{map[string]string{
		"to":   Hex20(to),
		"data": "0x" + hex.EncodeToString(data),
	}, "latest"}, &out)
	if err != nil {
		return nil, err
	}
	b, err := hex.DecodeString(strings.TrimPrefix(out, "0x"))
	if err != nil {
		return nil, errors.New("invalid eth_call result")
	}
	return b, nil
}

func (c *rpcClient) code(ctx context.Context, address [20]byte) (string, error) {
	var out string
	if err := c.call(ctx, "eth_getCode", []any{Hex20(address), "latest"}, &out); err != nil {
		return "", err
	}
	return out, nil
}
