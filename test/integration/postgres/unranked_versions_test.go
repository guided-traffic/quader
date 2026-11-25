//go:build integration

package postgres

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/guided-traffic/quader/internal/formats/versionkey"
	"github.com/guided-traffic/quader/internal/meta/postgres"
)

// TestR_HA_020_TheUnrankedVersionsStepPassesOnADatabaseOfThePreviousRelease: the step that makes an image tag that is no version a
// string its format does not rank (api-and-ui.md Q-API-431, formats/oci.md Q-OCI-269) migrates a database the release before it filled
// (R-HA-020): a local OCI tag of the old class (0x01 0x00) loses its key and flag, which the background migration version_sort_key fills
// again with the format's (Q-HA-492, Q-API-430), a version tag and an npm version keep theirs, and a remote's tag entry of the old class is
// written again, so its generated key is the new function's. One tiny row of each on a database of its own; about 3 s.
func TestR_HA_020_TheUnrankedVersionsStepPassesOnADatabaseOfThePreviousRelease(t *testing.T) {
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
		if strings.HasSuffix(e.Name(), "_unranked_versions.sql") {
			step = e.Name()[:5]
		}
	}
	later := 0
	for _, e := range entries {
		if step == "" || e.Name()[:5] >= step {
			later++
			continue
		}
		data, err := fs.ReadFile(emb, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		old[e.Name()] = &fstest.MapFile{Data: data}
	}
	if step == "" {
		t.Fatal("no step *_unranked_versions.sql")
	}
	if _, err := postgres.Migrate(ctxT(t, 60*time.Second), cfg, opts(old)); err != nil {
		t.Fatal(err)
	}
	// The rows of the release before: an OCI image with the tags latest (the old class 0x01 0x00) and 1.0, an npm version, and a remote's
	// tag entry main, whose generated key the old function computed.
	mustExec(t, admin, "INSERT INTO quader.project (id, key, display_name) VALUES (gen_random_uuid(), 'it', 'it')")
	for _, r := range [][3]string{{"oci-local", "oci", "local"}, {"oci-remote", "oci", "remote"}, {"npm-local", "npm", "local"}} {
		mustExec(t, admin, "INSERT INTO quader.repositories (key, format, class, project_id) VALUES ($1, $2, $3, (SELECT id FROM quader.project WHERE key = 'it'))",
			r[0], r[1], r[2])
	}
	mustExec(t, admin, "INSERT INTO quader.packages (repo_id, name, display_name) SELECT id, 'acme/app', 'acme/app' FROM quader.repositories WHERE key = 'oci-local'")
	mustExec(t, admin, "INSERT INTO quader.packages (repo_id, name, display_name) SELECT id, 'left-pad', 'left-pad' FROM quader.repositories WHERE key = 'npm-local'")
	write := func(pkg, version string, key []byte) {
		t.Helper()
		mustExec(t, admin, `INSERT INTO quader.package_versions (package_id, version, display_version, sort_key, prerelease)
		                    SELECT id, $2, $2, $3, false FROM quader.packages WHERE name = $1`, pkg, version, key)
	}
	oldLatest := versionkey.Text([]byte{0x01, 0x00}, "latest")
	write("acme/app", "latest", oldLatest)
	write("acme/app", "1.0", []byte{0x01, 0x01, 0x01, 0x00, 0x01, '1', 0x00, 0x02, 0x00, 0x01, '1', '.', '0', 0x00, 0x01})
	write("left-pad", "1.0.0", []byte{0x01, 0x00, 0x01, '1', 0x00, 0x01})
	sum := bytes.Repeat([]byte{7}, 32)
	mustExec(t, admin, `INSERT INTO quader.blobs (sha256, sha1, md5, sha512, size) VALUES ($1, $2, $3, $4, 10)`,
		sum, bytes.Repeat([]byte{1}, 20), bytes.Repeat([]byte{2}, 16), bytes.Repeat([]byte{3}, 64))
	mustExec(t, admin, `INSERT INTO quader.remote_cache_entries (repo_id, path, kind, blob_sha256, upstream_url, fetched_at, validated_at, expires_at, last_used_at)
	                    SELECT id, 'acme/app/_tags/main', 'metadata', $1, 'https://upstream.example/v2/acme/app/manifests/main', now(), now(), now(), now()
	                      FROM quader.repositories WHERE key = 'oci-remote'`, sum)
	if got := scalar[string](t, admin, "SELECT encode(tag_sort_key, 'hex') FROM quader.remote_cache_entries"); !strings.HasPrefix(got, "0100") {
		t.Fatalf("the entry's key before the step: %s, want the old class 0x01 0x00", got)
	}

	res, err := postgres.Migrate(ctxT(t, 60*time.Second), cfg, opts(nil))
	if err != nil || len(res.Applied) != later {
		t.Fatalf("the steps from %s on a database that holds versions: %+v, %v", step, res, err)
	}
	if got := scalar[string](t, admin, `SELECT string_agg(version, ',' ORDER BY version) FROM quader.package_versions
	                                     WHERE sort_key IS NULL AND prerelease IS NULL`); got != "latest" {
		t.Errorf("the rows without a key after the step: %q, want the OCI tag latest alone", got)
	}
	if got := scalar[string](t, admin, "SELECT encode(tag_sort_key, 'hex') FROM quader.remote_cache_entries"); got != "006d61696e0001" {
		t.Errorf("the entry's key after the step: %s, want NotVersion, main and the end of a text", got)
	}
	if n := scalar[int](t, admin, `SELECT count(*) FROM pg_indexes WHERE indexname IN ('package_versions_unranked', 'remote_cache_entries_tag_unranked')`); n != 2 {
		t.Errorf("%d of the two indexes of the unranked versions", n)
	}
}
