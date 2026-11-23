package postgres

import "github.com/guided-traffic/quader/internal/backup/tables"

// Registration of the table of 00109_storage_summary (operations-and-observability.md R-OPS-023, R-BAK-004):
//
//   - storage_summary: transient. The one row is derived from the other tables; a restored installation has none until the task
//     storage-summary runs, and GET /api/v1/storage/summary answers 404 until then.
func init() {
	tables.Register("storage_summary", tables.Transient, tables.Options{})
}
