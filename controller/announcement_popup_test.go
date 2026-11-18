package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const popupTestUserID = 9

func setupPopupAnnouncementTest(t *testing.T, announcements string, acked []int64) {
	t.Helper()
	oldDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	model.DB = db
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false

	cs := console_setting.GetConsoleSetting()
	oldSetting := *cs
	cs.AnnouncementsEnabled = true
	cs.Announcements = announcements

	t.Cleanup(func() {
		*cs = oldSetting
		common.RedisEnabled = oldRedisEnabled
		model.DB = oldDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	settingBytes, err := common.Marshal(dto.UserSetting{Language: "zh", AckAnnouncementIds: acked})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.User{
		Id:       popupTestUserID,
		Username: "popup_user",
		Status:   common.UserStatusEnabled,
		Setting:  string(settingBytes),
	}).Error)
}

func callPopupHandler(t *testing.T, handler gin.HandlerFunc, method string, body string) map[string]any {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", popupTestUserID)
	ctx.Request = httptest.NewRequest(method, "/api/user/announcements/popup", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler(ctx)
	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func loadPopupTestUserSetting(t *testing.T) dto.UserSetting {
	t.Helper()
	user, err := model.GetUserById(popupTestUserID, false)
	require.NoError(t, err)
	return user.GetSetting()
}

func popupAnnouncementsJSON(t *testing.T, items ...map[string]any) string {
	t.Helper()
	data, err := common.Marshal(items)
	require.NoError(t, err)
	return string(data)
}

func publishedAt(offset time.Duration) string {
	return time.Now().Add(offset).UTC().Format(time.RFC3339)
}

func TestGetPopupAnnouncementsListsOnlyPublishedUnconfirmedPopups(t *testing.T) {
	setupPopupAnnouncementTest(t, popupAnnouncementsJSON(t,
		map[string]any{"id": 1, "content": "newer popup", "publishDate": publishedAt(-time.Hour), "popup": true},
		map[string]any{"id": 2, "content": "older popup", "publishDate": publishedAt(-2 * time.Hour), "popup": true},
		map[string]any{"id": 3, "content": "confirmed popup", "publishDate": publishedAt(-time.Hour), "popup": true},
		map[string]any{"id": 4, "content": "scheduled popup", "publishDate": publishedAt(time.Hour), "popup": true},
		map[string]any{"id": 5, "content": "silent", "publishDate": publishedAt(-time.Hour)},
	), []int64{3})

	response := callPopupHandler(t, GetPopupAnnouncements, http.MethodGet, "")

	require.Equal(t, true, response["success"])
	items, ok := response["data"].([]any)
	require.True(t, ok)
	contents := make([]string, 0, len(items))
	for _, item := range items {
		contents = append(contents, item.(map[string]any)["content"].(string))
	}
	assert.Equal(t, []string{"older popup", "newer popup"}, contents)
}

func TestGetPopupAnnouncementsIsEmptyWhilePanelDisabled(t *testing.T) {
	setupPopupAnnouncementTest(t, popupAnnouncementsJSON(t,
		map[string]any{"id": 1, "content": "popup", "publishDate": publishedAt(-time.Hour), "popup": true},
	), nil)
	console_setting.GetConsoleSetting().AnnouncementsEnabled = false

	response := callPopupHandler(t, GetPopupAnnouncements, http.MethodGet, "")

	require.Equal(t, true, response["success"])
	assert.Empty(t, response["data"])
}

func TestAckPopupAnnouncementStoresIDAndDropsDeletedOnes(t *testing.T) {
	setupPopupAnnouncementTest(t, popupAnnouncementsJSON(t,
		map[string]any{"id": 1, "content": "kept", "publishDate": publishedAt(-time.Hour), "popup": true},
		map[string]any{"id": 2, "content": "new", "publishDate": publishedAt(-time.Hour), "popup": true},
	), []int64{1, 77})

	response := callPopupHandler(t, AckPopupAnnouncement, http.MethodPost, `{"id":2}`)

	require.Equal(t, true, response["success"])
	setting := loadPopupTestUserSetting(t)
	assert.Equal(t, []int64{1, 2}, setting.AckAnnouncementIds)
	assert.Equal(t, "zh", setting.Language, "other settings must survive the confirmation")
}

func TestAckPopupAnnouncementIgnoresIDsThatDoNotPopUp(t *testing.T) {
	setupPopupAnnouncementTest(t, popupAnnouncementsJSON(t,
		map[string]any{"id": 1, "content": "silent", "publishDate": publishedAt(-time.Hour)},
		map[string]any{"id": 2, "content": "scheduled", "publishDate": publishedAt(time.Hour), "popup": true},
	), nil)

	for _, body := range []string{`{"id":1}`, `{"id":2}`, `{"id":999}`} {
		response := callPopupHandler(t, AckPopupAnnouncement, http.MethodPost, body)
		require.Equal(t, true, response["success"], body)
	}

	assert.Empty(t, loadPopupTestUserSetting(t).AckAnnouncementIds)
}

func TestAckPopupAnnouncementRejectsInvalidID(t *testing.T) {
	setupPopupAnnouncementTest(t, "", nil)

	response := callPopupHandler(t, AckPopupAnnouncement, http.MethodPost, `{"id":0}`)

	assert.Equal(t, false, response["success"])
}

func TestValidateAnnouncementsRequiresUniqueIDForPopup(t *testing.T) {
	published := publishedAt(-time.Hour)
	testCases := []struct {
		name    string
		items   []map[string]any
		wantErr bool
	}{
		{name: "popup with unique id", items: []map[string]any{
			{"id": 1, "content": "a", "publishDate": published, "popup": true},
			{"id": 2, "content": "b", "publishDate": published},
		}},
		{name: "legacy entries without popup", items: []map[string]any{
			{"content": "a", "publishDate": published},
			{"content": "b", "publishDate": published},
		}},
		{name: "popup without id", wantErr: true, items: []map[string]any{
			{"content": "a", "publishDate": published, "popup": true},
		}},
		{name: "popup sharing id", wantErr: true, items: []map[string]any{
			{"id": 1, "content": "a", "publishDate": published, "popup": true},
			{"id": 1, "content": "b", "publishDate": published},
		}},
		{name: "popup not boolean", wantErr: true, items: []map[string]any{
			{"id": 1, "content": "a", "publishDate": published, "popup": "yes"},
		}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := console_setting.ValidateConsoleSettings(popupAnnouncementsJSON(t, tc.items...), "Announcements")
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
