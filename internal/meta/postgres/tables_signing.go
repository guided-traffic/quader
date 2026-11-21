package postgres

import "github.com/guided-traffic/quader/internal/backup/tables"

// Registration of the tables of 00101_signing_identity (signatures.md R-SIG-059, R-BAK-004):
//
//   - signing_identities: data. An uploaded identity's key travels sealed, so a restore needs the key ring, which the restore Job checks
//     (R-BAK-020); the key of the Helm values is in no backup, it is in its Secret. The journal re-applies a removal and a retirement made
//     after the restore point, so a withdrawn identity does not come back (Q-SIG-74).
//   - signing_work: data. The work that waited when the backup was made; discovery signs and attests what the restored rows lack.
//   - signing_settings: data. Its one row is updated in place and must exist; the refusal of the files is recorded again at the next
//     look of a worker whose files still fail.
//
// And of 00105_receipt_attestations (R-SIG-059):
//
//   - signing_attestations: data. The index of the attestations Quader issued, restored with the signatures and the attachments it
//     points at (stage 1's tables are data too); discovery makes again what a restore left without one.
func init() {
	tables.Register("signing_identities", tables.Data, tables.Options{})
	tables.Register("signing_work", tables.Data, tables.Options{})
	tables.Register("signing_settings", tables.Data, tables.Options{})
	tables.Register("signing_attestations", tables.Data, tables.Options{})
}
