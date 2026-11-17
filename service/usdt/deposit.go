package usdt

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
)

var rateHTTPClient = &http.Client{Timeout: 8 * time.Second}

var rateCache struct {
	mu     sync.Mutex
	value  decimal.Decimal
	source string
	at     time.Time
}

// rateBounds rejects a feed answer that cannot be a USDT price. A real depeg
// inside the bounds is credited at the market price, which is the point of
// market mode; anything outside needs the operator to switch to fixed mode.
var (
	rateMin = decimal.NewFromFloat(0.8)
	rateMax = decimal.NewFromFloat(1.2)
)

// CurrentRate returns the USD quota credited per USDT and where it came from.
func CurrentRate(ctx context.Context) (decimal.Decimal, string, error) {
	setting := operation_setting.GetUsdtSetting()
	if setting.RateMode == operation_setting.UsdtRateFixed {
		rate := decimal.NewFromFloat(setting.FixedRate)
		if rate.Sign() <= 0 {
			return decimal.Zero, "", errors.New("fixed USDT rate is not configured")
		}
		return rate, "fixed", nil
	}

	rateCache.mu.Lock()
	defer rateCache.mu.Unlock()
	if !rateCache.at.IsZero() && time.Since(rateCache.at) < time.Minute {
		return rateCache.value, rateCache.source, nil
	}
	var errs []error
	for _, feed := range []struct {
		name  string
		fetch func(context.Context) (decimal.Decimal, error)
	}{{"coinbase", fetchCoinbaseRate}, {"kraken", fetchKrakenRate}} {
		rate, err := feed.fetch(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", feed.name, err))
			continue
		}
		if rate.LessThan(rateMin) || rate.GreaterThan(rateMax) {
			errs = append(errs, fmt.Errorf("%s: price %s outside sanity bounds", feed.name, rate))
			continue
		}
		rateCache.value, rateCache.source, rateCache.at = rate, feed.name, time.Now()
		return rate, feed.name, nil
	}
	return decimal.Zero, "", fmt.Errorf("%w: USDT market price unavailable: %v", ErrNotReady, errors.Join(errs...))
}

func getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := rateHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return common.DecodeJson(resp.Body, out)
}

func fetchCoinbaseRate(ctx context.Context) (decimal.Decimal, error) {
	var body struct {
		Data struct {
			Rates map[string]string `json:"rates"`
		} `json:"data"`
	}
	if err := getJSON(ctx, "https://api.coinbase.com/v2/exchange-rates?currency=USDT", &body); err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromString(body.Data.Rates["USD"])
}

func fetchKrakenRate(ctx context.Context) (decimal.Decimal, error) {
	var body struct {
		Error  []string `json:"error"`
		Result map[string]struct {
			C []string `json:"c"`
		} `json:"result"`
	}
	if err := getJSON(ctx, "https://api.kraken.com/0/public/Ticker?pair=USDTUSD", &body); err != nil {
		return decimal.Zero, err
	}
	for _, ticker := range body.Result {
		if len(ticker.C) > 0 {
			return decimal.NewFromString(ticker.C[0])
		}
	}
	return decimal.Zero, errors.New("empty ticker")
}

// NetworkInfo describes a network to the user-facing page.
type NetworkInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MinAmount string `json:"min_amount"`
	Contract  string `json:"contract"`
	TxURL     string `json:"tx_url"`
}

// UserNetworks lists the networks a user may deposit on, in display order.
func UserNetworks() []NetworkInfo {
	setting := operation_setting.GetUsdtSetting()
	if !setting.Enabled {
		return nil
	}
	var out []NetworkInfo
	for _, id := range operation_setting.UsdtChainOrder {
		cfg := setting.Chains[id]
		chain, ok := LookupChain(id)
		if !ok || !cfg.Enabled || !cfg.Verified {
			continue
		}
		out = append(out, NetworkInfo{
			ID: id, Name: chain.Name, MinAmount: decimal.NewFromFloat(cfg.MinAmount).String(),
			Contract: DisplayAddress(chain, chain.USDT), TxURL: chain.TxURL,
		})
	}
	return out
}

// EnsureUserAddress returns the user's permanent wallet on chainID, creating
// the record on first request.
func EnsureUserAddress(chainID string, userID int) (string, error) {
	setting := operation_setting.GetUsdtSetting()
	chain, ok := LookupChain(chainID)
	if !ok || !setting.Enabled {
		return "", ErrChainUnavailable
	}
	cfg := setting.Chains[chain.ID]
	if !cfg.Enabled || !cfg.Verified {
		return "", ErrChainUnavailable
	}
	existing, err := model.GetUsdtDepositAddress(userID, chain.ID)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return existing.Address, nil
	}
	secret, err := ParseHex32(setting.SaltSecret)
	if err != nil {
		return "", errors.New("USDT salt secret is not configured")
	}
	factory, errFactory := ParseHex20(cfg.FactoryAddress)
	impl, errImpl := ParseHex20(cfg.ImplementationAddress)
	if errFactory != nil || errImpl != nil {
		return "", ErrChainUnavailable
	}
	salt := Salt(secret, userID)
	wallet := WalletAddress(chain.Create2Prefix, factory, impl, salt)
	row, err := model.CreateUsdtDepositAddress(&model.UsdtDepositAddress{
		UserId: userID, Chain: chain.ID, Address: Hex20(wallet), Salt: Hex32(salt),
	})
	if err != nil {
		return "", err
	}
	return row.Address, nil
}

// FormatAddress renders a stored 0x address for display on its chain.
func FormatAddress(chainID, stored string) string {
	chain, ok := LookupChain(chainID)
	if !ok {
		return stored
	}
	raw, err := ParseHex20(stored)
	if err != nil {
		return stored
	}
	return DisplayAddress(chain, raw)
}

// Outcome of processing one verified transfer.
type Outcome struct {
	Chain    string `json:"chain"`
	TxHash   string `json:"tx_hash"`
	LogIndex int    `json:"log_index"`
	Status   string `json:"status"` // credited, below_min, already, not_ours
	UserId   int    `json:"user_id,omitempty"`
	Amount   string `json:"amount"`
	Quota    int    `json:"quota,omitempty"`
}

// ProcessTransaction verifies a transaction on chain and credits every USDT
// transfer in it that reached one of our deposit wallets. onlyUser > 0 limits
// crediting to that user's wallets (a user claiming their own deposit).
func ProcessTransaction(ctx context.Context, chainID, txHash string, logIndex int, onlyUser int, source, callerIP string, force bool) ([]Outcome, error) {
	transfers, err := FinalTransfers(ctx, chainID, txHash)
	if err != nil {
		return nil, err
	}
	cfg := operation_setting.GetUsdtSetting().Chains[strings.ToLower(chainID)]
	chain, _ := LookupChain(chainID)
	var outcomes []Outcome
	var rate decimal.Decimal
	for _, t := range transfers {
		if logIndex >= 0 && t.LogIndex != logIndex {
			continue
		}
		amount := decimal.NewFromBigInt(t.Amount, -chain.Decimals)
		outcome := Outcome{Chain: t.Chain, TxHash: t.TxHash, LogIndex: t.LogIndex, Amount: amount.String()}
		wallet, err := model.FindUsdtDepositAddress(t.Chain, Hex20(t.To))
		if err != nil {
			return outcomes, err
		}
		if wallet == nil || (onlyUser > 0 && wallet.UserId != onlyUser) {
			outcome.Status = "not_ours"
			outcomes = append(outcomes, outcome)
			continue
		}
		if rate.IsZero() {
			if rate, _, err = CurrentRate(ctx); err != nil {
				return outcomes, err
			}
		}
		result, err := model.CreditUsdtDeposit(model.UsdtCreditInput{
			Chain: t.Chain, TxHash: t.TxHash, LogIndex: t.LogIndex, UserId: wallet.UserId,
			Address: Hex20(t.To), FromAddress: Hex20(t.From), RawAmount: t.Amount.String(), Amount: amount,
			Rate: rate, MinAmount: decimal.NewFromFloat(cfg.MinAmount), BlockNumber: int64(t.BlockNumber),
			Source: source, CallerIp: callerIP, Force: force,
		})
		if err != nil {
			return outcomes, err
		}
		outcome.UserId = wallet.UserId
		outcome.Status = result.Status
		// A resubmitted below-minimum deposit is still not credited; only a
		// deposit that was credited before reports "already".
		if result.AlreadyDone && result.Status == model.UsdtDepositCredited {
			outcome.Status = "already"
		}
		outcome.Quota = result.CreditedQuota
		outcomes = append(outcomes, outcome)
	}
	return outcomes, nil
}
