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

// TestR_HA_020_TheBuiltInFiguresStepKeepsAnAdministratorsValue: the step that sets the built-in profiles' byte, upstream and
// refused figures from the uses they hold (usage-and-limits.md R-LIM-010, R-LIM-042, Q-LIM-296, Q-LIM-297) migrates a database
// the release before it filled (R-HA-020): a figure an administrator changed keeps its value, a number or unlimited, the profile's
// other figures take the new values, and a profile left as the first migration wrote it takes them all. Five rows on a database of
// its own; about 3 s.
func TestR_HA_020_TheBuiltInFiguresStepKeepsAnAdministratorsValue(t *testing.T) {
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
		if strings.HasSuffix(e.Name(), "_limits_builtin_byte_figures.sql") {
			step = e.Name()[:5]
		}
	}
	if step == "" {
		t.Fatal("no step *_limits_builtin_byte_figures.sql")
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
	// The release before: an administrator set Anonymous to 100 upstream requests an hour and made its uncached bytes per hour
	// unlimited, and set Technical's refused requests to 3,000. Standard and Restricted are as the first migration wrote them.
	mustExec(t, admin, `UPDATE quader.limit_profile SET upstream_requests_per_hour = 100, uncached_bytes_per_hour = NULL WHERE key = 'anonymous'`)
	mustExec(t, admin, `UPDATE quader.limit_profile SET refused_per_hour = 3000 WHERE key = 'technical'`)
	before := scalar[string](t, admin, `SELECT string_agg(key || '=' || version, ',' ORDER BY key) FROM quader.limit_profile WHERE builtin AND NOT key LIKE 'bastion-%'`)

	if _, err := postgres.Migrate(ctxT(t, 60*time.Second), cfg, opts(nil)); err != nil {
		t.Fatalf("the steps from %s: %v", step, err)
	}
	const row = `key || ':' || concat_ws(',', COALESCE(cached_bytes_per_second::text, 'null'), COALESCE(cached_bytes_per_hour::text, 'null'),
	              COALESCE(uncached_bytes_per_second::text, 'null'), COALESCE(uncached_bytes_per_hour::text, 'null'),
	              COALESCE(upstream_requests_per_hour::text, 'null'), COALESCE(bytes_per_day::text, 'null'),
	              COALESCE(refused_per_hour::text, 'null'))`
	got := scalar[string](t, admin, `SELECT string_agg(`+row+`, ' ' ORDER BY key) FROM quader.limit_profile WHERE builtin AND NOT key LIKE 'bastion-%'`)
	want := strings.Join([]string{
		"anonymous:100000000,30000000000,50000000,null,100,200000000000,300",
		"restricted:100000000,50000000000,50000000,20000000000,3000,200000000000,300",
		"standard:250000000,300000000000,125000000,100000000000,20000,1000000000000,1000",
		"technical:1250000000,3000000000000,250000000,500000000000,100000,30000000000000,3000",
		"unlimited:null,null,null,null,null,null,null",
	}, " ")
	if got != want {
		t.Errorf("the built-in profiles' figures:\n got %s\nwant %s\n(an administrator's 100, unlimited and 3,000 kept)", got, want)
	}
	after := scalar[string](t, admin, `SELECT string_agg(key || '=' || version, ',' ORDER BY key) FROM quader.limit_profile WHERE builtin AND NOT key LIKE 'bastion-%'`)
	if !strings.HasSuffix(after, "unlimited="+versionOf(before, "unlimited")) {
		t.Errorf("Unlimited's version moved: before %s, after %s", before, after)
	}
	for _, k := range []string{"anonymous", "restricted", "standard", "technical"} {
		if versionOf(after, k) == versionOf(before, k) {
			t.Errorf("the version of %s did not move: before %s, after %s", k, before, after)
		}
	}
}

// versionOf reads key's version out of "key=version,…".
func versionOf(list, key string) string {
	for _, kv := range strings.Split(list, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v
		}
	}
	return ""
}
