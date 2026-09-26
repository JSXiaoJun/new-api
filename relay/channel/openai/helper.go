package openai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

// 辅助函数
func HandleStreamFormat(c *gin.Context, info *relaycommon.RelayInfo, data string, forceFormat bool, thinkToContent bool) error {
	info.SendResponseCount++

	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		return sendStreamData(c, info, data, forceFormat, thinkToContent)
	case types.RelayFormatClaude:
		return handleClaudeFormat(c, data, info)
	case types.RelayFormatGemini:
		return handleGeminiFormat(c, data, info)
	}
	return nil
}

func handleClaudeFormat(c *gin.Context, data string, info *relaycommon.RelayInfo) error {
	var streamResponse dto.ChatCompletionsStreamResponse
	if err := common.Unmarshal(common.StringToByteSlice(data), &streamResponse); err != nil {
		return err
	}

	if streamResponse.Usage != nil {
		info.ClaudeConvertInfo.Usage = streamResponse.Usage
	}
	result, err := relayconvert.ConvertStreamResponse(c, info, types.RelayFormatClaude, &streamResponse)
	if err != nil {
		return err
	}
	claudeResponses, ok := result.Value.([]*dto.ClaudeResponse)
	if !ok {
		return fmt.Errorf("expected Claude stream responses, got %T", result.Value)
	}
	for _, resp := range claudeResponses {
		helper.ClaudeData(c, *resp)
	}
	return nil
}

func handleGeminiFormat(c *gin.Context, data string, info *relaycommon.RelayInfo) error {
	var streamResponse dto.ChatCompletionsStreamResponse
	if err := common.Unmarshal(common.StringToByteSlice(data), &streamResponse); err != nil {
		logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
		return err
	}

	result, err := relayconvert.ConvertStreamResponse(c, info, types.RelayFormatGemini, &streamResponse)
	if err != nil {
		return err
	}
	geminiResponse, ok := result.Value.(*dto.GeminiChatResponse)
	if !ok {
		return fmt.Errorf("expected Gemini stream response, got %T", result.Value)
	}

	// 如果返回 nil，表示没有实际内容，跳过发送
	if geminiResponse == nil {
		return nil
	}

	geminiResponseStr, err := common.Marshal(geminiResponse)
	if err != nil {
		logger.LogError(c, "failed to marshal gemini response: "+err.Error())
		return err
	}

	// send gemini format response
	c.Render(-1, common.CustomEvent{Data: "data: " + string(geminiResponseStr)})
	_ = helper.FlushWriter(c)
	return nil
}

func ProcessStreamResponse(streamResponse dto.ChatCompletionsStreamResponse, responseTextBuilder *strings.Builder, toolCount *int) error {
	for _, choice := range streamResponse.Choices {
		responseTextBuilder.WriteString(choice.Delta.GetContentString())
		responseTextBuilder.WriteString(choice.Delta.GetReasoningContent())
		if choice.Delta.ToolCalls != nil {
			if len(choice.Delta.ToolCalls) > *toolCount {
				*toolCount = len(choice.Delta.ToolCalls)
			}
			for _, tool := range choice.Delta.ToolCalls {
				responseTextBuilder.WriteString(tool.Function.Name)
				responseTextBuilder.WriteString(tool.Function.Arguments)
			}
		}
	}
	return nil
}

func processTokenData(relayMode int, data string, responseTextBuilder *strings.Builder, toolCount *int) error {
	switch relayMode {
	case relayconstant.RelayModeChatCompletions:
		var streamResponse dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			return err
		}
		return ProcessStreamResponse(streamResponse, responseTextBuilder, toolCount)
	case relayconstant.RelayModeCompletions:
		var streamResponse dto.CompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			return err
		}
		processCompletionsStreamResponse(streamResponse, responseTextBuilder)
	}
	return nil
}

func processCompletionsStreamResponse(streamResponse dto.CompletionsStreamResponse, responseTextBuilder *strings.Builder) {
	for _, choice := range streamResponse.Choices {
		responseTextBuilder.WriteString(choice.Text)
	}
}

type chatStreamUsageEnvelope struct {
	Usage    *dto.Usage      `json:"usage"`
	Response json.RawMessage `json:"response"`
	Data     json.RawMessage `json:"data"`
	Choices  []struct {
		Usage *dto.Usage `json:"usage"`
	} `json:"choices"`
}

// extractChatStreamUsage accepts the usage locations used by OpenAI-compatible
// providers. Some providers wrap the payload in response/data objects, while
// others report usage on an individual choice. The dto.Usage fields cover both
// prompt/completion_tokens and input/output_tokens aliases.
func extractChatStreamUsage(data string) *dto.Usage {
	if strings.TrimSpace(data) == "" {
		return nil
	}

	var merged dto.Usage
	found := false
	var collect func([]byte, int)
	collect = func(raw []byte, depth int) {
		if depth > 8 || len(raw) == 0 {
			return
		}

		var envelope chatStreamUsageEnvelope
		if err := common.Unmarshal(raw, &envelope); err != nil {
			// A few gateways encode a nested envelope as a JSON string.
			var encoded string
			if common.Unmarshal(raw, &encoded) == nil && encoded != string(raw) {
				collect(common.StringToByteSlice(encoded), depth+1)
			}
			return
		}

		if envelope.Usage != nil && mergeChatStreamUsage(&merged, envelope.Usage) {
			found = true
		}
		for _, choice := range envelope.Choices {
			if choice.Usage != nil && mergeChatStreamUsage(&merged, choice.Usage) {
				found = true
			}
		}
		for _, nested := range []json.RawMessage{envelope.Response, envelope.Data} {
			if len(nested) == 0 || common.GetJsonType(nested) == "null" {
				continue
			}
			collect(nested, depth+1)
		}
	}

	collect(common.StringToByteSlice(data), 0)
	if !found {
		return nil
	}
	return &merged
}

// mergeChatStreamUsage keeps the largest observed value for each usage field.
// OpenAI-compatible gateways commonly emit cumulative snapshots, and terminal
// SSE frames frequently carry an empty or partial usage object. A smaller
// later snapshot must not erase usage observed earlier in the stream.
func mergeChatStreamUsage(dst *dto.Usage, src *dto.Usage) bool {
	if dst == nil || src == nil {
		return false
	}

	inputTokens := maxResponsesUsageInt(dst.PromptTokens, dst.InputTokens, src.PromptTokens, src.InputTokens)
	outputTokens := maxResponsesUsageInt(dst.CompletionTokens, dst.OutputTokens, src.CompletionTokens, src.OutputTokens)
	dst.PromptTokens = inputTokens
	dst.InputTokens = inputTokens
	dst.CompletionTokens = outputTokens
	dst.OutputTokens = outputTokens
	dst.TotalTokens = maxResponsesUsageInt(dst.TotalTokens, src.TotalTokens,
		addResponsesUsageTokens(dst.PromptTokens, dst.CompletionTokens))

	dst.PromptCacheHitTokens = maxResponsesUsageInt(dst.PromptCacheHitTokens, src.PromptCacheHitTokens)
	dst.CacheReadInputTokens = maxResponsesUsageInt(dst.CacheReadInputTokens, src.CacheReadInputTokens)
	dst.CacheCreationInputTokens = maxResponsesUsageInt(dst.CacheCreationInputTokens, src.CacheCreationInputTokens)
	dst.CacheReadTokens = maxResponsesUsageInt(dst.CacheReadTokens, src.CacheReadTokens)
	dst.CacheWriteTokens = maxResponsesUsageInt(dst.CacheWriteTokens, src.CacheWriteTokens)
	dst.CacheCreationTokens = maxResponsesUsageInt(dst.CacheCreationTokens, src.CacheCreationTokens)
	if src.UsageSemantic != "" {
		dst.UsageSemantic = src.UsageSemantic
	}
	if src.UsageSource != "" {
		dst.UsageSource = src.UsageSource
	}

	mergeInputTokenDetails(&dst.PromptTokensDetails, src.PromptTokensDetails)
	if src.InputTokensDetails != nil {
		if dst.InputTokensDetails == nil {
			details := *src.InputTokensDetails
			dst.InputTokensDetails = &details
		} else {
			mergeInputTokenDetails(dst.InputTokensDetails, *src.InputTokensDetails)
		}
		mergeInputTokenDetails(&dst.PromptTokensDetails, *src.InputTokensDetails)
	}
	mergeOutputTokenDetails(&dst.CompletionTokenDetails, src.CompletionTokenDetails)
	if src.OutputTokensDetails != nil {
		if dst.OutputTokensDetails == nil {
			dst.OutputTokensDetails = &dto.OutputTokenDetails{}
		}
		mergeOutputTokenDetails(dst.OutputTokensDetails, *src.OutputTokensDetails)
		mergeOutputTokenDetails(&dst.CompletionTokenDetails, *src.OutputTokensDetails)
	}

	return dst.PromptTokens > 0 ||
		dst.CompletionTokens > 0 ||
		dst.TotalTokens > 0 ||
		dst.PromptCacheHitTokens > 0 ||
		dst.CacheReadInputTokens > 0 ||
		dst.CacheCreationInputTokens > 0 ||
		dst.CacheReadTokens > 0 ||
		dst.CacheWriteTokens > 0 ||
		dst.CacheCreationTokens > 0 ||
		dst.PromptTokensDetails.CachedTokens > 0 ||
		dst.PromptTokensDetails.CachedCreationTokens > 0 ||
		dst.PromptTokensDetails.CacheCreationTokens > 0 ||
		dst.PromptTokensDetails.CacheWriteTokens > 0 ||
		dst.PromptTokensDetails.TextTokens > 0 ||
		dst.PromptTokensDetails.AudioTokens > 0 ||
		dst.PromptTokensDetails.ImageTokens > 0 ||
		dst.CompletionTokenDetails.TextTokens > 0 ||
		dst.CompletionTokenDetails.AudioTokens > 0 ||
		dst.CompletionTokenDetails.ImageTokens > 0 ||
		dst.CompletionTokenDetails.ReasoningTokens > 0
}

func mergeInputTokenDetails(dst *dto.InputTokenDetails, src dto.InputTokenDetails) {
	if dst == nil {
		return
	}
	if src.CachedTokens > 0 {
		dst.CachedTokens = maxResponsesUsageInt(dst.CachedTokens, src.CachedTokens)
	}
	if src.CachedCreationTokens > 0 {
		dst.CachedCreationTokens = maxResponsesUsageInt(dst.CachedCreationTokens, src.CachedCreationTokens)
	}
	if src.CacheCreationTokens > 0 {
		dst.CacheCreationTokens = maxResponsesUsageInt(dst.CacheCreationTokens, src.CacheCreationTokens)
	}
	if src.CacheWriteTokens > 0 {
		dst.CacheWriteTokens = maxResponsesUsageInt(dst.CacheWriteTokens, src.CacheWriteTokens)
	}
	if src.TextTokens > 0 {
		dst.TextTokens = maxResponsesUsageInt(dst.TextTokens, src.TextTokens)
	}
	if src.AudioTokens > 0 {
		dst.AudioTokens = maxResponsesUsageInt(dst.AudioTokens, src.AudioTokens)
	}
	if src.ImageTokens > 0 {
		dst.ImageTokens = maxResponsesUsageInt(dst.ImageTokens, src.ImageTokens)
	}
}

func mergeOutputTokenDetails(dst *dto.OutputTokenDetails, src dto.OutputTokenDetails) {
	if dst == nil {
		return
	}
	if src.TextTokens > 0 {
		dst.TextTokens = maxResponsesUsageInt(dst.TextTokens, src.TextTokens)
	}
	if src.AudioTokens > 0 {
		dst.AudioTokens = maxResponsesUsageInt(dst.AudioTokens, src.AudioTokens)
	}
	if src.ImageTokens > 0 {
		dst.ImageTokens = maxResponsesUsageInt(dst.ImageTokens, src.ImageTokens)
	}
	if src.ReasoningTokens > 0 {
		dst.ReasoningTokens = maxResponsesUsageInt(dst.ReasoningTokens, src.ReasoningTokens)
	}
}

func handleLastResponse(lastStreamData string, responseId *string, createAt *int64,
	systemFingerprint *string, model *string, usage **dto.Usage,
	containStreamUsage *bool, info *relaycommon.RelayInfo,
	shouldSendLastResp *bool) error {

	var lastStreamResponse dto.ChatCompletionsStreamResponse
	if err := common.Unmarshal(common.StringToByteSlice(lastStreamData), &lastStreamResponse); err != nil {
		return err
	}

	*responseId = lastStreamResponse.Id
	*createAt = lastStreamResponse.Created
	*systemFingerprint = lastStreamResponse.GetSystemFingerprint()
	*model = lastStreamResponse.Model

	if streamUsage := extractChatStreamUsage(lastStreamData); streamUsage != nil {
		if *usage == nil {
			*usage = &dto.Usage{}
		}
		if mergeChatStreamUsage(*usage, streamUsage) {
			*containStreamUsage = true
		}
	}
	if *containStreamUsage && !info.ShouldIncludeUsage {
		*shouldSendLastResp = lo.SomeBy(lastStreamResponse.Choices, func(choice dto.ChatCompletionsStreamResponseChoice) bool {
			return choice.Delta.GetContentString() != "" || choice.Delta.GetReasoningContent() != ""
		})
	}

	return nil
}

func HandleFinalResponse(c *gin.Context, info *relaycommon.RelayInfo, lastStreamData string,
	responseId string, createAt int64, model string, systemFingerprint string,
	usage *dto.Usage, containStreamUsage bool) {

	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		if info.ShouldIncludeUsage && !containStreamUsage {
			response := helper.GenerateFinalUsageResponse(responseId, createAt, model, *usage)
			response.SetSystemFingerprint(systemFingerprint)
			helper.ObjectData(c, response)
		}
		helper.Done(c)

	case types.RelayFormatClaude:
		var streamResponse dto.ChatCompletionsStreamResponse
		if err := common.Unmarshal(common.StringToByteSlice(lastStreamData), &streamResponse); err != nil {
			common.SysLog("error unmarshalling stream response: " + err.Error())
			return
		}

		info.ClaudeConvertInfo.Usage = usage

		result, err := relayconvert.ConvertStreamResponse(c, info, types.RelayFormatClaude, &streamResponse)
		if err != nil {
			common.SysLog("error converting Claude stream response: " + err.Error())
			return
		}
		claudeResponses, ok := result.Value.([]*dto.ClaudeResponse)
		if !ok {
			common.SysLog(fmt.Sprintf("expected Claude stream responses, got %T", result.Value))
			return
		}
		for _, resp := range claudeResponses {
			_ = helper.ClaudeData(c, *resp)
		}
		info.ClaudeConvertInfo.Done = true

	case types.RelayFormatGemini:
		var streamResponse dto.ChatCompletionsStreamResponse
		if err := common.Unmarshal(common.StringToByteSlice(lastStreamData), &streamResponse); err != nil {
			common.SysLog("error unmarshalling stream response: " + err.Error())
			return
		}

		// 这里处理的是 openai 最后一个流响应，其 delta 为空，有 finish_reason 字段
		// 因此相比较于 google 官方的流响应，由 openai 转换而来会多一个 parts 为空，finishReason 为 STOP 的响应
		// 而包含最后一段文本输出的响应（倒数第二个）的 finishReason 为 null
		// 暂不知是否有程序会不兼容。

		result, err := relayconvert.ConvertStreamResponse(c, info, types.RelayFormatGemini, &streamResponse)
		if err != nil {
			common.SysLog("error converting Gemini stream response: " + err.Error())
			return
		}
		geminiResponse, ok := result.Value.(*dto.GeminiChatResponse)
		if !ok {
			common.SysLog(fmt.Sprintf("expected Gemini stream response, got %T", result.Value))
			return
		}

		// openai 流响应开头的空数据
		if geminiResponse == nil {
			return
		}

		geminiResponseStr, err := common.Marshal(geminiResponse)
		if err != nil {
			common.SysLog("error marshalling gemini response: " + err.Error())
			return
		}

		// 发送最终的 Gemini 响应
		c.Render(-1, common.CustomEvent{Data: "data: " + string(geminiResponseStr)})
		_ = helper.FlushWriter(c)
	}
}

func sendResponsesStreamData(c *gin.Context, streamResponse dto.ResponsesStreamResponse, data string) {
	if data == "" {
		return
	}
	_ = helper.ResponseChunkData(c, streamResponse, data)
}
