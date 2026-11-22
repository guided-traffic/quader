package postgres

import "github.com/guided-traffic/quader/internal/backup/tables"

// Registration of the table of 00107_storage_keys (storage.md R-STOR-083, R-STOR-047, R-BAK-004):
//
//   - storage_keys: data. Every backup holds the storage keys and name keys, sealed as they lie, so a restore needs the key ring that
//     sealed them, which the restore Job checks (R-BAK-020); without the rows the objects of an encrypted connection do not open. No
//     foreign key to storage_connections, which is transient: a key outlives its connection (Q-STOR-309).
func init() {
	tables.Register("storage_keys", tables.Data, tables.Options{})
}
