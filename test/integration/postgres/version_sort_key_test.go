//go:build integration

package postgres

import (
	"io/fs"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/guided-traffic/quader/internal/meta/postgres"
)

// TestR_HA_020_TheSortKeyStepPassesOnADatabaseOfThePreviousRelease: migration 00116 adds the versions' sort key (Q-API-429) to a
// database the release before it filled, without refusing it, and the release before it keeps writing version rows after the step,
// as it does during a rolling upgrade (R-HA-020). The rows of both carry no key until the background migration version_sort_key fills
// them (Q-HA-492, Q-API-430); a row has a key and a flag or neither. Three tiny versions on a database of its own; about 3 s.
func TestR_HA_020_TheSortKeyStepPassesOnADatabaseOfThePreviousRelease(t *testing.T) {
	st := getStack(t)
	name, admin := st.newDatabase(t)
	cfg := st.config(t, name)
	old := fstest.MapFS{}
	emb := postgres.EmbeddedFS()
	entries, err := fs.ReadDir(emb, ".")
	if err != nil {
		t.Fatal(err)
	}
	later := 0
	for _, e := range entries {
		if n, _ := strconv.Atoi(e.Name()[:5]); n >= 116 {
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
	// The rows the release before 00116 wrote: a package with two versions, one of them trashed.
	mustExec(t, admin, "INSERT INTO quader.project (id, key, display_name) VALUES (gen_random_uuid(), 'it', 'it')")
	mustExec(t, admin, "INSERT INTO quader.repositories (key, format, class, project_id) VALUES ('npm-local', 'npm', 'local', (SELECT id FROM quader.project WHERE key = 'it'))")
	mustExec(t, admin, "INSERT INTO quader.packages (repo_id, name, display_name) SELECT id, 'left-pad', 'left-pad' FROM quader.repositories WHERE key = 'npm-local'")
	writeOld := func(version string) {
		t.Helper()
		mustExec(t, admin, "INSERT INTO quader.package_versions (package_id, version, display_version) SELECT id, $1, $1 FROM quader.packages WHERE name = 'left-pad'", version)
	}
	writeOld("1.0.0")
	writeOld("1.1.0-rc.1")
	mustExec(t, admin, "UPDATE quader.package_versions SET trashed_at = now() WHERE version = '1.1.0-rc.1'")

	res, err := postgres.Migrate(ctxT(t, 60*time.Second), cfg, opts(nil))
	if err != nil || len(res.Applied) != later {
		t.Fatalf("the steps from 00116 on a database that holds versions: %+v, %v", res, err)
	}
	if n := scalar[int](t, admin, "SELECT count(*) FROM quader.package_versions WHERE sort_key IS NULL AND prerelease IS NULL"); n != 2 {
		t.Fatalf("%d rows without a key after the step, want the two stored before it", n)
	}

	t.Run("the release before 00116 writes a version after the step, without a key", func(t *testing.T) {
		writeOld("2.0.0")
		if n := scalar[int](t, admin, "SELECT count(*) FROM quader.package_versions WHERE sort_key IS NULL"); n != 3 {
			t.Fatalf("%d rows without a key, want 3", n)
		}
		if n := scalar[int](t, admin, "SELECT count(*) FROM quader.package_version_listing WHERE version = '2.0.0' AND sort_key IS NULL"); n != 1 {
			t.Fatalf("the listing shows the row %d times", n)
		}
	})

	t.Run("a row has a key and a flag or neither", func(t *testing.T) {
		_, err := admin.Exec(ctxT(t, 10*time.Second), `UPDATE quader.package_versions SET sort_key = '\x01'::bytea WHERE version = '1.0.0'`)
		if err == nil || !strings.Contains(err.Error(), "package_versions_sort_key_whole") {
			t.Fatalf("a key without a flag: %v", err)
		}
		mustExec(t, admin, `UPDATE quader.package_versions SET sort_key = '\x01'::bytea, prerelease = false WHERE version = '1.0.0'`)
	})
}
