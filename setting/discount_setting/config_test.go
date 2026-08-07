package discount_setting

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useSchedule(t *testing.T, schedule DiscountSchedule) {
	t.Helper()
	original := setting.Schedule
	setting.Schedule = schedule
	UpdateAndSync()
	t.Cleanup(func() {
		setting.Schedule = original
		UpdateAndSync()
	})
}

func TestRatioForOneTimeScheduleOnlyDiscountsSelectedGroupsInWindow(t *testing.T) {
	useSchedule(t, DiscountSchedule{
		Enabled:  true,
		Ratio:    0.8,
		Groups:   []string{"vip"},
		StartAt:  "2026-08-08T00:00",
		EndAt:    "2026-08-09T00:00",
		Timezone: "Asia/Shanghai",
	})

	inWindow := time.Date(2026, 8, 8, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	assert.Equal(t, 0.8, RatioFor("vip", inWindow))
	assert.Equal(t, 1.0, RatioFor("default", inWindow))
	assert.Equal(t, 1.0, RatioFor("vip", inWindow.Add(24*time.Hour)))
}

func TestRatioForDailyScheduleSupportsCrossMidnightWindow(t *testing.T) {
	useSchedule(t, DiscountSchedule{
		Enabled:     true,
		Ratio:       0.7,
		Groups:      []string{"default"},
		DailyRepeat: true,
		StartTime:   "22:00",
		EndTime:     "06:00",
		Timezone:    "Asia/Shanghai",
	})

	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	assert.Equal(t, 0.7, RatioFor("default", time.Date(2026, 8, 8, 23, 0, 0, 0, location)))
	assert.Equal(t, 0.7, RatioFor("default", time.Date(2026, 8, 9, 5, 59, 0, 0, location)))
	assert.Equal(t, 1.0, RatioFor("default", time.Date(2026, 8, 9, 6, 0, 0, 0, location)))
	assert.Equal(t, 1.0, RatioFor("default", time.Date(2026, 8, 9, 12, 0, 0, 0, location)))
}

func TestValidateScheduleJSONRejectsUnsafeOrIncompleteEnabledSchedules(t *testing.T) {
	tests := []string{
		`{"enabled":true,"ratio":-0.1,"groups":["vip"],"start_at":"2026-08-08T00:00","end_at":"2026-08-09T00:00","timezone":"Asia/Shanghai"}`,
		`{"enabled":true,"ratio":0,"groups":["vip"],"start_at":"2026-08-08T00:00","end_at":"2026-08-09T00:00","timezone":"Asia/Shanghai"}`,
		`{"enabled":true,"ratio":0.009,"groups":["vip"],"start_at":"2026-08-08T00:00","end_at":"2026-08-09T00:00","timezone":"Asia/Shanghai"}`,
		`{"enabled":true,"ratio":1.01,"groups":["vip"],"start_at":"2026-08-08T00:00","end_at":"2026-08-09T00:00","timezone":"Asia/Shanghai"}`,
		`{"enabled":true,"ratio":0.8,"groups":[],"start_at":"2026-08-08T00:00","end_at":"2026-08-09T00:00","timezone":"Asia/Shanghai"}`,
		`{"enabled":true,"ratio":0.8,"groups":["vip"],"daily_repeat":true,"start_time":"22:00","end_time":"22:00","timezone":"Asia/Shanghai"}`,
		`{"enabled":true,"ratio":0.8,"groups":["vip"],"start_at":"2026-08-09T00:00","end_at":"2026-08-08T00:00","timezone":"Asia/Shanghai"}`,
		`{"enabled":true,"ratio":0.8,"groups":["vip"],"start_at":"2026-08-08T00:00","end_at":"2026-08-09T00:00","timezone":"Invalid/Timezone"}`,
	}
	for _, value := range tests {
		assert.Error(t, ValidateScheduleJSON(value))
	}
}

func TestValidateScheduleJSONAcceptsMinimumDiscountRatio(t *testing.T) {
	value := `{"enabled":true,"ratio":0.01,"groups":["vip"],"start_at":"2026-08-08T00:00","end_at":"2026-08-09T00:00","timezone":"Asia/Shanghai"}`
	require.NoError(t, ValidateScheduleJSON(value))
}
