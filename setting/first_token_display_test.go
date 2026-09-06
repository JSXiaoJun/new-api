package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateFirstTokenDisplayRulesJSONAcceptsDefaultAndCustomRules(t *testing.T) {
	require.NoError(t, ValidateFirstTokenDisplayRulesJSON(DefaultFirstTokenDisplayRulesJSON))
	require.NoError(t, ValidateFirstTokenDisplayRulesJSON(
		`{"enabled":false,"rules":[{"id":"custom","comparison":"gte","threshold":2.5,"operation":"add","value":1.25}]}`,
	))
}

func TestValidateFirstTokenDisplayRulesJSONRejectsUnsafeOrAmbiguousRules(t *testing.T) {
	tests := map[string]string{
		"invalid json":        `{`,
		"missing enabled":     `{"rules":[]}`,
		"missing rules":       `{"enabled":true}`,
		"missing id":          `{"enabled":true,"rules":[{"comparison":"gt","threshold":9,"operation":"multiply","value":0.5}]}`,
		"invalid comparison":  `{"enabled":true,"rules":[{"id":"a","comparison":"lt","threshold":9,"operation":"subtract","value":1}]}`,
		"negative threshold":  `{"enabled":true,"rules":[{"id":"a","comparison":"gte","threshold":-1,"operation":"subtract","value":1}]}`,
		"invalid operation":   `{"enabled":true,"rules":[{"id":"a","comparison":"gte","threshold":3,"operation":"divide","value":2}]}`,
		"negative value":      `{"enabled":true,"rules":[{"id":"a","comparison":"gte","threshold":3,"operation":"subtract","value":-2}]}`,
		"duplicate condition": `{"enabled":true,"rules":[{"id":"a","comparison":"gte","threshold":3,"operation":"subtract","value":2},{"id":"b","comparison":"gte","threshold":3,"operation":"add","value":1}]}`,
	}

	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, ValidateFirstTokenDisplayRulesJSON(value))
		})
	}
}
