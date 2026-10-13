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

// GoogleSearchGroundedPromptTool bills Gemini 2.5-and-older Google Search
// grounding, charged once per grounded prompt.
const GoogleSearchGroundedPromptTool = "google_search_grounded_prompt"

// reservedBillableToolNames are hosted-tool keys without a built-in price
// (Anthropic server tools reported in usage.server_tool_use). They and every
// tool name the built-in price seed prices
// (operation_setting.IsBuiltInToolPriceKey) bill hosted tools; a client
// function or tool_use with the same name is never counted under them.
var reservedBillableToolNames = map[string]struct{}{
	"web_fetch":      {},
	"code_execution": {},
	"tool_search":    {},
}

// MaxBillableToolCallCount bounds every tool count at settlement, whatever its
// source, before it becomes a surcharge multiplier (see
// .agents/rules/billing.md).
const MaxBillableToolCallCount = 10_000

// CountBillableToolCall is the single entry point for per-call tool billing counts.
// Built-in call types always count; custom function/tool_use names only count when priced.
func (info *RelayInfo) CountBillableToolCall(itemType string, functionName string) {
	if info == nil {
		return
	}
	info.ensureBuiltInTools()

	switch itemType {
	case dto.BuildInCallWebSearchCall, dto.BuildInCallXSearchCall:
		info.incrementBillableToolCall(resolveWebSearchToolName(info.ResponsesUsageInfo.BuiltInTools))
	case dto.BuildInCallFileSearchCall:
		info.incrementBillableToolCall(dto.BuildInToolFileSearch)
	case dto.BuildInCallFunctionCall, dto.BuildInCallToolUse:
		if functionName == "" {
			return
		}
		if _, reserved := reservedBillableToolNames[functionName]; reserved || operation_setting.IsBuiltInToolPriceKey(functionName) {
			return
		}
		if operation_setting.GetToolPriceForModel(functionName, info.GetBillingModelName()) <= 0 {
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

// SetBillableToolCount records an upstream-reported count for one tool-price
// key, replacing whatever per-item counting produced for the same key. Zero
// is a valid value: a vendor billing field that reports zero suppresses the
// protocol items counted under that key. Negative counts are ignored.
func (info *RelayInfo) SetBillableToolCount(name string, count int) {
	if info == nil || name == "" {
		return
	}
	if count < 0 {
		common.SysError(common.LogText("billable tool count ignored: tool=%s count=%d", name, count))
		return
	}
	info.ensureBuiltInTools()
	if existing, ok := info.ResponsesUsageInfo.BuiltInTools[name]; ok && existing != nil {
		existing.CallCount = count
		return
	}
	info.ResponsesUsageInfo.BuiltInTools[name] = &BuildInToolInfo{ToolName: name, CallCount: count}
}

// ApplyVendorToolUsage writes the vendor-billed counts of one upstream
// response into BuiltInTools. Protocol items counted earlier for the same key
// are replaced; keys the vendor does not mention are kept.
func (info *RelayInfo) ApplyVendorToolUsage(raw []byte) {
	if info == nil || info.VendorToolUsage == nil || len(raw) == 0 {
		return
	}
	for name, count := range info.VendorToolUsage(raw) {
		info.SetBillableToolCount(name, count)
	}
}

// RecordRequestInferredToolCall bills one call of key that the request itself
// proves: a forced hosted search on an upstream whose response reports no
// search usage. The record belongs to the current channel attempt;
// InitChannelMeta drops it when the request moves to another channel.
func (info *RelayInfo) RecordRequestInferredToolCall(key string) {
	if info == nil || key == "" {
		return
	}
	info.SetBillableToolCount(key, 1)
	info.requestInferredToolKey = key
}

func (info *RelayInfo) ensureBuiltInTools() {
	if info.ResponsesUsageInfo == nil {
		info.ResponsesUsageInfo = &ResponsesUsageInfo{}
	}
	if info.ResponsesUsageInfo.BuiltInTools == nil {
		info.ResponsesUsageInfo.BuiltInTools = make(map[string]*BuildInToolInfo)
	}
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
	info.ensureBuiltInTools()

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
