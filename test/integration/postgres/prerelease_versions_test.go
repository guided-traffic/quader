//go:build integration

package postgres

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/guided-traffic/quader/internal/meta/postgres"
)

// TestR_HA_020_ThePrereleaseStepRefillsTheRowsWhoseAnswerChanged: the step that has a format say which version is a pre-release beside
// its order (api-and-ui.md Q-API-433, ADR-282) migrates a database the release before it filled (R-HA-020): a local image tag and an
// Ansible collection version that are pre-releases, stored as none, lose their key and flag, which the background migration
// version_sort_key fills again with the format's answer (Q-HA-492, Q-API-430); an image release, an Ansible version with build metadata
// and an npm pre-release keep theirs; a remote's tag entry gets the flag the format gives its tag. One tiny row of each on a database of
// its own; about 3 s.
func TestR_HA_020_ThePrereleaseStepRefillsTheRowsWhoseAnswerChanged(t *testing.T) {
	st := getStack(t)
	name, admin := st.newDatabase(t)
	cfg := st.config(t, name)
	old := fstest.MapFS{}
	emb := postgres.EmbeddedFS()
	entries, err := fs.ReadDir(emb, ".")
	if err != nil {
		t.Fatal(err)
	}
	step := ""
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "_version_prerelease.sql") {
			step = e.Name()[:5]
		}
	}
	if step == "" {
		t.Fatal("no step *_version_prerelease.sql")
	}
	later := 0
	for _, e := range entries {
		if e.Name()[:5] >= step {
			later++
			continue
		}
		data, err := fs.ReadFile(emb, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		old[e.Name()] = &fstest.MapFile{Data: data}
	}
	if _, err := postgres.Migrate(ctxT(t, 60*time.Second), cfg, opts(old)); err != nil {
		t.Fatal(err)
	}
	mustExec(t, admin, "INSERT INTO quader.project (id, key, display_name) VALUES (gen_random_uuid(), 'it', 'it')")
	for _, r := range [][4]string{
		{"oci-local", "oci", "local", "acme/app"},
		{"oci-remote", "oci", "remote", "acme/app"},
		{"ansible-local", "ansible", "local", "acme.tools"},
		{"npm-local", "npm", "local", "left-pad"},
	} {
		mustExec(t, admin, "INSERT INTO quader.repositories (key, format, class, project_id) VALUES ($1, $2, $3, (SELECT id FROM quader.project WHERE key = 'it'))",
			r[0], r[1], r[2])
		mustExec(t, admin, "INSERT INTO quader.packages (repo_id, name, display_name) SELECT id, $2, $2 FROM quader.repositories WHERE key = $1", r[0], r[3])
	}
	// The rows of the release before: the flag the exposure adapter gave, false for every OCI and Ansible version.
	write := func(repo, version string, pre bool) {
		t.Helper()
		mustExec(t, admin, `INSERT INTO quader.package_versions (package_id, version, display_version, sort_key, prerelease)
		                    SELECT p.id, $2, $2, '\x01'::bytea, $3 FROM quader.packages p JOIN quader.repositories r ON r.id = p.repo_id
		                     WHERE r.key = $1`, repo, version, pre)
	}
	write("oci-local", "2.0.0-rc.1", false)
	write("oci-local", "1.0", false)
	write("oci-local", "1.0-alpine", false)
	write("ansible-local", "1.0.0-beta.1", false)
	write("ansible-local", "1.0.0+build.5", false)
	write("npm-local", "1.0.0-rc.1", true)
	sum := bytes.Repeat([]byte{7}, 32)
	mustExec(t, admin, `INSERT INTO quader.blobs (sha256, sha1, md5, sha512, size) VALUES ($1, $2, $3, $4, 10)`,
		sum, bytes.Repeat([]byte{1}, 20), bytes.Repeat([]byte{2}, 16), bytes.Repeat([]byte{3}, 64))
	for _, tag := range []string{"2.0-rc.1", "1.0"} {
		mustExec(t, admin, `INSERT INTO quader.remote_cache_entries (repo_id, path, kind, blob_sha256, upstream_url, fetched_at, validated_at, expires_at, last_used_at)
		                    SELECT id, 'acme/app/_tags/' || $2, 'metadata', $1, 'https://upstream.example/v2/acme/app/manifests/' || $2, now(), now(), now(), now()
		                      FROM quader.repositories WHERE key = 'oci-remote'`, sum, tag)
	}

	res, err := postgres.Migrate(ctxT(t, 60*time.Second), cfg, opts(nil))
	if err != nil || len(res.Applied) != later {
		t.Fatalf("the steps from %s on a database that holds versions: %+v, %v", step, res, err)
	}
	if got := scalar[string](t, admin, `SELECT string_agg(version, ',' ORDER BY version) FROM quader.package_versions
	                                     WHERE sort_key IS NULL AND prerelease IS NULL`); got != "1.0.0-beta.1,2.0.0-rc.1" {
		t.Errorf("the rows without a key after the step: %q, want the Ansible and the OCI pre-release alone", got)
	}
	if got := scalar[string](t, admin, `SELECT string_agg(tag || '=' || tag_prerelease::text, ',' ORDER BY tag) FROM quader.remote_cache_entries`); got != "1.0=false,2.0-rc.1=true" {
		t.Errorf("the flags of the remote's tag entries: %q", got)
	}
	if n := scalar[int](t, admin, `SELECT count(*) FROM pg_indexes WHERE indexname IN ('remote_cache_entries_tag_latest_release', 'remote_cache_entries_tag_latest_prerelease')`); n != 2 {
		t.Errorf("%d of the two indexes of a remote's latest tag", n)
	}
}
