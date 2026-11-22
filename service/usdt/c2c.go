package usdt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
)

// C2C mode prices USDT at what customers pay for it in CNY on the OKX and
// Binance peer-to-peer markets. These are the endpoints the exchanges' own web
// pages call, not documented APIs, so the price is refreshed only every
// c2cRefreshInterval, the last good price is kept for c2cMaxAge while the
// feeds fail, and deposits wait once it is older than that.
var (
	okxC2CURL     = "https://www.okx.com/v3/c2c/tradingOrders/books?quoteCurrency=cny&baseCurrency=usdt&side=sell&paymentMethod=all&userType=all"
	binanceC2CURL = "https://p2p.binance.com/bapi/c2c/v2/friendly/c2c/adv/search"

	c2cRefreshInterval = 30 * time.Minute
	c2cRetryInterval   = time.Minute
	c2cMaxAge          = 2 * time.Hour
	// c2cAdCount is how many of the best ads the median is taken over, so a
	// single ad priced far off the market cannot move the rate.
	c2cAdCount = 10
	// c2cPriceMin/Max reject an answer that cannot be a CNY price of USDT.
	c2cPriceMin = decimal.NewFromInt(5)
	c2cPriceMax = decimal.NewFromInt(10)
)

var c2cCache struct {
	mu      sync.Mutex
	price   decimal.Decimal
	source  string
	at      time.Time // last successful refresh
	tried   time.Time // last refresh attempt
	lastErr error
}

// currentC2CPrice returns the CNY price of one USDT on the C2C markets.
func currentC2CPrice(ctx context.Context) (decimal.Decimal, string, error) {
	c2cCache.mu.Lock()
	defer c2cCache.mu.Unlock()
	now := time.Now()
	if !c2cCache.at.IsZero() && now.Sub(c2cCache.at) < c2cRefreshInterval {
		return c2cCache.price, c2cCache.source, nil
	}
	if now.Sub(c2cCache.tried) >= c2cRetryInterval {
		c2cCache.tried = now
		var errs []error
		for _, feed := range []struct {
			name  string
			fetch func(context.Context) ([]string, error)
		}{{"okx_c2c", fetchOkxC2CPrices}, {"binance_c2c", fetchBinanceC2CPrices}} {
			prices, err := feed.fetch(ctx)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", feed.name, err))
				continue
			}
			price, err := medianPrice(prices)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", feed.name, err))
				continue
			}
			if price.LessThan(c2cPriceMin) || price.GreaterThan(c2cPriceMax) {
				errs = append(errs, fmt.Errorf("%s: price %s outside sanity bounds", feed.name, price))
				continue
			}
			c2cCache.price, c2cCache.source, c2cCache.at, c2cCache.lastErr = price, feed.name, now, nil
			return price, feed.name, nil
		}
		c2cCache.lastErr = errors.Join(errs...)
	}
	if !c2cCache.at.IsZero() && now.Sub(c2cCache.at) < c2cMaxAge {
		return c2cCache.price, c2cCache.source, nil
	}
	return decimal.Zero, "", fmt.Errorf("%w: USDT C2C price unavailable: %v", ErrNotReady, c2cCache.lastErr)
}

// medianPrice is the median of the first c2cAdCount ad prices.
func medianPrice(raw []string) (decimal.Decimal, error) {
	var prices []decimal.Decimal
	for _, s := range raw[:min(len(raw), c2cAdCount)] {
		price, err := decimal.NewFromString(s)
		if err != nil {
			return decimal.Zero, fmt.Errorf("invalid price %q", s)
		}
		prices = append(prices, price)
	}
	if len(prices) == 0 {
		return decimal.Zero, errors.New("no ads")
	}
	slices.SortFunc(prices, func(a, b decimal.Decimal) int { return a.Cmp(b) })
	mid := len(prices) / 2
	if len(prices)%2 == 1 {
		return prices[mid], nil
	}
	return prices[mid-1].Add(prices[mid]).Div(decimal.NewFromInt(2)), nil
}

// doC2CRequest sends a request with a browser user agent: OKX answers the Go
// default one with HTTP 429.
func doC2CRequest(req *http.Request, out any) error {
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")
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

// fetchOkxC2CPrices lists the prices of ads selling USDT for CNY, best first.
func fetchOkxC2CPrices(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, okxC2CURL, nil)
	if err != nil {
		return nil, err
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			Sell []struct {
				Price string `json:"price"`
			} `json:"sell"`
		} `json:"data"`
	}
	if err := doC2CRequest(req, &body); err != nil {
		return nil, err
	}
	if body.Code != 0 {
		return nil, fmt.Errorf("code %d", body.Code)
	}
	prices := make([]string, 0, len(body.Data.Sell))
	for _, ad := range body.Data.Sell {
		prices = append(prices, ad.Price)
	}
	return prices, nil
}

// fetchBinanceC2CPrices lists the prices of ads a buyer of USDT can take with
// CNY, best first.
func fetchBinanceC2CPrices(ctx context.Context) ([]string, error) {
	payload, err := common.Marshal(map[string]any{
		"asset": "USDT", "fiat": "CNY", "tradeType": "BUY", "page": 1, "rows": c2cAdCount, "payTypes": []string{},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, binanceC2CURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	var body struct {
		Code string `json:"code"`
		Data []struct {
			Adv struct {
				Price string `json:"price"`
			} `json:"adv"`
		} `json:"data"`
	}
	if err := doC2CRequest(req, &body); err != nil {
		return nil, err
	}
	if body.Code != "000000" {
		return nil, fmt.Errorf("code %s", body.Code)
	}
	prices := make([]string, 0, len(body.Data))
	for _, ad := range body.Data {
		prices = append(prices, ad.Adv.Price)
	}
	return prices, nil
}
