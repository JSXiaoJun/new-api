package operation_setting

import (
	"time"

	"github.com/QuantumNous/new-api/setting/config"
)

// ImageLogSetting controls how long the drawing log keeps the public links of
// generated images.
//
// The links are issued by an image middleware with a short lifetime, so keeping
// the rows longer than the links live only grows the table. Expiry therefore
// follows the middleware, and the default matches the middleware default.
//
// The setting only covers the drawing log rows this gateway writes. The
// desensitized links themselves are served and expired by the middleware.
type ImageLogSetting struct {
	// RetentionHours is the drawing log retention window. 0 disables expiry.
	RetentionHours int `json:"retention_hours"`
	// CleanupIntervalMinutes is how often the expiry sweep runs. 0 disables it.
	CleanupIntervalMinutes int `json:"cleanup_interval_minutes"`
	// CleanupBatchSize bounds how many rows one sweep deletes per statement.
	CleanupBatchSize int `json:"cleanup_batch_size"`
}

var imageLogSetting = ImageLogSetting{
	RetentionHours:         24,
	CleanupIntervalMinutes: 30,
	CleanupBatchSize:       500,
}

func init() {
	config.GlobalConfig.Register("image_log_setting", &imageLogSetting)
}

func GetImageLogSetting() *ImageLogSetting {
	return &imageLogSetting
}

// Retention is the configured retention window. A non-positive value disables
// expiry.
func (s *ImageLogSetting) Retention() time.Duration {
	if s == nil || s.RetentionHours <= 0 {
		return 0
	}
	return time.Duration(s.RetentionHours) * time.Hour
}

// CleanupInterval is how often the expiry sweep runs.
func (s *ImageLogSetting) CleanupInterval() time.Duration {
	if s == nil || s.CleanupIntervalMinutes <= 0 {
		return 0
	}
	return time.Duration(s.CleanupIntervalMinutes) * time.Minute
}

// BatchSize is the per-statement delete bound, clamped so a bad config value
// cannot turn one sweep into an unbounded delete.
func (s *ImageLogSetting) BatchSize() int {
	if s == nil || s.CleanupBatchSize <= 0 {
		return 500
	}
	if s.CleanupBatchSize > 10000 {
		return 10000
	}
	return s.CleanupBatchSize
}

// RetentionCutoff returns the oldest creation time a drawing log row may have,
// or 0 when expiry is disabled.
func (s *ImageLogSetting) RetentionCutoff() int64 {
	if s == nil || s.RetentionHours <= 0 {
		return 0
	}
	return time.Now().Add(-time.Duration(s.RetentionHours) * time.Hour).Unix()
}
