package openai

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func OaiResponsesToChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var responsesResp dto.OpenAIResponsesResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	if err := common.Unmarshal(body, &responsesResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if oaiError := responsesResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	info.ObserveResponseModel(responsesResp.Model)
	billing := newResponsesChatBillingTracker()
	billing.observeResponse(info, &responsesResp)
	billing.commit(info)
	responseValue, usage, err := convertResponsesResponseForClient(c, info, &responsesResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	responseBody, err := common.Marshal(responseValue)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)
	return usage, nil
}

func OaiResponsesToChatBufferedStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.RequireTerminal()
	accumulator := relayconvert.NewResponsesBufferedAccumulator()
	billing := newResponsesChatBillingTracker()
	var finalResponse *dto.OpenAIResponsesResponse
	var streamErr *types.NewAPIError
	eventType := ""

	scanner := helper.NewStreamScanner(resp.Body)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if line == "[DONE]" {
			break
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			if data == "[DONE]" {
				break
			}
			continue
		}

		var streamResp dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResp); err != nil {
			logger.LogError(c, common.LogText("failed to unmarshal buffered responses stream event: %s", err.Error()))
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
			break
		}
		if streamResp.Type == "" {
			streamResp.Type = eventType
		}
		eventType = ""
		if streamResp.Response != nil {
			info.ObserveResponseModel(streamResp.Response.Model)
		}
		service.ObserveResponsesOutcome(info, &streamResp)
		accumulator.ProcessEvent(&streamResp)
		billing.observeEvent(info, &streamResp)
		switch streamResp.Type {
		case "response.completed", "response.done", "response.incomplete":
			if streamResp.Response != nil {
				finalResponse = streamResp.Response
			}
			if streamResp.Type == "response.incomplete" {
				if finalResponse == nil {
					finalResponse = &dto.OpenAIResponsesResponse{}
				}
				if len(finalResponse.Status) == 0 {
					finalResponse.Status = []byte(`"incomplete"`)
				}
			}
		case "response.failed", "response.error":
			if streamResp.Response != nil {
				if oaiErr := streamResp.Response.GetOpenAIError(); oaiErr != nil && oaiErr.Type != "" {
					streamErr = types.WithOpenAIError(*oaiErr, http.StatusInternalServerError)
					break
				}
			}
			streamErr = types.NewOpenAIError(fmt.Errorf("responses stream error: %s", streamResp.Type), types.ErrorCodeBadResponse, http.StatusInternalServerError)
		}
		if streamErr != nil {
			break
		}
	}
	if streamErr != nil {
		return nil, streamErr
	}
	if err := scanner.Err(); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	if finalResponse == nil {
		finalResponse = &dto.OpenAIResponsesResponse{
			ID:        helper.GetResponseID(c),
			CreatedAt: dto.IntValue(time.Now().Unix()),
			Model:     info.UpstreamModelName,
			Status:    []byte(`"completed"`),
		}
	}
	accumulator.SupplementResponseOutput(finalResponse)
	billing.observeResponse(info, finalResponse)
	billing.commit(info)

	responseValue, usage, err := convertResponsesResponseForClient(c, info, finalResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	responseBody, err := common.Marshal(responseValue)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)
	return usage, nil
}

func convertResponsesResponseForClient(c *gin.Context, info *relaycommon.RelayInfo, response *dto.OpenAIResponsesResponse) (any, *dto.Usage, error) {
	if responseID := helper.GetResponseID(c); responseID != "" {
		response.ID = responseID
	}

	// Normalize alias spellings and hosted-tool usage, and estimate missing
	// output before conversion so the client body and settlement agree.
	usage := relayconvert.UsageFromResponsesUsage(response.Usage)
	if usage == nil {
		usage = &dto.Usage{}
	}
	mergeResponsesToolUsage(usage, response.ToolUsage)
	usage = normalizeResponsesUsage(usage, info.GetEstimatePromptTokens())
	ensureOpenAIUsageCompletion(c, usage, service.ExtractOutputTextFromResponses(response), info.UpstreamModelName, info.GetEstimatePromptTokens())
	response.Usage = relayconvert.UsageFromChatUsage(usage)

	result, err := service.ConvertResponse(c, info, info.RelayFormat, response)
	if err != nil {
		return nil, nil, err
	}
	if result.Usage != nil {
		mergeResponsesUsage(usage, result.Usage)
	}
	return result.Value, usage, nil
}

func OaiResponsesToChatStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	responseId := helper.GetResponseID(c)
	createAt := time.Now().Unix()
	state, err := relayconvert.NewResponseStreamState(types.RelayFormatOpenAIResponses, info.RelayFormat, relayconvert.ResponseStreamOptions{
		ID:      responseId,
		Model:   info.UpstreamModelName,
		Created: createAt,
	})
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	streamErr := (*types.NewAPIError)(nil)
	billing := newResponsesChatBillingTracker()
	// upstreamFailed records a response.failed / response.error event and
	// failedUsage the usage it reported, if any.
	upstreamFailed := false
	var failedUsage *dto.Usage

	if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo == nil {
		info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
	}

	// writeFailed stops the stream after a failed client write. When the client
	// went away the upstream already produced output, so the request settles
	// normally instead of failing; any other write error stays a server error.
	writeFailed := func(err error) bool {
		if c.Request.Context().Err() != nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
			return false
		}
		streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		return false
	}

	sendGeminiResponse := func(geminiResponse *dto.GeminiChatResponse) bool {
		if geminiResponse == nil {
			return true
		}
		geminiResponseStr, err := common.Marshal(geminiResponse)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
			return false
		}
		c.Render(-1, common.CustomEvent{Data: "data: " + string(geminiResponseStr)})
		_ = helper.FlushWriter(c)
		return true
	}

	sendStreamResult := func(result relayconvert.ResponseResult) bool {
		switch value := result.Value.(type) {
		case dto.ChatCompletionsStreamResponse:
			if len(value.Choices) == 0 && value.Usage == nil {
				return true
			}
			if err := helper.ObjectData(c, &value); err != nil {
				return writeFailed(err)
			}
			return true
		case *dto.ChatCompletionsStreamResponse:
			if value == nil || (len(value.Choices) == 0 && value.Usage == nil) {
				return true
			}
			if err := helper.ObjectData(c, value); err != nil {
				return writeFailed(err)
			}
			return true
		case dto.ClaudeResponse:
			if err := helper.ClaudeData(c, value); err != nil {
				return writeFailed(err)
			}
			return true
		case *dto.ClaudeResponse:
			if value == nil {
				return true
			}
			if err := helper.ClaudeData(c, *value); err != nil {
				return writeFailed(err)
			}
			return true
		case dto.GeminiChatResponse:
			return sendGeminiResponse(&value)
		case *dto.GeminiChatResponse:
			return sendGeminiResponse(value)
		default:
			streamErr = types.NewOpenAIError(fmt.Errorf("unsupported converted stream response type %T", result.Value), types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if streamErr != nil {
			stopStream(sr, streamErr)
			return
		}

		var streamResp dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResp); err != nil {
			logger.LogError(c, common.LogText("failed to unmarshal responses stream event: %s", err.Error()))
			sr.Error(err)
			return
		}
		if streamResp.Type == "" {
			streamResp.Type = sr.EventType()
		}
		billing.observeEvent(info, &streamResp)

		if streamResp.Response != nil {
			info.ObserveResponseModel(streamResp.Response.Model)
		}
		if streamResp.Type == "response.error" || streamResp.Type == "response.failed" {
			upstreamFailed = true
			if streamResp.Response != nil {
				failedUsage = streamResp.Response.Usage
				if oaiErr := streamResp.Response.GetOpenAIError(); oaiErr != nil && oaiErr.Type != "" {
					streamErr = types.WithOpenAIError(*oaiErr, http.StatusInternalServerError)
					stopStream(sr, streamErr)
					return
				}
			}
			streamErr = types.NewOpenAIError(fmt.Errorf("responses stream error: %s", streamResp.Type), types.ErrorCodeBadResponse, http.StatusInternalServerError)
			stopStream(sr, streamErr)
			return
		}

		results, err := service.ConvertStreamResponseChunk(c, info, state, &streamResp)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			stopStream(sr, streamErr)
			return
		}
		for _, result := range results {
			if !sendStreamResult(result) {
				stopStream(sr, streamErr)
				return
			}
		}
	})

	if streamErr != nil {
		if !upstreamFailed || state.UsageText() == "" {
			return nil, streamErr
		}
		// The upstream failed after output reached the client: bill the
		// delivered output (upstream usage when reported, otherwise the
		// estimate) and send the client the same error the failed request
		// path writes. stream_status already records the failure.
		usage := relayconvert.UsageFromResponsesUsage(failedUsage)
		if usage.TotalTokens == 0 {
			usage = state.Usage()
		}
		if usage == nil || usage.TotalTokens == 0 {
			usage = service.ResponseText2Usage(c, state.UsageText(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		} else if usage.CompletionTokens == 0 || usage.PromptTokens == 0 {
			// As in the native Responses accumulator, reported usage that
			// omits the output or the prompt is completed from the estimate of
			// the delivered output and the prompt.
			estimate := service.ResponseText2Usage(c, state.UsageText(), info.UpstreamModelName, info.GetEstimatePromptTokens())
			if usage.CompletionTokens == 0 {
				usage.CompletionTokens = estimate.CompletionTokens
			}
			if usage.PromptTokens == 0 {
				usage.PromptTokens = estimate.PromptTokens
			}
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
			if usage.BillingUsage != nil {
				usage.BillingUsage = dto.CloneBillingUsageWithEstimatedCompletion(usage.BillingUsage, usage.CompletionTokens)
			}
		}
		billing.commitObserved(info)
		logger.LogError(c, common.LogText("responses stream failed after output was delivered, billing the delivered output: %s", streamErr.Error()))
		streamErr.SetMessage(common.MessageWithRequestId(streamErr.Error(), c.GetString(common.RequestIdKey)))
		if info.RelayFormat == types.RelayFormatClaude {
			c.JSON(streamErr.StatusCode, gin.H{"type": "error", "error": streamErr.ToClaudeError()})
		} else {
			c.JSON(streamErr.StatusCode, gin.H{"error": streamErr.ToOpenAIError()})
		}
		return usage, nil
	}

	billing.commit(info)
	usage := normalizeResponsesUsage(state.Usage(), info.GetEstimatePromptTokens())
	ensureOpenAIUsageCompletion(c, usage, state.UsageText(), info.UpstreamModelName, info.GetEstimatePromptTokens())
	state.SetUsage(usage)
	if info.StreamStatus.EndReason == relaycommon.StreamEndReasonClientGone {
		return usage, nil
	}

	if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo != nil {
		info.ClaudeConvertInfo.Usage = usage
	}
	finalResults, err := service.FinalizeStreamResponse(c, info, state)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	for _, result := range finalResults {
		if !sendStreamResult(result) {
			if streamErr != nil {
				return nil, streamErr
			}
			return usage, nil
		}
	}
	if info.RelayFormat == types.RelayFormatOpenAI && info.ShouldIncludeUsage && usage != nil {
		if err := helper.ObjectData(c, helper.GenerateFinalUsageResponse(responseId, createAt, info.UpstreamModelName, *usage)); err != nil {
			writeFailed(err)
			if streamErr != nil {
				return nil, streamErr
			}
			return usage, nil
		}
	}

	if info.RelayFormat == types.RelayFormatOpenAI {
		helper.Done(c)
	}
	return usage, nil
}

// responsesChatBillingTracker collects billable Responses output while the
// protocol is being converted to Chat Completions. The same output can appear
// in item.done and in a terminal response, so the shared counters deduplicate
// those observations before the request is settled. Completed images reached
// the client and stay billable whatever terminal follows them; a provider
// image count is billed only when the response completed.
type responsesChatBillingTracker struct {
	tools          relaycommon.ResponsesToolCallCounter
	images         relaycommon.ImageGenerationCallCounter
	imageToolCount int
	unfinished     bool
}

func newResponsesChatBillingTracker() *responsesChatBillingTracker {
	return &responsesChatBillingTracker{}
}

func (t *responsesChatBillingTracker) observeEvent(info *relaycommon.RelayInfo, event *dto.ResponsesStreamResponse) {
	if t == nil || event == nil {
		return
	}
	switch event.Type {
	case "response.failed", "response.error", "response.incomplete", "response.cancelled", "response.canceled":
		t.unfinished = true
	}
	if event.ToolUsage != nil {
		t.imageToolCount = maxResponsesInt(t.imageToolCount, responsesImageToolCount(event.ToolUsage))
	}
	if event.Item != nil {
		t.observeItem(info, event.Item, event.OutputIndex)
	}
	if event.Response != nil {
		t.observeResponse(info, event.Response)
	}
	if len(event.Data) == 0 {
		return
	}
	var envelope struct {
		Response  *dto.OpenAIResponsesResponse `json:"response"`
		ToolUsage *dto.ResponsesToolUsage      `json:"tool_usage"`
	}
	if err := common.Unmarshal(event.Data, &envelope); err != nil {
		return
	}
	if envelope.ToolUsage != nil {
		t.imageToolCount = maxResponsesInt(t.imageToolCount, responsesImageToolCount(envelope.ToolUsage))
	}
	if envelope.Response != nil {
		t.observeResponse(info, envelope.Response)
	}
}

func (t *responsesChatBillingTracker) observeResponse(info *relaycommon.RelayInfo, response *dto.OpenAIResponsesResponse) {
	if t == nil || response == nil {
		return
	}
	if relaycommon.IsUnfinishedResponsesStatus(response.Status) {
		t.unfinished = true
	}
	if response.ToolUsage != nil {
		t.imageToolCount = maxResponsesInt(t.imageToolCount, responsesImageToolCount(response.ToolUsage))
	}
	for i := range response.Output {
		index := i
		t.tools.Count(info, &response.Output[i], &index)
		t.images.Observe(&response.Output[i], &index)
	}
}

func (t *responsesChatBillingTracker) observeItem(info *relaycommon.RelayInfo, item *dto.ResponsesOutput, outputIndex *int) {
	if t == nil || item == nil {
		return
	}
	t.tools.Count(info, item, outputIndex)
	t.images.Observe(item, outputIndex)
}

func (t *responsesChatBillingTracker) commit(info *relaycommon.RelayInfo) {
	if t == nil {
		return
	}
	if !t.unfinished {
		t.images.EnsureAtLeast(t.imageToolCount)
	}
	t.images.Commit(info)
}

// commitObserved bills only the completed image items seen on a failed
// stream; a provider image count does not prove those images were delivered.
func (t *responsesChatBillingTracker) commitObserved(info *relaycommon.RelayInfo) {
	if t == nil {
		return
	}
	t.images.Commit(info)
}

func responsesImageToolCount(toolUsage *dto.ResponsesToolUsage) int {
	if toolUsage == nil || toolUsage.ImageGen == nil || toolUsage.ImageGen.Images < 0 {
		return 0
	}
	return toolUsage.ImageGen.Images
}
