package service

import (
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
)

const (
	usageBillingPathLocal              = "local"
	usageBillingPathUpstream           = "upstream"
	usageBillingPathOpenAI             = "billing-usage-openai"
	usageBillingPathOpenAIEstimated    = "billing-usage-openai-estimated"
	usageBillingPathAnthropic          = "billing-usage-anthropic"
	usageBillingPathAnthropicEstimated = "billing-usage-anthropic-estimated"
	usageBillingPathGemini             = "billing-usage-gemini"
	usageBillingPathGeminiEstimated    = "billing-usage-gemini-estimated"
)

func effectiveBillingUsage(usage *dto.Usage) *dto.Usage {
	if billingUsage, ok := usageFromBillingUsage(usage); ok {
		return billingUsage
	}
	return usage
}

func usageBillingPathForLog(isLocalCountTokens bool, usage *dto.Usage) string {
	effectiveUsage, ok := usageFromBillingUsage(usage)
	if !ok {
		if isLocalCountTokens {
			return usageBillingPathLocal
		}
		return usageBillingPathUpstream
	}

	switch effectiveUsage.UsageSemantic {
	case dto.BillingUsageSemanticOpenAI:
		if usage.BillingUsage.Estimated {
			return usageBillingPathOpenAIEstimated
		}
		return usageBillingPathOpenAI
	case dto.BillingUsageSemanticAnthropic:
		if usage.BillingUsage.Estimated {
			return usageBillingPathAnthropicEstimated
		}
		return usageBillingPathAnthropic
	case dto.BillingUsageSemanticGemini:
		if usage.BillingUsage.Estimated {
			return usageBillingPathGeminiEstimated
		}
		return usageBillingPathGemini
	}

	return usageBillingPathUpstream
}

func appendUsageBillingPathForLog(other map[string]interface{}, isLocalCountTokens bool, usage *dto.Usage) {
	if other == nil {
		return
	}
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	if !ok || adminInfo == nil {
		adminInfo = make(map[string]interface{})
		other["admin_info"] = adminInfo
	}
	adminInfo["usage_billing_path"] = usageBillingPathForLog(isLocalCountTokens, usage)
}

func usageFromBillingUsage(usage *dto.Usage) (*dto.Usage, bool) {
	if usage == nil || usage.BillingUsage == nil {
		return nil, false
	}
	billingUsage := usage.BillingUsage
	source := strings.TrimSpace(billingUsage.Source)
	semantic := strings.TrimSpace(billingUsage.Semantic)

	if billingUsage.OpenAIUsage != nil &&
		(strings.EqualFold(source, dto.BillingUsageSourceOAIChat) ||
			strings.EqualFold(source, dto.BillingUsageSourceOAIResponses) ||
			strings.EqualFold(semantic, dto.BillingUsageSemanticOpenAI)) {
		return usageFromOpenAIBillingUsage(billingUsage), true
	}

	if billingUsage.ClaudeUsage != nil &&
		(strings.EqualFold(source, dto.BillingUsageSourceClaudeMessages) ||
			strings.EqualFold(semantic, dto.BillingUsageSemanticAnthropic)) {
		return usageFromClaudeBillingUsage(billingUsage), true
	}

	if billingUsage.GeminiUsageMetadata != nil &&
		(strings.EqualFold(source, dto.BillingUsageSourceGeminiChat) ||
			strings.EqualFold(semantic, dto.BillingUsageSemanticGemini)) {
		return usageFromGeminiBillingUsage(billingUsage), true
	}

	return nil, false
}

func usageFromOpenAIBillingUsage(billingUsage *dto.BillingUsage) *dto.Usage {
	usage := *billingUsage.OpenAIUsage
	// Compatible providers sometimes populate both OpenAI spellings. Treat
	// them as cumulative aliases and retain the largest observation so a richer
	// input/output snapshot cannot be overwritten by a smaller prompt/completion
	// field (or vice versa).
	usage.PromptTokens = maxBillingUsageInt(usage.PromptTokens, usage.InputTokens)
	usage.InputTokens = maxBillingUsageInt(usage.InputTokens, usage.PromptTokens)
	usage.CompletionTokens = maxBillingUsageInt(usage.CompletionTokens, usage.OutputTokens)
	usage.OutputTokens = maxBillingUsageInt(usage.OutputTokens, usage.CompletionTokens)
	if usage.TotalTokens == 0 {
		usage.TotalTokens = addUsageInts(usage.PromptTokens, usage.CompletionTokens)
	} else if usage.TotalTokens < addUsageInts(usage.PromptTokens, usage.CompletionTokens) {
		usage.TotalTokens = addUsageInts(usage.PromptTokens, usage.CompletionTokens)
	}
	if inputDetails := usage.InputTokensDetails; inputDetails != nil {
		mergeBillingInputTokenDetails(&usage.PromptTokensDetails, *inputDetails)
	}
	if outputDetails := usage.OutputTokensDetails; outputDetails != nil {
		mergeBillingOutputTokenDetails(&usage.CompletionTokenDetails, *outputDetails)
	}
	usage.PromptTokensDetails.CachedTokens = maxBillingUsageInt(usage.PromptTokensDetails.CachedTokens, usage.PromptCacheHitTokens, usage.CacheReadInputTokens, usage.CacheReadTokens)
	usage.PromptTokensDetails.CachedCreationTokens = maxBillingUsageInt(usage.PromptTokensDetails.CachedCreationTokens, usage.CacheCreationInputTokens, usage.CacheCreationTokens)
	usage.PromptTokensDetails.CacheCreationTokens = maxBillingUsageInt(usage.PromptTokensDetails.CacheCreationTokens, usage.CacheCreationInputTokens, usage.CacheCreationTokens)
	usage.PromptTokensDetails.CacheWriteTokens = maxBillingUsageInt(usage.PromptTokensDetails.CacheWriteTokens, usage.CacheWriteTokens)
	usage.UsageSemantic = dto.BillingUsageSemanticOpenAI
	usage.UsageSource = billingUsage.Source
	usage.BillingUsage = dto.CloneBillingUsage(billingUsage)
	return &usage
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

func usageFromClaudeBillingUsage(billingUsage *dto.BillingUsage) *dto.Usage {
	claudeUsage := billingUsage.ClaudeUsage
	cacheCreation5m := claudeUsage.GetCacheCreation5mTokens()
	if cacheCreation5m == 0 {
		cacheCreation5m = claudeUsage.ClaudeCacheCreation5mTokens
	}
	cacheCreation1h := claudeUsage.GetCacheCreation1hTokens()
	if cacheCreation1h == 0 {
		cacheCreation1h = claudeUsage.ClaudeCacheCreation1hTokens
	}

	usage := &dto.Usage{
		PromptTokens:                claudeUsage.InputTokens,
		CompletionTokens:            claudeUsage.OutputTokens,
		TotalTokens:                 addUsageInts(claudeUsage.InputTokens, claudeUsage.OutputTokens),
		InputTokens:                 addUsageInts(addUsageInts(claudeUsage.InputTokens, claudeUsage.CacheReadInputTokens), claudeUsage.CacheCreationInputTokens),
		OutputTokens:                claudeUsage.OutputTokens,
		UsageSemantic:               dto.BillingUsageSemanticAnthropic,
		UsageSource:                 dto.BillingUsageSourceClaudeMessages,
		BillingUsage:                dto.CloneBillingUsage(billingUsage),
		ClaudeCacheCreation5mTokens: cacheCreation5m,
		ClaudeCacheCreation1hTokens: cacheCreation1h,
	}
	usage.PromptTokensDetails.CachedTokens = claudeUsage.CacheReadInputTokens
	usage.PromptTokensDetails.CachedCreationTokens = claudeUsage.CacheCreationInputTokens
	return usage
}

func usageFromGeminiBillingUsage(billingUsage *dto.BillingUsage) *dto.Usage {
	metadata := *billingUsage.GeminiUsageMetadata
	promptTokens := addUsageInts(metadata.PromptTokenCount, metadata.ToolUsePromptTokenCount)
	usage := &dto.Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: addUsageInts(metadata.CandidatesTokenCount, metadata.ThoughtsTokenCount),
		TotalTokens:      metadata.TotalTokenCount,
		UsageSemantic:    dto.BillingUsageSemanticGemini,
		UsageSource:      dto.BillingUsageSourceGeminiChat,
		BillingUsage:     dto.CloneBillingUsage(billingUsage),
	}
	usage.CompletionTokenDetails.ReasoningTokens = metadata.ThoughtsTokenCount
	usage.PromptTokensDetails.CachedTokens = metadata.CachedContentTokenCount

	for _, detail := range metadata.PromptTokensDetails {
		addGeminiInputTokenDetail(&usage.PromptTokensDetails, detail)
	}
	for _, detail := range metadata.ToolUsePromptTokensDetails {
		addGeminiInputTokenDetail(&usage.PromptTokensDetails, detail)
	}
	for _, detail := range metadata.CandidatesTokensDetails {
		switch detail.Modality {
		case "IMAGE":
			usage.CompletionTokenDetails.ImageTokens += detail.TokenCount
		case "AUDIO":
			usage.CompletionTokenDetails.AudioTokens += detail.TokenCount
		case "TEXT":
			usage.CompletionTokenDetails.TextTokens += detail.TokenCount
		}
	}

	if usage.TotalTokens == 0 {
		usage.TotalTokens = addUsageInts(usage.PromptTokens, usage.CompletionTokens)
	} else if usage.CompletionTokens <= 0 {
		usage.CompletionTokens = usage.TotalTokens - usage.PromptTokens
	}
	if usage.PromptTokens > 0 && usage.PromptTokensDetails.TextTokens == 0 && usage.PromptTokensDetails.AudioTokens == 0 {
		usage.PromptTokensDetails.TextTokens = usage.PromptTokens
	}
	return usage
}

func addGeminiInputTokenDetail(details *dto.InputTokenDetails, detail dto.GeminiPromptTokensDetails) {
	switch detail.Modality {
	case "AUDIO":
		details.AudioTokens += detail.TokenCount
	case "IMAGE":
		details.ImageTokens += detail.TokenCount
	case "TEXT":
		details.TextTokens += detail.TokenCount
	}
}
