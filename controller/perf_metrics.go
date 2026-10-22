package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

func GetPerfMetricsSummary(c *gin.Context) {
	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	recentModels := c.QueryArray("recent_model")
	if len(recentModels) > perfmetrics.MaxRecentModels {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid parameters"))
		return
	}
	activeGroups := append(lo.Keys(ratio_setting.GetGroupRatioCopy()), "auto")
	result, err := perfmetrics.QuerySummaryAll(hours, activeGroups, recentModels...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// GetLiveServiceStatus reports recent success rates of the groups the viewer
// can use; anonymous visitors see the default usable groups.
func GetLiveServiceStatus(c *gin.Context) {
	userGroup := ""
	if userId := c.GetInt("id"); userId > 0 {
		if user, err := model.GetUserCache(userId); err == nil {
			userGroup = user.Group
		}
	}
	common.ApiSuccess(c, perfmetrics.QueryLiveStatus(time.Now(), service.GetUserUsableGroups(userGroup)))
}

// GetLogSuccessRates returns the platform success rate around each usage log,
// aligned with the requested points. Users see only groups they can use;
// admins, who read other users' logs, also see every configured group.
func GetLogSuccessRates(c *gin.Context) {
	var req struct {
		Points []perfmetrics.LiveComparePoint `json:"points"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || len(req.Points) > perfmetrics.MaxLiveComparePoints {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid parameters"))
		return
	}
	userGroup := ""
	if user, err := model.GetUserCache(c.GetInt("id")); err == nil {
		userGroup = user.Group
	}
	allowedGroups := service.GetUserUsableGroups(userGroup)
	if c.GetInt("role") >= common.RoleAdminUser {
		for group := range ratio_setting.GetGroupRatioCopy() {
			allowedGroups[group] = ""
		}
	}
	common.ApiSuccess(c, perfmetrics.QueryLiveSuccessRates(time.Now(), req.Points, allowedGroups))
}

func GetPerfMetrics(c *gin.Context) {
	modelName := c.Query("model")
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "model is required",
		})
		return
	}

	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	result, err := perfmetrics.Query(perfmetrics.QueryParams{
		Model:         modelName,
		Group:         c.Query("group"),
		Hours:         hours,
		AllowedGroups: append(lo.Keys(ratio_setting.GetGroupRatioCopy()), "auto"),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}
