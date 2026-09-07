package common

import "github.com/QuantumNous/new-api/setting/billing_setting"

// ResolvePeakPricing pins both the selected period and its prices across retries.
func (info *RelayInfo) ResolvePeakPricing() error {
	if info.PeakPricingResolved {
		return nil
	}
	if schedule, ok := billing_setting.GetPeakPricing(info.OriginModelName); ok {
		snapshot, err := schedule.Resolve(info.StartTime)
		if err != nil {
			return err
		}
		info.PeakPricing = snapshot
	}
	info.PeakPricingResolved = true
	return nil
}

func (info *RelayInfo) EffectiveBillingMode() string {
	if info.PeakPricing != nil {
		return info.PeakPricing.Tariff.Mode
	}
	return billing_setting.GetBillingMode(info.OriginModelName)
}
