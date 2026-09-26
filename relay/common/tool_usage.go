package common

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

var reservedBillableToolNames = map[string]struct{}{
	dto.BuildInToolWebSearchPreview: {},
	dto.BuildInToolWebSearch:        {},
	dto.BuildInToolFileSearch:       {},
	dto.BuildInToolGoogleSearch:     {},
	dto.BuildInToolImageGeneration:  {},
}

// CountBillableToolCall is the single entry point for per-call tool billing counts.
// Built-in call types always count; custom function/tool_use names only count when priced.
func (info *RelayInfo) CountBillableToolCall(itemType string, functionName string) {
	if info == nil {
		return
	}
	if info.ResponsesUsageInfo == nil {
		info.ResponsesUsageInfo = &ResponsesUsageInfo{
			BuiltInTools: make(map[string]*BuildInToolInfo),
		}
	}
	if info.ResponsesUsageInfo.BuiltInTools == nil {
		info.ResponsesUsageInfo.BuiltInTools = make(map[string]*BuildInToolInfo)
	}

	switch itemType {
	case dto.BuildInCallWebSearchCall, dto.BuildInCallXSearchCall:
		info.incrementBillableToolCall(resolveWebSearchToolName(info.ResponsesUsageInfo.BuiltInTools))
	case dto.BuildInCallFileSearchCall:
		info.incrementBillableToolCall(dto.BuildInToolFileSearch)
	case dto.BuildInCallFunctionCall, dto.BuildInCallToolUse:
		if functionName == "" {
			return
		}
		if _, reserved := reservedBillableToolNames[functionName]; reserved {
			return
		}
		if operation_setting.GetToolPriceForModel(functionName, info.OriginModelName) <= 0 {
			return
		}
		info.incrementBillableToolCall(functionName)
	}
}

// ResponsesToolCallCounter deduplicates tool-call observations that can arrive
// both as response.output_item.done events and in the terminal response.output
// array. Responses output indexes are unique within one response, while id and
// call_id are stable across those two representations when an upstream sends
// them. Calls without any identity are counted on every observation so a
// malformed upstream cannot silently suppress distinct calls.
type ResponsesToolCallCounter struct {
	seen map[string]struct{}
}

// Count records a Responses output item once and forwards billable calls to
// RelayInfo. It returns true when the observation was accepted (including
// unpriced calls, which are filtered by CountBillableToolCall).
func (c *ResponsesToolCallCounter) Count(info *RelayInfo, item *dto.ResponsesOutput, outputIndex *int) bool {
	if c == nil || info == nil || item == nil {
		return false
	}
	// A function/tool output can arrive once with only an id or index and then
	// again with its name on the terminal response. Do not reserve the identity
	// until the item is complete enough to be billable, or the later named item
	// would be mistaken for a duplicate and undercharged.
	switch item.Type {
	case dto.BuildInCallWebSearchCall, dto.BuildInCallXSearchCall, dto.BuildInCallFileSearchCall:
	case dto.BuildInCallFunctionCall, dto.BuildInCallToolUse:
		if strings.TrimSpace(item.Name) == "" {
			return false
		}
	default:
		return false
	}

	aliases := responsesToolCallAliases(item, outputIndex)
	if len(aliases) > 0 {
		if c.seen == nil {
			c.seen = make(map[string]struct{})
		}
		for _, alias := range aliases {
			if _, ok := c.seen[alias]; ok {
				return false
			}
		}
		for _, alias := range aliases {
			c.seen[alias] = struct{}{}
		}
	}

	info.CountBillableToolCall(item.Type, item.Name)
	return true
}

func responsesToolCallAliases(item *dto.ResponsesOutput, outputIndex *int) []string {
	if item == nil {
		return nil
	}
	aliases := make([]string, 0, 3)
	if item.ID != "" {
		aliases = append(aliases, "id:"+item.ID)
	}
	if item.CallId != "" {
		aliases = append(aliases, "call:"+item.CallId)
	}
	if outputIndex != nil && *outputIndex >= 0 {
		aliases = append(aliases, fmt.Sprintf("index:%d", *outputIndex))
	}
	return aliases
}

func resolveWebSearchToolName(tools map[string]*BuildInToolInfo) string {
	if _, ok := tools[dto.BuildInToolWebSearchPreview]; ok {
		return dto.BuildInToolWebSearchPreview
	}
	if _, ok := tools[dto.BuildInToolWebSearch]; ok {
		return dto.BuildInToolWebSearch
	}
	return dto.BuildInToolWebSearchPreview
}

func (info *RelayInfo) incrementBillableToolCall(name string) {
	if existing, ok := info.ResponsesUsageInfo.BuiltInTools[name]; ok && existing != nil {
		existing.CallCount++
		return
	}
	info.ResponsesUsageInfo.BuiltInTools[name] = &BuildInToolInfo{
		ToolName:  name,
		CallCount: 1,
	}
}

// ImageGenerationCallCounter counts completed Responses image_generation_call
// outputs with stream-safe identity deduplication.
type ImageGenerationCallCounter struct {
	seen         map[string]struct{}
	count        int
	resetPending bool
}

// Observe records one completed final image output when billable.
// outputIndex may be nil; when set and nonnegative it participates in dedup.
func (c *ImageGenerationCallCounter) Observe(item *dto.ResponsesOutput, outputIndex *int) {
	if c == nil || item == nil {
		return
	}
	if item.Type != dto.ResponsesOutputTypeImageGenerationCall {
		return
	}
	if strings.TrimSpace(item.Result) == "" {
		return
	}
	switch strings.ToLower(strings.TrimSpace(item.Status)) {
	case "failed", "cancelled", "canceled", "incomplete", "partial":
		return
	}
	// A reset marks an explicitly failed terminal response. If a later valid
	// output is observed while reusing the counter, start a new successful
	// observation set instead of clearing it again at commit time.
	c.resetPending = false

	aliases := make([]string, 0, 4)
	if item.ID != "" {
		aliases = append(aliases, "id:"+item.ID)
	}
	if item.CallId != "" {
		aliases = append(aliases, "call:"+item.CallId)
	}
	if outputIndex != nil && *outputIndex >= 0 {
		aliases = append(aliases, fmt.Sprintf("index:%d", *outputIndex))
	}
	sum := sha256.Sum256([]byte(item.Result))
	aliases = append(aliases, "result:"+hex.EncodeToString(sum[:]))

	if c.seen == nil {
		c.seen = make(map[string]struct{})
	}
	for _, alias := range aliases {
		if _, ok := c.seen[alias]; ok {
			return
		}
	}
	for _, alias := range aliases {
		c.seen[alias] = struct{}{}
	}
	c.count++
}

// Reset clears pending observations (used when a terminal response fails).
func (c *ImageGenerationCallCounter) Reset() {
	if c == nil {
		return
	}
	c.seen = nil
	c.count = 0
	c.resetPending = true
}

// Count returns the deduplicated completed image output count before commit capping.
func (c *ImageGenerationCallCounter) Count() int {
	if c == nil {
		return 0
	}
	return c.count
}

// EnsureAtLeast records provider-reported image counts when a Responses
// gateway omits image_generation_call items from response.output. The count is
// intentionally capped only at commit time so later output observations can
// still raise it to the actual completed count.
func (c *ImageGenerationCallCounter) EnsureAtLeast(count int) {
	if c == nil || count <= c.count {
		return
	}
	c.count = count
	c.resetPending = false
}

// Commit writes the capped completed-output count into RelayInfo. An empty
// normal commit preserves a count already observed on the request, while a
// Reset followed by Commit explicitly clears it for failed terminal responses.
// Request tool declarations alone must not become billable calls.
func (c *ImageGenerationCallCounter) Commit(info *RelayInfo) {
	if info == nil {
		return
	}
	if info.ResponsesUsageInfo == nil {
		info.ResponsesUsageInfo = &ResponsesUsageInfo{
			BuiltInTools: make(map[string]*BuildInToolInfo),
		}
	}
	if info.ResponsesUsageInfo.BuiltInTools == nil {
		info.ResponsesUsageInfo.BuiltInTools = make(map[string]*BuildInToolInfo)
	}

	count := 0
	resetPending := false
	if c != nil {
		count = c.count
		resetPending = c.resetPending
	}
	if count > dto.MaxImageN {
		count = dto.MaxImageN
	}

	if existing, ok := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration]; ok && existing != nil {
		if resetPending {
			existing.CallCount = 0
		} else if count > existing.CallCount {
			existing.CallCount = count
		}
		if c != nil {
			c.resetPending = false
		}
		return
	}
	info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration] = &BuildInToolInfo{
		ToolName:  dto.BuildInToolImageGeneration,
		CallCount: count,
	}
	if c != nil {
		c.resetPending = false
	}
}

// IsNonBillableResponsesStatus reports terminal response statuses that must not
// bill pending image_generation observations.
func IsNonBillableResponsesStatus(status []byte) bool {
	if len(status) == 0 {
		return false
	}
	var s string
	if err := common.Unmarshal(status, &s); err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "failed", "cancelled", "canceled", "incomplete":
		return true
	default:
		return false
	}
}
