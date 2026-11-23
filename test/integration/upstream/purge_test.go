//go:build integration

package upstream

import (
	"strings"
	"testing"
)

// TestR_REPO_067_ThePurgeOfARetiredRepository: the purge removes what the removal of the row would have taken with it and keeps the
// rows, and it refuses a repository that is not retired (Q-REPO-376). Which tables it treats, purged or kept, is held against the
// schema's foreign keys by the area conventions (TestR_REPO_067_EveryTableThatReferencesARepositoryIsPurgedOrKeptByADecision).
func TestR_REPO_067_ThePurgeOfARetiredRepository(t *testing.T) {
	f := newFixture(t)
	id := f.id("galaxy")
	f.e.Exec(`INSERT INTO remote_url_map (repo_id, path, upstream_url) VALUES ($1, 'x', 'https://galaxy.ansible.com/api/x')`, id)
	// A signature of a remote's document: its attachment names no item, so only the purge's own delete removes it (R-SIG-004).
	f.e.Exec(`INSERT INTO signature_objects (sha256, scheme, raw, size) VALUES (decode(repeat('ab', 32), 'hex'), 'openpgp', '\x01', 1)`)
	sig := f.e.Number(`INSERT INTO signatures (object_sha256, subject_sha256, role)
	                   VALUES (decode(repeat('ab', 32), 'hex'), decode(repeat('cd', 32), 'hex'), 'registry') RETURNING id`)
	f.e.Exec(`INSERT INTO signature_attachments (repo_id, signature_id, location, source) VALUES ($1, $2, 'x#/signatures/0', 'document')`, id, sig)
	f.listVirtual("web", "galaxy-all", "ansible", "galaxy")
	t.Run("Q-REPO-376 a repository that is not retired is not purged", func(t *testing.T) {
		if err := f.e.TryExec("SELECT upstream_repository_purge($1)", id); err == nil || !strings.Contains(err.Error(), "AI010") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("Q-REPO-376 the purge removes the content and keeps the rows and the member that names it", func(t *testing.T) {
		f.e.Exec("UPDATE upstream_repository SET retired_at = now() WHERE repository_id = $1", id)
		f.e.Exec("SELECT upstream_repository_purge($1)", id)
		if f.e.Number("SELECT count(*) FROM remote_url_map WHERE repo_id = $1", id) != 0 {
			t.Error("the URL mapping stayed")
		}
		if f.e.Number("SELECT count(*) FROM signature_attachments WHERE repo_id = $1", id) != 0 {
			t.Error("the signature attachment stayed")
		}
		// The signature left without an attachment is the store's orphan, removed by the job of R-SIG-011 like any other, not by the purge.
		if f.e.Number("SELECT count(*) FROM signatures WHERE id = $1", sig) != 1 {
			t.Error("the purge removed the signature, which belongs to the installation, not to the repository")
		}
		if f.id("galaxy") != id || f.e.Number("SELECT count(*) FROM virtual_members WHERE member_id = $1", id) != 1 {
			t.Error("the tombstone or the member row went")
		}
	})
}
