package controller

import (
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/console_setting"

	"github.com/gin-gonic/gin"
)

type AckPopupAnnouncementRequest struct {
	Id int64 `json:"id"`
}

// GetPopupAnnouncements lists the published sign-in popup announcements the
// current user has not confirmed yet, oldest first.
func GetPopupAnnouncements(c *gin.Context) {
	user, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	acked := user.GetSetting().AckAnnouncementIds
	pending := make([]map[string]any, 0)
	for _, item := range console_setting.PublishedPopupAnnouncements(time.Now()) {
		id, _ := console_setting.AnnouncementID(item)
		if !slices.Contains(acked, id) {
			pending = append(pending, item)
		}
	}
	common.ApiSuccess(c, pending)
}

// AckPopupAnnouncement records that the current user confirmed a sign-in popup
// announcement. Confirming an announcement that no longer pops up succeeds
// without storing anything, so a popup removed by an administrator can still
// be closed.
func AckPopupAnnouncement(c *gin.Context) {
	var req AckPopupAnnouncementRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || req.Id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	isPopup := slices.ContainsFunc(console_setting.PublishedPopupAnnouncements(time.Now()), func(item map[string]any) bool {
		id, _ := console_setting.AnnouncementID(item)
		return id == req.Id
	})
	if !isPopup {
		common.ApiSuccess(c, nil)
		return
	}

	user, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	settings := user.GetSetting()
	if slices.Contains(settings.AckAnnouncementIds, req.Id) {
		common.ApiSuccess(c, nil)
		return
	}
	// Drop confirmations of deleted announcements so the list stays bounded by
	// the announcement list.
	existing := make(map[int64]bool)
	for _, item := range console_setting.GetAnnouncements() {
		if id, ok := console_setting.AnnouncementID(item); ok {
			existing[id] = true
		}
	}
	acked := make([]int64, 0, len(settings.AckAnnouncementIds)+1)
	for _, id := range settings.AckAnnouncementIds {
		if existing[id] {
			acked = append(acked, id)
		}
	}
	settings.AckAnnouncementIds = append(acked, req.Id)
	if err := model.UpdateUserSetting(user.Id, settings); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
