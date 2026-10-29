package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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
		for name, handler := range map[string]gin.HandlerFunc{"list": GetAllTopUps, "export": ExportAllTopUps} {
			t.Run(name+" "+query, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodGet, "/api/user/topup?"+query, nil)
				handler(c)

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
}

func TestExportAllTopUpsDatabaseMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ kind, env string }{
		{"sqlite", ""},
		{"mysql", "AUDIT_MYSQL_DSN"},
		{"postgres", "AUDIT_POSTGRES_DSN"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			dsn := os.Getenv(tc.env)
			if tc.env != "" && dsn == "" {
				t.Skip(tc.env + " is not configured")
			}
			var db *gorm.DB
			if tc.kind == "sqlite" {
				var err error
				db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
				require.NoError(t, err)
				sqlDB, err := db.DB()
				require.NoError(t, err)
				sqlDB.SetMaxOpenConns(1)
				t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			} else {
				db, _ = newAuditTestDatabase(t, tc.kind, dsn)
			}
			previousDB := model.DB
			model.DB = db
			t.Cleanup(func() { model.DB = previousDB })
			require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}))
			require.NoError(t, db.Create(&model.User{Id: 282, Username: "owner", DisplayName: "Owner", Email: "owner@example.com", AffCode: "aff-282"}).Error)
			require.NoError(t, db.Create(&[]model.TopUp{
				{UserId: 282, TradeNo: "MATCH-old", CreateTime: 100, Status: common.TopUpStatusSuccess},
				{UserId: 282, TradeNo: "MATCH-in-range", CreateTime: 200, CompleteTime: 210, Amount: 10, Money: 9.5, PaymentProvider: "epay", PaymentMethod: "alipay", Status: common.TopUpStatusSuccess},
				{UserId: 256, TradeNo: "MATCH-other-user", CreateTime: 250, Status: common.TopUpStatusPending},
				{UserId: 282, TradeNo: "OTHER-order", CreateTime: 260, Status: common.TopUpStatusSuccess},
				{UserId: 282, TradeNo: "MATCH-latest", CreateTime: 300, Status: common.TopUpStatusPending},
			}).Error)

			export := func(query string) (orders []model.AdminTopUp, result map[string]any) {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodGet, "/api/user/topup/export?"+query, nil)
				ExportAllTopUps(c)
				require.Equal(t, http.StatusOK, recorder.Code)
				lines := strings.Split(strings.TrimSpace(recorder.Body.String()), "\n")
				for _, line := range lines[:len(lines)-1] {
					var order model.AdminTopUp
					require.NoError(t, common.UnmarshalJsonStr(line, &order))
					orders = append(orders, order)
				}
				require.NoError(t, common.UnmarshalJsonStr(lines[len(lines)-1], &result))
				return orders, result
			}

			orders, result := export("keyword=MATCH%25&user_id=282&start_timestamp=150&end_timestamp=300")
			require.Len(t, orders, 2)
			assert.Equal(t, "MATCH-latest", orders[0].TradeNo)
			assert.Equal(t, "MATCH-in-range", orders[1].TradeNo)
			assert.Equal(t, "owner", orders[1].Username)
			assert.Equal(t, int64(210), orders[1].CompleteTime)
			assert.Equal(t, 9.5, orders[1].Money)
			assert.Equal(t, map[string]any{"success": true, "message": "", "data": map[string]any{"count": float64(2)}}, result)

			orders, result = export("")
			assert.Len(t, orders, 5)
			assert.Empty(t, orders[2].Username, "orders of a missing user are still exported")
			assert.Equal(t, true, result["success"])

			bulk := make([]model.TopUp, 1000)
			for i := range bulk {
				bulk[i] = model.TopUp{UserId: 999, TradeNo: fmt.Sprintf("BULK-%04d", i), CreateTime: int64(1000 + i)}
			}
			require.NoError(t, db.CreateInBatches(&bulk, 200).Error)
			orders, result = export("user_id=999")
			require.Len(t, orders, 1000, "an export reads past the first batch without repeating orders")
			assert.Equal(t, "BULK-0999", orders[0].TradeNo)
			assert.Equal(t, "BULK-0500", orders[499].TradeNo)
			assert.Equal(t, "BULK-0499", orders[500].TradeNo)
			assert.Equal(t, "BULK-0000", orders[999].TradeNo)
			assert.Equal(t, float64(1000), result["data"].(map[string]any)["count"])

			orders, result = export("keyword=%25M%25")
			assert.Empty(t, orders)
			assert.Equal(t, false, result["success"])
			assert.Equal(t, "Fuzzy search keywords must be at least 2 characters long", result["message_key"])
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
