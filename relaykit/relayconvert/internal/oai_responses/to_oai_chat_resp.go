package oairesponses

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

const (
	responsesEventCreated                  = "response.created"
	responsesEventCompleted                = "response.completed"
	responsesEventDone                     = "response.done"
	responsesEventIncomplete               = "response.incomplete"
	responsesEventFailed                   = "response.failed"
	responsesEventError                    = "response.error"
	responsesEventOutputTextDelta          = "response.output_text.delta"
	responsesEventOutputItemAdded          = "response.output_item.added"
	responsesEventOutputItemDone           = "response.output_item.done"
	responsesEventFunctionArgsDelta        = "response.function_call_arguments.delta"
	responsesEventFunctionArgsDone         = "response.function_call_arguments.done"
	responsesEventCustomToolInputDelta     = "response.custom_tool_call_input.delta"
	responsesEventCustomToolInputDone      = "response.custom_tool_call_input.done"
	responsesEventReasoningSummaryDelta    = "response.reasoning_summary_text.delta"
	responsesEventReasoningSummaryDone     = "response.reasoning_summary_text.done"
	responsesEventReasoningTextDelta       = "response.reasoning_text.delta"
	responsesEventReasoningTextDone        = "response.reasoning_text.done"
	responsesOutputTypeFunctionCall        = "function_call"
	responsesOutputTypeCustomToolCall      = "custom_tool_call"
	responsesOutputTypeMessage             = "message"
	responsesOutputTypeReasoning           = "reasoning"
	responsesIncompleteReasonContentFilter = "content_filter"
	responsesIncompleteReasonMaxTokens     = "max_output_tokens"
)

func ResponsesFinishReasonFromStatus(resp *dto.OpenAIResponsesResponse) (string, bool) {
	if resp == nil {
		return "", false
	}

	status := responseStatusString(resp)
	if status != "incomplete" {
		return "", false
	}

	reason := ""
	if resp.IncompleteDetails != nil {
		reason = strings.TrimSpace(resp.IncompleteDetails.Reason)
	}
	if reason == responsesIncompleteReasonContentFilter {
		return "content_filter", true
	}
	return "length", true
}

func ResponsesResponseToChatCompletionsResponse(resp *dto.OpenAIResponsesResponse, id string) (*dto.OpenAITextResponse, *dto.Usage, error) {
	if resp == nil {
		return nil, nil, errors.New("response is nil")
	}

	text := ExtractOutputTextFromResponses(resp)
	reasoning := ExtractReasoningTextFromResponses(resp)

	usage := UsageFromResponsesResponse(resp)

	created := resp.CreatedAt

	var toolCalls []dto.ToolCallResponse
	if len(resp.Output) > 0 {
		for _, out := range resp.Output {
			if !isResponsesToolOutputType(out.Type) {
				continue
			}
			name := strings.TrimSpace(out.Name)
			if name == "" {
				continue
			}
			callId := strings.TrimSpace(out.CallId)
			if callId == "" {
				callId = strings.TrimSpace(out.ID)
			}
			toolCalls = append(toolCalls, dto.ToolCallResponse{
				ID:   callId,
				Type: "function",
				Function: dto.FunctionResponse{
					Name:      name,
					Arguments: out.ArgumentsString(),
				},
			})
		}
	}

	finishReason := "stop"
	if mappedReason, ok := ResponsesFinishReasonFromStatus(resp); ok {
		finishReason = mappedReason
	} else if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	msg := dto.Message{
		Role:    "assistant",
		Content: text,
	}
	if reasoning != "" {
		msg.ReasoningContent = &reasoning
	}
	if len(toolCalls) > 0 {
		msg.SetToolCalls(toolCalls)
	}

	out := &dto.OpenAITextResponse{
		Id:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   resp.Model,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: finishReason,
			},
		},
		Usage: *usage,
	}

	return out, usage, nil
}

// UsageFromResponsesResponse converts a complete Responses response into the
// canonical usage shape used by the host billing layer. Compatible gateways
// sometimes put tool usage next to the regular usage object, so both sources
// are merged before returning.
func UsageFromResponsesResponse(resp *dto.OpenAIResponsesResponse) *dto.Usage {
	if resp == nil {
		return nil
	}
	usage := UsageFromResponsesUsage(resp.Usage)
	mergeResponsesToolUsage(usage, resp.ToolUsage)
	return usage
}

// UsageFromResponsesUsage normalizes both native Responses names
// (input/output_tokens) and Chat Completions compatible aliases
// (prompt/completion_tokens). It deliberately keeps total-only usage as
// total-only; the request-aware billing layer can split it using its prompt
// estimate without inventing a provider-side breakdown here.
func UsageFromResponsesUsage(src *dto.Usage) *dto.Usage {
	usage := &dto.Usage{}
	if src == nil {
		return usage
	}
	if src.BillingUsage != nil && src.BillingUsage.OpenAIUsage != nil {
		mergeResponsesUsageFields(usage, src.BillingUsage.OpenAIUsage)
	}
	mergeResponsesUsageFields(usage, src)
	if src.BillingUsage != nil {
		usage.BillingUsage = dto.CloneBillingUsage(src.BillingUsage)
	} else {
		// Keep the nested source payload faithful to the upstream spelling. The
		// service billing layer normalizes input/output aliases when it consumes
		// this nested record, while preserving this shape avoids changing the
		// serialized response contract for native Responses usage.
		usage.BillingUsage = dto.NewOpenAIResponsesBillingUsage(src)
	}
	syncResponsesBillingUsage(usage)
	return usage
}

// MergeResponsesUsage merges a usage snapshot into dst. Responses streams can
// report cumulative snapshots more than once, so token counts use the largest
// non-negative value rather than summing snapshots. Missing snapshots never
// erase a previously observed authoritative value.
func MergeResponsesUsage(dst, src *dto.Usage) *dto.Usage {
	if src == nil {
		return dst
	}
	if dst == nil {
		dst = &dto.Usage{}
	}
	// Compatible Responses gateways may expose the authoritative snapshot only
	// under billing_usage.openai_usage. Promote it before the top-level merge so
	// callers never fall back to local text estimation for a real usage record.
	if src.BillingUsage != nil && src.BillingUsage.OpenAIUsage != nil {
		mergeResponsesUsageFields(dst, src.BillingUsage.OpenAIUsage)
	}
	mergeResponsesUsageFields(dst, src)
	if dst.BillingUsage == nil && src.BillingUsage != nil {
		dst.BillingUsage = dto.CloneBillingUsage(src.BillingUsage)
	}
	syncResponsesBillingUsage(dst)
	return dst
}

// UsageFromResponsesStreamResponse finds usage in all envelopes emitted by
// Responses-compatible gateways: event.usage, event.response.usage,
// event.data.usage, and nested data.response.usage. The raw data traversal is
// intentionally bounded because Data is provider-controlled JSON.
func UsageFromResponsesStreamResponse(event *dto.ResponsesStreamResponse) *dto.Usage {
	if event == nil {
		return nil
	}
	var usage *dto.Usage
	usage = MergeResponsesUsage(usage, event.Usage)
	if event.Response != nil {
		usage = MergeResponsesUsage(usage, UsageFromResponsesResponse(event.Response))
	}
	usage = MergeResponsesUsage(usage, usageFromResponsesRawEnvelope(event.Data, 0))
	if usage == nil || !hasResponsesUsageData(usage) {
		return nil
	}
	return usage
}

func usageFromResponsesRawEnvelope(raw json.RawMessage, depth int) *dto.Usage {
	if depth > 5 {
		return nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var object map[string]json.RawMessage
	if err := kitutil.Unmarshal(trimmed, &object); err != nil {
		var nested string
		if kitutil.Unmarshal(trimmed, &nested) == nil && strings.TrimSpace(nested) != "" {
			return usageFromResponsesRawEnvelope([]byte(nested), depth+1)
		}
		return nil
	}

	var usage *dto.Usage
	if usageRaw, ok := object["usage"]; ok {
		var parsed dto.Usage
		if kitutil.Unmarshal(usageRaw, &parsed) == nil {
			usage = MergeResponsesUsage(usage, &parsed)
		}
	}
	if toolRaw, ok := object["tool_usage"]; ok {
		var toolUsage dto.ResponsesToolUsage
		if kitutil.Unmarshal(toolRaw, &toolUsage) == nil {
			if usage == nil {
				usage = &dto.Usage{}
			}
			mergeResponsesToolUsage(usage, &toolUsage)
		}
	}
	for _, key := range []string{"response", "data"} {
		if nestedRaw, ok := object[key]; ok {
			usage = MergeResponsesUsage(usage, usageFromResponsesRawEnvelope(nestedRaw, depth+1))
		}
	}
	return usage
}

func mergeResponsesUsageFields(dst, src *dto.Usage) {
	if dst == nil || src == nil {
		return
	}
	dst.UsageSemantic = firstNonEmptyUsageString(dst.UsageSemantic, src.UsageSemantic)
	dst.UsageSource = firstNonEmptyUsageString(dst.UsageSource, src.UsageSource)
	if dst.Cost == nil && src.Cost != nil {
		dst.Cost = src.Cost
	}

	// Keep both spellings populated in the canonical result. This also handles
	// Sub2API responses that only expose prompt_tokens/completion_tokens.
	inputTokens := maxUsageInt(dst.PromptTokens, dst.InputTokens, src.PromptTokens, src.InputTokens)
	outputTokens := maxUsageInt(dst.CompletionTokens, dst.OutputTokens, src.CompletionTokens, src.OutputTokens)
	dst.PromptTokens = inputTokens
	dst.InputTokens = inputTokens
	dst.CompletionTokens = outputTokens
	dst.OutputTokens = outputTokens
	dst.TotalTokens = maxUsageInt(dst.TotalTokens, src.TotalTokens)

	dst.PromptCacheHitTokens = maxUsageInt(dst.PromptCacheHitTokens, src.PromptCacheHitTokens, src.CacheReadInputTokens, src.CacheReadTokens)
	dst.CacheReadInputTokens = maxUsageInt(dst.CacheReadInputTokens, src.CacheReadInputTokens, src.CacheReadTokens, src.PromptCacheHitTokens)
	dst.CacheReadTokens = maxUsageInt(dst.CacheReadTokens, src.CacheReadTokens, src.CacheReadInputTokens, src.PromptCacheHitTokens)
	dst.CacheCreationInputTokens = maxUsageInt(dst.CacheCreationInputTokens, src.CacheCreationInputTokens, src.CacheCreationTokens)
	dst.CacheCreationTokens = maxUsageInt(dst.CacheCreationTokens, src.CacheCreationTokens, src.CacheCreationInputTokens)
	dst.CacheWriteTokens = maxUsageInt(dst.CacheWriteTokens, src.CacheWriteTokens)
	dst.ClaudeCacheCreation5mTokens = maxUsageInt(dst.ClaudeCacheCreation5mTokens, src.ClaudeCacheCreation5mTokens)
	dst.ClaudeCacheCreation1hTokens = maxUsageInt(dst.ClaudeCacheCreation1hTokens, src.ClaudeCacheCreation1hTokens)

	if src.InputTokensDetails != nil {
		if dst.InputTokensDetails == nil {
			details := *src.InputTokensDetails
			dst.InputTokensDetails = &details
		} else {
			mergeInputTokenDetails(dst.InputTokensDetails, *src.InputTokensDetails)
		}
		mergeInputTokenDetails(&dst.PromptTokensDetails, *src.InputTokensDetails)
	}
	mergeInputTokenDetails(&dst.PromptTokensDetails, src.PromptTokensDetails)
	if src.OutputTokensDetails != nil {
		if dst.OutputTokensDetails == nil {
			details := *src.OutputTokensDetails
			dst.OutputTokensDetails = &details
		} else {
			mergeOutputTokenDetails(dst.OutputTokensDetails, *src.OutputTokensDetails)
		}
		mergeOutputTokenDetails(&dst.CompletionTokenDetails, *src.OutputTokensDetails)
	}
	mergeOutputTokenDetails(&dst.CompletionTokenDetails, src.CompletionTokenDetails)

	sides := saturatingUsageSum(dst.PromptTokens, dst.CompletionTokens)
	if dst.TotalTokens == 0 {
		dst.TotalTokens = sides
	}
	if dst.InputTokens == 0 {
		dst.InputTokens = dst.PromptTokens
	}
	if dst.OutputTokens == 0 {
		dst.OutputTokens = dst.CompletionTokens
	}
}

func mergeInputTokenDetails(dst *dto.InputTokenDetails, src dto.InputTokenDetails) {
	if dst == nil {
		return
	}
	dst.CachedTokens = maxUsageInt(dst.CachedTokens, src.CachedTokens)
	dst.CachedCreationTokens = maxUsageInt(dst.CachedCreationTokens, src.CachedCreationTokens)
	dst.CacheCreationTokens = maxUsageInt(dst.CacheCreationTokens, src.CacheCreationTokens)
	dst.CacheWriteTokens = maxUsageInt(dst.CacheWriteTokens, src.CacheWriteTokens)
	dst.TextTokens = maxUsageInt(dst.TextTokens, src.TextTokens)
	dst.AudioTokens = maxUsageInt(dst.AudioTokens, src.AudioTokens)
	dst.ImageTokens = maxUsageInt(dst.ImageTokens, src.ImageTokens)
}

func mergeOutputTokenDetails(dst *dto.OutputTokenDetails, src dto.OutputTokenDetails) {
	if dst == nil {
		return
	}
	dst.ReasoningTokens = maxUsageInt(dst.ReasoningTokens, src.ReasoningTokens)
	dst.TextTokens = maxUsageInt(dst.TextTokens, src.TextTokens)
	dst.AudioTokens = maxUsageInt(dst.AudioTokens, src.AudioTokens)
	dst.ImageTokens = maxUsageInt(dst.ImageTokens, src.ImageTokens)
}

func mergeResponsesToolUsage(dst *dto.Usage, toolUsage *dto.ResponsesToolUsage) {
	if dst == nil || toolUsage == nil || toolUsage.ImageGen == nil {
		return
	}
	imageGen := toolUsage.ImageGen
	inputTokens := maxUsageInt(dst.PromptTokens, dst.InputTokens, imageGen.InputTokens)
	outputTokens := maxUsageInt(dst.CompletionTokens, dst.OutputTokens, imageGen.OutputTokens)
	dst.PromptTokens = inputTokens
	dst.InputTokens = inputTokens
	dst.CompletionTokens = outputTokens
	dst.OutputTokens = outputTokens
	if imageGen.InputTokensDetails != nil {
		mergeInputTokenDetails(&dst.PromptTokensDetails, *imageGen.InputTokensDetails)
	}
	if imageGen.OutputTokensDetails != nil {
		mergeOutputTokenDetails(&dst.CompletionTokenDetails, *imageGen.OutputTokensDetails)
	}
	if dst.TotalTokens < saturatingUsageSum(dst.PromptTokens, dst.CompletionTokens) {
		dst.TotalTokens = saturatingUsageSum(dst.PromptTokens, dst.CompletionTokens)
	}
	syncResponsesBillingUsage(dst)
}

// syncResponsesBillingUsage keeps the nested billing payload in lockstep with
// the canonical top-level usage. Settlement intentionally prefers
// BillingUsage when present, so leaving this record at an earlier stream
// snapshot would silently undercharge later token usage or tool usage.
func syncResponsesBillingUsage(usage *dto.Usage) {
	if usage == nil || usage.BillingUsage == nil || usage.BillingUsage.OpenAIUsage == nil {
		return
	}
	mergeResponsesUsageFields(usage.BillingUsage.OpenAIUsage, usage)
}

func hasResponsesUsageData(usage *dto.Usage) bool {
	if usage == nil {
		return false
	}
	return usage.PromptTokens != 0 || usage.CompletionTokens != 0 || usage.TotalTokens != 0 ||
		usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.PromptCacheHitTokens != 0 ||
		usage.CacheReadInputTokens != 0 || usage.CacheCreationInputTokens != 0 ||
		usage.CacheReadTokens != 0 || usage.CacheWriteTokens != 0 || usage.CacheCreationTokens != 0 ||
		usage.PromptTokensDetails != (dto.InputTokenDetails{}) ||
		usage.CompletionTokenDetails != (dto.OutputTokenDetails{}) || usage.InputTokensDetails != nil ||
		usage.OutputTokensDetails != nil
}

func maxUsageInt(values ...int) int {
	max := 0
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}

func saturatingUsageSum(a, b int) int {
	a = maxUsageInt(a)
	b = maxUsageInt(b)
	maxInt := int(^uint(0) >> 1)
	if a > maxInt-b {
		return maxInt
	}
	return a + b
}

func firstNonEmptyUsageString(current, incoming string) string {
	if strings.TrimSpace(current) != "" {
		return current
	}
	return incoming
}

func ExtractOutputTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || len(resp.Output) == 0 {
		return ""
	}

	var sb strings.Builder

	// Prefer assistant message outputs.
	for _, out := range resp.Output {
		if out.Type != "message" {
			continue
		}
		if out.Role != "" && out.Role != "assistant" {
			continue
		}
		for _, c := range out.Content {
			if c.Type == "output_text" && c.Text != "" {
				sb.WriteString(c.Text)
			}
		}
	}
	if sb.Len() > 0 {
		return sb.String()
	}
	for _, out := range resp.Output {
		for _, c := range out.Content {
			if c.Text != "" {
				sb.WriteString(c.Text)
			}
		}
	}
	return sb.String()
}

func ExtractReasoningTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || len(resp.Output) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, out := range resp.Output {
		if out.Type != responsesOutputTypeReasoning {
			continue
		}
		for _, c := range out.Content {
			if c.Text != "" {
				sb.WriteString(c.Text)
			}
		}
	}
	return sb.String()
}

func responseStatusString(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || len(resp.Status) == 0 {
		return ""
	}
	var status string
	_ = kitutil.Unmarshal(resp.Status, &status)
	return strings.TrimSpace(status)
}

func ensureIncompleteResponse(resp *dto.OpenAIResponsesResponse) *dto.OpenAIResponsesResponse {
	if resp == nil {
		resp = &dto.OpenAIResponsesResponse{}
	}
	if len(resp.Status) == 0 {
		resp.Status = []byte(`"incomplete"`)
	}
	return resp
}

func isResponsesToolOutputType(outputType string) bool {
	return outputType == responsesOutputTypeFunctionCall || outputType == responsesOutputTypeCustomToolCall
}

func responseStreamEventItemID(event *dto.ResponsesStreamResponse) string {
	if event == nil {
		return ""
	}
	if event.Item != nil {
		if itemID := strings.TrimSpace(event.Item.ID); itemID != "" {
			return itemID
		}
	}
	return strings.TrimSpace(event.ItemID)
}

func fallbackToolKey(itemID string, callID string, outputIndex *int) string {
	if outputIndex != nil {
		return fmt.Sprintf("output:%d", *outputIndex)
	}
	if strings.TrimSpace(itemID) != "" {
		return "item:" + strings.TrimSpace(itemID)
	}
	if strings.TrimSpace(callID) != "" {
		return "call:" + strings.TrimSpace(callID)
	}
	return ""
}

func fallbackCallID(event *dto.ResponsesStreamResponse) string {
	if event == nil {
		return ""
	}
	if strings.TrimSpace(event.ItemID) != "" {
		return strings.TrimSpace(event.ItemID)
	}
	if event.OutputIndex != nil {
		return fmt.Sprintf("call_output_%d", *event.OutputIndex)
	}
	return ""
}
