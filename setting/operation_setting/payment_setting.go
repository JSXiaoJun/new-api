package operation_setting

import (
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// MaxTopUpBonusPercent bounds an administrator bonus tier so a mistyped value
// cannot multiply credited quota without limit.
const MaxTopUpBonusPercent = 1000

type PaymentSetting struct {
	AmountOptions  []int           `json:"amount_options"`
	AmountDiscount map[int]float64 `json:"amount_discount"` // 充值金额对应的折扣，例如 100 元 0.9 表示 100 元充值享受 9 折优惠
	// AmountBonus 充值赠送阶梯：键为最低充值数量，值为赠送百分比。
	// 例如 {"100":10,"500":20} 表示充值满 100 送 10%，满 500 送 20%，取满足条件的最高一档。
	AmountBonus map[int]float64 `json:"amount_bonus"`

	ComplianceConfirmed    bool   `json:"compliance_confirmed"`
	ComplianceTermsVersion string `json:"compliance_terms_version"`
	ComplianceConfirmedAt  int64  `json:"compliance_confirmed_at"`
	ComplianceConfirmedBy  int    `json:"compliance_confirmed_by"`
	ComplianceConfirmedIP  string `json:"compliance_confirmed_ip"`
}

const CurrentComplianceTermsVersion = "v1"

// 默认配置
var paymentSetting = PaymentSetting{
	AmountOptions:  []int{10, 20, 50, 100, 200, 500},
	AmountDiscount: map[int]float64{},
	AmountBonus:    map[int]float64{},
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("payment_setting", &paymentSetting)
}

func GetPaymentSetting() *PaymentSetting {
	return &paymentSetting
}

func IsPaymentComplianceConfirmed() bool {
	return paymentSetting.ComplianceConfirmed &&
		paymentSetting.ComplianceTermsVersion == CurrentComplianceTermsVersion
}

// TopUpBonusPercent returns the bonus percentage of the highest tier whose
// threshold the amount reaches, or 0 when no tier applies. Tiers that fail
// validation are ignored so a corrupted stored value never inflates credit.
func TopUpBonusPercent(amount int64) float64 {
	bestThreshold := int64(-1)
	bestPercent := 0.0
	for threshold, percent := range paymentSetting.AmountBonus {
		if !isValidTopUpBonusTier(threshold, percent) || amount < int64(threshold) {
			continue
		}
		if int64(threshold) > bestThreshold {
			bestThreshold = int64(threshold)
			bestPercent = percent
		}
	}
	return bestPercent
}

func isValidTopUpBonusTier(threshold int, percent float64) bool {
	return threshold > 0 &&
		!math.IsNaN(percent) && !math.IsInf(percent, 0) &&
		percent > 0 && percent <= MaxTopUpBonusPercent
}

// ValidateAmountBonusJSON rejects tier maps that cannot be applied safely.
func ValidateAmountBonusJSON(raw string) error {
	tiers := map[int]float64{}
	if err := common.UnmarshalJsonStr(raw, &tiers); err != nil {
		return fmt.Errorf("充值赠送配置必须是 {\"最低充值数量\": 赠送百分比} 格式的 JSON 对象")
	}
	for threshold, percent := range tiers {
		if !isValidTopUpBonusTier(threshold, percent) {
			return fmt.Errorf("充值赠送档位无效：最低充值数量必须大于 0，赠送百分比必须大于 0 且不超过 %d", MaxTopUpBonusPercent)
		}
	}
	return nil
}
