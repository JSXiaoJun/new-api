package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetStatusReturnsConfiguredNavigationLinks(t *testing.T) {
	settings := operation_setting.GetGeneralSetting()
	originalDocsLink := settings.DocsLink
	originalGalleryLink := settings.GalleryLink
	originalInfiniteCanvasLink := settings.InfiniteCanvasLink
	originalOptionMap := common.OptionMap
	t.Cleanup(func() {
		settings.DocsLink = originalDocsLink
		settings.GalleryLink = originalGalleryLink
		settings.InfiniteCanvasLink = originalInfiniteCanvasLink
		common.OptionMap = originalOptionMap
	})

	settings.DocsLink = "https://docs.example.com"
	settings.GalleryLink = "https://gallery.example.com"
	settings.InfiniteCanvasLink = "https://canvas.example.com"
	common.OptionMap = map[string]string{}

	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)

	GetStatus(context)

	var payload struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	assert.Equal(t, settings.DocsLink, payload.Data["docs_link"])
	assert.Equal(t, settings.GalleryLink, payload.Data["gallery_link"])
	assert.Equal(t, settings.InfiniteCanvasLink, payload.Data["infinite_canvas_link"])
}
