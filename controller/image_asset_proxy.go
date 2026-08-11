package controller

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

func PublicImageAssetProxy(c *gin.Context) {
	assetID := strings.TrimSpace(c.Param("asset_id"))
	if assetID == "" {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error", "asset_id is required")
		return
	}

	asset, exists, err := model.GetImageAssetByAssetID(assetID)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to query image asset %s: %s", assetID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to query image asset")
		return
	}
	if !exists || asset == nil {
		videoProxyError(c, http.StatusNotFound, "invalid_request_error", "Image asset not found")
		return
	}

	assetURL := strings.TrimSpace(asset.URL)
	if assetURL == "" {
		videoProxyError(c, http.StatusBadGateway, "server_error", "Image asset URL is empty")
		return
	}

	proxy := ""
	if channel, channelErr := model.CacheGetChannel(asset.ChannelID); channelErr == nil && channel != nil {
		proxy = channel.GetSetting().Proxy
	}

	client := service.GetSSRFProtectedHTTPClient()
	if proxy != "" {
		client, err = service.GetHttpClientWithProxy(proxy)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create proxy client for image asset %s: %s", assetID, err.Error()))
			videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy client")
			return
		}
	}

	var validateErr error
	if proxy == "" {
		validateErr = service.ValidateSSRFProtectedFetchURL(assetURL)
	} else {
		fetchSetting := system_setting.GetFetchSetting()
		validateErr = common.ValidateURLWithFetchSetting(assetURL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain)
	}
	if validateErr != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Image asset URL blocked for %s: %v", assetID, validateErr))
		videoProxyError(c, http.StatusForbidden, "server_error", fmt.Sprintf("request blocked: %v", validateErr))
		return
	}

	parsedURL, err := url.Parse(assetURL)
	if err != nil {
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to parse image asset URL")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create image asset request")
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to fetch image asset %s: %s", assetID, err.Error()))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch image asset")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Image asset upstream returned status %d for %s", resp.StatusCode, assetID))
		videoProxyError(c, http.StatusBadGateway, "server_error", fmt.Sprintf("Upstream service returned status %d", resp.StatusCode))
		return
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !isSupportedPublicImageType(mediaType) {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Image asset upstream returned unsupported content type %q for %s", mediaType, assetID))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Upstream service did not return a supported image")
		return
	}

	copyPublicMediaResponseHeaders(c, resp)
	c.Writer.Header().Set("Cache-Control", "public, max-age=86400")
	c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
	c.Writer.WriteHeader(resp.StatusCode)
	if _, err = io.Copy(c.Writer, resp.Body); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to stream image asset %s: %s", assetID, err.Error()))
	}
}

func isSupportedPublicImageType(mediaType string) bool {
	switch strings.ToLower(mediaType) {
	case "image/avif", "image/gif", "image/jpeg", "image/jpg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}
