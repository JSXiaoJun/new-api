package setting

import (
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// GroupVendorIDs are the vendor categories the web console can show for a
// group when users create API keys.
var GroupVendorIDs = []string{"openai", "anthropic", "domestic", "google", "xai", "other"}

var groupVendor = map[string]string{}
var groupVendorMutex sync.RWMutex

func GroupVendor2JSONString() string {
	groupVendorMutex.RLock()
	defer groupVendorMutex.RUnlock()

	jsonBytes, err := common.Marshal(groupVendor)
	if err != nil {
		common.SysLog(common.LogText("error marshalling group vendors: %s", err.Error()))
	}
	return string(jsonBytes)
}

func parseGroupVendor(jsonStr string) (map[string]string, error) {
	parsed := make(map[string]string)
	if jsonStr == "" {
		return parsed, nil
	}
	if err := common.UnmarshalJsonStr(jsonStr, &parsed); err != nil {
		return nil, err
	}
	for group, vendor := range parsed {
		if !slices.Contains(GroupVendorIDs, vendor) {
			return nil, fmt.Errorf("unknown vendor %q for group %q", vendor, group)
		}
	}
	return parsed, nil
}

func ValidateGroupVendor(jsonStr string) error {
	_, err := parseGroupVendor(jsonStr)
	return err
}

func UpdateGroupVendorByJSONString(jsonStr string) error {
	parsed, err := parseGroupVendor(jsonStr)
	if err != nil {
		return err
	}
	groupVendorMutex.Lock()
	defer groupVendorMutex.Unlock()
	groupVendor = make(map[string]string, len(parsed))
	maps.Copy(groupVendor, parsed)
	return nil
}

// GetGroupVendor returns the vendor set for a group, or "" when none is set.
func GetGroupVendor(groupName string) string {
	groupVendorMutex.RLock()
	defer groupVendorMutex.RUnlock()
	return groupVendor[groupName]
}
