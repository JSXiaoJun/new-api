package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetUserQuotaLogs(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	user, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionSameLevel)
		return
	}
	pageInfo := common.GetPageQuery(c)
	if pageInfo.GetPage() < 1 || pageInfo.GetPageSize() < 1 || pageInfo.GetPage()-1 > int(^uint(0)>>1)/pageInfo.GetPageSize() {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	view := c.Query("view")
	if len(c.QueryArray("view")) > 1 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if view == "" {
		view = "legacy"
	}
	if view != "legacy" && view != "credits" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if view == "credits" {
		credits, total, err := model.GetUserQuotaCredits(id, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
		if err != nil {
			common.ApiError(c, err)
			return
		}
		pageInfo.SetTotal(int(total))
		pageInfo.SetItems(credits)
		common.ApiSuccess(c, pageInfo)
		return
	}
	logs, total, err := model.GetUserQuotaLogs(id, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
}
