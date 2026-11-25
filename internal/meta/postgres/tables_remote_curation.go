package postgres

import (
	"github.com/guided-traffic/quader/internal/backup/tables"
	"github.com/guided-traffic/quader/internal/meta"
)

// Registration of the tables of 00114_remote_curation (remote-curation.md R-CUR-021, R-BAK-004). Every table is data:
//
//   - remote_delivery_rules: a rule is a decision of a person about what a remote delivers. One added after the restore point is lost,
//     which a narrowing's journal entry re-applies (repository.narrowed with the part rules, R-CUR-006, R-REPO-061).
//   - remote_delivery_requests, remote_delivery_request_askers: the requests and who asked; a decider's list survives a restore.
//   - remote_download_bulks, remote_download: the queue, so that a backup holds what was waiting and what failed and why; a restore
//     sets every running row waiting (R-CUR-021).
//   - remote_examinations: which stored versions are held for examination; without it the retention could not tell them from what a
//     rule refuses after it was delivered (R-CUR-016). A discard is journaled and replayed.
//
// deb_remote_descriptions, of the same migration, is a Debian remote's cache row and is registered with the format's tables
// (internal/formatdata/deb/tables_deb.go).
func init() {
	for _, t := range []string{
		"remote_delivery_rules", "remote_delivery_requests", "remote_delivery_request_askers",
		"remote_download_bulks", "remote_download", "remote_examinations",
	} {
		tables.Register(t, tables.Data, tables.Options{})
	}
	// remote_download_limits (00120): an upstream's rate limit that holds a repository's queue (R-CUR-011, Q-CUR-173) is a live state
	// of an upstream, no decision: it is empty after a restore, and the first row that meets the limit again writes it anew.
	tables.Register("remote_download_limits", tables.Transient, tables.Options{})
	// The revision of a repository's rule set is the key and the validator of a filtered document (R-CUR-003) and an answer of the
	// rules' API: a restore raises it like every client-held revision, so that no document rendered under rules after the restore
	// point is answered 304 under the restored ones (R-BAK-022).
	meta.RegisterRevisionColumn("repositories", "delivery_rules_revision")
}
