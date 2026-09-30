package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

//func GetPromptTokens(textRequest dto.GeneralOpenAIRequest, relayMode int) (int, error) {
//	switch relayMode {
//	case constant.RelayModeChatCompletions:
//		return CountTokenMessages(textRequest.Messages, textRequest.Model)
//	case constant.RelayModeCompletions:
//		return CountTokenInput(textRequest.Prompt, textRequest.Model), nil
//	case constant.RelayModeModerations:
//		return CountTokenInput(textRequest.Input, textRequest.Model), nil
//	}
//	return 0, errors.New("unknown relay mode")
//}

func ResponseText2Usage(c *gin.Context, responseText string, modeName string, promptTokens int) *dto.Usage {
	common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
	usage := &dto.Usage{}
	usage.PromptTokens = promptTokens
	usage.CompletionTokens = EstimateTokenByModel(modeName, responseText)
	usage.TotalTokens = addUsageInts(usage.PromptTokens, usage.CompletionTokens)
	return usage
}

// EnsureUsageCompletion fills a missing output-token side on a partial
// provider usage snapshot. Compatible providers sometimes report prompt/input
// tokens but omit completion/output tokens even though response text was sent.
// Existing provider usage and cache details are preserved; only the missing
// aggregate fields are completed.
func EnsureUsageCompletion(c *gin.Context, usage *dto.Usage, responseText, model string, estimatedPromptTokens int) bool {
	if usage == nil || responseText == "" {
		return false
	}

	promptTokens := usage.PromptTokens
	if usage.InputTokens > promptTokens {
		promptTokens = usage.InputTokens
	}
	if promptTokens == 0 && estimatedPromptTokens > 0 {
		promptTokens = estimatedPromptTokens
		if usage.TotalTokens > 0 && promptTokens > usage.TotalTokens {
			promptTokens = usage.TotalTokens
		}
	}
	completionTokens := usage.CompletionTokens
	if usage.OutputTokens > completionTokens {
		completionTokens = usage.OutputTokens
	}
	if completionTokens > 0 {
		return false
	}

	// A total that exceeds the reported prompt side is more authoritative than
	// a local estimate. Prompt-only totals still fall through to text counting.
	if usage.TotalTokens > promptTokens {
		completionTokens = usage.TotalTokens - promptTokens
	}
	if completionTokens == 0 {
		completionTokens = EstimateTokenByModel(model, responseText)
	}
	if completionTokens <= 0 {
		return false
	}

	usage.PromptTokens = promptTokens
	usage.CompletionTokens = completionTokens
	if usage.InputTokens == 0 {
		usage.InputTokens = promptTokens
	}
	if usage.OutputTokens == 0 {
		usage.OutputTokens = completionTokens
	}
	minimumTotal := addUsageInts(promptTokens, completionTokens)
	if usage.TotalTokens < minimumTotal {
		usage.TotalTokens = minimumTotal
	}
	return true
}

// RecalculateUsageTotal keeps the aggregate field consistent after a relay
// adds provider-specific output such as tool or workflow tokens.
func RecalculateUsageTotal(usage *dto.Usage) {
	if usage == nil {
		return
	}
	usage.TotalTokens = addUsageInts(usage.PromptTokens, usage.CompletionTokens)
}

func ValidUsage(usage *dto.Usage) bool {
	if usage == nil {
		return false
	}
	if validUsageFields(usage) {
		return true
	}
	// Relaykit keeps provider-native usage under BillingUsage while a
	// conversion is in progress. Treat that nested snapshot as authoritative
	// for fallback decisions too; otherwise a bridge can replace it with a
	// local text estimate when the canonical fields are still empty.
	if effectiveUsage, ok := usageFromBillingUsage(usage); ok {
		return validUsageFields(effectiveUsage)
	}
	return false
}

func validUsageFields(usage *dto.Usage) bool {
	if usage == nil {
		return false
	}
	return usage.PromptTokens > 0 ||
		usage.CompletionTokens > 0 ||
		usage.TotalTokens > 0 ||
		usage.InputTokens > 0 ||
		usage.OutputTokens > 0 ||
		usage.PromptCacheHitTokens > 0 ||
		usage.CacheReadInputTokens > 0 ||
		usage.CacheCreationInputTokens > 0 ||
		usage.CacheReadTokens > 0 ||
		usage.CacheWriteTokens > 0 ||
		usage.CacheCreationTokens > 0 ||
		usage.ClaudeCacheCreation5mTokens > 0 ||
		usage.ClaudeCacheCreation1hTokens > 0 ||
		usage.PromptTokensDetails.CachedTokens > 0 ||
		usage.PromptTokensDetails.CachedCreationTokens > 0 ||
		usage.PromptTokensDetails.CacheCreationTokens > 0 ||
		usage.PromptTokensDetails.CacheWriteTokens > 0 ||
		usage.PromptTokensDetails.TextTokens > 0 ||
		usage.PromptTokensDetails.AudioTokens > 0 ||
		usage.PromptTokensDetails.ImageTokens > 0 ||
		usage.CompletionTokenDetails.TextTokens > 0 ||
		usage.CompletionTokenDetails.AudioTokens > 0 ||
		usage.CompletionTokenDetails.ImageTokens > 0 ||
		usage.CompletionTokenDetails.ReasoningTokens > 0
}

// normalizeUsageForBilling maps the usage aliases emitted by compatible
// providers to the prompt/completion fields consumed by the quota calculator.
// A total-only response is split with the request estimate so it remains
// billable even when the provider omits the input/output breakdown.
func normalizeUsageForBilling(usage *dto.Usage, estimatedPromptTokens int) *dto.Usage {
	if usage == nil {
		return nil
	}

	normalized := *usage
	normalized.PromptTokens = nonNegativeUsageInt(normalized.PromptTokens)
	normalized.CompletionTokens = nonNegativeUsageInt(normalized.CompletionTokens)
	normalized.TotalTokens = nonNegativeUsageInt(normalized.TotalTokens)
	normalized.InputTokens = nonNegativeUsageInt(normalized.InputTokens)
	normalized.OutputTokens = nonNegativeUsageInt(normalized.OutputTokens)

	// Anthropic's InputTokens includes cache reads and writes while
	// PromptTokens is the billable uncached input side. They are intentionally
	// different quantities, so only apply the OpenAI-style aliases here.
	if normalized.UsageSemantic != dto.BillingUsageSemanticAnthropic {
		normalized.PromptTokens = maxBillingUsageInt(normalized.PromptTokens, normalized.InputTokens)
		normalized.InputTokens = maxBillingUsageInt(normalized.InputTokens, normalized.PromptTokens)
		normalized.CompletionTokens = maxBillingUsageInt(normalized.CompletionTokens, normalized.OutputTokens)
		normalized.OutputTokens = maxBillingUsageInt(normalized.OutputTokens, normalized.CompletionTokens)
	}
	if normalized.InputTokensDetails != nil {
		mergeBillingInputTokenDetails(&normalized.PromptTokensDetails, *normalized.InputTokensDetails)
	}
	if normalized.OutputTokensDetails != nil {
		mergeBillingOutputTokenDetails(&normalized.CompletionTokenDetails, *normalized.OutputTokensDetails)
	}
	normalized.PromptTokensDetails.CachedTokens = maxBillingUsageInt(
		normalized.PromptTokensDetails.CachedTokens,
		normalized.PromptCacheHitTokens,
		normalized.CacheReadInputTokens,
		normalized.CacheReadTokens,
	)
	normalized.PromptTokensDetails.CachedCreationTokens = maxBillingUsageInt(
		normalized.PromptTokensDetails.CachedCreationTokens,
		normalized.CacheCreationInputTokens,
		normalized.CacheCreationTokens,
	)
	normalized.PromptTokensDetails.CacheCreationTokens = maxBillingUsageInt(
		normalized.PromptTokensDetails.CacheCreationTokens,
		normalized.CacheCreationInputTokens,
		normalized.CacheCreationTokens,
	)
	normalized.PromptTokensDetails.CacheWriteTokens = maxBillingUsageInt(
		normalized.PromptTokensDetails.CacheWriteTokens,
		normalized.CacheWriteTokens,
	)

	if normalized.TotalTokens == 0 {
		normalized.TotalTokens = addUsageInts(normalized.PromptTokens, normalized.CompletionTokens)
	}
	total := normalized.TotalTokens
	if normalized.PromptTokens == 0 && normalized.CompletionTokens == 0 && total > 0 {
		if estimatedPromptTokens < 0 {
			estimatedPromptTokens = 0
		}
		if estimatedPromptTokens > total {
			estimatedPromptTokens = total
		}
		normalized.PromptTokens = estimatedPromptTokens
		normalized.CompletionTokens = total - estimatedPromptTokens
	} else if normalized.PromptTokens == 0 && total > normalized.CompletionTokens {
		normalized.PromptTokens = total - normalized.CompletionTokens
	} else if normalized.CompletionTokens == 0 && total > normalized.PromptTokens {
		normalized.CompletionTokens = total - normalized.PromptTokens
	}

	// Inconsistent upstream totals must not erase a reported side. Keep the
	// larger valid total instead of allowing a negative implied side.
	if normalized.UsageSemantic != dto.BillingUsageSemanticGemini {
		sides := addUsageInts(normalized.PromptTokens, normalized.CompletionTokens)
		if normalized.TotalTokens < sides {
			normalized.TotalTokens = sides
		}
	}
	if normalized.InputTokens == 0 {
		normalized.InputTokens = normalized.PromptTokens
	}
	if normalized.OutputTokens == 0 {
		normalized.OutputTokens = normalized.CompletionTokens
	}
	return &normalized
}

func nonNegativeUsageInt(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func addUsageInts(a, b int) int {
	if a < 0 {
		a = 0
	}
	if b < 0 {
		b = 0
	}
	maxInt := int(^uint(0) >> 1)
	if a > maxInt-b {
		return maxInt
	}
	return a + b
}

func mergeBillingInputTokenDetails(dst *dto.InputTokenDetails, src dto.InputTokenDetails) {
	if dst == nil {
		return
	}
	dst.CachedTokens = maxBillingUsageInt(dst.CachedTokens, src.CachedTokens)
	dst.CachedCreationTokens = maxBillingUsageInt(dst.CachedCreationTokens, src.CachedCreationTokens)
	dst.CacheCreationTokens = maxBillingUsageInt(dst.CacheCreationTokens, src.CacheCreationTokens)
	dst.CacheWriteTokens = maxBillingUsageInt(dst.CacheWriteTokens, src.CacheWriteTokens)
	dst.TextTokens = maxBillingUsageInt(dst.TextTokens, src.TextTokens)
	dst.AudioTokens = maxBillingUsageInt(dst.AudioTokens, src.AudioTokens)
	dst.ImageTokens = maxBillingUsageInt(dst.ImageTokens, src.ImageTokens)
}

func mergeBillingOutputTokenDetails(dst *dto.OutputTokenDetails, src dto.OutputTokenDetails) {
	if dst == nil {
		return
	}
	dst.ReasoningTokens = maxBillingUsageInt(dst.ReasoningTokens, src.ReasoningTokens)
	dst.TextTokens = maxBillingUsageInt(dst.TextTokens, src.TextTokens)
	dst.AudioTokens = maxBillingUsageInt(dst.AudioTokens, src.AudioTokens)
	dst.ImageTokens = maxBillingUsageInt(dst.ImageTokens, src.ImageTokens)
}

func maxBillingUsageInt(values ...int) int {
	max := 0
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}
