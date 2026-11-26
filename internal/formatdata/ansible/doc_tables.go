package ansible

import "github.com/guided-traffic/quader/internal/backup/tables"

// The five tables of the documentation of collections (migrations ansible_doc_tables, ansible_doc_fetches and ansible_changelogs,
// R-ANS-033, R-ANS-053) are derived data and no source of truth: a backup does not export them (R-BAK-004). A restore enqueues the
// reindex, which rebuilds a local repository's rows and the changelogs of both classes from the archives and a remote's documentation from
// the stored documents (R-ANS-044). They are registered Transient, the class that is not exported and is empty after a restore.
// Registered here and nowhere else (a table registered twice panics at start-up).
func init() {
	for _, t := range []string{"ansible_version_docs", "ansible_doc_entries", "ansible_doc_files", "ansible_doc_fetches", "ansible_changelogs"} {
		tables.Register(t, tables.Transient, tables.Options{})
	}
}
