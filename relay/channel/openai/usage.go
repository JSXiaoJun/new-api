package openai

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func applyUsagePostProcessing(info *relaycommon.RelayInfo, usage *dto.Usage, responseBody []byte) {
	if info == nil || usage == nil {
		return
	}

	switch info.ChannelType {
	case constant.ChannelTypeDeepSeek:
		if usage.PromptTokensDetails.CachedTokens == 0 && usage.PromptCacheHitTokens != 0 {
			usage.PromptTokensDetails.CachedTokens = usage.PromptCacheHitTokens
		}
	case constant.ChannelTypeZhipu_v4:
		// 智普的cached_tokens在标准位置: usage.prompt_tokens_details.cached_tokens
		if usage.PromptTokensDetails.CachedTokens == 0 {
			if usage.InputTokensDetails != nil && usage.InputTokensDetails.CachedTokens > 0 {
				usage.PromptTokensDetails.CachedTokens = usage.InputTokensDetails.CachedTokens
			} else if cachedTokens, ok := extractCachedTokensFromBody(responseBody); ok {
				usage.PromptTokensDetails.CachedTokens = cachedTokens
			} else if usage.PromptCacheHitTokens > 0 {
				usage.PromptTokensDetails.CachedTokens = usage.PromptCacheHitTokens
			}
		}
	case constant.ChannelTypeMoonshot:
		// Moonshot的cached_tokens在非标准位置: choices[].usage.cached_tokens
		if usage.PromptTokensDetails.CachedTokens == 0 {
			if usage.InputTokensDetails != nil && usage.InputTokensDetails.CachedTokens > 0 {
				usage.PromptTokensDetails.CachedTokens = usage.InputTokensDetails.CachedTokens
			} else if cachedTokens, ok := extractMoonshotCachedTokensFromBody(responseBody); ok {
				usage.PromptTokensDetails.CachedTokens = cachedTokens
			} else if cachedTokens, ok := extractCachedTokensFromBody(responseBody); ok {
				usage.PromptTokensDetails.CachedTokens = cachedTokens
			} else if usage.PromptCacheHitTokens > 0 {
				usage.PromptTokensDetails.CachedTokens = usage.PromptCacheHitTokens
			}
		}
	case constant.ChannelTypeOpenAI:
		if usage.PromptTokensDetails.CachedTokens == 0 {
			if cachedTokens, ok := extractLlamaCachedTokensFromBody(responseBody); ok {
				usage.PromptTokensDetails.CachedTokens = cachedTokens
			}
		}
	}
}

// ensureOpenAIUsageCompletion keeps a partial provider usage snapshot
// billable. OpenAI-compatible gateways sometimes report only input/prompt
// tokens even though the response contains generated text. A total token
// count is preferred when it can supply the missing side; local counting is
// used only when the provider total still leaves completion tokens missing.
func ensureOpenAIUsageCompletion(_ *gin.Context, usage *dto.Usage, outputText, model string, estimatedPromptTokens int) (changed, estimated bool) {
	if usage == nil {
		return false, false
	}
	originalPrompt := usage.PromptTokens
	originalCompletion := usage.CompletionTokens
	originalTotal := usage.TotalTokens
	originalInput := usage.InputTokens
	originalOutput := usage.OutputTokens

	promptTokens := maxResponsesUsageInt(usage.PromptTokens, usage.InputTokens)
	completionTokens := maxResponsesUsageInt(usage.CompletionTokens, usage.OutputTokens)
	totalTokens := maxResponsesUsageInt(usage.TotalTokens, addResponsesUsageTokens(promptTokens, completionTokens))

	// A total-only response needs the request estimate before deriving the
	// missing completion side. Cap the estimate so it cannot exceed the total.
	if promptTokens == 0 && estimatedPromptTokens > 0 && totalTokens > 0 {
		promptTokens = estimatedPromptTokens
		if promptTokens > totalTokens {
			promptTokens = totalTokens
		}
	}
	if completionTokens == 0 && totalTokens > promptTokens {
		completionTokens = totalTokens - promptTokens
	}

	// Output token details are authoritative even when the provider omits the
	// aggregate output_tokens/completion_tokens field.
	detailOutputTokens := maxResponsesUsageInt(
		usage.CompletionTokenDetails.TextTokens,
		usage.CompletionTokenDetails.AudioTokens,
		usage.CompletionTokenDetails.ImageTokens,
		usage.CompletionTokenDetails.ReasoningTokens,
	)
	if usage.OutputTokensDetails != nil {
		detailOutputTokens = maxResponsesUsageInt(
			detailOutputTokens,
			usage.OutputTokensDetails.TextTokens,
			usage.OutputTokensDetails.AudioTokens,
			usage.OutputTokensDetails.ImageTokens,
			usage.OutputTokensDetails.ReasoningTokens,
		)
	}
	if completionTokens == 0 && detailOutputTokens > 0 {
		completionTokens = detailOutputTokens
	}

	if completionTokens == 0 && outputText != "" {
		// Count only the missing output side. The full-request fallback also marks
		// the request as locally counted, which would incorrectly hide an upstream
		// input-token snapshot behind a whole-request "local" billing path.
		completionTokens = service.EstimateTokenByModel(model, outputText)
		if completionTokens > 0 {
			estimated = true
		}
	}
	if promptTokens == 0 && completionTokens > 0 && estimatedPromptTokens > 0 {
		promptTokens = estimatedPromptTokens
	}

	usage.PromptTokens = promptTokens
	usage.InputTokens = maxResponsesUsageInt(usage.InputTokens, promptTokens)
	usage.CompletionTokens = completionTokens
	usage.OutputTokens = maxResponsesUsageInt(usage.OutputTokens, completionTokens)
	usage.TotalTokens = maxResponsesUsageInt(totalTokens, addResponsesUsageTokens(promptTokens, completionTokens))
	// Settlement prefers billing_usage.openai_usage when a relay conversion
	// preserved provider-native usage there. Keep that authoritative snapshot
	// synchronized with the canonical fields we just completed; otherwise the
	// top-level response can show output tokens while quota settlement still
	// sees an input-only nested snapshot.
	if usage.BillingUsage != nil && usage.BillingUsage.OpenAIUsage != nil {
		mergeResponsesUsageFieldsWithoutBilling(usage.BillingUsage.OpenAIUsage, usage)
	}

	changed = originalPrompt != usage.PromptTokens ||
		originalCompletion != usage.CompletionTokens ||
		originalTotal != usage.TotalTokens ||
		originalInput != usage.InputTokens ||
		originalOutput != usage.OutputTokens
	return changed, estimated
}

func extractCachedTokensFromBody(body []byte) (int, bool) {
	if len(body) == 0 {
		return 0, false
	}

	var payload struct {
		Usage struct {
			PromptTokensDetails struct {
				CachedTokens *int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			CachedTokens         *int `json:"cached_tokens"`
			PromptCacheHitTokens *int `json:"prompt_cache_hit_tokens"`
		} `json:"usage"`
	}

	if err := common.Unmarshal(body, &payload); err != nil {
		return 0, false
	}

	if payload.Usage.PromptTokensDetails.CachedTokens != nil {
		return *payload.Usage.PromptTokensDetails.CachedTokens, true
	}
	if payload.Usage.CachedTokens != nil {
		return *payload.Usage.CachedTokens, true
	}
	if payload.Usage.PromptCacheHitTokens != nil {
		return *payload.Usage.PromptCacheHitTokens, true
	}
	return 0, false
}

// extractMoonshotCachedTokensFromBody 从Moonshot的非标准位置提取cached_tokens
// Moonshot的流式响应格式: {"choices":[{"usage":{"cached_tokens":111}}]}
func extractMoonshotCachedTokensFromBody(body []byte) (int, bool) {
	if len(body) == 0 {
		return 0, false
	}

	var payload struct {
		Choices []struct {
			Usage struct {
				CachedTokens *int `json:"cached_tokens"`
			} `json:"usage"`
		} `json:"choices"`
	}

	if err := common.Unmarshal(body, &payload); err != nil {
		return 0, false
	}

	// 遍历choices查找cached_tokens
	for _, choice := range payload.Choices {
		if choice.Usage.CachedTokens != nil && *choice.Usage.CachedTokens > 0 {
			return *choice.Usage.CachedTokens, true
		}
	}

	return 0, false
}

// extractLlamaCachedTokensFromBody 从llama.cpp的非标准位置提取cache_n
func extractLlamaCachedTokensFromBody(body []byte) (int, bool) {
	if len(body) == 0 {
		return 0, false
	}

	var payload struct {
		Timings struct {
			CachedTokens *int `json:"cache_n"`
		} `json:"timings"`
	}

	if err := common.Unmarshal(body, &payload); err != nil {
		return 0, false
	}

	if payload.Timings.CachedTokens == nil {
		return 0, false
	}
	return *payload.Timings.CachedTokens, true
}
