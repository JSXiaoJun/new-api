package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func performAbnormalChargeRequest(t *testing.T, handler gin.HandlerFunc, method, path, id, body string, role int) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if id != "" {
		c.Params = gin.Params{{Key: "id", Value: id}}
	}
	c.Set("id", 9999)
	c.Set("username", "root-operator")
	c.Set("role", role)
	handler(c)
	return recorder
}

func setupAbnormalChargeControllerTest(t *testing.T) {
	t.Helper()
	db := setupManageUserTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.AbnormalCharge{}, &model.Option{}))
	users := []model.User{
		{Id: 42, Username: "abuser", Password: "password", Role: common.RoleCommonUser, Quota: 1_000, Group: "default", AffCode: "abuser"},
		{Id: 43, Username: "other-admin", Password: "password", Role: common.RoleAdminUser, Quota: 1_000, Group: "default", AffCode: "other-admin"},
		{Id: 9999, Username: "root-operator", Password: "password", Role: common.RoleRootUser, Group: "default", AffCode: "root-operator"},
	}
	require.NoError(t, db.Create(&users).Error)
	now := common.GetTimestamp()
	for _, record := range []model.AbnormalCharge{
		{UserId: 42, CreatedAt: now - 60, UserCaused: true, Outcome: model.AbnormalChargeOutcomeWaived, ForgoneQuota: 3_000},
		{UserId: 42, CreatedAt: now - 30, UserCaused: true, Outcome: model.AbnormalChargeOutcomeRefunded, ForgoneQuota: 2_000},
		{UserId: 42, CreatedAt: now - 30, UserCaused: false, Outcome: model.AbnormalChargeOutcomeWaived, ForgoneQuota: 9_000},
		{UserId: 43, CreatedAt: now - 30, UserCaused: true, Outcome: model.AbnormalChargeOutcomeWaived, ForgoneQuota: 1_000},
	} {
		require.NoError(t, model.RecordAbnormalCharge(&record))
	}
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() { require.NoError(t, billing_setting.LoadStrictBillingUsers("[]")) })
}

func TestRecoverUserAbnormalChargesDebitsTheUserOnceAndLogsIt(t *testing.T) {
	setupAbnormalChargeControllerTest(t)

	recorder := performAbnormalChargeRequest(t, RecoverUserAbnormalCharges, http.MethodPost, "/api/user/42/abnormal_charges/recover", "42", `{"days":1}`, common.RoleRootUser)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Recovered int64 `json:"recovered_records"`
			Quota     int64 `json:"quota"`
			From      int   `json:"from"`
			To        int   `json:"to"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, recorder.Body.String())
	assert.EqualValues(t, 2, response.Data.Recovered, "the upstream fault is not recovered from the user")
	assert.EqualValues(t, 5_000, response.Data.Quota)
	assert.Equal(t, 1_000, response.Data.From)
	assert.Equal(t, -4_000, response.Data.To)
	user, err := model.GetUserById(42, false)
	require.NoError(t, err)
	assert.Equal(t, -4_000, user.Quota)

	history := performUserQuotaLogsRequest(t, "42", "?p=1&page_size=10", common.RoleRootUser)
	require.Equal(t, http.StatusOK, history.Code)
	assert.Contains(t, history.Body.String(), `user.quota_subtract`, "the user's quota history shows the recovery")
	gin.SetMode(gin.TestMode)
	selfRecorder := httptest.NewRecorder()
	self, _ := gin.CreateTestContext(selfRecorder)
	self.Request = httptest.NewRequest(http.MethodGet, "/api/log/self?p=1&page_size=10", nil)
	self.Set("id", 42)
	self.Set("role", common.RoleCommonUser)
	GetUserLogs(self)
	require.Contains(t, selfRecorder.Body.String(), `"success":true`)
	assert.Contains(t, selfRecorder.Body.String(), "Decreased user quota", "the user sees the deduction")
	assert.NotContains(t, selfRecorder.Body.String(), "abnormal", "the user never sees why the charge was recovered")
	var audits []model.AuditLog
	require.NoError(t, model.LOG_DB.Find(&audits).Error)
	auditText := ""
	for _, audit := range audits {
		data, err := common.Marshal(audit.Other)
		require.NoError(t, err)
		auditText += string(data)
	}
	assert.Contains(t, auditText, "abnormal_charge_recovery", "administrators keep the reason in the audit log")

	again := performAbnormalChargeRequest(t, RecoverUserAbnormalCharges, http.MethodPost, "/api/user/42/abnormal_charges/recover", "42", `{"days":1}`, common.RoleRootUser)
	assert.Contains(t, again.Body.String(), `"success":false`)
	user, err = model.GetUserById(42, false)
	require.NoError(t, err)
	assert.Equal(t, -4_000, user.Quota, "a second recovery charges nothing")
}

func TestAbnormalChargeEndpointsRejectInvalidRequests(t *testing.T) {
	setupAbnormalChargeControllerTest(t)
	for _, tt := range []struct {
		name    string
		handler gin.HandlerFunc
		method  string
		path    string
		id      string
		body    string
		role    int
	}{
		{name: "recover without ids or a valid period", handler: RecoverUserAbnormalCharges, method: http.MethodPost, path: "/api/user/42/abnormal_charges/recover", id: "42", body: `{"days":0}`, role: common.RoleRootUser},
		{name: "recover with a non-positive id", handler: RecoverUserAbnormalCharges, method: http.MethodPost, path: "/api/user/42/abnormal_charges/recover", id: "42", body: `{"ids":[0]}`, role: common.RoleRootUser},
		{name: "an admin recovering another admin", handler: RecoverUserAbnormalCharges, method: http.MethodPost, path: "/api/user/43/abnormal_charges/recover", id: "43", body: `{"days":1}`, role: common.RoleAdminUser},
		{name: "list with an out-of-range period", handler: GetAbnormalChargeUsers, method: http.MethodGet, path: "/api/user/abnormal_charges?days=91&p=1&page_size=10", role: common.RoleRootUser},
		{name: "records with an unknown status", handler: GetUserAbnormalCharges, method: http.MethodGet, path: "/api/user/42/abnormal_charges?status=all&p=1&page_size=10", id: "42", role: common.RoleRootUser},
		{name: "all records with an unknown case", handler: GetAbnormalChargeRecords, method: http.MethodGet, path: "/api/user/abnormal_charges/records?case=other&p=1&page_size=10", role: common.RoleRootUser},
		{name: "all records with a bad user id", handler: GetAbnormalChargeRecords, method: http.MethodGet, path: "/api/user/abnormal_charges/records?user_id=x&p=1&page_size=10", role: common.RoleRootUser},
		{name: "strict billing of an equal role", handler: SetUserStrictBilling, method: http.MethodPut, path: "/api/user/43/strict_billing", id: "43", body: `{"enabled":true}`, role: common.RoleAdminUser},
	} {
		t.Run(tt.name, func(t *testing.T) {
			response := performAbnormalChargeRequest(t, tt.handler, tt.method, tt.path, tt.id, tt.body, tt.role)
			assert.Contains(t, response.Body.String(), `"success":false`)
		})
	}
	for _, id := range []int{42, 43} {
		user, err := model.GetUserById(id, false)
		require.NoError(t, err)
		assert.Equal(t, 1_000, user.Quota)
	}
	assert.False(t, billing_setting.IsStrictBillingUser(43))
}

func TestSetUserStrictBillingShowsInTheUserList(t *testing.T) {
	setupAbnormalChargeControllerTest(t)

	recorder := performAbnormalChargeRequest(t, SetUserStrictBilling, http.MethodPut, "/api/user/42/strict_billing", "42", `{"enabled":true}`, common.RoleRootUser)
	require.Contains(t, recorder.Body.String(), `"success":true`)
	assert.True(t, billing_setting.IsStrictBillingUser(42))
	var option model.Option
	require.NoError(t, model.DB.Where(&model.Option{Key: billing_setting.StrictBillingUsersOptionKey}).First(&option).Error)
	assert.Equal(t, "[42]", option.Value)

	list := performAbnormalChargeRequest(t, GetAbnormalChargeUsers, http.MethodGet, "/api/user/abnormal_charges?days=1&p=1&page_size=10", "", "", common.RoleRootUser)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Total int                               `json:"total"`
			Items []model.AbnormalChargeUserSummary `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(list.Body.Bytes(), &response))
	require.True(t, response.Success, list.Body.String())
	assert.Equal(t, 2, response.Data.Total)
	strict := map[int]bool{}
	for _, item := range response.Data.Items {
		strict[item.UserId] = item.StrictBilling
	}
	assert.True(t, strict[42])
	assert.False(t, strict[43])

	recorder = performAbnormalChargeRequest(t, SetUserStrictBilling, http.MethodPut, "/api/user/42/strict_billing", "42", `{"enabled":false}`, common.RoleRootUser)
	require.Contains(t, recorder.Body.String(), `"success":true`)
	assert.False(t, billing_setting.IsStrictBillingUser(42))
}
