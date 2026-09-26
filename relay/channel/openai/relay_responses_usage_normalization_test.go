package openai

import (
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureOpenAIUsageCompletionEstimatesMissingOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	usage := &dto.Usage{InputTokens: 12}

	changed, estimated := ensureOpenAIUsageCompletion(c, usage, "generated response", "gpt-test", 12)

	assert.True(t, changed)
	assert.True(t, estimated)
	assert.Equal(t, 12, usage.PromptTokens)
	assert.Greater(t, usage.CompletionTokens, 0)
	assert.Equal(t, usage.PromptTokens+usage.CompletionTokens, usage.TotalTokens)
}

func TestEnsureOpenAIUsageCompletionSynchronizesNestedBillingUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	usage := &dto.Usage{
		PromptTokens: 12,
		TotalTokens:  12,
		BillingUsage: dto.NewOpenAIResponsesBillingUsage(&dto.Usage{
			InputTokens:  12,
			TotalTokens:  12,
		}),
	}

	changed, estimated := ensureOpenAIUsageCompletion(c, usage, "generated response", "gpt-test", 12)

	require.True(t, changed)
	require.True(t, estimated)
	require.NotNil(t, usage.BillingUsage)
	require.NotNil(t, usage.BillingUsage.OpenAIUsage)
	assert.Equal(t, usage.CompletionTokens, usage.BillingUsage.OpenAIUsage.CompletionTokens)
	assert.Equal(t, usage.OutputTokens, usage.BillingUsage.OpenAIUsage.OutputTokens)
	assert.Equal(t, usage.TotalTokens, usage.BillingUsage.OpenAIUsage.TotalTokens)
}

func TestNormalizeResponsesUsageTotalOnlyUsesPromptEstimate(t *testing.T) {
	usage := normalizeResponsesUsage(&dto.Usage{TotalTokens: 100}, 70)

	require.NotNil(t, usage)
	assert.Equal(t, 70, usage.PromptTokens)
	assert.Equal(t, 30, usage.CompletionTokens)
	assert.Equal(t, 100, usage.TotalTokens)
	assert.Equal(t, 70, usage.InputTokens)
	assert.Equal(t, 30, usage.OutputTokens)
}

func TestNormalizeResponsesUsageSupportsInputOutputAndDetails(t *testing.T) {
	usage := normalizeResponsesUsage(&dto.Usage{
		InputTokens:  120,
		OutputTokens: 40,
		TotalTokens:  160,
		InputTokensDetails: &dto.InputTokenDetails{
			CachedTokens:     20,
			CacheWriteTokens: 7,
			TextTokens:       80,
			AudioTokens:      10,
			ImageTokens:      30,
		},
		CompletionTokenDetails: dto.OutputTokenDetails{
			TextTokens:      30,
			AudioTokens:     4,
			ImageTokens:     6,
			ReasoningTokens: 12,
		},
	}, 0)

	require.NotNil(t, usage)
	assert.Equal(t, 120, usage.PromptTokens)
	assert.Equal(t, 40, usage.CompletionTokens)
	assert.Equal(t, 160, usage.TotalTokens)
	assert.Equal(t, 20, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 7, usage.PromptTokensDetails.CacheWriteTokens)
	assert.Equal(t, 80, usage.PromptTokensDetails.TextTokens)
	assert.Equal(t, 10, usage.PromptTokensDetails.AudioTokens)
	assert.Equal(t, 30, usage.PromptTokensDetails.ImageTokens)
	assert.Equal(t, 30, usage.CompletionTokenDetails.TextTokens)
	assert.Equal(t, 4, usage.CompletionTokenDetails.AudioTokens)
	assert.Equal(t, 6, usage.CompletionTokenDetails.ImageTokens)
	assert.Equal(t, 12, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestNormalizeResponsesUsageFillsMissingSideFromTotal(t *testing.T) {
	withPrompt := normalizeResponsesUsage(&dto.Usage{PromptTokens: 80, TotalTokens: 100}, 0)
	withCompletion := normalizeResponsesUsage(&dto.Usage{CompletionTokens: 25, TotalTokens: 100}, 0)

	require.NotNil(t, withPrompt)
	require.NotNil(t, withCompletion)
	assert.Equal(t, 80, withPrompt.PromptTokens)
	assert.Equal(t, 20, withPrompt.CompletionTokens)
	assert.Equal(t, 75, withCompletion.PromptTokens)
	assert.Equal(t, 25, withCompletion.CompletionTokens)
}

func TestNormalizeResponsesUsagePromotesNestedBillingSnapshot(t *testing.T) {
	usage := normalizeResponsesUsage(&dto.Usage{
		BillingUsage: dto.NewOpenAIResponsesBillingUsage(&dto.Usage{
			InputTokens:  120,
			OutputTokens: 30,
			TotalTokens:  150,
		}),
	}, 20)

	require.NotNil(t, usage)
	require.NotNil(t, usage.BillingUsage)
	require.NotNil(t, usage.BillingUsage.OpenAIUsage)
	assert.Equal(t, 120, usage.PromptTokens)
	assert.Equal(t, 30, usage.CompletionTokens)
	assert.Equal(t, 150, usage.TotalTokens)
	assert.Equal(t, 120, usage.BillingUsage.OpenAIUsage.PromptTokens)
	assert.Equal(t, 30, usage.BillingUsage.OpenAIUsage.CompletionTokens)
	assert.Equal(t, 150, usage.BillingUsage.OpenAIUsage.TotalTokens)
}

func TestMergeResponsesUsageUsesLargestAliasesAndDetails(t *testing.T) {
	dst := &dto.Usage{
		PromptTokens:         100,
		InputTokens:          90,
		CompletionTokens:     20,
		OutputTokens:         18,
		TotalTokens:          120,
		PromptCacheHitTokens: 11,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         12,
			CachedCreationTokens: 13,
			CacheCreationTokens:  14,
			CacheWriteTokens:     15,
			TextTokens:           16,
		},
		CompletionTokenDetails: dto.OutputTokenDetails{
			TextTokens:      17,
			ReasoningTokens: 18,
		},
		OutputTokensDetails: &dto.OutputTokenDetails{AudioTokens: 19},
	}

	mergeResponsesUsage(dst, &dto.Usage{
		PromptTokens:             40,
		InputTokens:              60,
		CompletionTokens:         5,
		OutputTokens:             15,
		TotalTokens:              75,
		PromptCacheHitTokens:     21,
		CacheReadInputTokens:     22,
		CacheReadTokens:          23,
		CacheCreationInputTokens: 24,
		CacheWriteTokens:         25,
		CacheCreationTokens:      26,
		InputTokensDetails: &dto.InputTokenDetails{
			CachedTokens:         30,
			CachedCreationTokens: 31,
			CacheCreationTokens:  32,
			CacheWriteTokens:     33,
			TextTokens:           34,
			AudioTokens:          35,
			ImageTokens:          36,
		},
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 37,
			TextTokens:   38,
		},
		CompletionTokenDetails: dto.OutputTokenDetails{
			TextTokens:      39,
			AudioTokens:     40,
			ImageTokens:     41,
			ReasoningTokens: 42,
		},
		OutputTokensDetails: &dto.OutputTokenDetails{
			TextTokens:      43,
			AudioTokens:     44,
			ImageTokens:     45,
			ReasoningTokens: 46,
		},
	})

	assert.Equal(t, 100, dst.PromptTokens)
	assert.Equal(t, 100, dst.InputTokens)
	assert.Equal(t, 20, dst.CompletionTokens)
	assert.Equal(t, 20, dst.OutputTokens)
	assert.Equal(t, 120, dst.TotalTokens)
	assert.Equal(t, 21, dst.PromptCacheHitTokens)
	assert.Equal(t, 22, dst.CacheReadInputTokens)
	assert.Equal(t, 23, dst.CacheReadTokens)
	assert.Equal(t, 24, dst.CacheCreationInputTokens)
	assert.Equal(t, 25, dst.CacheWriteTokens)
	assert.Equal(t, 26, dst.CacheCreationTokens)
	assert.Equal(t, 37, dst.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 31, dst.PromptTokensDetails.CachedCreationTokens)
	assert.Equal(t, 32, dst.PromptTokensDetails.CacheCreationTokens)
	assert.Equal(t, 33, dst.PromptTokensDetails.CacheWriteTokens)
	assert.Equal(t, 38, dst.PromptTokensDetails.TextTokens)
	assert.Equal(t, 35, dst.PromptTokensDetails.AudioTokens)
	assert.Equal(t, 36, dst.PromptTokensDetails.ImageTokens)
	assert.Equal(t, 43, dst.CompletionTokenDetails.TextTokens)
	assert.Equal(t, 44, dst.CompletionTokenDetails.AudioTokens)
	assert.Equal(t, 45, dst.CompletionTokenDetails.ImageTokens)
	assert.Equal(t, 46, dst.CompletionTokenDetails.ReasoningTokens)
	assert.Equal(t, 43, dst.OutputTokensDetails.TextTokens)
	assert.Equal(t, 44, dst.OutputTokensDetails.AudioTokens)
	assert.Equal(t, 45, dst.OutputTokensDetails.ImageTokens)
	assert.Equal(t, 46, dst.OutputTokensDetails.ReasoningTokens)
}

func TestMergeResponsesUsageKeepsNestedBillingUsageMonotonic(t *testing.T) {
	dst := &dto.Usage{
		InputTokens:  10,
		OutputTokens: 2,
		TotalTokens:  12,
		BillingUsage: dto.NewOpenAIResponsesBillingUsage(&dto.Usage{
			InputTokens:  10,
			OutputTokens: 2,
			TotalTokens:  12,
		}),
	}

	mergeResponsesUsage(dst, &dto.Usage{
		InputTokens:  50,
		OutputTokens: 7,
		TotalTokens:  57,
		BillingUsage: dto.NewOpenAIResponsesBillingUsage(&dto.Usage{
			InputTokens:  12,
			OutputTokens: 3,
			TotalTokens:  15,
		}),
	})

	mergeResponsesUsage(dst, &dto.Usage{
		InputTokens:  20,
		OutputTokens: 1,
		TotalTokens:  21,
		BillingUsage: dto.NewOpenAIResponsesBillingUsage(&dto.Usage{
			InputTokens:  20,
			OutputTokens: 1,
			TotalTokens:  21,
		}),
	})

	require.NotNil(t, dst.BillingUsage)
	require.NotNil(t, dst.BillingUsage.OpenAIUsage)
	assert.Equal(t, 50, dst.PromptTokens)
	assert.Equal(t, 7, dst.CompletionTokens)
	assert.Equal(t, 57, dst.TotalTokens)
	assert.Equal(t, 50, dst.BillingUsage.OpenAIUsage.PromptTokens)
	assert.Equal(t, 7, dst.BillingUsage.OpenAIUsage.CompletionTokens)
	assert.Equal(t, 57, dst.BillingUsage.OpenAIUsage.TotalTokens)
}
