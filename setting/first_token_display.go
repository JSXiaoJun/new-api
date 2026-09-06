package setting

import (
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const (
	FirstTokenDisplayRulesOptionKey   = "FirstTokenDisplayRules"
	DefaultFirstTokenDisplayRulesJSON = `{"enabled":true,"rules":[{"id":"over-9-half","comparison":"gt","threshold":9,"operation":"multiply","value":0.5},{"id":"from-5-subtract-4","comparison":"gte","threshold":5,"operation":"subtract","value":4},{"id":"from-3-subtract-2","comparison":"gte","threshold":3,"operation":"subtract","value":2}]}`
	maxFirstTokenDisplayRules         = 50
	maxFirstTokenDisplaySeconds       = 86400
)

type firstTokenDisplayConfig struct {
	Enabled *bool                    `json:"enabled"`
	Rules   *[]firstTokenDisplayRule `json:"rules"`
}

type firstTokenDisplayRule struct {
	ID         string  `json:"id"`
	Comparison string  `json:"comparison"`
	Threshold  float64 `json:"threshold"`
	Operation  string  `json:"operation"`
	Value      float64 `json:"value"`
}

func ValidateFirstTokenDisplayRulesJSON(value string) error {
	var config firstTokenDisplayConfig
	if err := common.UnmarshalJsonStr(value, &config); err != nil {
		return fmt.Errorf("invalid first-token display rules: %w", err)
	}
	if config.Enabled == nil || config.Rules == nil {
		return fmt.Errorf("first-token display rules must include enabled and rules")
	}
	rules := *config.Rules
	if len(rules) > maxFirstTokenDisplayRules {
		return fmt.Errorf("first-token display rules cannot exceed %d entries", maxFirstTokenDisplayRules)
	}

	ids := make(map[string]struct{}, len(rules))
	conditions := make(map[string]struct{}, len(rules))
	for index, rule := range rules {
		if strings.TrimSpace(rule.ID) == "" || len(rule.ID) > 100 {
			return fmt.Errorf("first-token display rule %d has an invalid id", index+1)
		}
		if _, exists := ids[rule.ID]; exists {
			return fmt.Errorf("first-token display rule %d has a duplicate id", index+1)
		}
		ids[rule.ID] = struct{}{}

		if rule.Comparison != "gt" && rule.Comparison != "gte" {
			return fmt.Errorf("first-token display rule %d has an invalid comparison", index+1)
		}
		if !isFiniteFirstTokenDisplayNumber(rule.Threshold) || rule.Threshold < 0 || rule.Threshold > maxFirstTokenDisplaySeconds {
			return fmt.Errorf("first-token display rule %d has an invalid threshold", index+1)
		}
		conditionKey := fmt.Sprintf("%s:%g", rule.Comparison, rule.Threshold)
		if _, exists := conditions[conditionKey]; exists {
			return fmt.Errorf("first-token display rule %d duplicates another condition", index+1)
		}
		conditions[conditionKey] = struct{}{}

		if rule.Operation != "add" && rule.Operation != "subtract" && rule.Operation != "multiply" {
			return fmt.Errorf("first-token display rule %d has an invalid operation", index+1)
		}
		if !isFiniteFirstTokenDisplayNumber(rule.Value) || rule.Value < 0 || rule.Value > maxFirstTokenDisplaySeconds {
			return fmt.Errorf("first-token display rule %d has an invalid value", index+1)
		}
	}

	return nil
}

func isFiniteFirstTokenDisplayNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
