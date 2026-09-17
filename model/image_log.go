package model

import (
	"context"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// ImageLog records one AI image generation (OpenAI-compatible or native Gemini)
// so the drawing log can show it next to Midjourney tasks.
//
// It is a separate table on purpose: the drawing log needs the generated image
// links, the prompt and the producing model in a queryable shape, which the
// consume log's "other" JSON blob cannot provide across SQLite, MySQL and
// PostgreSQL. Rows expire on their own; see DeleteExpiredImageLogs.
type ImageLog struct {
	Id        int    `json:"id" gorm:"primaryKey"`
	UserId    int    `json:"user_id" gorm:"index"`
	Username  string `json:"username" gorm:"index;default:''"`
	ChannelId int    `json:"channel_id" gorm:"index"`
	ModelName string `json:"model_name" gorm:"index;default:''"`
	Prompt    string `json:"prompt" gorm:"type:text"`
	// ImageURLs stores the desensitized public links exactly as the image
	// middleware reported them. Image payloads are never stored here.
	ImageURLs string `json:"image_urls" gorm:"type:text"`
	Quota     int    `json:"quota" gorm:"default:0"`
	UseTime   int    `json:"use_time" gorm:"default:0"`
	IsStream  bool   `json:"is_stream"`
	// RequestId links the row to the consume log of the same request.
	RequestId string `json:"request_id" gorm:"type:varchar(64);index;default:''"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index:idx_image_logs_created_at"`
}

// RecordImageLogParams describes one AI image generation to persist.
type RecordImageLogParams struct {
	UserId    int
	Username  string
	ChannelId int
	ModelName string
	Prompt    string
	ImageURLs []string
	Quota     int
	UseTime   int
	IsStream  bool
	RequestId string
}

// RecordImageLog stores one AI image generation. It is a no-op when there is no
// link to point at, so a request whose upstream never reported a stored image
// leaves no empty drawing log row behind.
func RecordImageLog(params RecordImageLogParams) {
	if len(params.ImageURLs) == 0 {
		return
	}
	links, err := common.Marshal(params.ImageURLs)
	if err != nil {
		common.SysError("failed to encode image log links: " + err.Error())
		return
	}
	row := &ImageLog{
		UserId:    params.UserId,
		Username:  params.Username,
		ChannelId: params.ChannelId,
		ModelName: params.ModelName,
		Prompt:    params.Prompt,
		ImageURLs: string(links),
		Quota:     params.Quota,
		UseTime:   params.UseTime,
		IsStream:  params.IsStream,
		RequestId: params.RequestId,
		CreatedAt: common.GetTimestamp(),
	}
	if err := DB.Create(row).Error; err != nil {
		common.SysError("failed to record image log: " + err.Error())
	}
}

// ImageLogQueryParams filters AI image logs. Timestamps are seconds.
type ImageLogQueryParams struct {
	// UserId is 0 for the admin view (all users) and the owner id otherwise.
	UserId         int
	ChannelId      string
	Filter         string
	StartTimestamp int64
	EndTimestamp   int64
}

func imageLogQuery(params ImageLogQueryParams, tx *gorm.DB) *gorm.DB {
	if params.UserId != 0 {
		tx = tx.Where("user_id = ?", params.UserId)
	}
	if params.ChannelId != "" {
		tx = tx.Where("channel_id = ?", params.ChannelId)
	}
	if params.Filter != "" {
		// The drawing log displays the request id as the task id of an AI row,
		// so the shared "task ID or model" filter must match either one.
		tx = tx.Where("model_name = ? OR request_id = ?", params.Filter, params.Filter)
	}
	if params.StartTimestamp > 0 {
		tx = tx.Where("created_at >= ?", params.StartTimestamp)
	}
	if params.EndTimestamp > 0 {
		tx = tx.Where("created_at <= ?", params.EndTimestamp)
	}
	return tx
}

// CountImageLogs returns the number of AI image logs matching the filter.
func CountImageLogs(params ImageLogQueryParams) int64 {
	var total int64
	if err := imageLogQuery(params, DB.Model(&ImageLog{})).Count(&total).Error; err != nil {
		common.SysError("failed to count image logs: " + err.Error())
		return 0
	}
	return total
}

// GetImageLogs returns one newest-first page of AI image logs.
func GetImageLogs(params ImageLogQueryParams, startIdx int, num int) ([]*ImageLog, error) {
	if num <= 0 {
		return nil, nil
	}
	var rows []*ImageLog
	err := imageLogQuery(params, DB.Model(&ImageLog{})).
		Order("created_at desc, id desc").
		Limit(num).
		Offset(startIdx).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// ImageLogLinks decodes the stored public links. A corrupt value yields no
// links instead of hiding the whole drawing log row.
func (row *ImageLog) ImageLogLinks() []string {
	if row == nil || strings.TrimSpace(row.ImageURLs) == "" {
		return nil
	}
	var links []string
	if err := common.Unmarshal([]byte(row.ImageURLs), &links); err != nil {
		return nil
	}
	return links
}

// DeleteExpiredImageLogs removes image logs created before the cutoff, oldest
// first, in bounded batches, and reports how many rows were deleted.
//
// Image links are only valid for a short window, so keeping the rows would grow
// the table without leaving any usable link behind.
func DeleteExpiredImageLogs(ctx context.Context, cutoffTimestamp int64, batchSize int) (int64, error) {
	if cutoffTimestamp <= 0 {
		return 0, nil
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	// The bound is applied to a SELECT, not to the DELETE: MySQL honours
	// "DELETE ... LIMIT" but the SQLite and PostgreSQL dialectors silently drop
	// the clause, which would turn one sweep into an unbounded delete. Selecting
	// the oldest ids first keeps every statement bounded on all three databases
	// and makes the delete order match the oldest-first contract.
	var expiredIds []int64
	if err := DB.WithContext(ctx).
		Model(&ImageLog{}).
		Where("created_at < ?", cutoffTimestamp).
		Order("created_at asc, id asc").
		Limit(batchSize).
		Pluck("id", &expiredIds).
		Error; err != nil {
		return 0, err
	}
	if len(expiredIds) == 0 {
		return 0, nil
	}
	result := DB.WithContext(ctx).Where("id IN ?", expiredIds).Delete(&ImageLog{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// CountExpiredImageLogs counts image logs older than the cutoff.
func CountExpiredImageLogs(ctx context.Context, cutoffTimestamp int64) (int64, error) {
	if cutoffTimestamp <= 0 {
		return 0, nil
	}
	var total int64
	if err := DB.WithContext(ctx).
		Model(&ImageLog{}).
		Where("created_at < ?", cutoffTimestamp).
		Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

// ImageLogRetentionCutoff returns the oldest creation time an image log may
// have. A non-positive retention window disables expiry.
func ImageLogRetentionCutoff(retention time.Duration) int64 {
	if retention <= 0 {
		return 0
	}
	return time.Now().Add(-retention).Unix()
}
