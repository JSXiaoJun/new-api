package service

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
)

// ResponsesUsageAccumulator owns the accounting facts for one Responses stream.
// HTTP SSE and WebSocket transports feed the same events into it, then settle
// Finish's usage through the normal text billing path, including interrupted
// streams. Observe and Finish must be called by the same stream owner.
type ResponsesUsageAccumulator struct {
	info           *relaycommon.RelayInfo
	usage          *dto.Usage
	outputText     strings.Builder
	imageCounter   relaycommon.ImageGenerationCallCounter
	imageCommitted bool
	toolCounter    relaycommon.ResponsesToolCallCounter
	// tool_usage.image_gen is a cumulative provider snapshot; it is max-merged
	// like the HTTP SSE handler does.
	imageGenImages        int
	imageGenInputTokens   int
	imageGenOutputTokens  int
	imageGenInputDetails  dto.InputTokenDetails
	imageGenOutputDetails dto.OutputTokenDetails
	started               bool
	finished              bool
}

func NewResponsesUsageAccumulator(info *relaycommon.RelayInfo) *ResponsesUsageAccumulator {
	return &ResponsesUsageAccumulator{info: info, usage: &dto.Usage{}}
}

func (a *ResponsesUsageAccumulator) Observe(event *dto.ResponsesStreamResponse) {
	if a == nil || event == nil || a.finished {
		return
	}
	if event.Response != nil {
		a.info.ObserveResponseModel(event.Response.Model)
	}
	a.started = true
	ObserveResponsesOutcome(a.info, event)
	a.observeImageGenerationUsage(event.ToolUsage)
	if event.Response != nil {
		a.observeImageGenerationUsage(event.Response.ToolUsage)
	}
	switch event.Type {
	case "response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled":
		if event.Response != nil {
			ApplyResponsesUsage(a.usage, event.Response.Usage)
			if a.outputText.Len() == 0 {
				// Some upstreams carry the output only on the terminal event.
				a.outputText.WriteString(relayconvert.ExtractOutputTextFromResponses(event.Response))
			}
		}
		failed := event.Type != "response.completed" && event.Type != "response.done"
		if !failed && event.Response != nil {
			// A terminal response can carry tool calls that never arrived as
			// output_item.done; the counter deduplicates repeated items.
			for i := range event.Response.Output {
				a.toolCounter.Count(a.info, &event.Response.Output[i], &i)
			}
		}
		if a.imageCommitted {
			return
		}
		if failed || (event.Response != nil && relaycommon.IsNonBillableResponsesStatus(event.Response.Status)) {
			a.imageCounter.Reset()
		} else {
			if event.Response != nil {
				for i := range event.Response.Output {
					a.imageCounter.Observe(&event.Response.Output[i], &i)
				}
			}
			a.imageCounter.EnsureAtLeast(a.imageGenImages)
		}
		a.imageCounter.Commit(a.info)
		a.imageCommitted = true
	case "response.output_text.delta", "response.function_call_arguments.delta",
		"response.reasoning_summary_text.delta", "response.reasoning_text.delta", "response.refusal.delta":
		// Every delta kind here is generated output that upstream bills as
		// output tokens, so all of them feed the missing-usage estimate.
		a.outputText.WriteString(event.Delta)
	case dto.ResponsesOutputTypeItemDone:
		if event.Item == nil {
			return
		}
		a.toolCounter.Count(a.info, event.Item, event.OutputIndex)
		if event.Item.Type == dto.ResponsesOutputTypeImageGenerationCall && !a.imageCommitted {
			a.imageCounter.Observe(event.Item, event.OutputIndex)
		}
	}
}

func (a *ResponsesUsageAccumulator) observeImageGenerationUsage(toolUsage *dto.ResponsesToolUsage) {
	if toolUsage == nil || toolUsage.ImageGen == nil {
		return
	}
	usage := toolUsage.ImageGen
	a.imageGenImages = max(a.imageGenImages, usage.Images)
	a.imageGenInputTokens = max(a.imageGenInputTokens, usage.InputTokens)
	a.imageGenOutputTokens = max(a.imageGenOutputTokens, usage.OutputTokens)
	if usage.InputTokensDetails != nil {
		mergeBillingInputTokenDetails(&a.imageGenInputDetails, *usage.InputTokensDetails)
	}
	if usage.OutputTokensDetails != nil {
		mergeBillingOutputTokenDetails(&a.imageGenOutputDetails, *usage.OutputTokensDetails)
	}
}

func (a *ResponsesUsageAccumulator) Finish() *dto.Usage {
	if a.finished {
		return a.usage
	}
	a.finished = true
	// A final image item can already have reached the client before the stream
	// disconnects. Explicit failed/incomplete terminals reset and commit zero in
	// Observe; otherwise retain completed tool usage even without a terminal.
	if !a.imageCommitted {
		a.imageCounter.EnsureAtLeast(a.imageGenImages)
		a.imageCounter.Commit(a.info)
		a.imageCommitted = true
	}
	// Image generation tool tokens join the request usage with the HTTP SSE
	// handler's max rule. Settlement prefers a nested billing snapshot, so it
	// receives the same values (on a detached copy).
	targets := []*dto.Usage{a.usage}
	if a.usage.BillingUsage != nil && a.usage.BillingUsage.OpenAIUsage != nil {
		a.usage.BillingUsage = dto.CloneBillingUsage(a.usage.BillingUsage)
		targets = append(targets, a.usage.BillingUsage.OpenAIUsage)
	}
	for _, target := range targets {
		if a.imageGenInputTokens > 0 || a.imageGenOutputTokens > 0 {
			target.PromptTokens = max(target.PromptTokens, target.InputTokens, a.imageGenInputTokens)
			target.InputTokens = target.PromptTokens
			target.CompletionTokens = max(target.CompletionTokens, target.OutputTokens, a.imageGenOutputTokens)
			target.OutputTokens = target.CompletionTokens
			target.TotalTokens = max(target.TotalTokens, addUsageInts(target.PromptTokens, target.CompletionTokens))
		}
		mergeBillingInputTokenDetails(&target.PromptTokensDetails, a.imageGenInputDetails)
		mergeBillingOutputTokenDetails(&target.CompletionTokenDetails, a.imageGenOutputDetails)
	}
	// Split a total-only or one-sided upstream snapshot before any local
	// estimate, as settlement and the HTTP SSE handler do; otherwise the
	// estimate below would replace the reported total.
	total := a.usage.TotalTokens
	switch {
	case a.usage.PromptTokens == 0 && a.usage.CompletionTokens == 0 && total > 0:
		a.usage.PromptTokens = min(max(a.info.GetEstimatePromptTokens(), 0), total)
		a.usage.CompletionTokens = total - a.usage.PromptTokens
	case a.usage.PromptTokens == 0 && total > a.usage.CompletionTokens:
		a.usage.PromptTokens = total - a.usage.CompletionTokens
	case a.usage.CompletionTokens == 0 && total > a.usage.PromptTokens:
		a.usage.CompletionTokens = total - a.usage.PromptTokens
	}
	if a.usage.CompletionTokens == 0 {
		if output := a.outputText.String(); output != "" {
			a.usage.CompletionTokens = CountTextToken(output, a.info.GetUpstreamModelName())
		}
	}
	// Upstream bills the prompt as soon as it starts generating, so a stream
	// that produced any event but no usage still owes its input tokens unless
	// upstream reported an explicit failure.
	billsPrompt := a.usage.CompletionTokens != 0 || (a.started && !a.info.StreamStatus.ResponseFailed())
	if a.usage.PromptTokens == 0 && billsPrompt {
		a.usage.PromptTokens = a.info.GetEstimatePromptTokens()
	}
	a.usage.TotalTokens = max(a.usage.TotalTokens, addUsageInts(a.usage.PromptTokens, a.usage.CompletionTokens))
	a.usage.InputTokens = max(a.usage.InputTokens, a.usage.PromptTokens)
	a.usage.OutputTokens = max(a.usage.OutputTokens, a.usage.CompletionTokens)
	if a.usage.BillingUsage != nil {
		a.usage.BillingUsage = dto.CloneBillingUsageWithEstimatedCompletion(a.usage.BillingUsage, a.usage.CompletionTokens)
	}
	return a.usage
}

// ObserveResponsesOutcome records the protocol outcome of one Responses event
// on the stream status for health classification. Only codes and types are
// kept; messages never leave the event.
func ObserveResponsesOutcome(info *relaycommon.RelayInfo, event *dto.ResponsesStreamResponse) {
	if info == nil || info.StreamStatus == nil || event == nil {
		return
	}
	var responseStatus string
	if event.Response != nil {
		_ = common.Unmarshal(event.Response.Status, &responseStatus)
	}
	switch {
	case event.Type == "error" || event.Type == "response.failed" || event.Type == "response.error" || responseStatus == "failed":
		code, errorType := event.Code, ""
		if event.Response != nil {
			if oaiErr := event.Response.GetOpenAIError(); oaiErr != nil {
				if oaiErr.Code != nil {
					code = fmt.Sprint(oaiErr.Code)
				}
				errorType = oaiErr.Type
			}
		}
		info.StreamStatus.MarkFailed(code, errorType, 0)
	case event.Type == "response.incomplete" || responseStatus == "incomplete":
		reason := ""
		if event.Response != nil && event.Response.IncompleteDetails != nil {
			reason = event.Response.IncompleteDetails.Reason
		}
		info.StreamStatus.MarkIncomplete(reason)
	case event.Type == "response.cancelled" || event.Type == "response.canceled" || responseStatus == "cancelled":
		info.StreamStatus.MarkCancelled()
	case event.Type == "response.completed" || event.Type == "response.done" || responseStatus == "completed":
		info.StreamStatus.MarkCompleted()
	}
}

func ApplyResponsesUsage(dst *dto.Usage, src *dto.Usage) {
	if dst == nil || src == nil {
		return
	}
	incoming := relayconvert.NormalizeResponsesUsage(src)
	if src.InputTokensDetails != nil {
		inputDetails := *src.InputTokensDetails
		incoming.InputTokensDetails = &inputDetails
	}
	if src.OutputTokensDetails != nil {
		incoming.CompletionTokenDetails = *src.OutputTokensDetails
	}
	incoming.PromptCacheHitTokens = src.PromptCacheHitTokens
	dto.MergeUsageNonZero(dst, incoming)
	outputDetails := dst.CompletionTokenDetails
	if outputDetails != (dto.OutputTokenDetails{}) {
		dst.OutputTokensDetails = &outputDetails
	}
}
