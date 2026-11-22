package blob

import "github.com/guided-traffic/quader/internal/backup/tables"

// The tables this package owns (R-BAK-004, R-STOR-047). Migration 00002
// creates the first two; the registrations moved here from internal/meta/postgres.
func init() {
	// Only rows that the snapshot references are exported.
	tables.Register("blobs", tables.Blobs, tables.Options{})
	// Candidates only; a restored database has no unreferenced blob rows.
	tables.Register("blob_gc_queue", tables.Transient, tables.Options{})
	// The connections and the copies (00106, R-STOR-052): they describe the objects of the source's connections; a restore
	// writes its own copies and its fix-up records them (backup-and-restore.md R-BAK-030).
	tables.Register("storage_connections", tables.Transient, tables.Options{})
	tables.Register("blob_copies", tables.Transient, tables.Options{})
	tables.Register("blob_copy_queue", tables.Transient, tables.Options{})
}
