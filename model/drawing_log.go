package model

import (
	"sort"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// DrawingLog is one merged drawing log row.
//
// Midjourney tasks (table midjourneys) and AI image generations (table
// image_logs) are stored separately but shown as one log. The shared shape
// carries the fields the drawing log displays for both; the source-specific
// fields stay empty for the source that has no value for them.
type DrawingLog struct {
	Source string `json:"source"`
	// SubmitTime is milliseconds since the epoch, matching the Midjourney
	// unit so a single column can display both sources.
	SubmitTime int64    `json:"submit_time"`
	FinishTime int64    `json:"finish_time"`
	UserId     int      `json:"user_id"`
	Username   string   `json:"username"`
	ChannelId  int      `json:"channel_id"`
	ModelName  string   `json:"model_name"`
	Action     string   `json:"action"`
	MjId       string   `json:"mj_id"`
	Prompt     string   `json:"prompt"`
	Status     string   `json:"status"`
	Progress   string   `json:"progress"`
	Code       int      `json:"code"`
	ImageUrls  []string `json:"image_urls"`
	Quota      int      `json:"quota"`
	UseTime    int      `json:"use_time"`
	IsStream   bool     `json:"is_stream"`
	RequestId  string   `json:"request_id"`
	FailReason string   `json:"fail_reason"`
}

const (
	DrawingLogSourceMidjourney = "midjourney"
	DrawingLogSourceImage      = "image"
)

// DrawingLogQueryParams filters the merged drawing log. Timestamps are seconds,
// because that is what the AI image log stores; Midjourney submit_time is
// converted at the query site.
type DrawingLogQueryParams struct {
	// UserId is 0 for the admin view (all users) and the owner id otherwise.
	UserId         int
	ChannelId      string
	Filter         string
	StartTimestamp int64
	EndTimestamp   int64
}

// CountDrawingLogs returns the merged drawing log size for pagination.
func CountDrawingLogs(params DrawingLogQueryParams) int64 {
	return CountImageLogs(ImageLogQueryParams(params)) + CountMidjourneyLogs(params)
}

// CountMidjourneyLogs counts the Midjourney drawing tasks matching the filter.
func CountMidjourneyLogs(params DrawingLogQueryParams) int64 {
	var total int64
	if err := midjourneyDrawingLogQuery(params).Count(&total).Error; err != nil {
		common.SysError("failed to count drawing log tasks: " + err.Error())
		return 0
	}
	return total
}

func midjourneyDrawingLogQuery(params DrawingLogQueryParams) *gorm.DB {
	query := DB.Model(&Midjourney{})
	if params.UserId != 0 {
		query = query.Where("user_id = ?", params.UserId)
	}
	if params.ChannelId != "" {
		query = query.Where("channel_id = ?", params.ChannelId)
	}
	if params.Filter != "" {
		query = query.Where("mj_id = ?", params.Filter)
	}
	if params.StartTimestamp > 0 {
		query = query.Where("submit_time >= ?", params.StartTimestamp*1000)
	}
	if params.EndTimestamp > 0 {
		query = query.Where("submit_time <= ?", params.EndTimestamp*1000)
	}
	return query
}

// GetDrawingLogs merges AI image generations and Midjourney tasks into one
// newest-first page.
//
// Both sources are read with the same limit (offset+page size) and then merged,
// so the page is exact as long as it falls inside the newest offset+page size
// rows of each source. Beyond that a source runs out of rows it was asked for,
// which is the normal behaviour of a merged view over two tables: the requested
// rows exist in the union, and the merge order between the two sources beyond
// the fetched window is still by timestamp.
func GetDrawingLogs(params DrawingLogQueryParams, startIdx int, num int) ([]*DrawingLog, int64, error) {
	total := CountDrawingLogs(params)
	if num <= 0 {
		return []*DrawingLog{}, total, nil
	}
	fetchLimit := startIdx + num

	imageRows, err := GetImageLogs(ImageLogQueryParams(params), 0, fetchLimit)
	if err != nil {
		return nil, 0, err
	}

	var mjTasks []*Midjourney
	if err := midjourneyDrawingLogQuery(params).
		Order("submit_time desc, id desc").
		Limit(fetchLimit).
		Find(&mjTasks).Error; err != nil {
		return nil, 0, err
	}

	merged := make([]*DrawingLog, 0, len(imageRows)+len(mjTasks))
	for _, row := range imageRows {
		merged = append(merged, imageLogDrawingLog(row))
	}
	for _, task := range mjTasks {
		merged = append(merged, midjourneyDrawingLog(task))
	}
	sort.SliceStable(merged, func(i int, j int) bool {
		return merged[i].SubmitTime > merged[j].SubmitTime
	})

	if startIdx >= len(merged) {
		return []*DrawingLog{}, total, nil
	}
	merged = merged[startIdx:]
	if len(merged) > num {
		merged = merged[:num]
	}
	return merged, total, nil
}

func imageLogDrawingLog(row *ImageLog) *DrawingLog {
	return &DrawingLog{
		Source:     DrawingLogSourceImage,
		SubmitTime: row.CreatedAt * 1000,
		UserId:     row.UserId,
		Username:   row.Username,
		ChannelId:  row.ChannelId,
		ModelName:  row.ModelName,
		Action:     "IMAGINE",
		Status:     "SUCCESS",
		// The shared progress column reads a percentage; an AI image row is
		// written only after the image was stored, so it is always complete.
		Progress:  "100%",
		ImageUrls: row.ImageLogLinks(),
		Quota:     row.Quota,
		UseTime:   row.UseTime,
		IsStream:  row.IsStream,
		RequestId: row.RequestId,
		Prompt:    row.Prompt,
	}
}

func midjourneyDrawingLog(task *Midjourney) *DrawingLog {
	urls := make([]string, 0, 1)
	if task.ImageUrl != "" {
		urls = append(urls, task.ImageUrl)
	}
	return &DrawingLog{
		Source:     DrawingLogSourceMidjourney,
		SubmitTime: task.SubmitTime,
		FinishTime: task.FinishTime,
		UserId:     task.UserId,
		ChannelId:  task.ChannelId,
		ModelName:  task.Action,
		Action:     task.Action,
		MjId:       task.MjId,
		Prompt:     task.Prompt,
		Status:     task.Status,
		Progress:   task.Progress,
		Code:       task.Code,
		ImageUrls:  urls,
		Quota:      task.Quota,
		FailReason: task.FailReason,
	}
}
