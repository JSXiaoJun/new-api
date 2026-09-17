package model

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertImageLog(t *testing.T, row *ImageLog) {
	t.Helper()
	require.NoError(t, DB.Create(row).Error)
}

func insertMidjourney(t *testing.T, task *Midjourney) {
	t.Helper()
	require.NoError(t, DB.Create(task).Error)
}

// TestGetDrawingLogsMergesBothSourcesNewestFirst protects the drawing log
// contract the UI depends on: AI image generations and Midjourney tasks share
// one page ordered by submit time, including across pages.
func TestGetDrawingLogsMergesBothSourcesNewestFirst(t *testing.T) {
	truncateTables(t)

	now := time.Now().Unix()
	insertMidjourney(t, &Midjourney{
		UserId:     7,
		MjId:       "mj-1",
		Action:     "IMAGINE",
		Status:     "SUCCESS",
		SubmitTime: (now - 10) * 1000,
		ImageUrl:   "https://cdn.example/mj-1.png",
		Prompt:     "midjourney prompt",
	})
	insertImageLog(t, &ImageLog{
		UserId:    7,
		ChannelId: 3,
		ModelName: "gpt-image-2",
		ImageURLs: `["https://video-admin.example/public/images/assets/a1"]`,
		Prompt:    "ai prompt",
		CreatedAt: now - 5,
	})

	firstPage, total, err := GetDrawingLogs(DrawingLogQueryParams{UserId: 7}, 0, 1)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, firstPage, 1)
	require.Equal(t, DrawingLogSourceImage, firstPage[0].Source)
	require.Equal(t, []string{"https://video-admin.example/public/images/assets/a1"}, firstPage[0].ImageUrls)

	secondPage, _, err := GetDrawingLogs(DrawingLogQueryParams{UserId: 7}, 1, 1)
	require.NoError(t, err)
	require.Len(t, secondPage, 1)
	assert.Equal(t, DrawingLogSourceMidjourney, secondPage[0].Source)
	assert.Equal(t, "mj-1", secondPage[0].MjId)
	assert.Equal(t, []string{"https://cdn.example/mj-1.png"}, secondPage[0].ImageUrls)
}

// TestDrawingLogsScopedPerUser protects the self view: a user must not see
// another user's rows from either source.
func TestDrawingLogsScopedPerUser(t *testing.T) {
	truncateTables(t)

	now := time.Now().Unix()
	insertImageLog(t, &ImageLog{UserId: 1, ModelName: "gpt-image-2", ImageURLs: `["a"]`, CreatedAt: now})
	insertImageLog(t, &ImageLog{UserId: 2, ModelName: "gpt-image-2", ImageURLs: `["b"]`, CreatedAt: now})
	insertMidjourney(t, &Midjourney{UserId: 2, MjId: "mj-2", SubmitTime: now * 1000})

	rows, total, err := GetDrawingLogs(DrawingLogQueryParams{UserId: 1}, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].UserId)
}

// TestGetDrawingLogsFiltersByTimeWindow protects the date-range filter, which
// reaches both sources through different timestamp units.
func TestGetDrawingLogsFiltersByTimeWindow(t *testing.T) {
	truncateTables(t)

	now := time.Now().Unix()
	insertImageLog(t, &ImageLog{UserId: 5, ModelName: "gpt-image-2", ImageURLs: `["a"]`, CreatedAt: now - 3600})
	insertImageLog(t, &ImageLog{UserId: 5, ModelName: "gpt-image-2", ImageURLs: `["b"]`, CreatedAt: now})
	insertMidjourney(t, &Midjourney{UserId: 5, MjId: "mj-old", SubmitTime: (now - 7200) * 1000})
	insertMidjourney(t, &Midjourney{UserId: 5, MjId: "mj-new", SubmitTime: now * 1000})

	rows, total, err := GetDrawingLogs(DrawingLogQueryParams{
		UserId:         5,
		StartTimestamp: now - 60,
		EndTimestamp:   now + 60,
	}, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, rows, 2)
}

// TestRecordImageLogSkipsEmptyLinks protects the table from growing on requests
// that produced no stored image.
func TestRecordImageLogSkipsEmptyLinks(t *testing.T) {
	truncateTables(t)

	RecordImageLog(RecordImageLogParams{UserId: 1, ModelName: "gpt-image-2"})
	assert.Equal(t, int64(0), CountImageLogs(ImageLogQueryParams{}))

	RecordImageLog(RecordImageLogParams{
		UserId:    1,
		Username:  "tester",
		ChannelId: 9,
		ModelName: "gpt-image-2",
		Prompt:    "a cat",
		ImageURLs: []string{"https://video-admin.example/public/images/assets/a1"},
		Quota:     120,
		RequestId: "req-1",
	})

	rows, err := GetImageLogs(ImageLogQueryParams{UserId: 1}, 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, []string{"https://video-admin.example/public/images/assets/a1"}, rows[0].ImageLogLinks())
	assert.Equal(t, "a cat", rows[0].Prompt)
	assert.Equal(t, "req-1", rows[0].RequestId)
}

// TestDeleteExpiredImageLogsHonoursBatchBound protects the retention sweep
// contract: one call must delete at most batchSize of the oldest expired rows,
// and repeated calls must drain them without ever touching newer rows.
//
// The bound is asserted per call on purpose. Applying it to the DELETE itself
// would not hold: MySQL honours "DELETE ... LIMIT" but the SQLite and
// PostgreSQL dialectors silently drop the clause, so a regression that moves the
// bound back onto the delete would still drain everything and pass a test that
// only summed the deletions.
func TestDeleteExpiredImageLogsHonoursBatchBound(t *testing.T) {
	truncateTables(t)

	now := time.Now().Unix()
	oldestIds := make([]int64, 0, 5)
	for i := int64(0); i < 5; i++ {
		row := &ImageLog{
			UserId:    1,
			ModelName: "gpt-image-2",
			ImageURLs: `["a"]`,
			CreatedAt: now - 1000 + i,
		}
		insertImageLog(t, row)
		oldestIds = append(oldestIds, int64(row.Id))
	}
	insertImageLog(t, &ImageLog{UserId: 1, ModelName: "gpt-image-2", ImageURLs: `["b"]`, CreatedAt: now})

	cutoff := now - 500
	pending, err := CountExpiredImageLogs(context.Background(), cutoff)
	require.NoError(t, err)
	require.Equal(t, int64(5), pending)

	firstBatch, err := DeleteExpiredImageLogs(context.Background(), cutoff, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(2), firstBatch, "one sweep must be bounded by batchSize")

	var remaining []int64
	require.NoError(t, DB.Model(&ImageLog{}).Order("id asc").Pluck("id", &remaining).Error)
	// Only the newest row and the three youngest expired rows may survive: the
	// sweep deleted the two oldest, not an arbitrary pair.
	assert.Equal(t, oldestIds[2:], remaining[:3], "the oldest rows must be deleted first")

	lastBatch, err := DeleteExpiredImageLogs(context.Background(), cutoff, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(2), lastBatch)

	tailBatch, err := DeleteExpiredImageLogs(context.Background(), cutoff, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(1), tailBatch)

	drained, err := DeleteExpiredImageLogs(context.Background(), cutoff, 2)
	require.NoError(t, err)
	assert.Zero(t, drained, "a drained sweep deletes nothing")

	assert.Equal(t, int64(1), CountImageLogs(ImageLogQueryParams{}))
	leftover, err := CountExpiredImageLogs(context.Background(), cutoff)
	require.NoError(t, err)
	assert.Zero(t, leftover)
}

// TestCountImageLogsFilterMatchesTaskIdOrModel protects the shared drawing log
// filter: the table shows an AI row's request id as its task id, so filtering by
// either that id or the model name must find the row.
func TestCountImageLogsFilterMatchesTaskIdOrModel(t *testing.T) {
	truncateTables(t)

	insertImageLog(t, &ImageLog{
		UserId:    1,
		ModelName: "gpt-image-2",
		RequestId: "req-abc",
		ImageURLs: `["a"]`,
		CreatedAt: time.Now().Unix(),
	})
	insertImageLog(t, &ImageLog{
		UserId:    1,
		ModelName: "grok-imagine-image-2.0",
		RequestId: "req-xyz",
		ImageURLs: `["b"]`,
		CreatedAt: time.Now().Unix(),
	})

	assert.Equal(t, int64(1), CountImageLogs(ImageLogQueryParams{Filter: "gpt-image-2"}))
	assert.Equal(t, int64(1), CountImageLogs(ImageLogQueryParams{Filter: "req-xyz"}))
	assert.Equal(t, int64(0), CountImageLogs(ImageLogQueryParams{Filter: "req-nope"}))
	assert.Equal(t, int64(2), CountImageLogs(ImageLogQueryParams{}))

	rows, err := GetImageLogs(ImageLogQueryParams{Filter: "req-abc"}, 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "gpt-image-2", rows[0].ModelName)
}

// TestImageLogLinksTolerateCorruptPayload protects the drawing log from a
// malformed stored value: the row still renders, only without previews.
func TestImageLogLinksTolerateCorruptPayload(t *testing.T) {
	row := &ImageLog{ImageURLs: "not-json"}
	assert.Empty(t, row.ImageLogLinks())
	assert.Nil(t, (*ImageLog)(nil).ImageLogLinks())
	assert.Empty(t, (&ImageLog{}).ImageLogLinks())
}

// TestImageLogRetentionCutoff protects the retention window mapping.
func TestImageLogRetentionCutoff(t *testing.T) {
	assert.Zero(t, ImageLogRetentionCutoff(0))
	assert.Zero(t, ImageLogRetentionCutoff(-time.Hour))

	cutoff := ImageLogRetentionCutoff(24 * time.Hour)
	assert.InDelta(t, time.Now().Add(-24*time.Hour).Unix(), cutoff, 5)
}
