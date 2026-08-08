package service

import (
	"fmt"
	"math"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

const (
	BillingSourceWallet       = "wallet"
	BillingSourceSubscription = "subscription"
)

// ApplyBillingDiscount applies the frozen scheduled multiplier to an already
// computed integer charge. A paid request never becomes free solely because
// the discounted integer rounds to zero (for example, 1 * 0.49); in that
// boundary case the original charge is retained and the discount is skipped.
func ApplyBillingDiscount(relayInfo *relaycommon.RelayInfo, baseQuota int) int {
	if relayInfo == nil {
		return baseQuota
	}
	if baseQuota <= 0 || !relayInfo.BillingDiscountResolved {
		relayInfo.BillingDiscountSkipped = false
		return baseQuota
	}
	ratio := relayInfo.BillingDiscountRatio
	if ratio <= 0 || ratio >= 1 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		relayInfo.BillingDiscountSkipped = false
		return baseQuota
	}
	rounded, _ := common.QuotaRoundChecked(float64(baseQuota) * ratio)
	relayInfo.BillingDiscountSkipped = rounded <= 0
	return applyBillingDiscountRatio(baseQuota, ratio, func(clamp *common.QuotaClamp) {
		noteQuotaClamp(relayInfo, clamp)
	})
}

func applyBillingDiscountRatio(baseQuota int, ratio float64, noteClamp func(*common.QuotaClamp)) int {
	if ratio <= 0 || ratio >= 1 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return baseQuota
	}
	discounted, clamp := common.QuotaRoundChecked(float64(baseQuota) * ratio)
	if noteClamp != nil {
		noteClamp(clamp)
	}
	if discounted <= 0 {
		return baseQuota
	}
	return discounted
}

// PreConsumeBilling 根据用户计费偏好创建 BillingSession 并执行预扣费。
// 会话存储在 relayInfo.Billing 上，供后续 Settle / Refund 使用。
func PreConsumeBilling(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	if relayInfo != nil && relayInfo.QuotaClamp != nil {
		return types.NewErrorWithStatusCode(
			relayInfo.QuotaClamp,
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if preConsumedQuota < 0 {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("pre-consume quota cannot be negative: %d", preConsumedQuota),
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	preConsumedQuota = ApplyBillingDiscount(relayInfo, preConsumedQuota)
	session, apiErr := NewBillingSession(c, relayInfo, preConsumedQuota)
	if apiErr != nil {
		return apiErr
	}
	relayInfo.Billing = session
	return nil
}

// ---------------------------------------------------------------------------
// SettleBilling — 后结算辅助函数
// ---------------------------------------------------------------------------

// SettleBilling 执行计费结算。如果 RelayInfo 上有 BillingSession 则通过 session 结算，
// 否则回退到旧的 PostConsumeQuota 路径（兼容按次计费等场景）。
func SettleBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) error {
	if actualQuota < 0 {
		return fmt.Errorf("actual quota cannot be negative: %d", actualQuota)
	}
	if relayInfo.Billing != nil {
		preConsumed := relayInfo.Billing.GetPreConsumedQuota()
		delta := actualQuota - preConsumed

		if delta > 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后补扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else if delta < 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后返还扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(-delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费与实际消耗一致，无需调整：%s（按次计费）",
				logger.FormatQuota(actualQuota),
			))
		}

		if err := relayInfo.Billing.Settle(actualQuota); err != nil {
			return err
		}

		// 发送额度通知（订阅计费使用订阅剩余额度）
		if actualQuota != 0 {
			if relayInfo.BillingSource == BillingSourceSubscription {
				checkAndSendSubscriptionQuotaNotify(relayInfo)
			} else {
				checkAndSendQuotaNotify(relayInfo, actualQuota-preConsumed, preConsumed)
			}
		}
		return nil
	}

	// 回退：无 BillingSession 时使用旧路径
	quotaDelta := actualQuota - relayInfo.FinalPreConsumedQuota
	if quotaDelta != 0 {
		return PostConsumeQuota(relayInfo, quotaDelta, relayInfo.FinalPreConsumedQuota, true)
	}
	return nil
}
