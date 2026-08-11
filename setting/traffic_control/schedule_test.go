package traffic_control

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useTrafficSchedule(t *testing.T, manualEnabled bool, schedule TrafficSchedule) {
	t.Helper()
	originalManualEnabled := trafficControlSetting.MainlandWebBlockEnabled
	originalSchedule := trafficControlSetting.Schedule
	trafficControlSetting.MainlandWebBlockEnabled = manualEnabled
	trafficControlSetting.Schedule = schedule
	UpdateAndSync()
	t.Cleanup(func() {
		trafficControlSetting.MainlandWebBlockEnabled = originalManualEnabled
		trafficControlSetting.Schedule = originalSchedule
		UpdateAndSync()
	})
}

func TestDisabledScheduleUsesManualSwitch(t *testing.T) {
	useTrafficSchedule(t, true, defaultTrafficSchedule())
	assert.True(t, mainlandWebBlockEnabledAt(time.Now()))
}

func TestDailyScheduleOverridesManualSwitchAndSupportsMultipleRanges(t *testing.T) {
	useTrafficSchedule(t, true, TrafficSchedule{
		Enabled:     true,
		DailyRepeat: true,
		TimeRanges: []TrafficTimeRange{
			{StartTime: "08:00", EndTime: "10:00"},
			{StartTime: "22:00", EndTime: "06:00"},
		},
		Timezone: "Asia/Shanghai",
	})

	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	assert.True(t, mainlandWebBlockEnabledAt(time.Date(2026, 8, 11, 8, 30, 0, 0, location)))
	assert.True(t, mainlandWebBlockEnabledAt(time.Date(2026, 8, 11, 23, 30, 0, 0, location)))
	assert.True(t, mainlandWebBlockEnabledAt(time.Date(2026, 8, 12, 5, 59, 0, 0, location)))
	assert.False(t, mainlandWebBlockEnabledAt(time.Date(2026, 8, 11, 12, 0, 0, 0, location)))
	assert.False(t, mainlandWebBlockEnabledAt(time.Date(2026, 8, 12, 6, 0, 0, 0, location)))
}

func TestOneTimeScheduleRunsOnlyOnSelectedDate(t *testing.T) {
	schedule := TrafficSchedule{
		Enabled:     true,
		DailyRepeat: false,
		Date:        "2026-08-11",
		TimeRanges: []TrafficTimeRange{
			{StartTime: "22:00", EndTime: "02:00"},
		},
		Timezone: "Asia/Shanghai",
	}
	useTrafficSchedule(t, false, schedule)

	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	assert.False(t, mainlandWebBlockEnabledAt(time.Date(2026, 8, 11, 21, 59, 0, 0, location)))
	assert.True(t, mainlandWebBlockEnabledAt(time.Date(2026, 8, 11, 22, 0, 0, 0, location)))
	assert.True(t, mainlandWebBlockEnabledAt(time.Date(2026, 8, 12, 1, 59, 0, 0, location)))
	assert.False(t, mainlandWebBlockEnabledAt(time.Date(2026, 8, 12, 2, 0, 0, 0, location)))
	assert.False(t, mainlandWebBlockEnabledAt(time.Date(2026, 8, 12, 22, 0, 0, 0, location)))
}

func TestOneTimeScheduleUsesWallClockTimesAcrossDST(t *testing.T) {
	useTrafficSchedule(t, false, TrafficSchedule{
		Enabled:     true,
		DailyRepeat: false,
		Date:        "2026-03-08",
		TimeRanges: []TrafficTimeRange{
			{StartTime: "01:00", EndTime: "04:00"},
		},
		Timezone: "America/New_York",
	})

	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	assert.True(t, mainlandWebBlockEnabledAt(time.Date(2026, 3, 8, 3, 30, 0, 0, location)))
	assert.False(t, mainlandWebBlockEnabledAt(time.Date(2026, 3, 8, 4, 0, 0, 0, location)))
}

func TestValidateScheduleJSONRejectsInvalidEnabledSchedules(t *testing.T) {
	testCases := []string{
		`{"enabled":true,"daily_repeat":true,"time_ranges":[],"timezone":"Asia/Shanghai"}`,
		`{"enabled":true,"daily_repeat":true,"time_ranges":[{"start_time":"09:00","end_time":"09:00"}],"timezone":"Asia/Shanghai"}`,
		`{"enabled":true,"daily_repeat":true,"time_ranges":[{"start_time":"bad","end_time":"10:00"}],"timezone":"Asia/Shanghai"}`,
		`{"enabled":true,"daily_repeat":false,"date":"","time_ranges":[{"start_time":"09:00","end_time":"10:00"}],"timezone":"Asia/Shanghai"}`,
		`{"enabled":true,"daily_repeat":true,"time_ranges":[{"start_time":"09:00","end_time":"10:00"}],"timezone":"Invalid/Timezone"}`,
	}

	for _, value := range testCases {
		assert.Error(t, ValidateScheduleJSON(value), value)
	}
}

func TestValidateScheduleJSONAcceptsMultipleDailyRanges(t *testing.T) {
	value := `{"enabled":true,"daily_repeat":true,"time_ranges":[{"start_time":"08:00","end_time":"10:00"},{"start_time":"22:00","end_time":"06:00"}],"timezone":"Asia/Shanghai"}`
	require.NoError(t, ValidateScheduleJSON(value))
}
