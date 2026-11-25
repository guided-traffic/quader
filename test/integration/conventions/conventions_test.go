//go:build integration

// Package conventions holds the checks of a list or a convention that need the migrated schema and no replica: the tables a
// purge of a retired repository treats, the registry of revision columns. Each reads the catalog of one clone of the template
// (testenv.MigratedDatabase) and runs in milliseconds; the area builds and runs in seconds, so every builder and every landing
// runs it beside the areas of their change (docs/developer/build-and-test.md). The checks stood in the areas upstream and
// postgres until 2026-11-23, where a landing whose builder had run other areas broke them in the pipeline (Q-ARCH-701). The
// conventions that need no database at all are unit tests (internal/formats/all, internal/app).
package conventions

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

func TestMain(m *testing.M) { os.Exit(testenv.Main(m)) }

// schema opens a migrated database of the test's own, with the search path at its schema.
func schema(t *testing.T) *pgx.Conn {
	t.Helper()
	name := "quader_conv_" + testenv.RandHex(5)
	testenv.MigratedDatabase(t, name)
	conn, err := pgx.Connect(context.Background(), testenv.PgDSN(t, name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	if _, err := conn.Exec(context.Background(), "SET search_path TO quader"); err != nil {
		t.Fatal(err)
	}
	return conn
}

func text(t *testing.T, conn *pgx.Conn, sql string) string {
	t.Helper()
	var s string
	if err := conn.QueryRow(context.Background(), sql).Scan(&s); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return s
}

// purged and kept are the tables whose rows reference a repository by a cascading foreign key, as the purge of a retired well-known
// repository treats them (upstream_repository_purge of migration 00086, replaced last by 00120, Q-REPO-376, Q-CUR-111): content goes,
// configuration stays. A table a later migration adds fails this test until it is put into one of the lists and, when purged, into the function.
// What the purge does with the rows is TestR_REPO_067_ThePurgeOfARetiredRepository's (area upstream).
var (
	purged = []string{
		"ansible_collection_versions", "ansible_doc_fetches", "ansible_import_tasks", "ansible_tombstones", "ansible_upstream_versions",
		"ansible_version_docs", "deb_binaries", "deb_remote_files", "deb_remote_indexes", "deb_remote_releases", "deb_suites", "deb_tombstones",
		"items", "mirror_pending", "mirror_state", "npm_packages", "npm_remote_packages", "npm_tombstones", "nuget_symbol_file",
		"nuget_tombstone", "nuget_version", "oci_manifests", "oci_tombstones", "packages", "remote_cache_entries", "remote_download",
		"remote_download_bulks", "remote_download_limits", "remote_examinations",
		"remote_cache_invalidations", "remote_upstream_state", "remote_url_map", "repository_usage", "signature_attachments", "signature_fetches",
		"signature_refusals",
		"upload_sessions", "upstream_download_finished", "usage_bucket", "usage_daily_summary", "usage_limit_event", "vuln_finding",
	}
	kept = []string{
		"authz_policy_selector", "deb_signing_keys", "endpoint_exposure_rule", "limit_assignment", "limit_ceiling", "namespace_claims",
		"remote_delivery_requests", "remote_delivery_rules", "remote_download_syncs", "repository_secrets", "service_account", "signature_releases",
		"virtual_deploy_targets", "virtual_members", "virtual_pin_rules",
	}
)

// TestR_REPO_067_EveryTableThatReferencesARepositoryIsPurgedOrKeptByADecision: the schema's cascading keys to repositories are the
// two lists above, and the purge's function deletes from every purged one (Q-REPO-376).
func TestR_REPO_067_EveryTableThatReferencesARepositoryIsPurgedOrKeptByADecision(t *testing.T) {
	conn := schema(t)
	got := strings.Split(text(t, conn, `SELECT string_agg(DISTINCT c.conrelid::regclass::text, ',' ORDER BY c.conrelid::regclass::text)
	                                    FROM pg_constraint c WHERE c.contype = 'f' AND c.confrelid = 'repositories'::regclass AND c.confdeltype = 'c'`), ",")
	for i := range got {
		got[i] = strings.TrimPrefix(got[i], "quader.")
	}
	want := slices.Sorted(slices.Values(append(slices.Clone(purged), kept...)))
	if !slices.Equal(got, want) {
		t.Fatalf("tables with a cascading key to repositories\n got %v\nwant %v", got, want)
	}
	body := text(t, conn, "SELECT pg_get_functiondef('upstream_repository_purge(bigint)'::regprocedure)")
	for _, table := range purged {
		if !strings.Contains(body, "DELETE FROM "+table+" ") {
			t.Errorf("upstream_repository_purge does not purge %s", table)
		}
	}
}

// TestRevisionColumns: every bigint rev_seq, revision, epoch and *_version column of the schema is in the revision-column registry
// (meta.RegisterRevisionColumn). That the check finds a column a migration adds is the area postgres's (TestRevisionColumns there).
func TestRevisionColumns(t *testing.T) {
	conn := schema(t)
	missing, err := postgres.CheckRevisionColumns(context.Background(), conn, "quader", meta.DefaultRevisionRegistry())
	if err != nil || len(missing) != 0 {
		t.Fatalf("unregistered: %v, %v", missing, err)
	}
	if !meta.IsRevisionColumn("packages", "rev_seq") || !meta.IsRevisionColumn("cluster_epoch", "epoch") {
		t.Fatal("packages.rev_seq and cluster_epoch.epoch are the first revision columns")
	}
}
