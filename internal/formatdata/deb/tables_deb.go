package deb

import (
	"github.com/guided-traffic/quader/internal/backup/tables"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/secrets"
)

// SigningKeys is the sealed column of the private keys of the Debian signing keys (R-DEB-015, R-ARCH-021): the key is sealed with the
// stored-secret key ring, bound to its row (the id) and its column, so a sealed key copied to another row does not open. Registering it
// is what makes `quader admin secrets status`, the start-up canary and `secrets-reencrypt` cover it; a state `retired` has none (NULL).
var SigningKeys = secrets.Register("deb_signing_keys", "id", "private_key")

// Registrations of the tables the migrations 00060_deb, 00061_deb_signing_keys, 00062_deb_remote, 00064_deb_remote_files and 00114_remote_curation
// (deb_remote_descriptions, the long descriptions the search of R-DEB-036 reads) create, so that every table
// carries its export class from its migration on (R-BAK-004).
//
// Every table is data (R-DEB-032): a restore brings back the suites with their sequence numbers, the stored files' rows and
// coordinates, the generations that by-hash retention reads, the marks of withheld files and the signing keys with their sealed private
// keys. The five tables of a remote are its cache rows: a restore without remote caches deletes them with the cache (R-BAK-008,
// R-DEB-023, H-630); the purge that does so is PurgeRemoteCaches (restore_purge.go), and the descriptions go with their index by the
// foreign key's cascade.
//
// A table registered twice panics at start-up, so the registration lives here and nowhere else. This package is linked by the
// composition root and by cmd/quader/registry.go, which `quader migrate` needs to check the migrated schema against the registry.
func init() {
	for _, t := range []string{
		"deb_suites", "deb_binaries", "deb_coordinates", "deb_generations", "deb_tombstones",
		"deb_signing_keys",
		"deb_remote_releases", "deb_remote_files", "deb_remote_indexes", "deb_remote_entries", "deb_remote_descriptions",
	} {
		tables.Register(t, tables.Data, tables.Options{})
	}
	// What the reader of the data archives kept (R-DEB-039, migration 00132_deb_contents) is derived from the stored files: a backup
	// does not export it, and after a restore the task remote-describe, which a starting worker queues while a file is unread, reads
	// the files again.
	tables.Register("deb_contents", tables.Transient, tables.Options{})
	// Clients hold a suite's changeSeq for the index state read (R-DEB-014): a restore raises both numbers by 2^32 so that none
	// repeats with different content (R-ARCH-009, R-BAK-022, R-DEB-032).
	meta.RegisterRevisionColumn("deb_suites", "change_seq")
	meta.RegisterRevisionColumn("deb_suites", "built_seq")
}
