package perfmetrics

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"

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
	// liveMinSamples hides rates beside usage logs computed from too few
	// requests to mean anything. The status page rates every minute with traffic.
	liveMinSamples = 3
	// liveCompareRadiusMinutes widens a log's minute on both sides, so the rate
	// beside one request does not rest on that minute alone.
	liveCompareRadiusMinutes = 2
	// MaxLiveComparePoints bounds one success rate lookup for usage logs.
	MaxLiveComparePoints = 100
	// maxLiveCompareMinutes bounds the distinct minutes one lookup reads; points
	// past it get no rate. A page of logs normally spans far fewer.
	maxLiveCompareMinutes = 150
	liveRedisFieldSep     = "\x1f"
	// liveFlushInterval is how often each instance adds its new counts to Redis.
	liveFlushInterval = 5 * time.Second
	// liveFlushHorizon is how far back a flush looks for counts Redis has not
	// seen yet, so counts held back by a Redis outage still reach it.
	liveFlushHorizon = 10 * time.Minute
	// liveSettleDelay is how long after a minute ends every instance has
	// flushed it, with room for clock skew; Redis counts read later are final.
	liveSettleDelay = liveFlushInterval + 25*time.Second
	// liveCacheTTL is how long Redis counts of a minute that is not final yet
	// are reused by status page reads.
	liveCacheTTL = 10 * time.Second
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
	// flushedRequests and flushedSuccesses are the counts already added to
	// Redis. Only flushLiveRedis reads and writes them.
	flushedRequests  int64
	flushedSuccesses int64
}

// liveCachedMinute is one minute of Redis counts as read at fetchedAt.
type liveCachedMinute struct {
	counts    map[liveSeries]liveCounts
	fetchedAt time.Time
}

// validAt reports whether the cached counts can still be served at now: they
// were read after the minute settled, or recently enough.
func (m liveCachedMinute) validAt(minuteTs int64, now time.Time) bool {
	settledAt := time.Unix(minuteTs+liveBucketSeconds, 0).Add(liveSettleDelay)
	return !m.fetchedAt.Before(settledAt) || now.Sub(m.fetchedAt) < liveCacheTTL
}

var (
	// liveCacheMu guards liveCache. liveStatusFetchMu lets one status page read
	// fetch from Redis while concurrent ones wait and reuse what it read.
	liveCacheMu       sync.RWMutex
	liveCache         = map[int64]liveCachedMinute{}
	liveStatusFetchMu sync.Mutex
	// liveStatusFetchFailedAt is when a status page read from Redis last
	// failed. liveStatusFetchMu guards it.
	liveStatusFetchFailedAt time.Time
)

type liveMinute struct {
	series sync.Map // liveSeries -> *liveCounter
}

var liveMinutes sync.Map // minute start -> *liveMinute

type LiveMinutePoint struct {
	Ts          int64   `json:"ts"`
	SuccessRate float64 `json:"success_rate"`
}

type LiveModelStatus struct {
	ModelName         string            `json:"model_name"`
	ShortSuccessRate  *float64          `json:"short_success_rate"`
	WindowSuccessRate *float64          `json:"window_success_rate"`
	Minutes           []LiveMinutePoint `json:"minutes"`
}

type LiveGroupStatus struct {
	Group       string `json:"group"`
	Description string `json:"description"`
	// Vendor is the group's vendor category, or "" when none is set.
	Vendor            string            `json:"vendor"`
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

func liveFlushLoop() {
	for {
		time.Sleep(liveFlushInterval)
		flushLiveRedis(time.Now())
	}
}

// flushLiveRedis adds the counts recorded since the last successful flush to
// Redis in one transaction. When it fails the same counts are sent next time.
func flushLiveRedis(now time.Time) {
	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	type pendingDelta struct {
		counter             *liveCounter
		requests, successes int64
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	horizon := liveMinuteStart(now.Add(-liveFlushHorizon).Unix())
	ttl := liveRetention + time.Hour
	pipe := common.RDB.TxPipeline()
	var pending []pendingDelta
	liveMinutes.Range(func(key, value any) bool {
		minuteTs := key.(int64)
		if minuteTs < horizon {
			return true
		}
		requestKey, successKey := liveRedisKeys(minuteTs)
		touchedRequests, touchedSuccesses := false, false
		value.(*liveMinute).series.Range(func(seriesKey, counterValue any) bool {
			counter := counterValue.(*liveCounter)
			// Successes are read first: a request is counted before its success,
			// so Redis never holds more successes than requests.
			successes := counter.successes.Load()
			requests := counter.requests.Load()
			delta := pendingDelta{
				counter:   counter,
				requests:  requests - counter.flushedRequests,
				successes: successes - counter.flushedSuccesses,
			}
			if delta.requests == 0 && delta.successes == 0 {
				return true
			}
			series := seriesKey.(liveSeries)
			field := series.group + liveRedisFieldSep + series.model
			if delta.requests > 0 {
				pipe.HIncrBy(ctx, requestKey, field, delta.requests)
				touchedRequests = true
			}
			if delta.successes > 0 {
				pipe.HIncrBy(ctx, successKey, field, delta.successes)
				touchedSuccesses = true
			}
			pending = append(pending, delta)
			return true
		})
		if touchedRequests {
			pipe.Expire(ctx, requestKey, ttl)
		}
		if touchedSuccesses {
			pipe.Expire(ctx, successKey, ttl)
		}
		return true
	})
	if len(pending) == 0 {
		return
	}
	if _, err := pipe.Exec(ctx); err != nil {
		common.SysError(common.LogText("failed to flush live perf metrics to redis: %s", err.Error()))
		return
	}
	for _, delta := range pending {
		delta.counter.flushedRequests += delta.requests
		delta.counter.flushedSuccesses += delta.successes
	}
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

// loadLiveStatusMinutes returns the counts of the requested minutes for the
// status page. With Redis, minutes read before are served from liveCache and
// concurrent page loads share one Redis read; without it, or while Redis
// fails, this instance's own counts are used.
func loadLiveStatusMinutes(minutes []int64, now time.Time) map[int64]map[liveSeries]liveCounts {
	if !common.RedisEnabled || common.RDB == nil {
		return localLiveMinutes(minutes)
	}
	loaded, missing := cachedLiveMinutes(minutes, now)
	if len(missing) == 0 {
		return loaded
	}
	liveStatusFetchMu.Lock()
	defer liveStatusFetchMu.Unlock()
	// The read that held the lock may have fetched these minutes already.
	loaded, missing = cachedLiveMinutes(minutes, now)
	if len(missing) == 0 {
		return loaded
	}
	// After a failed read, waiting loads go local instead of each timing out.
	if now.Sub(liveStatusFetchFailedAt) < liveCacheTTL {
		return localLiveMinutes(minutes)
	}
	fetched, err := loadLiveMinutesRedis(missing)
	if err != nil {
		liveStatusFetchFailedAt = now
		common.SysError(common.LogText("failed to read live perf metrics from redis: %s", err.Error()))
		return localLiveMinutes(minutes)
	}
	pruneBefore := liveMinuteStart(now.Unix()) - (LiveWindowMinutes+liveShortWindowMinutes)*liveBucketSeconds
	liveCacheMu.Lock()
	for _, minuteTs := range missing {
		liveCache[minuteTs] = liveCachedMinute{counts: fetched[minuteTs], fetchedAt: now}
		loaded[minuteTs] = fetched[minuteTs]
	}
	for minuteTs := range liveCache {
		if minuteTs < pruneBefore {
			delete(liveCache, minuteTs)
		}
	}
	liveCacheMu.Unlock()
	return loaded
}

// cachedLiveMinutes splits minutes into the counts liveCache can still serve
// at now and the minutes that must be read from Redis.
func cachedLiveMinutes(minutes []int64, now time.Time) (map[int64]map[liveSeries]liveCounts, []int64) {
	loaded := make(map[int64]map[liveSeries]liveCounts, len(minutes))
	var missing []int64
	liveCacheMu.RLock()
	defer liveCacheMu.RUnlock()
	for _, minuteTs := range minutes {
		cached, ok := liveCache[minuteTs]
		if !ok || !cached.validAt(minuteTs, now) {
			missing = append(missing, minuteTs)
			continue
		}
		loaded[minuteTs] = cached.counts
	}
	return loaded, missing
}

// loadLiveCompareCounts returns the counts of the wanted series in each minute.
// Minutes liveCache can serve come from it; Redis is asked only for the wanted
// fields of the others.
func loadLiveCompareCounts(wanted map[int64]map[liveSeries]struct{}, now time.Time) map[int64]map[liveSeries]liveCounts {
	minutes := make([]int64, 0, len(wanted))
	for minuteTs := range wanted {
		minutes = append(minutes, minuteTs)
	}
	if !common.RedisEnabled || common.RDB == nil {
		return localLiveMinutes(minutes)
	}
	loaded, missing := cachedLiveMinutes(minutes, now)
	if len(missing) == 0 {
		return loaded
	}
	fetched, err := loadLiveSeriesRedis(missing, wanted)
	if err != nil {
		common.SysError(common.LogText("failed to read live perf metrics from redis: %s", err.Error()))
		return localLiveMinutes(minutes)
	}
	maps.Copy(loaded, fetched)
	return loaded
}

// localLiveMinutes returns this instance's own counts of the requested minutes.
func localLiveMinutes(minutes []int64) map[int64]map[liveSeries]liveCounts {
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

// loadLiveSeriesRedis reads only the wanted series of each minute from Redis.
func loadLiveSeriesRedis(minutes []int64, wanted map[int64]map[liveSeries]struct{}) (map[int64]map[liveSeries]liveCounts, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	type minuteRead struct {
		minuteTs            int64
		series              []liveSeries
		requests, successes *redis.SliceCmd
	}
	pipe := common.RDB.Pipeline()
	reads := make([]minuteRead, 0, len(minutes))
	for _, minuteTs := range minutes {
		read := minuteRead{minuteTs: minuteTs}
		fields := make([]string, 0, len(wanted[minuteTs]))
		for series := range wanted[minuteTs] {
			read.series = append(read.series, series)
			fields = append(fields, series.group+liveRedisFieldSep+series.model)
		}
		if len(fields) == 0 {
			continue
		}
		requestKey, successKey := liveRedisKeys(minuteTs)
		read.requests = pipe.HMGet(ctx, requestKey, fields...)
		read.successes = pipe.HMGet(ctx, successKey, fields...)
		reads = append(reads, read)
	}
	if len(reads) == 0 {
		return map[int64]map[liveSeries]liveCounts{}, nil
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	loaded := make(map[int64]map[liveSeries]liveCounts, len(reads))
	for _, read := range reads {
		requests, successes := read.requests.Val(), read.successes.Val()
		counts := make(map[liveSeries]liveCounts, len(read.series))
		for i, series := range read.series {
			requestCount := redisSliceInt(requests, i)
			if requestCount == 0 {
				continue
			}
			counts[series] = liveCounts{requests: requestCount, successes: redisSliceInt(successes, i)}
		}
		loaded[read.minuteTs] = counts
	}
	return loaded, nil
}

// redisSliceInt reads entry i of an HMGET reply, where a missing field is nil.
func redisSliceInt(values []any, i int) int64 {
	if i >= len(values) {
		return 0
	}
	value, ok := values[i].(string)
	if !ok {
		return 0
	}
	return parseRedisInt(value)
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
	loaded := loadLiveStatusMinutes(minutes, now)

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
			totals.minutes = append(totals.minutes, LiveMinutePoint{Ts: minuteTs, SuccessRate: *liveStatusRate(counts)})
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
				ShortSuccessRate:  liveStatusRate(totals.short),
				WindowSuccessRate: liveStatusRate(totals.window),
				Minutes:           totals.minutes,
			})
		}
		slices.SortFunc(statuses, func(a, b LiveModelStatus) int {
			return strings.Compare(a.ModelName, b.ModelName)
		})
		result.Groups = append(result.Groups, LiveGroupStatus{
			Group:             group,
			Description:       groups[group],
			Vendor:            setting.GetGroupVendor(group),
			ShortSuccessRate:  liveStatusRate(groupShort),
			WindowSuccessRate: liveStatusRate(groupWindow),
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
// rate is nil outside retention, for groups missing from allowedGroups, below
// liveMinSamples, and for points whose window would read more than
// maxLiveCompareMinutes distinct minutes in total.
func QueryLiveSuccessRates(now time.Time, points []LiveComparePoint, allowedGroups map[string]string) LiveSuccessRatesResult {
	currentMinute := liveMinuteStart(now.Unix())
	oldestMinute := liveMinuteStart(now.Add(-liveRetention).Unix())
	radius := int64(liveCompareRadiusMinutes * liveBucketSeconds)

	windows := make([][2]int64, len(points))
	wanted := map[int64]map[liveSeries]struct{}{}
	for i, point := range points {
		if _, ok := allowedGroups[point.Group]; !ok || point.ModelName == "" {
			continue
		}
		center := liveMinuteStart(point.CreatedAt)
		if center < oldestMinute || center > currentMinute {
			continue
		}
		start, end := max(center-radius, oldestMinute), min(center+radius, currentMinute)
		newMinutes := 0
		for minuteTs := start; minuteTs <= end; minuteTs += liveBucketSeconds {
			if _, ok := wanted[minuteTs]; !ok {
				newMinutes++
			}
		}
		if len(wanted)+newMinutes > maxLiveCompareMinutes {
			continue
		}
		windows[i] = [2]int64{start, end}
		series := liveSeries{group: point.Group, model: point.ModelName}
		for minuteTs := start; minuteTs <= end; minuteTs += liveBucketSeconds {
			if wanted[minuteTs] == nil {
				wanted[minuteTs] = map[liveSeries]struct{}{}
			}
			wanted[minuteTs][series] = struct{}{}
		}
	}
	loaded := loadLiveCompareCounts(wanted, now)

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

// liveStatusRate is the success rate the status page shows, nil only without
// requests: a few successful requests read as working, not as unknown.
func liveStatusRate(counts liveCounts) *float64 {
	if counts.requests == 0 {
		return nil
	}
	rate := math.Round(float64(counts.successes)/float64(counts.requests)*10000) / 100
	return &rate
}

func (c liveCounts) add(other liveCounts) liveCounts {
	return liveCounts{requests: c.requests + other.requests, successes: c.successes + other.successes}
}
