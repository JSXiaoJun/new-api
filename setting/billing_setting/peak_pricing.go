package billing_setting

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

const PeakPricingOptionKey = "billing_setting.peak_pricing"
const BillingModePeak = "peak"
const BillingModePerRequest = "per_request"
const BillingModePerToken = "per_token"

// PeakTariff is self-contained: token prices are USD per million tokens,
// price is USD per request/second, and expressions retain their usual units.
type PeakTariff struct {
	Mode             string   `json:"mode"`
	Price            *float64 `json:"price,omitempty"`
	InputPrice       *float64 `json:"input_price,omitempty"`
	OutputPrice      *float64 `json:"output_price,omitempty"`
	CachePrice       *float64 `json:"cache_price,omitempty"`
	CreateCachePrice *float64 `json:"create_cache_price,omitempty"`
	ImagePrice       *float64 `json:"image_price,omitempty"`
	AudioPrice       *float64 `json:"audio_price,omitempty"`
	AudioOutputPrice *float64 `json:"audio_output_price,omitempty"`
	Expression       string   `json:"expression,omitempty"`
}

type PeakPeriod struct {
	Start  string     `json:"start"`
	End    string     `json:"end"`
	Tariff PeakTariff `json:"tariff"`
}

type PeakPricing struct {
	Timezone string       `json:"timezone"`
	Default  PeakTariff   `json:"default"`
	Periods  []PeakPeriod `json:"periods"`
}

type PeakSnapshot struct {
	Timezone string     `json:"timezone"`
	Period   string     `json:"period"`
	Tariff   PeakTariff `json:"tariff"`
}

var peakMu sync.RWMutex
var peakPrices = map[string]PeakPricing{}

func ParsePeakPricing(value string) (map[string]PeakPricing, error) {
	var prices map[string]PeakPricing
	if err := common.UnmarshalJsonStr(value, &prices); err != nil {
		return nil, err
	}
	if prices == nil {
		return nil, fmt.Errorf("peak pricing must be a JSON object")
	}
	for model, schedule := range prices {
		if strings.TrimSpace(model) == "" {
			return nil, fmt.Errorf("peak pricing requires a model name")
		}
		if err := schedule.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", model, err)
		}
	}
	return prices, nil
}

func LoadPeakPricing(value string) error {
	prices, err := ParsePeakPricing(value)
	if err != nil {
		return err
	}
	peakMu.Lock()
	peakPrices = prices
	peakMu.Unlock()
	return nil
}

func GetPeakPricing(model string) (PeakPricing, bool) {
	peakMu.RLock()
	defer peakMu.RUnlock()
	schedule, ok := peakPrices[model]
	// Published schedules are immutable. Callers must not mutate their fields.
	return schedule, ok
}

func peakMinute(value string) (int, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil || parsed.Format("15:04") != value {
		return 0, fmt.Errorf("invalid time %q; expected HH:MM", value)
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

func (p PeakPricing) Validate() error {
	if p.Timezone == "" || p.Timezone == "Local" {
		return fmt.Errorf("an explicit IANA timezone is required")
	}
	if _, err := time.LoadLocation(p.Timezone); err != nil {
		return fmt.Errorf("invalid timezone: %w", err)
	}
	if err := p.Default.Validate(); err != nil {
		return fmt.Errorf("default tariff: %w", err)
	}
	if len(p.Periods) == 0 || len(p.Periods) > 48 {
		return fmt.Errorf("peak pricing requires 1 to 48 periods")
	}
	var occupied [1440]bool
	for i, period := range p.Periods {
		start, err := peakMinute(period.Start)
		if err != nil {
			return err
		}
		end, err := peakMinute(period.End)
		if err != nil {
			return err
		}
		if start == end {
			return fmt.Errorf("period %d has identical start and end", i+1)
		}
		if err := period.Tariff.Validate(); err != nil {
			return fmt.Errorf("period %d: %w", i+1, err)
		}
		for minute := start; minute != end; minute = (minute + 1) % 1440 {
			if occupied[minute] {
				return fmt.Errorf("period %d overlaps another period", i+1)
			}
			occupied[minute] = true
		}
	}
	return nil
}

func (p PeakTariff) Validate() error {
	for _, price := range []*float64{p.Price, p.InputPrice, p.OutputPrice, p.CachePrice, p.CreateCachePrice, p.ImagePrice, p.AudioPrice, p.AudioOutputPrice} {
		if price != nil && (*price < 0 || math.IsNaN(*price) || math.IsInf(*price, 0)) {
			return fmt.Errorf("prices must be finite and non-negative")
		}
	}
	switch p.Mode {
	case BillingModePerRequest, BillingModePerSecond:
		if p.Price == nil {
			return fmt.Errorf("price is required")
		}
	case BillingModePerToken:
		if p.InputPrice == nil || p.OutputPrice == nil {
			return fmt.Errorf("input and output prices are required")
		}
		if *p.InputPrice == 0 {
			for _, price := range []*float64{p.OutputPrice, p.CachePrice, p.CreateCachePrice, p.ImagePrice, p.AudioPrice, p.AudioOutputPrice} {
				if price != nil && *price != 0 {
					return fmt.Errorf("positive dependent token prices require a positive input price; use an expression for zero input pricing")
				}
			}
		}
		if p.AudioOutputPrice != nil && *p.AudioOutputPrice > 0 && (p.AudioPrice == nil || *p.AudioPrice == 0) {
			return fmt.Errorf("audio output price requires a positive audio input price")
		}
		data := p.PriceData()
		for _, ratio := range []float64{data.CompletionRatio, data.CacheRatio, data.CacheCreation1hRatio, data.ImageRatio, data.AudioRatio, data.AudioCompletionRatio} {
			if math.IsInf(ratio, 0) || math.IsNaN(ratio) {
				return fmt.Errorf("token price ratios exceed the supported range")
			}
		}
	case BillingModeTieredExpr:
		if strings.TrimSpace(p.Expression) == "" {
			return fmt.Errorf("billing expression is required")
		}
		return SmokeTestExpr(p.Expression)
	default:
		return fmt.Errorf("unsupported peak tariff mode %q", p.Mode)
	}
	return nil
}

func (p PeakPricing) Resolve(at time.Time) (*PeakSnapshot, error) {
	location, err := time.LoadLocation(p.Timezone)
	if err != nil {
		return nil, err
	}
	local := at.In(location)
	minute := local.Hour()*60 + local.Minute()
	snapshot := &PeakSnapshot{Timezone: p.Timezone, Period: "default", Tariff: p.Default}
	for _, period := range p.Periods {
		start, err := peakMinute(period.Start)
		if err != nil {
			return nil, err
		}
		end, err := peakMinute(period.End)
		if err != nil {
			return nil, err
		}
		if (start < end && minute >= start && minute < end) || (start > end && (minute >= start || minute < end)) {
			snapshot.Period = period.Start + "-" + period.End
			snapshot.Tariff = period.Tariff
			break
		}
	}
	return snapshot, nil
}

func (p PeakTariff) PriceData() types.PriceData {
	data := types.PriceData{CompletionRatio: 1, CacheRatio: 1, CacheCreationRatio: 1, ImageRatio: 1, AudioRatio: 1, AudioCompletionRatio: 1}
	if p.Mode == BillingModePerRequest || p.Mode == BillingModePerSecond {
		data.UsePrice = true
		data.ModelPrice = *p.Price
		return data
	}
	if p.Mode != BillingModePerToken {
		return data
	}
	data.ModelRatio = *p.InputPrice / 2
	if *p.InputPrice > 0 {
		data.CompletionRatio = *p.OutputPrice / *p.InputPrice
		if p.CachePrice != nil {
			data.CacheRatio = *p.CachePrice / *p.InputPrice
		}
		if p.CreateCachePrice != nil {
			data.CacheCreationRatio = *p.CreateCachePrice / *p.InputPrice
		}
		if p.ImagePrice != nil {
			data.ImageRatio = *p.ImagePrice / *p.InputPrice
		}
		if p.AudioPrice != nil {
			data.AudioRatio = *p.AudioPrice / *p.InputPrice
		}
	}
	if p.AudioPrice != nil && *p.AudioPrice > 0 && p.AudioOutputPrice != nil {
		data.AudioCompletionRatio = *p.AudioOutputPrice / *p.AudioPrice
	}
	data.CacheCreation5mRatio = data.CacheCreationRatio
	data.CacheCreation1hRatio = data.CacheCreationRatio * 1.6
	return data
}
