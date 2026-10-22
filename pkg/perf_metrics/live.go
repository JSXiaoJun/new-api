package perfmetrics

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/go-redis/redis/v8"
)

// Minute-level success counts behind the live service status page and the
// platform success rate shown beside failed usage logs. They are kept in
// memory, and in Redis when it is enabled so every instance reports the same
// numbers, for liveRetention only; the hourly buckets stay the durable record.
const (
	liveBucketSeconds = 60
	liveRetention     = 24 * time.Hour
	// LiveWindowMinutes is how far back the status page looks.
	LiveWindowMinutes      = 60
	liveShortWindowMinutes = 5
	// liveMinSamples hides rates computed from too few requests to mean anything.
	liveMinSamples = 3
	// liveCompareRadiusMinutes widens a log's minute on both sides, so the rate
	// beside one request does not rest on that minute alone.
	liveCompareRadiusMinutes = 2
	// MaxLiveComparePoints bounds one success rate lookup for usage logs.
	MaxLiveComparePoints = 100
	liveRedisFieldSep    = "\x1f"
)

type liveSeries struct {
	group string
	model string
}

type liveCounts struct {
	requests  int64
	successes int64
}

type liveCounter struct {
	requests  atomic.Int64
	successes atomic.Int64
}

type liveMinute struct {
	series sync.Map // liveSeries -> *liveCounter
}

var liveMinutes sync.Map // minute start -> *liveMinute

type LiveMinutePoint struct {
	Ts int64 `json:"ts"`
	// SuccessRate is nil when the minute had fewer than liveMinSamples requests.
	SuccessRate *float64 `json:"success_rate"`
}

type LiveModelStatus struct {
	ModelName         string            `json:"model_name"`
	ShortSuccessRate  *float64          `json:"short_success_rate"`
	WindowSuccessRate *float64          `json:"window_success_rate"`
	Minutes           []LiveMinutePoint `json:"minutes"`
}

type LiveGroupStatus struct {
	Group             string            `json:"group"`
	Description       string            `json:"description"`
	ShortSuccessRate  *float64          `json:"short_success_rate"`
	WindowSuccessRate *float64          `json:"window_success_rate"`
	Models            []LiveModelStatus `json:"models"`
}

type LiveStatusResult struct {
	// WindowStart and WindowEnd are the first and the current minute starts.
	WindowStart        int64             `json:"window_start"`
	WindowEnd          int64             `json:"window_end"`
	BucketSeconds      int64             `json:"bucket_seconds"`
	ShortWindowMinutes int               `json:"short_window_minutes"`
	Groups             []LiveGroupStatus `json:"groups"`
}

// LiveSuccessRatesResult holds the rate around each requested usage log,
// aligned with the request, and the length of the window each rate covers.
type LiveSuccessRatesResult struct {
	WindowMinutes int        `json:"window_minutes"`
	SuccessRates  []*float64 `json:"success_rates"`
}

// LiveComparePoint names one usage log by when it was written and what it used.
type LiveComparePoint struct {
	CreatedAt int64  `json:"created_at"`
	Group     string `json:"group"`
	ModelName string `json:"model_name"`
}

func liveMinuteStart(ts int64) int64 {
	return ts - ts%liveBucketSeconds
}

func recordLive(sample Sample, now time.Time) {
	minuteTs := liveMinuteStart(now.Unix())
	minuteValue, _ := liveMinutes.LoadOrStore(minuteTs, &liveMinute{})
	counterValue, _ := minuteValue.(*liveMinute).series.LoadOrStore(
		liveSeries{group: sample.Group, model: sample.Model}, &liveCounter{})
	counter := counterValue.(*liveCounter)
	counter.requests.Add(1)
	if sample.Success {
		counter.successes.Add(1)
	}
}

func recordLiveRedis(sample Sample, now time.Time) {
	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	requestKey, successKey := liveRedisKeys(liveMinuteStart(now.Unix()))
	field := sample.Group + liveRedisFieldSep + sample.Model
	ttl := liveRetention + time.Hour
	pipe := common.RDB.TxPipeline()
	pipe.HIncrBy(ctx, requestKey, field, 1)
	pipe.Expire(ctx, requestKey, ttl)
	if sample.Success {
		pipe.HIncrBy(ctx, successKey, field, 1)
		pipe.Expire(ctx, successKey, ttl)
	}
	_, _ = pipe.Exec(ctx)
}

func liveRedisKeys(minuteTs int64) (string, string) {
	return fmt.Sprintf("perf:live:%d:req", minuteTs), fmt.Sprintf("perf:live:%d:ok", minuteTs)
}

func liveCleanupLoop() {
	for {
		time.Sleep(time.Minute)
		pruneLiveMinutes(time.Now())
	}
}

func pruneLiveMinutes(now time.Time) {
	cutoff := liveMinuteStart(now.Add(-liveRetention).Unix())
	liveMinutes.Range(func(key, _ any) bool {
		if key.(int64) < cutoff {
			liveMinutes.Delete(key)
		}
		return true
	})
}

// loadLiveMinutes returns the counts of the requested minutes, read from Redis
// when it is enabled and falling back to this instance's own counts.
func loadLiveMinutes(minutes []int64) map[int64]map[liveSeries]liveCounts {
	if common.RedisEnabled && common.RDB != nil {
		loaded, err := loadLiveMinutesRedis(minutes)
		if err == nil {
			return loaded
		}
		common.SysError(common.LogText("failed to read live perf metrics from redis: %s", err.Error()))
	}
	loaded := make(map[int64]map[liveSeries]liveCounts, len(minutes))
	for _, minuteTs := range minutes {
		minuteValue, ok := liveMinutes.Load(minuteTs)
		if !ok {
			continue
		}
		counts := map[liveSeries]liveCounts{}
		minuteValue.(*liveMinute).series.Range(func(key, value any) bool {
			counter := value.(*liveCounter)
			counts[key.(liveSeries)] = liveCounts{
				requests:  counter.requests.Load(),
				successes: counter.successes.Load(),
			}
			return true
		})
		loaded[minuteTs] = counts
	}
	return loaded
}

func loadLiveMinutesRedis(minutes []int64) (map[int64]map[liveSeries]liveCounts, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pipe := common.RDB.Pipeline()
	requestCmds := make([]*redis.StringStringMapCmd, len(minutes))
	successCmds := make([]*redis.StringStringMapCmd, len(minutes))
	for i, minuteTs := range minutes {
		requestKey, successKey := liveRedisKeys(minuteTs)
		requestCmds[i] = pipe.HGetAll(ctx, requestKey)
		successCmds[i] = pipe.HGetAll(ctx, successKey)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	loaded := make(map[int64]map[liveSeries]liveCounts, len(minutes))
	for i, minuteTs := range minutes {
		requests := requestCmds[i].Val()
		if len(requests) == 0 {
			continue
		}
		successes := successCmds[i].Val()
		counts := make(map[liveSeries]liveCounts, len(requests))
		for field, value := range requests {
			group, modelName, ok := strings.Cut(field, liveRedisFieldSep)
			if !ok {
				continue
			}
			counts[liveSeries{group: group, model: modelName}] = liveCounts{
				requests:  parseRedisInt(value),
				successes: parseRedisInt(successes[field]),
			}
		}
		loaded[minuteTs] = counts
	}
	return loaded, nil
}

// QueryLiveStatus reports the last LiveWindowMinutes of every group in groups
// (name to description) that served requests in that window.
func QueryLiveStatus(now time.Time, groups map[string]string) LiveStatusResult {
	windowEnd := liveMinuteStart(now.Unix())
	windowStart := windowEnd - (LiveWindowMinutes-1)*liveBucketSeconds
	shortStart := windowEnd - (liveShortWindowMinutes-1)*liveBucketSeconds
	minutes := make([]int64, 0, LiveWindowMinutes)
	for minuteTs := windowStart; minuteTs <= windowEnd; minuteTs += liveBucketSeconds {
		minutes = append(minutes, minuteTs)
	}
	loaded := loadLiveMinutes(minutes)

	type seriesTotals struct {
		short, window liveCounts
		minutes       []LiveMinutePoint
	}
	byGroup := map[string]map[string]*seriesTotals{}
	for _, minuteTs := range minutes {
		for series, counts := range loaded[minuteTs] {
			if _, ok := groups[series.group]; !ok || counts.requests == 0 {
				continue
			}
			if byGroup[series.group] == nil {
				byGroup[series.group] = map[string]*seriesTotals{}
			}
			totals := byGroup[series.group][series.model]
			if totals == nil {
				totals = &seriesTotals{}
				byGroup[series.group][series.model] = totals
			}
			totals.window = totals.window.add(counts)
			if minuteTs >= shortStart {
				totals.short = totals.short.add(counts)
			}
			totals.minutes = append(totals.minutes, LiveMinutePoint{Ts: minuteTs, SuccessRate: liveSuccessRate(counts)})
		}
	}

	result := LiveStatusResult{
		WindowStart:        windowStart,
		WindowEnd:          windowEnd,
		BucketSeconds:      liveBucketSeconds,
		ShortWindowMinutes: liveShortWindowMinutes,
		Groups:             make([]LiveGroupStatus, 0, len(byGroup)),
	}
	for group, models := range byGroup {
		groupShort, groupWindow := liveCounts{}, liveCounts{}
		statuses := make([]LiveModelStatus, 0, len(models))
		for modelName, totals := range models {
			groupShort = groupShort.add(totals.short)
			groupWindow = groupWindow.add(totals.window)
			statuses = append(statuses, LiveModelStatus{
				ModelName:         modelName,
				ShortSuccessRate:  liveSuccessRate(totals.short),
				WindowSuccessRate: liveSuccessRate(totals.window),
				Minutes:           totals.minutes,
			})
		}
		slices.SortFunc(statuses, func(a, b LiveModelStatus) int {
			return strings.Compare(a.ModelName, b.ModelName)
		})
		result.Groups = append(result.Groups, LiveGroupStatus{
			Group:             group,
			Description:       groups[group],
			ShortSuccessRate:  liveSuccessRate(groupShort),
			WindowSuccessRate: liveSuccessRate(groupWindow),
			Models:            statuses,
		})
	}
	slices.SortFunc(result.Groups, func(a, b LiveGroupStatus) int {
		return strings.Compare(a.Group, b.Group)
	})
	return result
}

// QueryLiveSuccessRates returns, for each point, the success rate of all
// requests for its group and model within liveCompareRadiusMinutes of it. A
// rate is nil outside retention, for groups missing from allowedGroups, and
// below liveMinSamples.
func QueryLiveSuccessRates(now time.Time, points []LiveComparePoint, allowedGroups map[string]string) LiveSuccessRatesResult {
	currentMinute := liveMinuteStart(now.Unix())
	oldestMinute := liveMinuteStart(now.Add(-liveRetention).Unix())
	radius := int64(liveCompareRadiusMinutes * liveBucketSeconds)

	windows := make([][2]int64, len(points))
	needed := map[int64]struct{}{}
	for i, point := range points {
		if _, ok := allowedGroups[point.Group]; !ok || point.ModelName == "" {
			continue
		}
		center := liveMinuteStart(point.CreatedAt)
		if center < oldestMinute || center > currentMinute {
			continue
		}
		start, end := max(center-radius, oldestMinute), min(center+radius, currentMinute)
		windows[i] = [2]int64{start, end}
		for minuteTs := start; minuteTs <= end; minuteTs += liveBucketSeconds {
			needed[minuteTs] = struct{}{}
		}
	}
	minutes := make([]int64, 0, len(needed))
	for minuteTs := range needed {
		minutes = append(minutes, minuteTs)
	}
	loaded := loadLiveMinutes(minutes)

	rates := make([]*float64, len(points))
	for i, point := range points {
		if windows[i][0] == 0 {
			continue
		}
		series := liveSeries{group: point.Group, model: point.ModelName}
		total := liveCounts{}
		for minuteTs := windows[i][0]; minuteTs <= windows[i][1]; minuteTs += liveBucketSeconds {
			total = total.add(loaded[minuteTs][series])
		}
		rates[i] = liveSuccessRate(total)
	}
	return LiveSuccessRatesResult{WindowMinutes: 2*liveCompareRadiusMinutes + 1, SuccessRates: rates}
}

func liveSuccessRate(counts liveCounts) *float64 {
	if counts.requests < liveMinSamples {
		return nil
	}
	rate := math.Round(float64(counts.successes)/float64(counts.requests)*10000) / 100
	return &rate
}

func (c liveCounts) add(other liveCounts) liveCounts {
	return liveCounts{requests: c.requests + other.requests, successes: c.successes + other.successes}
}
