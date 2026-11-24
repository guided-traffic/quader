package postgres

import "github.com/guided-traffic/quader/internal/backup/tables"

// Registration of the tables of 00118_upstream_downloads (operations-and-observability.md R-OPS-050, R-OPS-051): both transient. The
// live rows describe replicas that do not exist after a restore, and the last 100 finished downloads are a live view, no record
// (R-BAK-004, Q-OPS-422).
func init() {
	tables.Register("upstream_download_live", tables.Transient, tables.Options{})
	tables.Register("upstream_download_finished", tables.Transient, tables.Options{})
}
