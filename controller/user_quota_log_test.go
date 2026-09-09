package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func performUserQuotaLogsRequest(t *testing.T, id, query string, role int) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/"+id+"/quota/log"+query, nil)
	c.Params = gin.Params{{Key: "id", Value: id}}
	c.Set("role", role)
	GetUserQuotaLogs(c)
	return recorder
}

func TestGetUserQuotaLogsReturnsAdministratorQuotaChangesForTarget(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := model.User{Id: 42, Username: "quota-target", Password: "password", Role: common.RoleCommonUser, Quota: 100, Group: "default", AffCode: "quota-target"}
	operator := model.User{Id: 9999, Username: "root-operator", Password: "password", Role: common.RoleRootUser, Group: "default", AffCode: "root-operator"}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&operator).Error)

	for _, mode := range []string{"add", "subtract", "override"} {
		recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":42,"action":"add_quota","mode":%q,"value":10}`, mode))
		assert.Equal(t, http.StatusOK, recorder.Code)
		require.Contains(t, recorder.Body.String(), `"success":true`)
	}

	recorder := performUserQuotaLogsRequest(t, "42", "?p=2&page_size=2", common.RoleAdminUser)
	assert.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Page     int         `json:"page"`
			PageSize int         `json:"page_size"`
			Total    int         `json:"total"`
			Items    []model.Log `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, 2, response.Data.Page)
	assert.Equal(t, 2, response.Data.PageSize)
	assert.Equal(t, 3, response.Data.Total)
	require.Len(t, response.Data.Items, 1)
	log := response.Data.Items[0]
	assert.Equal(t, 9999, log.UserId, "audit ownership must remain with the operator")
	assert.Contains(t, log.Other, `"action":"user.quota_add"`)
	assert.Contains(t, log.Other, `"target_user_id":42`)
	assert.Contains(t, log.Other, `"admin_username":"root-operator"`)
}

func TestGetUserQuotaLogsEnforcesTargetRoleAndValidPagination(t *testing.T) {
	db := setupManageUserTestDB(t)
	users := []model.User{
		{Id: 42, Username: "quota-common", Role: common.RoleCommonUser, AffCode: "quota-common"},
		{Id: 43, Username: "quota-admin", Role: common.RoleAdminUser, AffCode: "quota-admin"},
		{Id: 44, Username: "quota-root", Role: common.RoleRootUser, AffCode: "quota-root"},
	}
	require.NoError(t, db.Create(&users).Error)
	cases := []struct {
		name        string
		id          string
		query       string
		role        int
		wantSuccess bool
	}{
		{name: "admin can read common user", id: "42", role: common.RoleAdminUser, wantSuccess: true},
		{name: "admin cannot read peer", id: "43", role: common.RoleAdminUser},
		{name: "admin cannot read root", id: "44", role: common.RoleAdminUser},
		{name: "root can read root", id: "44", role: common.RoleRootUser, wantSuccess: true},
		{name: "missing user", id: "45", role: common.RoleRootUser},
		{name: "zero id", id: "0", role: common.RoleRootUser},
		{name: "negative id", id: "-1", role: common.RoleRootUser},
		{name: "invalid id", id: "invalid", role: common.RoleRootUser},
		{name: "negative page", id: "42", query: "?p=-1", role: common.RoleRootUser},
		{name: "negative page size", id: "42", query: "?page_size=-1", role: common.RoleRootUser},
		{name: "overflowing offset", id: "42", query: "?p=9223372036854775807&page_size=100", role: common.RoleRootUser},
		{name: "unknown view", id: "42", query: "?view=all", role: common.RoleRootUser},
		{name: "duplicate view", id: "42", query: "?view=credits&view=legacy", role: common.RoleRootUser},
		{name: "credits view", id: "42", query: "?view=credits", role: common.RoleAdminUser, wantSuccess: true},
		{name: "credits cannot read peer", id: "43", query: "?view=credits", role: common.RoleAdminUser},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := performUserQuotaLogsRequest(t, tc.id, tc.query, tc.role)
			var response struct {
				Success bool `json:"success"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.Equal(t, tc.wantSuccess, response.Success)
			if tc.wantSuccess {
				assert.Contains(t, recorder.Body.String(), `"items":[]`)
			}
		})
	}
}

func TestQuotaCreditsAPIIncludesAdministratorAndRefundIncreases(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := model.User{Id: 42, Username: "credit-api-user", Role: common.RoleCommonUser, Quota: 100}
	require.NoError(t, db.Create(&user).Error)
	recorder := performManageUserRequest(t, `{"id":42,"action":"add_quota","mode":"add","value":10}`)
	require.Contains(t, recorder.Body.String(), `"success":true`)
	require.NoError(t, model.IncreaseUserQuota(user.Id, 30, true, model.QuotaCreditMeta{Source: "wallet_refund", RequestId: "refund-id"}))
	recorder = performUserQuotaLogsRequest(t, "42", "?view=credits&p=2&page_size=1", common.RoleAdminUser)
	var response struct {
		Success bool
		Data    struct {
			Total int
			Items []model.QuotaCredit
		}
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, 2, response.Data.Total)
	require.Len(t, response.Data.Items, 1)
	credit := response.Data.Items[0]
	assert.Equal(t, 42, credit.UserId)
	assert.Equal(t, 9999, credit.OperatorId)
	assert.EqualValues(t, 10, credit.Delta)
	assert.Equal(t, "admin_add", credit.Source)
}

func TestTransferAffQuotaRecordsOnlyCommittedWalletCredits(t *testing.T) {
	db := setupManageUserTestDB(t)
	confirmPaymentComplianceForTest(t)
	previousQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = previousQuotaPerUnit })
	user := model.User{Id: 42, Username: "quota-transfer", Role: common.RoleCommonUser, Quota: 100, AffQuota: 500000}
	require.NoError(t, db.Create(&user).Error)

	for _, wantSuccess := range []bool{true, false} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/user/aff_transfer", strings.NewReader(`{"quota":500000}`))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("id", user.Id)
		TransferAffQuota(c)
		var response struct {
			Success bool `json:"success"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		assert.Equal(t, wantSuccess, response.Success)
	}

	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 500100, user.Quota)
	assert.Zero(t, user.AffQuota)
	logs, total, err := model.GetUserQuotaLogs(user.Id, 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, logs, 1)
	assert.Contains(t, logs[0].Content, "邀请奖励转入钱包，获得额度 ")
	credits, total, err := model.GetUserQuotaCredits(user.Id, 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, credits, 1)
	assert.EqualValues(t, 500000, credits[0].Delta)
	assert.Equal(t, "invitation_transfer", credits[0].Source)
}
