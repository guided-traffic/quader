package postgres

import (
	"github.com/guided-traffic/quader/internal/backup/tables"
	"github.com/guided-traffic/quader/internal/meta"
)

// Registration of the tables of 00092_signatures and 00098_signature_trust (signatures.md R-SIG-041, R-BAK-004):
//
//   - signature_objects, signatures, signature_attachments, artifact_origins: data. A signature object, its signature rows and their
//     verdicts cost a read of the content to make again, an attachment says where a signature reaches its subject, and an origin is a
//     snapshot of claims that may be gone from the issuance records (Q-SIG-40). After a restore, discovery names the signatures of
//     items the backup had no attachment for (R-SIG-011).
//   - signature_settings, signature_anchors, signature_blocks, signature_trust_roots: data. The switch, the anchors and the blocks are
//     security decisions (Q-SIG-39); a stored public root is what the installation trusted when the backup was made. The journal
//     re-applies what narrowed after the restore point (R-SIG-041, Q-SIG-42).
//   - signature_trust: transient. Every row is computed from the restored fields alone: after a restore no signature has a trust row,
//     and the trust job computes them all again, reading no artifact (R-SIG-012).
func init() {
	tables.Register("signature_objects", tables.Data, tables.Options{})
	tables.Register("signatures", tables.Data, tables.Options{})
	tables.Register("signature_attachments", tables.Data, tables.Options{})
	tables.Register("artifact_origins", tables.Data, tables.Options{})
	// The state of the background fetch of a remote's signatures (00096_signature_wire, R-SIG-026) is transient: after a restore the
	// next fill or refresh of a target queues its fetch again, which costs an upstream request and no data.
	tables.Register("signature_fetches", tables.Transient, tables.Options{})
	tables.Register("signature_settings", tables.Data, tables.Options{})
	tables.Register("signature_anchors", tables.Data, tables.Options{})
	tables.Register("signature_blocks", tables.Data, tables.Options{})
	tables.Register("signature_trust_roots", tables.Data, tables.Options{})
	tables.Register("signature_trust", tables.Transient, tables.Options{})
	// The downloads a gate refused (stage 3, R-SIG-073, R-SIG-076) are transient: a refusal is a recent fact, and the next refused
	// download writes it again.
	tables.Register("signature_refusals", tables.Transient, tables.Options{})
	// The releases of a repository's gate (stage 3, 00104, R-SIG-070, R-SIG-076) are data: a release is a security decision. One granted
	// after the restore point is lost, which narrows; a withdrawal is journaled and replayed.
	tables.Register("signature_releases", tables.Data, tables.Options{})

	// version is the ETag of the settings and of an anchor (R-API-012, R-SIG-019, R-SIG-034); a restore raises it like every
	// client-held revision (R-BAK-022), so a change prepared against the state before the restore is refused with 412.
	meta.RegisterRevisionColumn("signature_settings", "version")
	meta.RegisterRevisionColumn("signature_anchors", "version")
	// Not revision columns, and named so (00100_signature_trust_names): signature_settings.anchor_epoch and its copy
	// signature_trust.anchor_epoch are compared only with each other, and a restore leaves no trust row; a raised copy would make a
	// row look current for good. signature_trust_roots.tuf_version_number is the update repository's number: raised, the stored root
	// would look newer than every snapshot to come and never be refreshed (R-SIG-017).
}
