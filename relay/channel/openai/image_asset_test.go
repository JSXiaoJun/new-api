package openai

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRewriteOpenAIImageAssetURLs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalDB := model.DB
	originalServerAddress := system_setting.ServerAddress
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ImageAsset{}))
	model.DB = db
	system_setting.ServerAddress = "https://gateway.example"
	t.Cleanup(func() {
		model.DB = originalDB
		system_setting.ServerAddress = originalServerAddress
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelId:      7,
		ChannelBaseUrl: "https://middleware.example",
	}}
	body := []byte(`{"created":1786400000,"data":[{"url":"https://middleware.example/public/images/assets/img_123"}]}`)

	rewritten := rewriteOpenAIImageAssetURLs(ctx, body, info)
	assert.JSONEq(t, `{"created":1786400000,"data":[{"url":"https://gateway.example/public/images/assets/img_123"}]}`, string(rewritten))

	asset, exists, err := model.GetImageAssetByAssetID("img_123")
	require.NoError(t, err)
	require.True(t, exists)
	require.NotNil(t, asset)
	assert.Equal(t, 7, asset.ChannelID)
	assert.Equal(t, "https://middleware.example/public/images/assets/img_123", asset.URL)
}

func TestResolvePublicImageAssetUsesChannelBaseForGatewayURL(t *testing.T) {
	originalServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://gateway.example"
	t.Cleanup(func() { system_setting.ServerAddress = originalServerAddress })

	assetID, upstreamURL, ok := resolvePublicImageAsset(
		"https://gateway.example/public/images/assets/img_456?token=public",
		"https://middleware.example",
	)
	require.True(t, ok)
	assert.Equal(t, "img_456", assetID)
	assert.Equal(t, "https://middleware.example/public/images/assets/img_456?token=public", upstreamURL)
}
