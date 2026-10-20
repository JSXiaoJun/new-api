package controller

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxAbnormalChargeDays = 90

// abnormalChargeSince reads the reviewed period in days (default 1).
func abnormalChargeSince(c *gin.Context) (int64, bool) {
	days := 1
	if raw := c.Query("days"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxAbnormalChargeDays {
			return 0, false
		}
		days = parsed
	}
	return common.GetTimestamp() - int64(days)*86400, true
}

func validAbnormalChargePage(pageInfo *common.PageInfo) bool {
	return pageInfo.GetPage() >= 1 && pageInfo.GetPageSize() >= 1 &&
		pageInfo.GetPage()-1 <= int(^uint(0)>>1)/pageInfo.GetPageSize()
}

// GetAbnormalChargeUsers lists users whose abnormal requests went uncharged.
func GetAbnormalChargeUsers(c *gin.Context) {
	since, ok := abnormalChargeSince(c)
	pageInfo := common.GetPageQuery(c)
	if !ok || !validAbnormalChargePage(pageInfo) {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	settings := billing_setting.GetAbnormalRequestSetting()
	summaries, total, err := model.ListAbnormalChargeUsers(since, c.Query("keyword"), c.GetInt("role"), model.AbnormalChargeThresholds{
		HourlyCount:  settings.FlagHourlyCount,
		RatioPercent: settings.FlagRatioPercent,
		MinRequests:  settings.FlagMinRequests,
		ForgoneQuota: settings.FlagForgoneQuota,
	}, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	for _, summary := range summaries {
		summary.StrictBilling = billing_setting.IsStrictBillingUser(summary.UserId)
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(summaries)
	common.ApiSuccess(c, pageInfo)
}

// abnormalChargeTargetUser loads the user in the :id path parameter and checks
// that the operator may manage it.
func abnormalChargeTargetUser(c *gin.Context) (*model.User, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return nil, false
	}
	user, err := model.GetUserById(id, false)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorT(c, "User does not exist")
		} else {
			common.ApiError(c, err)
		}
		return nil, false
	}
	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiErrorT(c, "No permission to access users of same or higher level")
		return nil, false
	}
	return user, true
}

// GetUserAbnormalCharges lists one user's abnormal request records.
func GetUserAbnormalCharges(c *gin.Context) {
	user, ok := abnormalChargeTargetUser(c)
	if !ok {
		return
	}
	since, ok := abnormalChargeSince(c)
	pageInfo := common.GetPageQuery(c)
	status := c.Query("status")
	if !ok || !validAbnormalChargePage(pageInfo) || (status != "" && status != "pending" && status != "recovered") {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	records, total, err := model.ListAbnormalCharges(user.Id, since, status, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(records)
	common.ApiSuccess(c, pageInfo)
}

type recoverAbnormalChargesRequest struct {
	// Ids selects the records to recover; when empty every pending
	// user-caused record within Days, and no newer than MaxId when set, is
	// recovered.
	Ids   []int64 `json:"ids"`
	Days  int     `json:"days"`
	MaxId int64   `json:"max_id"`
}

const maxAbnormalChargeRecoverIds = 1000

// RecoverUserAbnormalCharges charges a user the uncollected quota of pending
// abnormal request records. The balance may become negative.
func RecoverUserAbnormalCharges(c *gin.Context) {
	user, ok := abnormalChargeTargetUser(c)
	if !ok {
		return
	}
	var req recoverAbnormalChargesRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || len(req.Ids) > maxAbnormalChargeRecoverIds ||
		req.MaxId < 0 || (len(req.Ids) == 0 && (req.Days < 1 || req.Days > maxAbnormalChargeDays)) {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	for _, id := range req.Ids {
		if id <= 0 {
			common.ApiErrorT(c, "Invalid parameters")
			return
		}
	}
	since := common.GetTimestamp() - int64(req.Days)*86400
	params := model.AuditFields{
		"target_user_id":  user.Id,
		"target_username": user.Username,
		"mode":            "subtract",
		"reason":          "abnormal_charge_recovery",
	}
	success := false
	defer func() {
		content := auditContentEN("user.quota_subtract", params)
		if !success {
			content = "Failed user quota adjustment"
		}
		model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), content, c.ClientIP(), "user.quota_subtract", params,
			auditOperatorInfo(c), &model.AuditRequestInfo{
				Method: c.Request.Method, Route: c.FullPath(), Status: c.Writer.Status(), Success: success,
			}, c)
		markAuditLogged(c)
	}()

	recovery, err := model.RecoverAbnormalCharges(user.Id, c.GetInt("id"), c.GetInt("role"), req.Ids, since, req.MaxId)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrNoAbnormalChargeToRecover):
			params["failure_reason"] = "nothing_to_recover"
			common.ApiErrorT(c, "No pending abnormal charges to recover")
		case errors.Is(err, model.ErrAbnormalChargePermission):
			params["failure_reason"] = "permission_denied"
			common.ApiErrorT(c, "No permission to update users of same or higher permission level")
		case errors.Is(err, model.ErrWalletQuotaLimitExceeded):
			params["failure_reason"] = "quota_limit_exceeded"
			common.ApiError(c, err)
		default:
			params["failure_reason"] = "database_error"
			common.ApiError(c, err)
		}
		return
	}
	params["quota"] = recovery.Quota
	params["requested_quota"] = recovery.Quota
	params["from"] = recovery.Before
	params["to"] = recovery.After
	params["recovered_records"] = recovery.Count
	params["recovery_id"] = recovery.RecoveryId
	success = true
	// The user's own quota history shows a plain deduction: why the user was
	// charged stays in the admin-only audit log.
	userParams := model.AuditFields{
		"target_user_id": user.Id,
		"mode":           "subtract",
		"quota":          recovery.Quota,
		"from":           recovery.Before,
		"to":             recovery.After,
	}
	operation := model.AuditOperation{Action: "user.quota_subtract", Params: userParams}
	model.RecordLogWithAdminInfo(recovery.UserId, model.LogTypeTopup,
		auditContentEN("user.quota_subtract", userParams), auditOperatorInfo(c), &operation, c)
	common.ApiSuccess(c, gin.H{
		"recovered_records": recovery.Count,
		"quota":             recovery.Quota,
		"from":              recovery.Before,
		"to":                recovery.After,
	})
}

type strictBillingRequest struct {
	Enabled bool `json:"enabled"`
}

// SetUserStrictBilling turns strict billing on or off for one user: the
// user's abnormal requests are then always charged by usage.
func SetUserStrictBilling(c *gin.Context) {
	user, ok := abnormalChargeTargetUser(c)
	if !ok {
		return
	}
	var req strictBillingRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	changed, err := model.SetStrictBillingUser(user.Id, req.Enabled)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !changed {
		common.ApiSuccess(c, gin.H{"enabled": req.Enabled})
		return
	}
	recordManageAuditFor(c, user.Id, "user.strict_billing", map[string]any{
		"username": user.Username,
		"id":       user.Id,
		"enabled":  req.Enabled,
	})
	common.ApiSuccess(c, gin.H{"enabled": req.Enabled})
}
