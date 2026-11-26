//go:build integration

package postgres

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/guided-traffic/quader/internal/meta/postgres"
)

// TestR_HA_020_TheParallelTransfersStepKeepsAnAdministratorsValue: the step that renames the figure parallelDownloads to
// parallelTransfers and sets the built-in profiles' new values (usage-and-limits.md R-LIM-010, Q-LIM-293) migrates a database the
// release before it filled (R-HA-020): a built-in profile whose figure an administrator changed keeps it, the others take the
// measured value, and a rule's and a project maximum's overrides and a recorded limit event carry the new name, an unlimited
// override (null) included. One row of each on a database of its own; about 3 s.
func TestR_HA_020_TheParallelTransfersStepKeepsAnAdministratorsValue(t *testing.T) {
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
		if strings.HasSuffix(e.Name(), "_limits_parallel_transfers.sql") {
			step = e.Name()[:5]
		}
	}
	if step == "" {
		t.Fatal("no step *_limits_parallel_transfers.sql")
	}
	for _, e := range entries {
		if e.Name()[:5] >= step {
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
	// The release before: an administrator set Standard to 30 parallel downloads; a rule and a project's maximum override the figure,
	// one with a value and one unlimited; the usage store holds a refusal of the figure.
	mustExec(t, admin, "UPDATE quader.limit_profile SET parallel_downloads = 30 WHERE key = 'standard'")
	mustExec(t, admin, "INSERT INTO quader.project (id, key, display_name) VALUES (gen_random_uuid(), 'it', 'it')")
	mustExec(t, admin, `INSERT INTO quader.limit_assignment (id, place_kind, who_kind, who_ref, profile_id, overrides, capped)
	                    SELECT gen_random_uuid(), 'instance', 'user', 'u-1', id, '{"parallelDownloads": 100, "bytesPerDay": 5}', false
	                      FROM quader.limit_profile WHERE key = 'standard'`)
	mustExec(t, admin, `INSERT INTO quader.limit_project_maximum (project_id, profile_id, overrides)
	                    SELECT (SELECT id FROM quader.project WHERE key = 'it'), id, '{"parallelDownloads": null}'
	                      FROM quader.limit_profile WHERE key = 'technical'`)
	mustExec(t, admin, `INSERT INTO quader.usage_limit_event (granularity, bucket_start, subject_kind, subject_ref, place, figure, mode, count)
	                    VALUES ('1h', now(), 'user', 'u-1', 'instance', 'parallelDownloads', 'enforce', 3)`)

	if _, err := postgres.Migrate(ctxT(t, 60*time.Second), cfg, opts(nil)); err != nil {
		t.Fatalf("the steps from %s: %v", step, err)
	}
	got := scalar[string](t, admin, `SELECT string_agg(key || '=' || COALESCE(parallel_transfers::text, 'null'), ',' ORDER BY key)
	                                   FROM quader.limit_profile WHERE builtin AND NOT key LIKE 'bastion-%'`)
	if want := "anonymous=64,restricted=64,standard=30,technical=1000,unlimited=null"; got != want {
		t.Errorf("the built-in profiles' parallel transfers: %s, want %s (an administrator's 30 kept)", got, want)
	}
	if got := scalar[string](t, admin, "SELECT overrides::text FROM quader.limit_assignment WHERE who_ref = 'u-1'"); got != `{"bytesPerDay": 5, "parallelTransfers": 100}` {
		t.Errorf("the rule's overrides: %s", got)
	}
	if got := scalar[string](t, admin, "SELECT overrides::text FROM quader.limit_project_maximum"); got != `{"parallelTransfers": null}` {
		t.Errorf("the maximum's overrides: %s", got)
	}
	if got := scalar[string](t, admin, "SELECT figure FROM quader.usage_limit_event"); got != "parallelTransfers" {
		t.Errorf("the limit event's figure: %s", got)
	}
}
