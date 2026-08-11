package traffic_control

import (
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	defaultScheduleTimezone = "Asia/Shanghai"
	dailyTimeLayout         = "15:04"
	oneTimeDateLayout       = "2006-01-02"
	maxScheduleTimeRanges   = 24
)

type TrafficTimeRange struct {
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

type TrafficSchedule struct {
	Enabled     bool               `json:"enabled"`
	DailyRepeat bool               `json:"daily_repeat"`
	Date        string             `json:"date"`
	TimeRanges  []TrafficTimeRange `json:"time_ranges"`
	Timezone    string             `json:"timezone"`
}

type compiledTrafficTimeRange struct {
	startMinute int
	endMinute   int
	startAt     time.Time
	endAt       time.Time
}

type compiledTrafficSchedule struct {
	enabled     bool
	dailyRepeat bool
	location    *time.Location
	timeRanges  []compiledTrafficTimeRange
}

var activeTrafficSchedule atomic.Pointer[compiledTrafficSchedule]

func defaultTrafficSchedule() TrafficSchedule {
	return TrafficSchedule{
		DailyRepeat: true,
		TimeRanges: []TrafficTimeRange{
			{StartTime: "09:00", EndTime: "18:00"},
		},
		Timezone: defaultScheduleTimezone,
	}
}

func ValidateScheduleJSON(value string) error {
	var schedule TrafficSchedule
	if err := common.UnmarshalJsonStr(value, &schedule); err != nil {
		return errors.New("traffic control schedule must be valid JSON")
	}
	_, err := compileTrafficSchedule(schedule)
	return err
}

func mainlandWebBlockEnabledAt(at time.Time) bool {
	schedule := activeTrafficSchedule.Load()
	if schedule == nil || !schedule.enabled {
		return mainlandWebBlockEnabled.Load()
	}

	localTime := at.In(schedule.location)
	minute := localTime.Hour()*60 + localTime.Minute()
	for _, timeRange := range schedule.timeRanges {
		if schedule.dailyRepeat {
			if timeRange.startMinute < timeRange.endMinute {
				if minute >= timeRange.startMinute && minute < timeRange.endMinute {
					return true
				}
				continue
			}
			if minute >= timeRange.startMinute || minute < timeRange.endMinute {
				return true
			}
			continue
		}

		if !localTime.Before(timeRange.startAt) && localTime.Before(timeRange.endAt) {
			return true
		}
	}
	return false
}

func compileTrafficSchedule(schedule TrafficSchedule) (*compiledTrafficSchedule, error) {
	timezone := strings.TrimSpace(schedule.Timezone)
	if timezone == "" {
		timezone = defaultScheduleTimezone
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, errors.New("traffic control timezone must be a valid IANA timezone")
	}

	compiled := &compiledTrafficSchedule{
		enabled:     schedule.Enabled,
		dailyRepeat: schedule.DailyRepeat,
		location:    location,
	}
	if !schedule.Enabled {
		return compiled, nil
	}
	if len(schedule.TimeRanges) == 0 {
		return nil, errors.New("at least one traffic control time range is required")
	}
	if len(schedule.TimeRanges) > maxScheduleTimeRanges {
		return nil, errors.New("traffic control schedule cannot contain more than 24 time ranges")
	}

	var scheduleDate time.Time
	if !schedule.DailyRepeat {
		scheduleDate, err = time.ParseInLocation(oneTimeDateLayout, schedule.Date, location)
		if err != nil {
			return nil, errors.New("one-time traffic control date must use YYYY-MM-DD format")
		}
	}

	compiled.timeRanges = make([]compiledTrafficTimeRange, 0, len(schedule.TimeRanges))
	for _, value := range schedule.TimeRanges {
		start, startErr := time.ParseInLocation(dailyTimeLayout, value.StartTime, location)
		end, endErr := time.ParseInLocation(dailyTimeLayout, value.EndTime, location)
		if startErr != nil || endErr != nil {
			return nil, errors.New("traffic control times must use HH:mm format")
		}

		startMinute := start.Hour()*60 + start.Minute()
		endMinute := end.Hour()*60 + end.Minute()
		if startMinute == endMinute {
			return nil, errors.New("traffic control start and end times must be different")
		}

		compiledRange := compiledTrafficTimeRange{
			startMinute: startMinute,
			endMinute:   endMinute,
		}
		if !schedule.DailyRepeat {
			compiledRange.startAt = time.Date(
				scheduleDate.Year(), scheduleDate.Month(), scheduleDate.Day(),
				start.Hour(), start.Minute(), 0, 0, location,
			)
			compiledRange.endAt = time.Date(
				scheduleDate.Year(), scheduleDate.Month(), scheduleDate.Day(),
				end.Hour(), end.Minute(), 0, 0, location,
			)
			if endMinute < startMinute {
				compiledRange.endAt = compiledRange.endAt.AddDate(0, 0, 1)
			}
		}
		compiled.timeRanges = append(compiled.timeRanges, compiledRange)
	}

	return compiled, nil
}
