package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFallbackImageUsageWithoutUpstreamUsage(t *testing.T) {
	n := uint(2)
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-image-1",
		Request: &dto.ImageRequest{
			Model: "gpt-image-1",
			N:     &n,
		},
	}
	info.SetEstimatePromptTokens(23)

	usage := fallbackImageUsage(info, &dto.Usage{})

	require.NotNil(t, usage)
	assert.Equal(t, common.PreConsumedQuota, usage.PromptTokens)
	assert.Equal(t, 3168, usage.CompletionTokens)
	assert.Equal(t, common.PreConsumedQuota+3168, usage.TotalTokens)
}

func TestFallbackImageUsageFillsMissingOutputFromImageCount(t *testing.T) {
	n := uint(2)
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-image-1",
		Request: &dto.ImageRequest{
			Model: "gpt-image-1",
			N:     &n,
		},
	}
	info.SetEstimatePromptTokens(23)

	usage := fallbackImageUsage(info, &dto.Usage{
		InputTokens: 12,
		TotalTokens: 12,
		PromptTokensDetails: dto.InputTokenDetails{
			ImageTokens: 12,
		},
	})

	require.NotNil(t, usage)
	assert.Equal(t, common.PreConsumedQuota, usage.PromptTokens, "respect the pre-consume floor")
	assert.Equal(t, 3168, usage.CompletionTokens, "bill output tokens for both requested images")
	assert.Equal(t, common.PreConsumedQuota+3168, usage.TotalTokens)
	assert.Equal(t, 12, usage.PromptTokensDetails.ImageTokens)
}

func TestImageUsageMissingOutputOnlyMatchesPartialUsage(t *testing.T) {
	assert.True(t, imageUsageMissingOutput(&dto.Usage{InputTokens: 12}))
	assert.True(t, imageUsageMissingOutput(&dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{ImageTokens: 12},
	}))
	assert.False(t, imageUsageMissingOutput(&dto.Usage{InputTokens: 12, OutputTokens: 1}))
	assert.False(t, imageUsageMissingOutput(&dto.Usage{
		CompletionTokenDetails: dto.OutputTokenDetails{ImageTokens: 1},
	}))
}

func TestFallbackImageUsageUsesReportedTotalToInferOutput(t *testing.T) {
	n := uint(2)
	info := &relaycommon.RelayInfo{
		Request: &dto.ImageRequest{Model: "gpt-image-1", N: &n},
	}

	usage := fallbackImageUsage(info, &dto.Usage{
		PromptTokens: 512,
		TotalTokens:  600,
	})

	require.NotNil(t, usage)
	assert.Equal(t, 512, usage.PromptTokens)
	assert.Equal(t, 88, usage.CompletionTokens, "prefer the upstream total over a local image-token estimate")
	assert.Equal(t, 600, usage.TotalTokens)
}

func TestFallbackImageUsageKeepsPreConsumedFloorWhenEstimateIsHigher(t *testing.T) {
	n := uint(1)
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-image-1",
		Request: &dto.ImageRequest{
			Model: "gpt-image-1",
			N:     &n,
		},
	}
	info.SetEstimatePromptTokens(common.PreConsumedQuota + 12)

	usage := fallbackImageUsage(info, nil)

	require.NotNil(t, usage)
	assert.Equal(t, common.PreConsumedQuota+12, usage.PromptTokens)
	assert.Equal(t, common.PreConsumedQuota+12+1584, usage.TotalTokens)
}

func TestFallbackImageUsageFixedPriceUsesSuccessMarker(t *testing.T) {
	n := uint(2)
	info := &relaycommon.RelayInfo{
		PriceData: hosttypes.PriceData{UsePrice: true},
		Request: &dto.ImageRequest{
			Model: "gpt-image-1",
			N:     &n,
		},
	}

	usage := fallbackImageUsage(info, nil)

	require.NotNil(t, usage)
	assert.Equal(t, 1, usage.PromptTokens)
	assert.Equal(t, 0, usage.CompletionTokens)
	assert.Equal(t, 1, usage.TotalTokens)
}
