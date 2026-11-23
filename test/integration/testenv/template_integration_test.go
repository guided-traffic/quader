//go:build integration

package testenv

import (
	"context"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/quader/internal/meta/postgres"
)

// The stacks are clones of one migrated template (Q-ARCH-581): each with identity rows of its own and the migration bookkeeping
// of the newest step; the template follows the migrations, is made once when two processes start at once, and is never left
// half migrated under its name.

var uuidText = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func TestStacksAreClonesOfTheTemplate(t *testing.T) {
	t.Run("Q-ARCH-581 two stacks differ in every identity row of system_state", func(t *testing.T) {
		a, b := NewStack(t, StackOptions{Name: "tplid"}), NewStack(t, StackOptions{Name: "tplid"})
		for _, key := range identityKeys {
			q := `SELECT value #>> '{}' FROM system_state WHERE key = $1`
			va, vb := a.Text(q, key), b.Text(q, key)
			if !uuidText.MatchString(va) || !uuidText.MatchString(vb) || va == vb {
				t.Errorf("%s: %q and %q, want two different UUIDs", key, va, vb)
			}
		}
		// The keyspace the replicas name their Valkey keys with: the UUID's hex digits (R-ARCH-030).
		ka := strings.ReplaceAll(a.Text(`SELECT value #>> '{}' FROM system_state WHERE key = 'keyspace_id'`), "-", "")
		kb := strings.ReplaceAll(b.Text(`SELECT value #>> '{}' FROM system_state WHERE key = 'keyspace_id'`), "-", "")
		if ka[:12] == kb[:12] {
			t.Errorf("both keyspaces begin with %s", ka[:12])
		}
	})
	t.Run("Q-ARCH-581 a clone's migration bookkeeping is the newest embedded step", func(t *testing.T) {
		s := NewStack(t, StackOptions{Name: "tplbook"})
		steps, err := postgres.EmbeddedSteps()
		if err != nil {
			t.Fatal(err)
		}
		newest := steps[len(steps)-1]
		if got := s.Number(`SELECT applied FROM schema_state`); got != int64(newest.Version) {
			t.Errorf("schema_state.applied = %d, want %d (%s)", got, newest.Version, newest.Name)
		}
		if n, last := s.Number(`SELECT count(*) FROM schema_migration`), s.Number(`SELECT max(version) FROM schema_migration`); n != int64(len(steps)) || last != int64(newest.Version) {
			t.Errorf("schema_migration holds %d steps up to %d, want %d up to %d", n, last, len(steps), newest.Version)
		}
		if got := s.Text(`SELECT encode(checksum, 'hex') FROM schema_migration WHERE version = $1`, newest.Version); got != fmt.Sprintf("%x", newest.Checksum) {
			t.Errorf("the checksum of %s is %s, want the embedded file's %x", newest.Name, got, newest.Checksum)
		}
	})
	t.Run("Q-ARCH-581 the template is a template that takes no connections and carries the harness's mark", func(t *testing.T) {
		NewStack(t, StackOptions{Name: "tplmark"})
		tpl := stackTemplate(t, databaseConfig(t, t.TempDir(), "unused"), false)
		var template, allow bool
		var mark string
		maintQueryRow(t, `SELECT datistemplate, datallowconn, coalesce(shobj_description(oid, 'pg_database'), '') FROM pg_database WHERE datname = $1`,
			[]any{tpl}, &template, &allow, &mark)
		if !template || allow || mark != templateMarker {
			t.Fatalf("%s: template %v, connections %v, mark %q", tpl, template, allow, mark)
		}
	})
}

func TestTemplateFollowsTheMigrations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cfg := databaseConfig(t, t.TempDir(), "unused")
	day := templateDay(time.Now())
	embedded := stackTemplate(t, cfg, false)
	// Whatever a subtest leaves, the template of the embedded migrations is there again for the tests after it.
	t.Cleanup(func() { stackTemplate(t, cfg, true) })

	t.Run("Q-ARCH-581 a migration added during development: two processes that start at once make its template once and drop the old one; "+
		"a stack that still clones the old one makes it again", func(t *testing.T) {
		// A step no server has seen: neither process finds the template, both ask for the lock, one builds, the other finds its result.
		changed := withStep(t, "it_template_probe_"+RandHex(4), "CREATE TABLE it_template_probe (id integer);\n")
		type result struct {
			name  string
			built bool
			err   error
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		results := make([]result, 2)
		for i := range results {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				// Each call opens a session of its own, as a second test binary would.
				n, b, err := ensureTemplate(ctx, PgDSN(t, "postgres"), cfg, changed, day, t.Logf)
				results[i] = result{n, b, err}
			}()
		}
		close(start)
		wg.Wait()
		builds := 0
		for _, r := range results {
			if r.err != nil {
				t.Fatal(r.err)
			}
			if r.built {
				builds++
			}
		}
		name := results[0].name
		if builds != 1 || results[1].name != name || name == embedded {
			t.Fatalf("two at once: %+v, want one build of one name other than %s", results, embedded)
		}
		if !databaseExists(t, name) || databaseExists(t, name+"_build") || databaseExists(t, embedded) {
			t.Fatalf("after the build: %s exists %v (want true), its build %v, the old template %s %v (want false)",
				name, databaseExists(t, name), databaseExists(t, name+"_build"), embedded, databaseExists(t, embedded))
		}
		clone := "quader_tplprobe_" + RandHex(5)
		maintExec(t, "CREATE DATABASE "+clone+" TEMPLATE "+name)
		t.Cleanup(func() { maintExec(t, "DROP DATABASE IF EXISTS "+clone+" WITH (FORCE)") })
		var applied int64
		var probe bool
		withConn(t, clone, func(c *pgx.Conn) error {
			return c.QueryRow(ctx, `SELECT (SELECT applied FROM quader.schema_state), to_regclass('quader.it_template_probe') IS NOT NULL`).Scan(&applied, &probe)
		})
		steps, _ := postgres.EmbeddedSteps()
		if applied != int64(len(steps)+1) || !probe {
			t.Fatalf("a clone of the changed migrations: schema_state.applied %d (want %d), the new step's table %v", applied, len(steps)+1, probe)
		}

		// This process still clones the embedded migrations' template, which the build above dropped: the next stack finds it gone
		// (SQLSTATE 3D000), makes it again and drops the changed one, as a run does after another checkout's run on its stack.
		st := NewStack(t, StackOptions{Name: "tplgone"})
		if !databaseExists(t, embedded) || databaseExists(t, name) || st.Number(`SELECT applied FROM schema_state`) != int64(len(steps)) {
			t.Fatalf("the stack after its template was dropped: %s exists %v (want true), %s exists %v (want false), applied %d",
				embedded, databaseExists(t, embedded), name, databaseExists(t, name), st.Number(`SELECT applied FROM schema_state`))
		}
	})

	t.Run("Q-ARCH-581 a migration that fails leaves neither a template nor its build", func(t *testing.T) {
		// One step that fails at once: what is left after a failure does not depend on how far the migration came.
		broken := fstest.MapFS{"00001_it_template_broken.sql": {Data: []byte("SELECT * FROM it_no_such_table;\n")}}
		want, err := templateName(broken, day)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := ensureTemplate(ctx, PgDSN(t, "postgres"), cfg, broken, day, t.Logf); err == nil || !strings.Contains(err.Error(), "it_no_such_table") {
			t.Fatalf("the broken migration: %v", err)
		}
		if databaseExists(t, want) || databaseExists(t, want+"_build") {
			t.Fatalf("after the failed migration: %s exists %v, %s_build exists %v", want, databaseExists(t, want), want, databaseExists(t, want+"_build"))
		}
		if !databaseExists(t, embedded) {
			t.Fatalf("the failed build dropped the template %s", embedded)
		}
	})
}

// withStep is the embedded migrations with one further step, name and sql: a migration added during development.
func withStep(tb testing.TB, name, sql string) fs.FS {
	tb.Helper()
	src := postgres.EmbeddedFS()
	entries, err := fs.ReadDir(src, ".")
	if err != nil {
		tb.Fatal(err)
	}
	out := fstest.MapFS{}
	for _, e := range entries {
		b, err := fs.ReadFile(src, e.Name())
		if err != nil {
			tb.Fatal(err)
		}
		out[e.Name()] = &fstest.MapFile{Data: b}
	}
	out[fmt.Sprintf("%05d_%s.sql", len(entries)+1, name)] = &fstest.MapFile{Data: []byte(sql)}
	return out
}

// withConn runs fn on a connection to database.
func withConn(tb testing.TB, database string, fn func(*pgx.Conn) error) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := pgx.Connect(ctx, PgDSN(tb, database))
	if err != nil {
		tb.Fatalf("connect to %s: %v", database, err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	if err := fn(c); err != nil {
		tb.Fatal(err)
	}
}

// withMaint runs fn on a connection to the maintenance database.
func withMaint(tb testing.TB, fn func(*pgx.Conn) error) { tb.Helper(); withConn(tb, "postgres", fn) }

func maintExec(tb testing.TB, sql string) {
	tb.Helper()
	withMaint(tb, func(c *pgx.Conn) error { _, err := c.Exec(context.Background(), sql); return err })
}

func maintQueryRow(tb testing.TB, sql string, args []any, dst ...any) {
	tb.Helper()
	withMaint(tb, func(c *pgx.Conn) error { return c.QueryRow(context.Background(), sql, args...).Scan(dst...) })
}

func databaseExists(tb testing.TB, name string) bool {
	tb.Helper()
	var ok bool
	maintQueryRow(tb, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, []any{name}, &ok)
	return ok
}
