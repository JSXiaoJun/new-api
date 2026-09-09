package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetAllTopUpsRejectsInvalidUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{
		"user_id=", "user_id=0", "user_id=-1", "user_id=abc", "user_id=1.5",
		"user_id=999999999999999999999999", "user_id=282&user_id=256",
	} {
		t.Run(query, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/user/topup?"+query, nil)
			GetAllTopUps(c)

			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			var response struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.False(t, response.Success)
			assert.Equal(t, "Invalid user ID", response.Message)
		})
	}
}

func TestGetAllTopUpsCombinesUserIDWithOrderSearch(t *testing.T) {
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}))
	require.NoError(t, db.Create(&[]model.TopUp{
		{UserId: 282, TradeNo: "MATCH-first"},
		{UserId: 256, TradeNo: "MATCH-other-user"},
		{UserId: 282, TradeNo: "OTHER-order"},
		{UserId: 282, TradeNo: "MATCH-last"},
	}).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/topup?user_id=282&keyword=MATCH%25&p=2&page_size=1", nil)
	GetAllTopUps(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Total    int                `json:"total"`
			Page     int                `json:"page"`
			PageSize int                `json:"page_size"`
			Items    []model.AdminTopUp `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, 2, response.Data.Total)
	assert.Equal(t, 2, response.Data.Page)
	assert.Equal(t, 1, response.Data.PageSize)
	require.Len(t, response.Data.Items, 1)
	assert.Equal(t, 282, response.Data.Items[0].UserId)
	assert.Equal(t, "MATCH-first", response.Data.Items[0].TradeNo)
}
