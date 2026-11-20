//go:build integration

package upstream

import (
	"slices"
	"strings"
	"testing"
)

// purged and kept are the tables whose rows reference a repository by a cascading foreign key, as the purge of a retired well-known
// repository treats them (upstream_repository_purge of migration 00086, Q-REPO-376): content goes, configuration stays. A table a
// later migration adds fails this test until it is put into one of the lists and, when purged, into the function.
var (
	purged = []string{
		"ansible_collection_versions", "ansible_doc_fetches", "ansible_import_tasks", "ansible_tombstones", "ansible_upstream_versions",
		"ansible_version_docs", "deb_binaries", "deb_remote_files", "deb_remote_indexes", "deb_remote_releases", "deb_suites", "deb_tombstones",
		"items", "mirror_pending", "mirror_state", "npm_packages", "npm_remote_packages", "npm_tombstones", "nuget_symbol_file",
		"nuget_tombstone", "nuget_version", "oci_manifests", "oci_tombstones", "packages", "remote_cache_entries",
		"remote_cache_invalidations", "remote_upstream_state", "remote_url_map", "repository_usage", "upload_sessions", "usage_bucket",
		"usage_daily_summary", "usage_limit_event", "vuln_finding",
	}
	kept = []string{
		"authz_policy_selector", "deb_signing_keys", "endpoint_exposure_rule", "limit_assignment", "limit_ceiling", "namespace_claims",
		"repository_secrets", "service_account", "virtual_deploy_targets", "virtual_members", "virtual_pin_rules",
	}
)

// TestR_REPO_067_ThePurgeOfARetiredRepository: the purge removes what the removal of the row would have taken with it and keeps the
// rows, and it refuses a repository that is not retired (Q-REPO-376). The lists above hold the schema's foreign keys.
func TestR_REPO_067_ThePurgeOfARetiredRepository(t *testing.T) {
	f := newFixture(t)
	t.Run("Q-REPO-376 every table that references a repository by a cascading key is purged or kept by a decision", func(t *testing.T) {
		got := strings.Split(f.e.Text(`SELECT string_agg(DISTINCT c.conrelid::regclass::text, ',' ORDER BY c.conrelid::regclass::text)
		                                FROM pg_constraint c WHERE c.contype = 'f' AND c.confrelid = 'repositories'::regclass AND c.confdeltype = 'c'`), ",")
		for i := range got {
			got[i] = strings.TrimPrefix(got[i], "quader.")
		}
		want := slices.Sorted(slices.Values(append(slices.Clone(purged), kept...)))
		if !slices.Equal(got, want) {
			t.Fatalf("tables with a cascading key to repositories\n got %v\nwant %v", got, want)
		}
		body := f.e.Text("SELECT pg_get_functiondef('upstream_repository_purge(bigint)'::regprocedure)")
		for _, table := range purged {
			if !strings.Contains(body, "DELETE FROM "+table+" ") {
				t.Errorf("upstream_repository_purge does not purge %s", table)
			}
		}
	})
	id := f.id("galaxy")
	f.e.Exec(`INSERT INTO remote_url_map (repo_id, path, upstream_url) VALUES ($1, 'x', 'https://galaxy.ansible.com/api/x')`, id)
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
		if f.id("galaxy") != id || f.e.Number("SELECT count(*) FROM virtual_members WHERE member_id = $1", id) != 1 {
			t.Error("the tombstone or the member row went")
		}
	})
}
