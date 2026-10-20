package billing_setting

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const AbnormalRequestOptionPrefix = "abnormal_request_setting."

// AbnormalRequestSetting holds the thresholds that flag a user whose requests
// keep ending abnormally after the charge was waived or refunded. A user is
// flagged when any two of the three rules match. Flagging only marks the user
// for review; nothing is charged automatically.
// DB keys: abnormal_request_setting.<json field>
type AbnormalRequestSetting struct {
	// FlagHourlyCount is the number of user-caused abnormal requests within
	// one clock hour; requests whose client already received output count
	// twice.
	FlagHourlyCount int `json:"flag_hourly_count"`
	// FlagRatioPercent is the share of user-caused abnormal requests among
	// all requests of that hour, checked once the hour has FlagMinRequests.
	FlagRatioPercent int `json:"flag_ratio_percent"`
	FlagMinRequests  int `json:"flag_min_requests"`
	// FlagForgoneQuota is the uncollected quota within the reviewed period.
	FlagForgoneQuota int `json:"flag_forgone_quota"`
}

var abnormalRequestSetting = AbnormalRequestSetting{
	FlagHourlyCount:  10,
	FlagRatioPercent: 80,
	FlagMinRequests:  20,
	// About ¥1 at the default 7.3 CNY/USD exchange rate.
	FlagForgoneQuota: 68_500,
}

func init() {
	config.GlobalConfig.Register("abnormal_request_setting", &abnormalRequestSetting)
}

func GetAbnormalRequestSetting() AbnormalRequestSetting {
	return abnormalRequestSetting
}

// ValidateAbnormalRequestOption accepts only the declared thresholds with a
// value inside their range.
func ValidateAbnormalRequestOption(key, value string) error {
	field, ok := strings.CutPrefix(key, AbnormalRequestOptionPrefix)
	if !ok {
		return nil
	}
	limits := map[string][2]int{
		"flag_hourly_count":  {1, 1_000_000},
		"flag_ratio_percent": {1, 100},
		"flag_min_requests":  {1, 1_000_000},
		"flag_forgone_quota": {1, common.MaxQuota},
	}
	limit, known := limits[field]
	if !known {
		return fmt.Errorf("unknown abnormal request option %q", key)
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < limit[0] || number > limit[1] {
		return fmt.Errorf("%s must be an integer between %d and %d", key, limit[0], limit[1])
	}
	return nil
}

// StrictBillingUsersOptionKey stores the users whose abnormal requests are
// always charged by usage, whatever the abnormal billing switches say. It is a
// JSON array of user ids kept apart from user settings, which users rewrite.
const StrictBillingUsersOptionKey = "AbnormalStrictBillingUserIds"

var strictBillingUsers atomic.Pointer[[]int]

// ParseStrictBillingUsers returns the sorted, de-duplicated user ids.
func ParseStrictBillingUsers(value string) ([]int, error) {
	ids := []int{}
	if strings.TrimSpace(value) != "" {
		if err := common.UnmarshalJsonStr(value, &ids); err != nil {
			return nil, fmt.Errorf("strict billing users must be a JSON array of user ids: %w", err)
		}
	}
	for _, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf("invalid strict billing user id %d", id)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids), nil
}

func LoadStrictBillingUsers(value string) error {
	ids, err := ParseStrictBillingUsers(value)
	if err != nil {
		return err
	}
	strictBillingUsers.Store(&ids)
	return nil
}

// StrictBillingUsers returns a copy of the strict billing user ids.
func StrictBillingUsers() []int {
	if ids := strictBillingUsers.Load(); ids != nil {
		return slices.Clone(*ids)
	}
	return []int{}
}

func IsStrictBillingUser(userId int) bool {
	ids := strictBillingUsers.Load()
	if ids == nil || userId <= 0 {
		return false
	}
	_, found := slices.BinarySearch(*ids, userId)
	return found
}
