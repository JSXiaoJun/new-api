package discount_setting

import (
	"errors"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	defaultTimezone      = "Asia/Shanghai"
	oneTimeLayout        = "2006-01-02T15:04"
	dailyTimeLayout      = "15:04"
	minimumDiscountRatio = 0.01
	defaultDiscountRatio = 1.0
)

type DiscountSchedule struct {
	Enabled     bool     `json:"enabled"`
	Ratio       float64  `json:"ratio"`
	Groups      []string `json:"groups"`
	DailyRepeat bool     `json:"daily_repeat"`
	StartAt     string   `json:"start_at"`
	EndAt       string   `json:"end_at"`
	StartTime   string   `json:"start_time"`
	EndTime     string   `json:"end_time"`
	Timezone    string   `json:"timezone"`
}

type discountSetting struct {
	Schedule DiscountSchedule `json:"schedule"`
}

type compiledSchedule struct {
	enabled     bool
	ratio       float64
	groups      map[string]struct{}
	dailyRepeat bool
	location    *time.Location
	startAt     time.Time
	endAt       time.Time
	startMinute int
	endMinute   int
}

var (
	setting = discountSetting{
		Schedule: DiscountSchedule{
			Ratio:    defaultDiscountRatio,
			Groups:   []string{},
			Timezone: defaultTimezone,
		},
	}
	activeSchedule atomic.Pointer[compiledSchedule]
)

func init() {
	config.GlobalConfig.Register("discount_setting", &setting)
	UpdateAndSync()
}

func ValidateScheduleJSON(value string) error {
	var schedule DiscountSchedule
	if err := common.UnmarshalJsonStr(value, &schedule); err != nil {
		return errors.New("discount schedule must be valid JSON")
	}
	_, err := compileSchedule(schedule)
	return err
}

func UpdateAndSync() {
	compiled, err := compileSchedule(setting.Schedule)
	if err != nil {
		compiled = &compiledSchedule{ratio: defaultDiscountRatio}
	}
	activeSchedule.Store(compiled)
}

func RatioFor(userGroup string, at time.Time) float64 {
	schedule := activeSchedule.Load()
	if schedule == nil || !schedule.enabled {
		return defaultDiscountRatio
	}
	if _, ok := schedule.groups[userGroup]; !ok {
		return defaultDiscountRatio
	}

	localTime := at.In(schedule.location)
	if schedule.dailyRepeat {
		minute := localTime.Hour()*60 + localTime.Minute()
		if schedule.startMinute < schedule.endMinute {
			if minute >= schedule.startMinute && minute < schedule.endMinute {
				return schedule.ratio
			}
			return defaultDiscountRatio
		}
		if minute >= schedule.startMinute || minute < schedule.endMinute {
			return schedule.ratio
		}
		return defaultDiscountRatio
	}

	if !localTime.Before(schedule.startAt) && localTime.Before(schedule.endAt) {
		return schedule.ratio
	}
	return defaultDiscountRatio
}

func compileSchedule(schedule DiscountSchedule) (*compiledSchedule, error) {
	if schedule.Ratio < minimumDiscountRatio || schedule.Ratio > 1 || math.IsNaN(schedule.Ratio) || math.IsInf(schedule.Ratio, 0) {
		return nil, errors.New("discount ratio must be at least 0.01 and no greater than 1")
	}

	timezone := strings.TrimSpace(schedule.Timezone)
	if timezone == "" {
		timezone = defaultTimezone
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, errors.New("discount timezone must be a valid IANA timezone")
	}

	compiled := &compiledSchedule{
		enabled:     schedule.Enabled,
		ratio:       schedule.Ratio,
		groups:      make(map[string]struct{}, len(schedule.Groups)),
		dailyRepeat: schedule.DailyRepeat,
		location:    location,
	}
	for _, group := range schedule.Groups {
		group = strings.TrimSpace(group)
		if group != "" {
			compiled.groups[group] = struct{}{}
		}
	}

	if !schedule.Enabled {
		return compiled, nil
	}
	if len(compiled.groups) == 0 {
		return nil, errors.New("at least one user group must be selected when discounting is enabled")
	}

	if schedule.DailyRepeat {
		start, startErr := time.ParseInLocation(dailyTimeLayout, schedule.StartTime, location)
		end, endErr := time.ParseInLocation(dailyTimeLayout, schedule.EndTime, location)
		if startErr != nil || endErr != nil {
			return nil, errors.New("daily discount times must use HH:mm format")
		}
		compiled.startMinute = start.Hour()*60 + start.Minute()
		compiled.endMinute = end.Hour()*60 + end.Minute()
		if compiled.startMinute == compiled.endMinute {
			return nil, errors.New("daily discount start and end times must be different")
		}
		return compiled, nil
	}

	startAt, startErr := time.ParseInLocation(oneTimeLayout, schedule.StartAt, location)
	endAt, endErr := time.ParseInLocation(oneTimeLayout, schedule.EndAt, location)
	if startErr != nil || endErr != nil {
		return nil, errors.New("one-time discount dates must use YYYY-MM-DDTHH:mm format")
	}
	if !endAt.After(startAt) {
		return nil, errors.New("discount end time must be after the start time")
	}
	compiled.startAt = startAt
	compiled.endAt = endAt
	return compiled, nil
}
