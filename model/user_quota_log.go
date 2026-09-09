package model

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// GetUserQuotaLogs includes historical balance events without changing audit-log
// ownership: current administrator operations belong to their operator, while
// older operations belong to the affected user.
func GetUserQuotaLogs(userId, startIdx, num int) (logs []*Log, total int64, err error) {
	if common.UsingLogDatabase(common.DatabaseTypePostgreSQL) {
		return getUserQuotaLogsWithoutJSONCast(userId, startIdx, num)
	}
	logs = make([]*Log, 0)
	var action, targetUserId string
	switch {
	case common.UsingLogDatabase(common.DatabaseTypeMySQL):
		other := "CASE WHEN JSON_VALID(logs.other) THEN logs.other ELSE '{}' END"
		action = "NULLIF(JSON_UNQUOTE(JSON_EXTRACT(" + other + ", '$.op.action')), 'null')"
		targetUserId = "COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(" + other + ", '$.op.params.target_user_id')), 'null'), CAST(logs.user_id AS CHAR))"
	case common.UsingLogDatabase(common.DatabaseTypeClickHouse):
		action = "JSONExtractString(logs.other, 'op', 'action')"
		targetUserId = "COALESCE(NULLIF(JSONExtractString(logs.other, 'op', 'params', 'target_user_id'), ''), NULLIF(NULLIF(JSONExtractRaw(logs.other, 'op', 'params', 'target_user_id'), ''), 'null'), toString(logs.user_id))"
	default:
		other := "CASE WHEN json_valid(logs.other) THEN logs.other ELSE '{}' END"
		action = "json_extract(" + other + ", '$.op.action')"
		targetUserId = "CAST(COALESCE(json_extract(" + other + ", '$.op.params.target_user_id'), logs.user_id) AS TEXT)"
	}

	base := LOG_DB.Session(&gorm.Session{NewDB: true})
	// Keep all system events as audit evidence: a text whitelist can hide an
	// older or unexpected credit. Negative consumption is especially important
	// when investigating historical overflow or settlement bugs.
	ownedEvents := base.Where("logs.user_id = ?", userId).Where(
		base.Where("logs.type IN ?", []int{LogTypeTopup, LogTypeRefund, LogTypeSystem}).
			Or("logs.type = ? AND logs.quota < 0", LogTypeConsume),
	)
	quotaActions := []string{"user.quota_add", "user.quota_subtract", "user.quota_override"}
	managedEvents := base.Where("logs.type = ?", LogTypeManage).Where(
		base.Where(action+" IN ? AND "+targetUserId+" = ?", quotaActions, strconv.Itoa(userId)).
			Or("COALESCE("+action+", '') = '' AND logs.user_id = ? AND (logs.content LIKE ? OR logs.content LIKE ? OR logs.content LIKE ?)",
				userId, "管理员增加用户额度 %", "管理员减少用户额度 %", "管理员覆盖用户额度从 %"),
	)
	tx := base.Model(&Log{}).Where(ownedEvents.Or(managedEvents))
	if err = tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := "logs.created_at desc, logs.id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
	}
	if err = tx.Order(order).Offset(startIdx).Limit(num).Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		assignDisplayLogIds(logs, startIdx)
	}
	stripResponseBodyFromLogs(logs)
	return logs, total, nil
}

// PostgreSQL 9.6 has no non-throwing JSON validity check. Decode legacy text in
// bounded batches so a single malformed Other cannot break the entire history.
func getUserQuotaLogsWithoutJSONCast(userId, startIdx, num int) ([]*Log, int64, error) {
	logs := make([]*Log, 0)
	var total int64
	base := LOG_DB.Session(&gorm.Session{NewDB: true})
	owned := base.Where("user_id = ?", userId).Where(
		base.Where("type IN ?", []int{LogTypeTopup, LogTypeRefund, LogTypeSystem}).
			Or("type = ? AND quota < 0", LogTypeConsume),
	)
	query := base.Model(&Log{}).Where(owned.Or("type = ?", LogTypeManage))
	var lastTime int64
	var lastId int
	first := true
	for {
		batch := make([]*Log, 0, 200)
		page := query.Session(&gorm.Session{})
		if !first {
			page = page.Where("created_at < ? OR (created_at = ? AND id < ?)", lastTime, lastTime, lastId)
		}
		if err := page.Order("created_at DESC, id DESC").Limit(200).Find(&batch).Error; err != nil {
			return nil, 0, err
		}
		if len(batch) == 0 {
			break
		}
		for _, log := range batch {
			if log.Type == LogTypeManage && !isQuotaManagementLogForUser(log, userId) {
				continue
			}
			if total >= int64(startIdx) && len(logs) < num {
				logs = append(logs, log)
			}
			total++
		}
		last := batch[len(batch)-1]
		lastTime, lastId = last.CreatedAt, last.Id
		first = false
	}
	stripResponseBodyFromLogs(logs)
	return logs, total, nil
}

func isQuotaManagementLogForUser(log *Log, userId int) bool {
	var other struct {
		Op struct {
			Action string `json:"action"`
			Params struct {
				TargetUserId interface{} `json:"target_user_id"`
			} `json:"params"`
		} `json:"op"`
	}
	if err := common.UnmarshalJsonStr(log.Other, &other); err != nil {
		other.Op.Action = ""
	}
	switch other.Op.Action {
	case "user.quota_add", "user.quota_subtract", "user.quota_override":
		switch target := other.Op.Params.TargetUserId.(type) {
		case nil:
			return log.UserId == userId
		case float64:
			return target == float64(userId)
		case string:
			return target == strconv.Itoa(userId)
		default:
			return false
		}
	case "":
		return log.UserId == userId && (strings.HasPrefix(log.Content, "管理员增加用户额度 ") ||
			strings.HasPrefix(log.Content, "管理员减少用户额度 ") || strings.HasPrefix(log.Content, "管理员覆盖用户额度从 "))
	default:
		return false
	}
}
