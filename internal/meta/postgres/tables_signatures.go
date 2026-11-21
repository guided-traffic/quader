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
	// The state of the background fetch of a remote's signatures (00096_signature_wire, R-SIG-026) is transient: after a restore the
	// next fill or refresh of a target queues its fetch again, which costs an upstream request and no data.
	tables.Register("signature_fetches", tables.Transient, tables.Options{})
}
