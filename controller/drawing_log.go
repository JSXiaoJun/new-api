package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// GetDrawingLogs returns the admin view of the merged drawing log: AI image
// generations and Midjourney tasks, newest first.
func GetDrawingLogs(c *gin.Context) {
	params := drawingLogQueryParams(c, 0)
	pageInfo := common.GetPageQuery(c)
	items, total, err := model.GetDrawingLogs(params, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(forwardDrawingLogImages(items))
	common.ApiSuccess(c, pageInfo)
}

// GetUserDrawingLogs returns the same merged drawing log scoped to the caller.
func GetUserDrawingLogs(c *gin.Context) {
	params := drawingLogQueryParams(c, c.GetInt("id"))
	pageInfo := common.GetPageQuery(c)
	items, total, err := model.GetDrawingLogs(params, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(forwardDrawingLogImages(items))
	common.ApiSuccess(c, pageInfo)
}

// drawingLogQueryParams reads the drawing log filter. The drawing log UI sends
// millisecond timestamps (the Midjourney convention) while the AI image log
// stores seconds, so the range is converted here.
func drawingLogQueryParams(c *gin.Context, userId int) model.DrawingLogQueryParams {
	startMs, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endMs, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	return model.DrawingLogQueryParams{
		UserId:         userId,
		ChannelId:      c.Query("channel_id"),
		Filter:         strings.TrimSpace(c.Query("filter")),
		StartTimestamp: millisecondsToSeconds(startMs),
		EndTimestamp:   millisecondsToSeconds(endMs),
	}
}

// millisecondsToSeconds converts a drawing log timestamp, tolerating a value
// that is already in seconds so an API caller cannot silently get an empty page.
func millisecondsToSeconds(timestamp int64) int64 {
	if timestamp <= 0 {
		return 0
	}
	if timestamp < 1_000_000_000_000 {
		return timestamp
	}
	return timestamp / 1000
}

// forwardDrawingLogImages keeps the Midjourney image forwarding setting working
// for merged rows: when forwarding is enabled, Midjourney images are served
// through this gateway instead of the upstream URL. AI image links are already
// desensitized by the image middleware, so they are left unchanged.
func forwardDrawingLogImages(items []*model.DrawingLog) []*model.DrawingLog {
	if !setting.MjForwardUrlEnabled {
		return items
	}
	for _, item := range items {
		if item.Source != model.DrawingLogSourceMidjourney || item.MjId == "" {
			continue
		}
		item.ImageUrls = []string{system_setting.ServerAddress + "/mj/image/" + item.MjId}
	}
	return items
}
