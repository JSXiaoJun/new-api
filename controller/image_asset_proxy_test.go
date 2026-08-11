package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPublicImageAssetProxyReturnsImageWithoutAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	imageBody := []byte("test-png-binary")
	var upstreamAuthorization string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Set-Cookie", "session=upstream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(imageBody)
	}))
	defer upstream.Close()

	previousDB := model.DB
	fetchSetting := system_setting.GetFetchSetting()
	previousSSRFProtection := fetchSetting.EnableSSRFProtection
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ImageAsset{}, &model.Channel{}))
	model.DB = db
	fetchSetting.EnableSSRFProtection = false
	service.InitHttpClient()
	t.Cleanup(func() {
		model.DB = previousDB
		fetchSetting.EnableSSRFProtection = previousSSRFProtection
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, model.UpsertImageAsset("img_public", 0, upstream.URL+"/public/images/assets/img_public"))

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/public/images/assets/img_public", nil)
	c.Params = gin.Params{{Key: "asset_id", Value: "img_public"}}

	PublicImageAssetProxy(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "image/png", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
	assert.Empty(t, recorder.Header().Get("Set-Cookie"))
	assert.Equal(t, imageBody, recorder.Body.Bytes())
	assert.Empty(t, upstreamAuthorization)
}
