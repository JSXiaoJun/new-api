package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetUserQuotaLogs(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	user, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiErrorT(c, "No permission to access users of same or higher level")
		return
	}
	pageInfo := common.GetPageQuery(c)
	if pageInfo.GetPage() < 1 || pageInfo.GetPageSize() < 1 || pageInfo.GetPage()-1 > int(^uint(0)>>1)/pageInfo.GetPageSize() {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	view := c.Query("view")
	if len(c.QueryArray("view")) > 1 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if view == "" {
		view = "legacy"
	}
	if view != "legacy" && view != "credits" {
		common.ApiErrorT(c, "Invalid parameters")
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
