package postgres

import "github.com/guided-traffic/quader/internal/backup/tables"

// Registration of the tables of 00092_signatures (signatures.md R-SIG-041, R-BAK-004): every one is data. A signature object, its
// signature rows and their verdicts cost a read of the content to make again, an attachment says where a signature reaches its
// subject, and an origin is a snapshot of claims that may be gone from the issuance records (Q-SIG-40). After a restore, discovery
// names the signatures of items the backup had no attachment for (R-SIG-011).
func init() {
	tables.Register("signature_objects", tables.Data, tables.Options{})
	tables.Register("signatures", tables.Data, tables.Options{})
	tables.Register("signature_attachments", tables.Data, tables.Options{})
	tables.Register("artifact_origins", tables.Data, tables.Options{})
}
