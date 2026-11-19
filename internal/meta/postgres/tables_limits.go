package postgres

import "github.com/guided-traffic/quader/internal/backup/tables"

// Registration of the tables of 00081_limits and 00082_usage (usage-and-limits.md, R-BAK-004):
//
//   - the settings of the download limits (profiles, rules, ceilings, project maxima, the mode and the block time) are data:
//     a restore brings back what the administrators set;
//   - the record of blocks and the five-minute and hourly usage rows are transient: a block's marker lives in Valkey and ends
//     on its own, and the single values hold personal data for 30 days at most (R-LIM-028), which a restore must not bring
//     back;
//   - the daily sums hold no person and are data.
func init() {
	tables.Register("limit_profile", tables.Data, tables.Options{})
	tables.Register("limit_assignment", tables.Data, tables.Options{})
	tables.Register("limit_ceiling", tables.Data, tables.Options{})
	tables.Register("limit_project_maximum", tables.Data, tables.Options{})
	tables.Register("limit_settings", tables.Data, tables.Options{})
	tables.Register("limit_block", tables.Transient, tables.Options{})
	tables.Register("usage_bucket", tables.Transient, tables.Options{})
	tables.Register("usage_limit_event", tables.Transient, tables.Options{})
	tables.Register("usage_daily_summary", tables.Data, tables.Options{})
}
