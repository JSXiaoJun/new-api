package channel

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assetLinkTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	return ctx
}

// TestApplyImageAssetLinksRequestHeaderOptsInImageRequests protects the
// middleware contract: the header is only sent for requests that can produce
// images, so a plain chat request never asks an upstream to store anything.
func TestApplyImageAssetLinksRequestHeaderOptsInImageRequests(t *testing.T) {
	imageInfo := func(relayMode int, modelName string) *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			RelayMode:   relayMode,
			ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: modelName},
		}
	}
	cases := []struct {
		name     string
		info     *relaycommon.RelayInfo
		expected string
	}{
		{
			name:     "image generation mode",
			info:     imageInfo(relayconstant.RelayModeImagesGenerations, "gpt-image-2"),
			expected: "1",
		},
		{
			name:     "gemini image model",
			info:     imageInfo(relayconstant.RelayModeGemini, "gemini-3-pro-image-preview"),
			expected: "1",
		},
		{
			name:     "gemini text model",
			info:     imageInfo(relayconstant.RelayModeGemini, "gemini-2.5-pro"),
			expected: "",
		},
		{
			name:     "chat completion",
			info:     imageInfo(relayconstant.RelayModeChatCompletions, "gpt-4o"),
			expected: "",
		},
		{
			name:     "nil relay info",
			info:     nil,
			expected: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			header := http.Header{}
			applyImageAssetLinksRequestHeader(tc.info, &header)
			assert.Equal(t, tc.expected, header.Get(ImageAssetLinksRequestHeader))
		})
	}
}

// TestCaptureImageAssetLinksFromHeaderRecordsMiddlewareReport protects the
// out-of-band link channel used when the upstream returned base64 and therefore
// could not rewrite a URL into the body.
func TestCaptureImageAssetLinksFromHeaderRecordsMiddlewareReport(t *testing.T) {
	ctx := assetLinkTestContext(t)
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set(ImageAssetLinksResponseHeader,
		`["https://video-admin.example/public/images/assets/a1","https://video-admin.example/public/images/assets/a2"]`)

	captureImageAssetLinksFromHeader(ctx, resp)

	require.Equal(t,
		[]string{
			"https://video-admin.example/public/images/assets/a1",
			"https://video-admin.example/public/images/assets/a2",
		},
		common.GetContextKeyStringSlice(ctx, constant.ContextKeyImageAssetLinks))
}

// TestCaptureImageAssetLinksFromHeaderIgnoresMalformedValue protects the relay
// from a bad upstream header: a non-JSON value must not fail the request.
func TestCaptureImageAssetLinksFromHeaderIgnoresMalformedValue(t *testing.T) {
	ctx := assetLinkTestContext(t)
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set(ImageAssetLinksResponseHeader, "not-json")

	captureImageAssetLinksFromHeader(ctx, resp)

	assert.Empty(t, common.GetContextKeyStringSlice(ctx, constant.ContextKeyImageAssetLinks))
}

// TestImageAssetLinksFromJSONExtractsOnlyPublicLinks protects the log contents:
// a URL that was never desensitized by an image middleware must not be recorded
// as if it were a public link.
func TestImageAssetLinksFromJSONExtractsOnlyPublicLinks(t *testing.T) {
	body := []byte(`{"data":[
		{"url":"https://video-admin.example/public/images/assets/a1"},
		{"url":"https://upstream.example/tmp/raw.png"},
		{"b64_json":"aGVsbG8="}
	]}`)

	assert.Equal(t,
		[]string{"https://video-admin.example/public/images/assets/a1"},
		ImageAssetLinksFromJSON(body))
	assert.Empty(t, ImageAssetLinksFromJSON([]byte(`{"data":[]}`)))
	assert.Empty(t, ImageAssetLinksFromJSON([]byte("not-json")))
	assert.Empty(t, ImageAssetLinksFromJSON(nil))
}

// TestRecordImageAssetLinksDeduplicates protects the log from listing the same
// stored image twice when both the body and the response header report it.
func TestRecordImageAssetLinksDeduplicates(t *testing.T) {
	ctx := assetLinkTestContext(t)

	RecordImageAssetLinks(ctx, []string{"https://video-admin.example/public/images/assets/a1"})
	RecordImageAssetLinks(ctx, []string{
		"https://video-admin.example/public/images/assets/a1",
		"https://video-admin.example/public/images/assets/a2",
	})

	assert.Equal(t,
		[]string{
			"https://video-admin.example/public/images/assets/a1",
			"https://video-admin.example/public/images/assets/a2",
		},
		common.GetContextKeyStringSlice(ctx, constant.ContextKeyImageAssetLinks))
}

// TestRecordImageAssetLinksCapsCount protects log size: a runaway upstream
// cannot make one log row carry an unbounded link list.
func TestRecordImageAssetLinksCapsCount(t *testing.T) {
	ctx := assetLinkTestContext(t)

	links := make([]string, 0, imageAssetLinksMaxCount+10)
	for i := 0; i < imageAssetLinksMaxCount+10; i++ {
		links = append(links, "https://video-admin.example/public/images/assets/"+strconv.Itoa(i))
	}
	RecordImageAssetLinks(ctx, links)

	assert.Len(t, common.GetContextKeyStringSlice(ctx, constant.ContextKeyImageAssetLinks), imageAssetLinksMaxCount)
}
