package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := dto.Usage{}
	if responsesResponse.Usage != nil {
		usage.PromptTokens = responsesResponse.Usage.InputTokens
		usage.CompletionTokens = responsesResponse.Usage.OutputTokens
		usage.TotalTokens = responsesResponse.Usage.TotalTokens
		if responsesResponse.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = responsesResponse.Usage.InputTokensDetails.CachedTokens
			usage.PromptTokensDetails.CacheWriteTokens = responsesResponse.Usage.InputTokensDetails.CacheWriteTokens
		}
	}
	// Count actual tool invocations from Output (not tool declarations).
	for _, output := range responsesResponse.Output {
		switch output.Type {
		case dto.BuildInCallWebSearchCall:
			info.CountBillableToolCall(dto.BuildInCallWebSearchCall, "")
		case dto.BuildInCallFileSearchCall:
			info.CountBillableToolCall(dto.BuildInCallFileSearchCall, "")
		case dto.BuildInCallFunctionCall:
			info.CountBillableToolCall(dto.BuildInCallFunctionCall, output.Name)
		}
	}

	imageCounter := &relaycommon.ImageGenerationCallCounter{}
	if !relaycommon.IsNonBillableResponsesStatus(responsesResponse.Status) {
		for i := range responsesResponse.Output {
			idx := i
			imageCounter.Observe(&responsesResponse.Output[i], &idx)
		}
	}
	imageCounter.Commit(info)

	return &usage, nil
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var usage = &dto.Usage{}
	var responseTextBuilder strings.Builder
	imageCounter := &relaycommon.ImageGenerationCallCounter{}
	imageCommitted := false

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {

		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
			sr.Error(err)
			return
		}
		sendResponsesStreamData(c, streamResponse, data)
		mergeResponsesUsage(usage, streamResponse.Usage)
		if streamResponse.Response != nil {
			mergeResponsesUsage(usage, streamResponse.Response.Usage)
		}
		var wrappedResponse *dto.OpenAIResponsesResponse
		if streamResponse.Data != nil {
			var dataResponse struct {
				Usage    *dto.Usage                   `json:"usage"`
				Response *dto.OpenAIResponsesResponse `json:"response"`
			}
			if err := common.Unmarshal(streamResponse.Data, &dataResponse); err == nil {
				mergeResponsesUsage(usage, dataResponse.Usage)
				wrappedResponse = dataResponse.Response
				if wrappedResponse != nil {
					mergeResponsesUsage(usage, wrappedResponse.Usage)
				}
			}
		}
		response := streamResponse.Response
		if response == nil {
			response = wrappedResponse
		}
		switch streamResponse.Type {
		case "response.completed", "response.done":
			if response != nil {
				if !imageCommitted {
					if relaycommon.IsNonBillableResponsesStatus(response.Status) {
						imageCounter.Reset()
						imageCounter.Commit(info)
						imageCommitted = true
					} else {
						for i := range response.Output {
							idx := i
							imageCounter.Observe(&response.Output[i], &idx)
						}
						imageCounter.Commit(info)
						imageCommitted = true
					}
				}
			} else if !imageCommitted {
				imageCounter.Commit(info)
				imageCommitted = true
			}
		case "response.failed", "response.incomplete", "response.cancelled", "response.canceled":
			// Responses providers can include authoritative usage on any terminal
			// event, not only response.completed/response.done. Preserve it so an
			// interrupted or cancelled stream is still billed when usage is present.
			if !imageCommitted {
				imageCounter.Reset()
				imageCounter.Commit(info)
				imageCommitted = true
			}
		case "response.output_text.delta":
			// 处理输出文本
			responseTextBuilder.WriteString(streamResponse.Delta)
		case dto.ResponsesOutputTypeItemDone:
			if streamResponse.Item != nil {
				switch streamResponse.Item.Type {
				case dto.BuildInCallWebSearchCall:
					info.CountBillableToolCall(dto.BuildInCallWebSearchCall, "")
				case dto.BuildInCallFileSearchCall:
					info.CountBillableToolCall(dto.BuildInCallFileSearchCall, "")
				case dto.BuildInCallFunctionCall:
					info.CountBillableToolCall(dto.BuildInCallFunctionCall, streamResponse.Item.Name)
				case dto.ResponsesOutputTypeImageGenerationCall:
					if !imageCommitted {
						imageCounter.Observe(streamResponse.Item, streamResponse.OutputIndex)
					}
				}
			}
		}
	})

	if usage.CompletionTokens == 0 {
		// 计算输出文本的 token 数量
		tempStr := responseTextBuilder.String()
		if len(tempStr) > 0 {
			// 非正常结束，使用输出文本的 token 数量
			completionTokens := service.CountTextToken(tempStr, info.UpstreamModelName)
			usage.CompletionTokens = completionTokens
		}
	}

	if usage.PromptTokens == 0 && usage.CompletionTokens != 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	return usage, nil
}

func mergeResponsesUsage(dst *dto.Usage, src *dto.Usage) {
	if dst == nil || src == nil {
		return
	}

	inputTokens := src.InputTokens
	if inputTokens == 0 {
		inputTokens = src.PromptTokens
	}
	if inputTokens > 0 {
		dst.PromptTokens = inputTokens
	}

	outputTokens := src.OutputTokens
	if outputTokens == 0 {
		outputTokens = src.CompletionTokens
	}
	if outputTokens > 0 {
		dst.CompletionTokens = outputTokens
	}

	if src.TotalTokens > 0 {
		dst.TotalTokens = src.TotalTokens
	}
	if src.InputTokensDetails != nil {
		if src.InputTokensDetails.CachedTokens > 0 {
			dst.PromptTokensDetails.CachedTokens = src.InputTokensDetails.CachedTokens
		}
		if src.InputTokensDetails.CacheWriteTokens > 0 {
			dst.PromptTokensDetails.CacheWriteTokens = src.InputTokensDetails.CacheWriteTokens
		}
		if src.InputTokensDetails.CachedCreationTokens > 0 {
			dst.PromptTokensDetails.CachedCreationTokens = src.InputTokensDetails.CachedCreationTokens
		}
	}
	if src.PromptTokensDetails.CachedTokens > 0 || src.PromptTokensDetails.CacheWriteTokens > 0 || src.PromptTokensDetails.CachedCreationTokens > 0 {
		if src.PromptTokensDetails.CachedTokens > 0 {
			dst.PromptTokensDetails.CachedTokens = src.PromptTokensDetails.CachedTokens
		}
		if src.PromptTokensDetails.CacheWriteTokens > 0 {
			dst.PromptTokensDetails.CacheWriteTokens = src.PromptTokensDetails.CacheWriteTokens
		}
		if src.PromptTokensDetails.CachedCreationTokens > 0 {
			dst.PromptTokensDetails.CachedCreationTokens = src.PromptTokensDetails.CachedCreationTokens
		}
	}

	if src.CacheReadInputTokens > 0 {
		dst.PromptTokensDetails.CachedTokens = src.CacheReadInputTokens
	} else if src.CacheReadTokens > 0 {
		dst.PromptTokensDetails.CachedTokens = src.CacheReadTokens
	} else if src.PromptCacheHitTokens > 0 {
		dst.PromptTokensDetails.CachedTokens = src.PromptCacheHitTokens
	}
	if src.CacheCreationInputTokens > 0 {
		dst.PromptTokensDetails.CachedCreationTokens = src.CacheCreationInputTokens
	} else if src.CacheWriteTokens > 0 {
		dst.PromptTokensDetails.CacheWriteTokens = src.CacheWriteTokens
	} else if src.CacheCreationTokens > 0 {
		dst.PromptTokensDetails.CachedCreationTokens = src.CacheCreationTokens
	}
}
