package model

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUserQuotaLogsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousType := LOG_DB, common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "quota-logs.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		LOG_DB = previousDB
		common.SetLogDatabaseType(previousType)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func TestGetUserQuotaLogsFiltersHistoricalEventsAndPaginatesByTarget(t *testing.T) {
	db := setupUserQuotaLogsTestDB(t)
	logs := []Log{
		{Id: 1, UserId: 42, Type: LogTypeTopup, Content: "通过兑换码充值 10 点额度，兑换码ID 9"},
		{Id: 2, UserId: 42, Type: LogTypeRefund, Quota: 40},
		{Id: 3, UserId: 42, Type: LogTypeSystem, Content: "新用户注册赠送 1 点额度"},
		{Id: 4, UserId: 42, Type: LogTypeSystem, Content: "使用邀请码赠送 2 点额度"},
		{Id: 5, UserId: 42, Type: LogTypeSystem, Content: "邀请用户赠送 3 点额度"},
		{Id: 6, UserId: 42, Type: LogTypeSystem, Content: "用户签到，获得额度 4 点额度"},
		{Id: 7, UserId: 1, Type: LogTypeManage, Other: `{"op":{"action":"user.quota_add","params":{"quota":"5 点额度","target_user_id":42}}}`},
		{Id: 8, UserId: 2, Type: LogTypeManage, Other: `{"op":{"action":"user.quota_subtract","params":{"quota":"6 点额度","target_user_id":42}}}`},
		{Id: 9, UserId: 1, Type: LogTypeManage, Other: `{"op":{"action":"user.quota_override","params":{"from":"1 点额度","to":"7 点额度","target_user_id":42}}}`},
		{Id: 10, UserId: 42, Type: LogTypeManage, Content: "管理员增加用户额度 8 点额度"},
		{Id: 11, UserId: 42, Type: LogTypeManage, Content: "管理员减少用户额度 9 点额度", Other: "{}"},
		{Id: 12, UserId: 42, Type: LogTypeManage, Content: "管理员覆盖用户额度从 9 点额度 为 10 点额度", Other: "null"},
		{Id: 13, UserId: 42, Type: LogTypeManage, Other: `{"op":{"action":"user.quota_add","params":{"quota":"11 点额度"}}}`},
		{Id: 14, UserId: 42, Type: LogTypeTopup, Content: "使用在线充值成功，充值金额: 12 点额度"},
		{Id: 15, UserId: 2, Type: LogTypeManage, Other: `{ "op": { "action": "user.quota_add", "params": { "target_user_id": "42" } } }`},
		{Id: 21, UserId: 420, Type: LogTypeTopup},
		{Id: 22, UserId: 1, Type: LogTypeRefund},
		{Id: 23, UserId: 42, Type: LogTypeConsume, Quota: 1},
		{Id: 24, UserId: 42, Type: LogTypeLogin},
		{Id: 25, UserId: 42, Type: LogTypeSystem, Content: "成功启用两步验证"},
		{Id: 26, UserId: 42, Type: LogTypeManage, Content: "admin cleared github binding for user test"},
		{Id: 27, UserId: 1, Type: LogTypeManage, Other: `{"op":{"action":"user.update","params":{"target_user_id":42}}}`},
		{Id: 28, UserId: 42, Type: LogTypeManage, Other: `{"op":{"action":"user.quota_add","params":{"target_user_id":420}}}`},
		{Id: 29, UserId: 1, Type: LogTypeManage, Other: `{"op":{"action":"user.quota_add","params":{"target_user_id":142}}}`},
		{Id: 30, UserId: 42, Type: LogTypeManage, Other: `{"op":{"action":"user.manage","params":{"target_user_id":42}}}`, Content: "管理员增加用户额度 20 点额度"},
		{Id: 31, UserId: 42, Type: LogTypeSystem, Content: "充值配置更新完成"},
		{Id: 32, UserId: 42, Type: LogTypeError, Other: "not JSON"},
		{Id: 33, UserId: 42, Type: LogTypeManage, Other: `{"op": broken}`},
		{Id: 34, UserId: 42, Type: LogTypeConsume, Quota: -100, Content: "historical negative charge"},
	}
	for i := range logs {
		logs[i].CreatedAt = 1000
	}
	require.NoError(t, db.Create(&logs).Error)

	var ids []int
	for offset := 0; offset < 18; offset += 4 {
		page, total, err := GetUserQuotaLogs(42, offset, 4)
		require.NoError(t, err)
		assert.EqualValues(t, 18, total)
		assert.LessOrEqual(t, len(page), 4)
		for _, log := range page {
			ids = append(ids, log.Id)
		}
	}
	assert.Equal(t, []int{34, 31, 25, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}, ids)

	page, total, err := GetUserQuotaLogs(42, 100, 4)
	require.NoError(t, err)
	assert.EqualValues(t, 18, total)
	assert.NotNil(t, page)
	assert.Empty(t, page)
}

func TestGetUserQuotaLogsPreservesHistoricalContentAndOperatorDetails(t *testing.T) {
	db := setupUserQuotaLogsTestDB(t)
	content := "管理员覆盖用户额度从 9 点额度 为 10 点额度"
	other := common.MapToJsonStr(map[string]interface{}{
		"admin_info": map[string]interface{}{
			"admin_id": 1, "admin_username": "operator", "response_body": "private response", "response_status": 200,
		},
	})
	require.NoError(t, db.Create(&Log{UserId: 42, Type: LogTypeManage, Content: content, Other: other}).Error)

	page, total, err := GetUserQuotaLogs(42, 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, page, 1)
	assert.Equal(t, content, page[0].Content)
	assert.Equal(t, 42, page[0].UserId)
	parsed, err := common.StrToMap(page[0].Other)
	require.NoError(t, err)
	adminInfo, ok := parsed["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "operator", adminInfo["admin_username"])
	assert.NotContains(t, adminInfo, "response_body")
	assert.NotContains(t, adminInfo, "response_status")
}

func TestLegacyQuotaHistoryHandlesMalformedJSONWithoutDatabaseJSONFunctions(t *testing.T) {
	db := setupUserQuotaLogsTestDB(t)
	common.SetLogDatabaseType(common.DatabaseTypePostgreSQL)
	entries := []Log{
		{Id: 1, UserId: 42, Type: LogTypeTopup, CreatedAt: 100},
		{Id: 2, UserId: 1, Type: LogTypeManage, CreatedAt: 101, Other: `{"op":{"action":"user.quota_add","params":{"target_user_id":"42"}}}`},
		{Id: 3, UserId: 1, Type: LogTypeManage, CreatedAt: 102, Other: `{"op":{"action":"user.quota_add","params":{"target_user_id":420}}}`},
		{Id: 4, UserId: 42, Type: LogTypeManage, CreatedAt: 103, Other: `{"op":broken}`, Content: "管理员增加用户额度 5 点额度"},
		{Id: 5, UserId: 42, Type: LogTypeManage, CreatedAt: 103, Other: "not JSON"},
		{Id: 6, UserId: 42, Type: LogTypeConsume, CreatedAt: 103, Quota: -10},
	}
	// Put an entire scan batch of unrelated management operations ahead of the
	// relevant history, protecting cursor traversal and target filtering.
	for i := 0; i < 200; i++ {
		entries = append(entries, Log{Id: 100 + i, UserId: 1, Type: LogTypeManage, CreatedAt: 104, Other: "not JSON"})
	}
	require.NoError(t, db.Create(&entries).Error)
	first, total, err := GetUserQuotaLogs(42, 0, 2)
	require.NoError(t, err)
	assert.EqualValues(t, 4, total)
	require.Len(t, first, 2)
	assert.Equal(t, 6, first[0].Id)
	assert.Equal(t, 4, first[1].Id)
	second, total, err := GetUserQuotaLogs(42, 2, 2)
	require.NoError(t, err)
	assert.EqualValues(t, 4, total)
	require.Len(t, second, 2)
	assert.Equal(t, 2, second[0].Id)
	assert.Equal(t, 1, second[1].Id)
}
