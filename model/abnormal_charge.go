package model

import (
	"cmp"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"gorm.io/gorm"
)

// Outcomes of an abnormal request whose charge was dropped or reduced.
const (
	AbnormalChargeOutcomeWaived   = "waived"
	AbnormalChargeOutcomeRefunded = "refunded"
	// AbnormalChargeOutcomePartial: the user paid part of what upstream
	// charged, such as the usage produced before disconnecting.
	AbnormalChargeOutcomePartial = "partial"
)

// Upstream billing marks of an abnormal request.
const (
	UpstreamBillingBilled  = "billed"
	UpstreamBillingUnknown = "unknown"
)

// AbnormalCharge records one request that ended abnormally and was charged
// less than its usage: a waiver by the abnormal billing switches or a refund
// under the official alignment. ForgoneQuota is the charge the normal
// settlement would have made. A record is recovered at most once.
type AbnormalCharge struct {
	Id                 int64  `json:"id" gorm:"primaryKey"`
	UserId             int    `json:"user_id" gorm:"index:idx_abnormal_charge_user_time,priority:1;index:idx_abnormal_charge_user_bucket,priority:1"`
	CreatedAt          int64  `json:"created_at" gorm:"index:idx_abnormal_charge_user_time,priority:2;index:idx_abnormal_charge_created"`
	HourBucket         int64  `json:"hour_bucket" gorm:"index:idx_abnormal_charge_user_bucket,priority:2"`
	RequestId          string `json:"request_id" gorm:"type:varchar(64)"`
	TokenId            int    `json:"token_id"`
	TokenName          string `json:"token_name" gorm:"type:varchar(255)"`
	ModelName          string `json:"model_name" gorm:"type:varchar(255)"`
	ChannelId          int    `json:"channel_id"`
	Ip                 string `json:"ip" gorm:"type:varchar(64)"`
	IsStream           bool   `json:"is_stream"`
	IsTask             bool   `json:"is_task"`
	Case               string `json:"case" gorm:"type:varchar(32)"`
	UserCaused         bool   `json:"user_caused"`
	EndedBy            string `json:"ended_by" gorm:"type:varchar(16)"`
	Phase              string `json:"phase" gorm:"type:varchar(32)"`
	Reason             string `json:"reason" gorm:"type:varchar(32)"`
	ElapsedMs          int64  `json:"elapsed_ms"`
	ClientReceivedData bool   `json:"client_received_data"`
	Outcome            string `json:"outcome" gorm:"type:varchar(16)"`
	ForgoneQuota       int    `json:"forgone_quota"`
	// UpstreamBilling is "billed" when the gateway kept reading upstream after
	// the client left and observed the usage upstream charged, "unknown" when
	// it tried but could not, and empty when it did not try.
	UpstreamBilling          string `json:"upstream_billing" gorm:"type:varchar(16)"`
	UpstreamQuota            int    `json:"upstream_quota"`
	UpstreamPromptTokens     int    `json:"upstream_prompt_tokens"`
	UpstreamCompletionTokens int    `json:"upstream_completion_tokens"`
	ChargedQuota             int    `json:"charged_quota"`
	RecoveryId               string `json:"recovery_id" gorm:"type:varchar(64);index:idx_abnormal_charge_recovery"`
	RecoveredAt              int64  `json:"recovered_at"`
	RecoveredBy              int    `json:"recovered_by"`
}

func RecordAbnormalCharge(record *AbnormalCharge) error {
	if record == nil || record.UserId <= 0 || record.ForgoneQuota <= 0 {
		return nil
	}
	record.Id = 0
	if record.CreatedAt == 0 {
		record.CreatedAt = common.GetTimestamp()
	}
	record.HourBucket = record.CreatedAt / 3600
	record.RecoveryId, record.RecoveredAt, record.RecoveredBy = "", 0, 0
	return DB.Create(record).Error
}

// AbnormalChargeUserSummary aggregates the user-caused abnormal requests of
// one user within the reviewed period.
type AbnormalChargeUserSummary struct {
	UserId              int      `json:"user_id"`
	Username            string   `json:"username" gorm:"-"`
	Count               int64    `json:"count"`
	ReceivedCount       int64    `json:"received_count"`
	ForgoneQuota        int64    `json:"forgone_quota"`
	PendingQuota        int64    `json:"pending_quota"`
	PendingCount        int64    `json:"pending_count"`
	MaxPendingId        int64    `json:"max_pending_id"`
	UpstreamBilledCount int64    `json:"upstream_billed_count"`
	LastAt              int64    `json:"last_at"`
	AvgElapsedMs        float64  `json:"avg_elapsed_ms"`
	ElapsedStddevMs     float64  `json:"elapsed_stddev_ms"`
	PeakHourCount       int64    `json:"peak_hour_count"`
	PeakHourWeighted    int64    `json:"peak_hour_weighted"`
	PeakHourRequests    int64    `json:"peak_hour_requests"`
	PeakHourRatioKnown  bool     `json:"peak_hour_ratio_known"`
	PeakHourRatioPct    float64  `json:"peak_hour_ratio_percent"`
	FixedTiming         bool     `json:"fixed_timing"`
	Flagged             bool     `json:"flagged"`
	FlagReasons         []string `json:"flag_reasons" gorm:"-"`
	StrictBilling       bool     `json:"strict_billing" gorm:"-"`
	elapsedSquareMean   float64
}

type abnormalChargeAggregate struct {
	UserId           int
	Count            int64
	ReceivedCount    int64
	ForgoneQuota     int64
	PendingQuota     int64
	PendingCount     int64
	MaxPendingId     int64
	UpstreamBilled   int64
	LastAt           int64
	AvgElapsedMs     float64
	AvgElapsedSquare float64
}

type abnormalChargeHourAggregate struct {
	UserId        int
	HourBucket    int64
	Count         int64
	ReceivedCount int64
}

// AbnormalChargeThresholds are the flag rules applied to a summary.
type AbnormalChargeThresholds struct {
	HourlyCount  int
	RatioPercent int
	MinRequests  int
	ForgoneQuota int
}

// ListAbnormalChargeUsers returns users with user-caused abnormal requests
// since `since`, flagged users first, then by uncollected quota. An operator
// below root sees only users of a lower role, as with every user management
// endpoint.
func ListAbnormalChargeUsers(since int64, keyword string, operatorRole int, thresholds AbnormalChargeThresholds, startIdx, num int) ([]*AbnormalChargeUserSummary, int64, error) {
	base := DB.Model(&AbnormalCharge{}).Where("user_caused = ? AND created_at >= ?", true, since)
	keyword = strings.TrimSpace(keyword)
	if keyword != "" || operatorRole != common.RoleRootUser {
		users := DB.Unscoped().Model(&User{}).Select("id")
		if keyword != "" {
			if id, err := strconv.Atoi(keyword); err == nil && id > 0 {
				users = users.Where("id = ? OR username LIKE ?", id, "%"+keyword+"%")
			} else {
				users = users.Where("username LIKE ?", "%"+keyword+"%")
			}
		}
		if operatorRole != common.RoleRootUser {
			users = users.Where("role < ?", operatorRole)
		}
		base = base.Where("user_id IN (?)", users)
	}

	var aggregates []abnormalChargeAggregate
	err := base.Session(&gorm.Session{}).Select(
		"user_id, COUNT(*) AS count, "+
			"SUM(CASE WHEN client_received_data = ? THEN 1 ELSE 0 END) AS received_count, "+
			"SUM(forgone_quota) AS forgone_quota, "+
			"SUM(CASE WHEN recovery_id = '' THEN forgone_quota ELSE 0 END) AS pending_quota, "+
			"SUM(CASE WHEN recovery_id = '' THEN 1 ELSE 0 END) AS pending_count, "+
			"MAX(CASE WHEN recovery_id = '' THEN id ELSE 0 END) AS max_pending_id, "+
			"SUM(CASE WHEN upstream_billing = ? THEN 1 ELSE 0 END) AS upstream_billed, "+
			"MAX(created_at) AS last_at, "+
			"AVG(elapsed_ms) AS avg_elapsed_ms, "+
			"AVG(elapsed_ms * elapsed_ms) AS avg_elapsed_square",
		true, UpstreamBillingBilled,
	).Group("user_id").Scan(&aggregates).Error
	if err != nil {
		return nil, 0, err
	}
	if len(aggregates) == 0 {
		return []*AbnormalChargeUserSummary{}, 0, nil
	}

	userIds := make([]int, 0, len(aggregates))
	summaries := make(map[int]*AbnormalChargeUserSummary, len(aggregates))
	for _, aggregate := range aggregates {
		userIds = append(userIds, aggregate.UserId)
		summaries[aggregate.UserId] = &AbnormalChargeUserSummary{
			UserId:              aggregate.UserId,
			Count:               aggregate.Count,
			ReceivedCount:       aggregate.ReceivedCount,
			ForgoneQuota:        aggregate.ForgoneQuota,
			PendingQuota:        aggregate.PendingQuota,
			PendingCount:        aggregate.PendingCount,
			MaxPendingId:        aggregate.MaxPendingId,
			UpstreamBilledCount: aggregate.UpstreamBilled,
			LastAt:              aggregate.LastAt,
			AvgElapsedMs:        aggregate.AvgElapsedMs,
			elapsedSquareMean:   aggregate.AvgElapsedSquare,
		}
	}

	var hours []abnormalChargeHourAggregate
	err = base.Session(&gorm.Session{}).Select(
		"user_id, hour_bucket, COUNT(*) AS count, SUM(CASE WHEN client_received_data = ? THEN 1 ELSE 0 END) AS received_count",
		true,
	).Group("user_id, hour_bucket").Scan(&hours).Error
	if err != nil {
		return nil, 0, err
	}
	peakBuckets := make(map[int]int64, len(summaries))
	for _, hour := range hours {
		summary := summaries[hour.UserId]
		if summary == nil {
			continue
		}
		weighted := hour.Count + hour.ReceivedCount
		if weighted > summary.PeakHourWeighted {
			summary.PeakHourWeighted = weighted
			summary.PeakHourCount = hour.Count
			peakBuckets[hour.UserId] = hour.HourBucket
		}
	}

	if common.LogConsumeEnabled {
		for userId, bucket := range peakBuckets {
			requests, err := countAbnormalChargeHourRequests(userId, bucket)
			if err != nil {
				return nil, 0, err
			}
			summary := summaries[userId]
			summary.PeakHourRequests = requests
			summary.PeakHourRatioKnown = true
			if requests > 0 {
				summary.PeakHourRatioPct = math.Min(100, float64(summary.PeakHourCount)*100/float64(requests))
			}
		}
	}

	var users []User
	if err := DB.Select("id", "username").Where("id IN ?", userIds).Find(&users).Error; err != nil {
		return nil, 0, err
	}
	for _, user := range users {
		summaries[user.Id].Username = user.Username
	}

	result := make([]*AbnormalChargeUserSummary, 0, len(summaries))
	for _, summary := range summaries {
		applyAbnormalChargeFlags(summary, thresholds)
		result = append(result, summary)
	}
	// Flagged users first, then by uncollected quota.
	slices.SortFunc(result, func(a, b *AbnormalChargeUserSummary) int {
		if a.Flagged != b.Flagged {
			if a.Flagged {
				return -1
			}
			return 1
		}
		return cmp.Or(
			cmp.Compare(b.PendingQuota, a.PendingQuota),
			cmp.Compare(b.ForgoneQuota, a.ForgoneQuota),
			cmp.Compare(a.UserId, b.UserId),
		)
	})
	total := int64(len(result))
	if startIdx >= len(result) {
		return []*AbnormalChargeUserSummary{}, total, nil
	}
	return result[startIdx:min(len(result), startIdx+num)], total, nil
}

// countAbnormalChargeHourRequests counts every request of the hour: consume
// logs, which include waived requests, plus refunded requests, which leave no
// consume log.
func countAbnormalChargeHourRequests(userId int, bucket int64) (int64, error) {
	start, end := bucket*3600, bucket*3600+3600
	var consumed int64
	if err := LOG_DB.Model(&Log{}).
		Where("user_id = ? AND type = ? AND created_at >= ? AND created_at < ?", userId, LogTypeConsume, start, end).
		Count(&consumed).Error; err != nil {
		return 0, err
	}
	var refunded int64
	if err := DB.Model(&AbnormalCharge{}).
		Where("user_id = ? AND outcome = ? AND created_at >= ? AND created_at < ?", userId, AbnormalChargeOutcomeRefunded, start, end).
		Count(&refunded).Error; err != nil {
		return 0, err
	}
	return consumed + refunded, nil
}

func applyAbnormalChargeFlags(summary *AbnormalChargeUserSummary, thresholds AbnormalChargeThresholds) {
	if summary.Count >= 5 {
		variance := summary.elapsedSquareMean - summary.AvgElapsedMs*summary.AvgElapsedMs
		summary.ElapsedStddevMs = math.Sqrt(math.Max(variance, 0))
		summary.FixedTiming = summary.AvgElapsedMs > 0 && summary.ElapsedStddevMs <= summary.AvgElapsedMs*0.1
	}
	reasons := []string{}
	if thresholds.HourlyCount > 0 && summary.PeakHourWeighted >= int64(thresholds.HourlyCount) {
		reasons = append(reasons, "hourly_count")
	}
	if summary.PeakHourRatioKnown && thresholds.MinRequests > 0 && summary.PeakHourRequests >= int64(thresholds.MinRequests) &&
		summary.PeakHourRatioPct >= float64(thresholds.RatioPercent) {
		reasons = append(reasons, "ratio")
	}
	if thresholds.ForgoneQuota > 0 && summary.ForgoneQuota >= int64(thresholds.ForgoneQuota) {
		reasons = append(reasons, "forgone_quota")
	}
	summary.FlagReasons = reasons
	summary.Flagged = len(reasons) >= 2
}

// ListAbnormalCharges returns one user's records since `since`, newest first.
// status is "pending", "recovered", or empty for both.
func ListAbnormalCharges(userId int, since int64, status string, startIdx, num int) ([]*AbnormalCharge, int64, error) {
	query := DB.Model(&AbnormalCharge{}).Where("user_id = ? AND created_at >= ?", userId, since)
	switch status {
	case "pending":
		query = query.Where("recovery_id = ''")
	case "recovered":
		query = query.Where("recovery_id <> ''")
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	records := []*AbnormalCharge{}
	err := query.Order("id desc").Offset(startIdx).Limit(num).Find(&records).Error
	return records, total, err
}

var (
	ErrNoAbnormalChargeToRecover = errors.New("no abnormal charge to recover")
	ErrAbnormalChargePermission  = errors.New("no permission to recover charges of this user")
)

// AbnormalChargeRecovery is the result of one committed recovery.
type AbnormalChargeRecovery struct {
	RecoveryId string
	UserId     int
	Username   string
	Count      int64
	Quota      int64
	Before     int
	After      int
}

// RecoverAbnormalCharges charges a user the uncollected quota of the given
// pending user-caused records, or of all of them since `since` when ids is
// empty; a positive maxId leaves out records newer than the ones the operator
// reviewed. The records are claimed and the wallet debited in one transaction,
// so each record is recovered at most once even under concurrent calls. The
// balance may become negative.
func RecoverAbnormalCharges(userId, operatorId, operatorRole int, ids []int64, since, maxId int64) (*AbnormalChargeRecovery, error) {
	if userId <= 0 {
		return nil, ErrNoAbnormalChargeToRecover
	}
	recovery := &AbnormalChargeRecovery{RecoveryId: common.GetUUID(), UserId: userId}
	err := withWalletTransaction(userId, func(tx *gorm.DB) error {
		var user User
		if err := tx.Select("id", "username", "role", "quota").First(&user, userId).Error; err != nil {
			return err
		}
		if operatorRole != common.RoleRootUser && operatorRole <= user.Role {
			return ErrAbnormalChargePermission
		}
		claim := tx.Model(&AbnormalCharge{}).
			Where("user_id = ? AND user_caused = ? AND recovery_id = ''", userId, true)
		if len(ids) > 0 {
			claim = claim.Where("id IN ?", ids)
		} else {
			claim = claim.Where("created_at >= ?", since)
		}
		if maxId > 0 {
			claim = claim.Where("id <= ?", maxId)
		}
		claimed := claim.Updates(map[string]any{
			"recovery_id":  recovery.RecoveryId,
			"recovered_at": common.GetTimestamp(),
			"recovered_by": operatorId,
		})
		if claimed.Error != nil {
			return claimed.Error
		}
		if claimed.RowsAffected == 0 {
			return ErrNoAbnormalChargeToRecover
		}
		var totals struct {
			Count int64
			Quota int64
		}
		if err := tx.Model(&AbnormalCharge{}).Where("recovery_id = ?", recovery.RecoveryId).
			Select("COUNT(*) AS count, COALESCE(SUM(forgone_quota), 0) AS quota").Scan(&totals).Error; err != nil {
			return err
		}
		if totals.Quota <= 0 {
			return ErrNoAbnormalChargeToRecover
		}
		if totals.Quota > walletQuotaLimit || int64(user.Quota)-totals.Quota < -walletQuotaLimit {
			return ErrWalletQuotaLimitExceeded
		}
		if err := tx.Model(&User{}).Where("id = ?", userId).
			Update("quota", gorm.Expr("quota - ?", totals.Quota)).Error; err != nil {
			return err
		}
		recovery.Username = user.Username
		recovery.Count = totals.Count
		recovery.Quota = totals.Quota
		recovery.Before = user.Quota
		recovery.After = user.Quota - int(totals.Quota)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return recovery, nil
}

// SetStrictBillingUser turns strict billing on or off for one user. The list
// is read from its locked option row, so a change made through another node
// is never overwritten with a stale copy. It reports whether the list
// changed.
func SetStrictBillingUser(userId int, enabled bool) (bool, error) {
	key := billing_setting.StrictBillingUsersOptionKey
	var value string
	changed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		option := Option{Key: key}
		if err := tx.Where(Option{Key: key}).Attrs(Option{Value: "[]"}).FirstOrCreate(&option).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).Where(Option{Key: key}).First(&option).Error; err != nil {
			return err
		}
		ids, err := billing_setting.ParseStrictBillingUsers(option.Value)
		if err != nil {
			return err
		}
		index, found := slices.BinarySearch(ids, userId)
		switch {
		case enabled && !found:
			ids = slices.Insert(ids, index, userId)
		case !enabled && found:
			ids = slices.Delete(ids, index, index+1)
		default:
			value = option.Value
			return nil
		}
		changed = true
		encoded, err := common.Marshal(ids)
		if err != nil {
			return err
		}
		value = string(encoded)
		return tx.Model(&Option{}).Where(Option{Key: key}).Update("value", value).Error
	})
	if err != nil {
		return false, err
	}
	return changed, updateOptionMap(key, value)
}
