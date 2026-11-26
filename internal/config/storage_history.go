package config

import "time"

// StorageHistory configures the history of the stored content (storage.md R-STOR-108 to R-STOR-110): the findings of damaged copies,
// the intervals of a connection a replica could not reach or whose start check failed, and the hourly counts of what the reads and
// writes absorbed. The task storage-history deletes what is older than the retention (Q-STOR-521); an open finding stays whatever
// its age. The key has no line in the chart (Q-HA-506): its default serves, and the configuration's escape hatch sets another.
type StorageHistory struct {
	Retention time.Duration `yaml:"retention" default:"9600h" validate:"min=2208h,max=26280h" doc:"How long the history of the stored content is kept: 400 days by default, at least 92 days so that the storage page's 90-day period always has its data, at most three years (R-STOR-110)."`
}
