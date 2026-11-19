package common

import (
	"bytes"

	"github.com/tidwall/gjson"
)

// ObserveCacheWriteTTL records whether every cache_control breakpoint in the
// upstream request body asks for the 1-hour TTL. Anthropic keeps a breakpoint
// without ttl for 5 minutes, so one such breakpoint, or a body without any,
// leaves unsplit cache writes at the 5-minute price.
func (info *RelayInfo) ObserveCacheWriteTTL(body []byte) {
	if info == nil {
		return
	}
	info.CacheWritesAll1h = false
	if !bytes.Contains(body, []byte(`"cache_control"`)) || !bytes.Contains(body, []byte(`"1h"`)) {
		return
	}
	has1h, hasOther := cacheControlTTLs(gjson.ParseBytes(body))
	info.CacheWritesAll1h = has1h && !hasOther
}

// cacheControlTTLs walks a JSON value for cache_control objects anywhere in
// it: the top-level automatic breakpoint, system, message, and tool blocks.
// It stops at the first breakpoint that is not a 1-hour one.
func cacheControlTTLs(value gjson.Result) (has1h, hasOther bool) {
	value.ForEach(func(key, child gjson.Result) bool {
		if key.Str == "cache_control" && child.IsObject() {
			if child.Get("ttl").Str == "1h" {
				has1h = true
			} else {
				hasOther = true
			}
		} else if child.IsObject() || child.IsArray() {
			child1h, childOther := cacheControlTTLs(child)
			has1h = has1h || child1h
			hasOther = hasOther || childOther
		}
		return !hasOther
	})
	return has1h, hasOther
}
