package relay

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
)

func ImageHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)

	imageReq, ok := info.Request.(*dto.ImageRequest)
	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected dto.ImageRequest, got %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	request, err := common.DeepCopy(imageReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to ImageRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	var requestBody io.Reader

	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		requestBody = common.NewReplayableBodyReader(storage)
	} else {
		convertedRequest, err := adaptor.ConvertImageRequest(c, info, *request)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed)
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

		switch convertedRequest.(type) {
		case *bytes.Buffer:
			requestBody = convertedRequest.(io.Reader)
		default:
			jsonData, err := common.Marshal(convertedRequest)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}

			// apply param override
			if len(info.ParamOverride) > 0 {
				jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
				if err != nil {
					return newAPIErrorFromParamOverride(err)
				}
			}

			logger.LogDebug(c, "image request body: %s", jsonData)
			body, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}
			defer closer.Close()
			jsonData = nil
			requestBody = body
		}
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")

	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	var httpResp *http.Response
	if resp != nil {
		httpResp = resp.(*http.Response)
		info.IsStream = info.IsStream || strings.HasPrefix(httpResp.Header.Get("Content-Type"), "text/event-stream")
		if httpResp.StatusCode != http.StatusOK {
			if httpResp.StatusCode == http.StatusCreated && info.ApiType == constant.APITypeReplicate {
				// replicate channel returns 201 Created when using Prefer: wait, treat it as success.
				httpResp.StatusCode = http.StatusOK
			} else {
				newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
				// reset status code 重置状态码
				service.ResetStatusCode(newAPIError, statusCodeMappingStr)
				return newAPIError
			}
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}
	imageUsage := usage.(*dto.Usage)
	if !service.ValidUsage(imageUsage) || (!info.PriceData.UsePrice && imageUsageMissingOutput(imageUsage)) {
		imageUsage = fallbackImageUsage(info, imageUsage)
	}

	imageN := uint(1)
	if request.N != nil {
		imageN = *request.N
	}

	quality := request.Quality
	if quality == "" {
		quality = "standard"
	}

	var logContent []string

	if len(request.Size) > 0 {
		logContent = append(logContent, fmt.Sprintf("大小 %s", request.Size))
	}
	if len(quality) > 0 {
		logContent = append(logContent, fmt.Sprintf("品质 %s", quality))
	}
	if imageN > 0 {
		logContent = append(logContent, fmt.Sprintf("生成数量 %d", imageN))
	}

	service.PostTextConsumeQuota(c, info, imageUsage, logContent)
	return nil
}

func fallbackImageUsage(info *relaycommon.RelayInfo, usage *dto.Usage) *dto.Usage {
	if usage == nil {
		usage = &dto.Usage{}
	}
	if info == nil || info.PriceData.UsePrice {
		usage.PromptTokens = 1
		usage.TotalTokens = 1
		return usage
	}

	usage.PromptTokens = max(
		usage.PromptTokens,
		usage.InputTokens,
		info.GetEstimatePromptTokens(),
		common.PreConsumedQuota,
	)
	if usage.CompletionTokens < usage.OutputTokens {
		usage.CompletionTokens = usage.OutputTokens
	}
	if imageRequest, ok := info.Request.(*dto.ImageRequest); ok && imageRequest != nil {
		if usage.CompletionTokens <= 0 {
			if usage.TotalTokens > usage.PromptTokens {
				usage.CompletionTokens = usage.TotalTokens - usage.PromptTokens
			} else {
				usage.CompletionTokens = max(imageRequest.GetTokenCountMeta().MaxTokens, 0)
			}
		}
	}
	upstreamTotal := usage.TotalTokens
	service.RecalculateUsageTotal(usage)
	if upstreamTotal > usage.TotalTokens {
		usage.TotalTokens = upstreamTotal
	}
	if usage.TotalTokens == 0 {
		usage.PromptTokens = 1
		usage.TotalTokens = 1
	}
	return usage
}

// imageUsageMissingOutput catches the partial usage shape returned by some
// OpenAI-compatible image gateways (including sub2api). Input usage or input
// details alone are not a complete proportional billing result: the requested
// image output still has to be charged when output_tokens are omitted.
func imageUsageMissingOutput(usage *dto.Usage) bool {
	if usage == nil {
		return true
	}
	if usage.CompletionTokens > 0 || usage.OutputTokens > 0 {
		return false
	}
	return usage.CompletionTokenDetails.TextTokens == 0 &&
		usage.CompletionTokenDetails.AudioTokens == 0 &&
		usage.CompletionTokenDetails.ImageTokens == 0 &&
		usage.CompletionTokenDetails.ReasoningTokens == 0 &&
		(usage.OutputTokensDetails == nil ||
			(usage.OutputTokensDetails.TextTokens == 0 &&
				usage.OutputTokensDetails.AudioTokens == 0 &&
				usage.OutputTokensDetails.ImageTokens == 0 &&
				usage.OutputTokensDetails.ReasoningTokens == 0))
}
