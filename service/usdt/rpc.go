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
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// rpcRequestTimeout bounds one JSON-RPC request; a round of verification has
// its own, larger budget (verifyRoundBudget).
const rpcRequestTimeout = 5 * time.Second

var rpcHTTPClient = &http.Client{}

// rpcClient speaks Ethereum JSON-RPC to one endpoint. TRON nodes expose the
// same methods on /jsonrpc, with 20-byte hex addresses, so one client serves
// all four chains. Failover between endpoints and providers happens above it,
// so every answer of one piece of evidence comes from the same endpoint.
type rpcClient struct {
	url string
	// health, when set, records each call's outcome for this endpoint.
	health *endpointHealth
}

func newRPCClient(url string) (*rpcClient, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, errors.New("no RPC endpoint configured")
	}
	return &rpcClient{url: url}, nil
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

// rpcFaultKind classifies why an endpoint could not answer. It decides how
// long the endpoint rests before it is asked again.
type rpcFaultKind string

const (
	faultRateLimited  rpcFaultKind = "rate_limited"
	faultDenied       rpcFaultKind = "denied"
	faultUnavailable  rpcFaultKind = "unavailable"
	faultRPCError     rpcFaultKind = "rpc_error"
	faultUnsupported  rpcFaultKind = "unsupported"
	faultWrongNetwork rpcFaultKind = "wrong_network"
)

// rpcFault is an endpoint failure. Its text never contains the endpoint URL,
// which can carry an API key.
type rpcFault struct {
	Kind       rpcFaultKind
	Method     string
	Detail     string
	RetryAfter time.Duration
}

func (e *rpcFault) Error() string { return e.Method + ": " + e.Detail }

// call posts one request and decodes its result. Cancellation of ctx by the
// caller is returned as is and never counted against the endpoint.
func (c *rpcClient) call(ctx context.Context, method string, params []any, out any) error {
	body, err := common.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return err
	}
	err = c.exchange(ctx, method, body, out)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if c.health != nil {
		c.health.record(method, err)
	}
	return err
}

func (c *rpcClient) exchange(ctx context.Context, method string, body []byte, out any) error {
	reqCtx, cancel := context.WithTimeout(ctx, rpcRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return &rpcFault{Kind: faultUnavailable, Method: method, Detail: "invalid endpoint URL"}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := rpcHTTPClient.Do(req)
	if err != nil {
		detail := "connection failed"
		if reqCtx.Err() != nil {
			detail = "timeout"
		}
		return &rpcFault{Kind: faultUnavailable, Method: method, Detail: detail}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return &rpcFault{Kind: faultUnavailable, Method: method, Detail: "read failed"}
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return &rpcFault{Kind: faultRateLimited, Method: method, Detail: "HTTP 429",
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return &rpcFault{Kind: faultDenied, Method: method, Detail: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	case resp.StatusCode != http.StatusOK:
		return &rpcFault{Kind: faultUnavailable, Method: method, Detail: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	var decoded rpcResponse
	if err := common.Unmarshal(raw, &decoded); err != nil {
		return &rpcFault{Kind: faultUnavailable, Method: method, Detail: "invalid JSON response"}
	}
	if decoded.Error != nil {
		kind := faultRPCError
		// -32601 method not found; -32602 often an unsupported block tag.
		if decoded.Error.Code == -32601 || decoded.Error.Code == -32602 {
			kind = faultUnsupported
		}
		if decoded.Error.Code == 429 || decoded.Error.Code == -32005 {
			kind = faultRateLimited
		}
		return &rpcFault{Kind: kind, Method: method, Detail: fmt.Sprintf("RPC error %d: %s", decoded.Error.Code, truncate(decoded.Error.Message, 120))}
	}
	if out == nil {
		return nil
	}
	if err := common.Unmarshal(decoded.Result, out); err != nil {
		return &rpcFault{Kind: faultUnavailable, Method: method, Detail: "invalid result"}
	}
	return nil
}

// parseRetryAfter reads delay-seconds or an HTTP date; zero means absent or
// invalid.
func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
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
	TransactionHash string   `json:"transactionHash"`
	Status          string   `json:"status"`
	BlockNumber     string   `json:"blockNumber"`
	BlockHash       string   `json:"blockHash"`
	Logs            []rpcLog `json:"logs"`
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
