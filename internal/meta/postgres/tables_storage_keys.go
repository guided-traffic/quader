package postgres

import "github.com/guided-traffic/quader/internal/backup/tables"

// Registration of the tables of 00107_storage_keys and 00112_storage_keys_destroyed (storage.md R-STOR-083, R-STOR-097, R-STOR-103,
// R-STOR-047, R-BAK-004):
//
//   - storage_keys: data. Every backup holds the storage keys and name keys, sealed as they lie, so a restore needs the key ring that
//     sealed them, which the restore Job checks (R-BAK-020); without the rows the objects of an encrypted connection do not open. No
//     foreign key to storage_connections, which is transient: a key outlives its connection (Q-STOR-309). A destroyed key keeps its
//     row without its sealed value, so the mark survives a restore and keeps the connection id retired (R-STOR-102).
//   - storage_key_exports: data. The record of every export of the storage keys, which a destruction reports, also after the audit
//     trail's retention and after a restore (Q-STOR-408).
func init() {
	tables.Register("storage_keys", tables.Data, tables.Options{})
	tables.Register("storage_key_exports", tables.Data, tables.Options{})
}
