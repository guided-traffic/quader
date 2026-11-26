//go:build integration && measure

// Package measure is the one-off measurement of the storage summary's statements and the storage overview's reads (storage.md
// R-STOR-028, R-STOR-106, R-STOR-107, Q-STOR-501, Q-STOR-507): a measurement, not a test (it asserts nothing and runs in no pipeline;
// the build tag measure keeps it out of every `go test` the project runs). It clones the migrated template of a free instance of the
// integration stack into a scratch database, generates 1,000,000 live blobs of 1 to 5 KiB and 3,000,000 copy rows on three
// connections (one encrypted, its copies naming a storage key; 0.5 % of one connection's copies missing; 1,234 blobs on two
// connections only; 315,000 queued gaps), a snapshot with 10,000 repositories, and times the statements, three runs each, with the
// plan of the per-blob pass. The numbers it prints are written on docs/developer/blob-store.md. The scratch database is dropped at the
// end.
//
//	test/integration/locked.sh go test -count=1 -tags integration,measure -timeout 30m -run Measure -v ./hack/storage-summary-measure/
package measure

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/quader/internal/blob/scrub"
	"github.com/guided-traffic/quader/internal/blob/summary"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// oldConnections is StorageSummaryConnections before Q-STOR-501: one pass with a probe of the primary key per present copy for the sole
// copies.
const oldConnections = `WITH periods AS (
    SELECT unnest(sqlc.arg(ids)::text[]) AS id, unnest(sqlc.arg(period_seconds)::float8[]) AS seconds
), copies AS (
    SELECT c.connection_id, c.state, c.stored_size, b.size, c.verified_at, c.created_at,
           (c.state = 'present' AND NOT EXISTS (
               SELECT 1 FROM blob_copies o
                WHERE o.sha256 = c.sha256 AND o.connection_id <> c.connection_id AND o.state = 'present')) AS sole
      FROM blob_copies c
      JOIN blobs b ON b.sha256 = c.sha256 AND b.state = 'live'
)
SELECT sc.id,
       count(c.connection_id) FILTER (WHERE c.state = 'present')::bigint AS present_copies,
       COALESCE(sum(c.stored_size) FILTER (WHERE c.state = 'present'), 0)::bigint AS present_bytes,
       COALESCE(sum(c.size) FILTER (WHERE c.state = 'present'), 0)::bigint AS present_blob_bytes,
       count(c.connection_id) FILTER (WHERE c.state = 'missing')::bigint AS missing_copies,
       count(c.connection_id) FILTER (WHERE c.state = 'corrupt')::bigint AS corrupt_copies,
       count(c.connection_id) FILTER (
           WHERE c.state = 'present'
             AND c.verified_at >= now() - make_interval(secs => COALESCE(p.seconds, sqlc.arg(default_period_seconds)::float8))
       )::bigint AS verified_within_period,
       count(c.connection_id) FILTER (
           WHERE c.state = 'present'
             AND COALESCE(c.verified_at, c.created_at)
                 < now() - make_interval(secs => COALESCE(p.seconds, sqlc.arg(default_period_seconds)::float8) + sqlc.arg(grace_seconds)::float8)
       )::bigint AS scrub_overdue,
       count(c.connection_id) FILTER (WHERE c.sole)::bigint AS sole_copies
  FROM storage_connections sc
  LEFT JOIN periods p ON p.id = sc.id
  LEFT JOIN copies c ON c.connection_id = sc.id
 WHERE sc.retired_at IS NULL
 GROUP BY sc.id, p.seconds
 ORDER BY sc.id`

// draftConnections and draftCopies are the first forms of the two statements of Q-STOR-501, measured beside the forms kept: the copies of
// live blobs grouped by the connection's row and the period (a sort of every copy), and grouped by blob after a join of every copy with
// its blob (a probe of the blobs' primary key per copy).
const draftConnections = `WITH periods AS (
    SELECT unnest(sqlc.arg(ids)::text[]) AS id, unnest(sqlc.arg(period_seconds)::float8[]) AS seconds
), copies AS (
    SELECT c.connection_id, c.state, c.stored_size, b.size, c.verified_at, c.created_at, c.storage_key_id
      FROM blob_copies c
      JOIN blobs b ON b.sha256 = c.sha256 AND b.state = 'live'
)
SELECT sc.id,
       count(c.connection_id) FILTER (WHERE c.state = 'present')::bigint AS present_copies,
       COALESCE(sum(c.stored_size) FILTER (WHERE c.state = 'present'), 0)::bigint AS present_bytes,
       COALESCE(sum(c.size) FILTER (WHERE c.state = 'present'), 0)::bigint AS present_blob_bytes,
       count(c.connection_id) FILTER (WHERE c.state = 'missing')::bigint AS missing_copies,
       count(c.connection_id) FILTER (WHERE c.state = 'corrupt')::bigint AS corrupt_copies,
       count(c.connection_id) FILTER (
           WHERE c.state = 'present'
             AND c.verified_at >= now() - make_interval(secs => COALESCE(p.seconds, sqlc.arg(default_period_seconds)::float8))
       )::bigint AS verified_within_period,
       count(c.connection_id) FILTER (
           WHERE c.state = 'present'
             AND COALESCE(c.verified_at, c.created_at)
                 < now() - make_interval(secs => COALESCE(p.seconds, sqlc.arg(default_period_seconds)::float8) + sqlc.arg(grace_seconds)::float8)
       )::bigint AS scrub_overdue,
       count(c.connection_id) FILTER (WHERE c.state = 'present' AND c.storage_key_id IS NOT NULL)::bigint AS keyed_copies
  FROM storage_connections sc
  LEFT JOIN periods p ON p.id = sc.id
  LEFT JOIN copies c ON c.connection_id = sc.id
 WHERE sc.retired_at IS NULL
 GROUP BY sc.id, p.seconds
 ORDER BY sc.id`

const draftCopies = `WITH per_blob AS (
    SELECT max(b.size) AS size,
           count(*) FILTER (WHERE c.state = 'present') AS present,
           count(*) FILTER (WHERE c.state = 'present' AND c.connection_id = ANY (sqlc.arg(targets)::text[])) AS on_target,
           min(c.connection_id) FILTER (WHERE c.state = 'present') AS first_present
      FROM blob_copies c
      JOIN blobs b ON b.sha256 = c.sha256 AND b.state = 'live'
      JOIN storage_connections sc ON sc.id = c.connection_id AND sc.retired_at IS NULL
     GROUP BY c.sha256
)
SELECT pb.present::int AS copies,
       (pb.on_target >= cardinality(sqlc.arg(targets)::text[]))::boolean AS on_every_target,
       (CASE WHEN pb.present = 1 THEN pb.first_present ELSE '' END)::text AS sole_connection,
       count(*)::bigint AS blobs,
       COALESCE(sum(pb.size), 0)::bigint AS bytes
  FROM per_blob pb
 GROUP BY 1, 2, 3
 ORDER BY 1, 2, 3`

// args are the literal values of the statements' sqlc.arg placeholders for EXPLAIN.
var args = map[string]string{
	"ids":                    `ARRAY['archive','default','secondary']`,
	"period_seconds":         `ARRAY[2592000,2592000,2592000]`,
	"default_period_seconds": `2592000`,
	"grace_seconds":          `86400`,
	"targets":                `ARRAY['archive','default','secondary']`,
}

var sqlcArg = regexp.MustCompile(`sqlc\.arg\((\w+)\)`)

func literal(stmt string) string {
	return sqlcArg.ReplaceAllStringFunc(stmt, func(m string) string { return args[sqlcArg.FindStringSubmatch(m)[1]] })
}

// statement is the body of a named statement of storage_summary.sql, without its comment lines.
func statement(t *testing.T, name string) string {
	raw, err := os.ReadFile("../../internal/meta/postgres/queries/storage_summary.sql")
	if err != nil {
		t.Fatal(err)
	}
	_, after, ok := strings.Cut(string(raw), "-- name: "+name+" ")
	if !ok {
		t.Fatalf("no statement %s", name)
	}
	body, _, _ := strings.Cut(after, "-- name: ")
	var lines []string
	for _, l := range strings.Split(body, "\n")[1:] {
		if !strings.HasPrefix(strings.TrimSpace(l), "--") {
			lines = append(lines, l)
		}
	}
	return strings.TrimSuffix(strings.TrimSpace(strings.Join(lines, "\n")), ";")
}

func TestMeasureStorageSummary(t *testing.T) {
	ctx := context.Background()
	name := fmt.Sprintf("quader_measure_summary_%d", time.Now().Unix())
	testenv.MigratedDatabase(t, name)
	maint, err := pgx.Connect(ctx, testenv.PgDSN(t, "postgres"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = maint.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		_ = maint.Close(ctx)
	}()
	conn, err := pgx.Connect(ctx, testenv.PgDSN(t, name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	exec := func(what, sql string) {
		start := time.Now()
		if _, err := conn.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		t.Logf("generate: %-48s %8.1f s", what, time.Since(start).Seconds())
	}
	exec("search path", "SET search_path TO quader")
	var version, workMem string
	_ = conn.QueryRow(ctx, "SELECT version(), current_setting('work_mem')").Scan(&version, &workMem)
	t.Logf("server: %s, work_mem %s", version, workMem)

	exec("connections and a storage key", `
		INSERT INTO storage_connections (id, bucket, prefix, encrypted, filled_at) VALUES
		  ('default', 'b1', 'quader', false, now()), ('secondary', 'b2', 'quader', false, now()), ('archive', 'b3', 'quader', true, now());
		INSERT INTO storage_keys (key_id, connection_id, kind, generation, sealed_key, state)
		VALUES ('sk-archive-1-0a1b2c3d', 'archive', 'storage', 1, 'v1:measure', 'active')`)
	exec("1,000,000 blobs", `
		INSERT INTO blobs (sha256, sha1, md5, sha512, size)
		SELECT d, substr(d, 1, 20), substr(d, 1, 16), d || d, 1024 + (i % 4096)
		  FROM (SELECT i, sha256(int8send(i)) AS d FROM generate_series(1, 1000000) i) g`)
	exec("3,000,000 copy rows", `
		INSERT INTO blob_copies (sha256, connection_id, object_key, stored_size, state, verified_at, storage_key_id)
		SELECT b.sha256, c.id, c.id || '/' || encode(b.sha256, 'hex'), b.size + CASE WHEN c.id = 'archive' THEN 96 ELSE 0 END, 'present',
		       now() - (random() * interval '40 days'), CASE WHEN c.id = 'archive' THEN 'sk-archive-1-0a1b2c3d' END
		  FROM blobs b CROSS JOIN (VALUES ('default'), ('secondary'), ('archive')) c(id)`)
	exec("0.5 % of secondary missing, 1,234 blobs off archive", `
		UPDATE blob_copies SET state = 'missing' WHERE connection_id = 'secondary' AND get_byte(sha256, 1) = 0 AND get_byte(sha256, 2) < 128;
		DELETE FROM blob_copies WHERE connection_id = 'archive' AND sha256 IN (SELECT sha256 FROM blobs ORDER BY sha256 LIMIT 1234)`)
	exec("315,000 queued gaps", `
		INSERT INTO blob_copy_queue (connection_id, sha256, enqueued_at)
		SELECT 'archive', sha256, now() - interval '10 minutes' FROM blobs ORDER BY sha256 DESC LIMIT 315000`)
	exec("analyze", "ANALYZE blobs; ANALYZE blob_copies; ANALYZE blob_copy_queue")
	var copyRows int64
	_ = conn.QueryRow(ctx, "SELECT count(*) FROM blob_copies").Scan(&copyRows)
	t.Logf("rows: %d copies", copyRows)

	qs := q.New(conn)
	ids, periods := []string{"archive", "default", "secondary"}, []float64{2592000, 2592000, 2592000}
	timed := func(what string, run func() error) {
		var runs []time.Duration
		for range 3 {
			start := time.Now()
			if err := run(); err != nil {
				t.Fatalf("%s: %v", what, err)
			}
			runs = append(runs, time.Since(start))
		}
		sort.Slice(runs, func(i, j int) bool { return runs[i] < runs[j] })
		t.Logf("measure: %-52s median %8.1f ms (runs %v)", what, float64(runs[1].Microseconds())/1000, runs)
	}
	text := func(sql string) func() error {
		return func() error {
			rows, err := conn.Query(ctx, literal(sql))
			if err != nil {
				return err
			}
			rows.Close()
			return rows.Err()
		}
	}
	timed("before: StorageSummaryConnections with the probe", text(oldConnections))
	timed("draft: StorageSummaryConnections grouped by row", text(draftConnections))
	timed("draft: StorageSummaryCopies joined per copy", text(draftCopies))
	timed("after: StorageSummaryConnections", func() error {
		_, err := qs.StorageSummaryConnections(ctx, q.StorageSummaryConnectionsParams{Ids: ids, PeriodSeconds: periods,
			DefaultPeriodSeconds: 2592000, GraceSeconds: scrub.OverdueGrace.Seconds()})
		return err
	})
	timed("after: StorageSummaryCopies (the pass per blob)", func() error { _, err := qs.StorageSummaryCopies(ctx, ids); return err })
	timed("StorageSummaryKeyCopies", func() error { _, err := qs.StorageSummaryKeyCopies(ctx); return err })
	timed("StorageSummaryTotals", func() error { _, err := qs.StorageSummaryTotals(ctx); return err })

	for _, st := range []struct{ label, sql string }{
		{"after: StorageSummaryConnections", literal(statement(t, "StorageSummaryConnections"))},
		{"after: StorageSummaryCopies", literal(statement(t, "StorageSummaryCopies"))},
	} {
		rows, err := conn.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, TIMING OFF) "+st.sql)
		if err != nil {
			t.Fatalf("explain %s: %v", st.label, err)
		}
		var plan []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, line)
		}
		rows.Close()
		t.Logf("plan of %s:\n%s", st.label, strings.Join(plan, "\n"))
	}

	// the snapshot with 10,000 repositories, and the overview's reads
	s := summary.Summary{ComputedAt: time.Now().UTC(), Connections: []summary.Connection{{ID: "default"}, {ID: "secondary"}, {ID: "archive"}}}
	for i := range 10000 {
		s.Repositories = append(s.Repositories, summary.Repository{Project: fmt.Sprintf("project-%04d", i/10), Key: fmt.Sprintf("npm-local-%d", i),
			Class: "local", Format: "npm", Files: 123456, Folders: 9876, Bytes: 81233920123, Packages: 812, Versions: 9120, ShareOfLogicalBytes: 0.0001})
	}
	body, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := qs.StorageSummarySave(ctx, q.StorageSummarySaveParams{ComputedAt: s.ComputedAt.Format(time.RFC3339Nano), Summary: body}); err != nil {
		t.Fatal(err)
	}
	t.Logf("snapshot: %d bytes of JSON", len(body))
	timed("StorageSummaryLatest, the read alone", func() error { _, err := qs.StorageSummaryLatest(ctx); return err })
	timed("StorageSummaryLatest and its decoding", func() error {
		rows, err := qs.StorageSummaryLatest(ctx)
		if err != nil {
			return err
		}
		var back summary.Summary
		return json.Unmarshal(rows[0].Summary, &back)
	})
	timed("overview: StorageSummaryLatestFigures and its decoding", func() error {
		rows, err := qs.StorageSummaryLatestFigures(ctx)
		if err != nil {
			return err
		}
		var back summary.Summary
		return json.Unmarshal(rows[0].Summary, &back)
	})
	timed("overview: StorageConnectionsList (315,000 queued)", func() error { _, err := qs.StorageConnectionsList(ctx, 300); return err })
	timed("overview: StorageConnectionKeys", func() error { _, err := qs.StorageConnectionKeys(ctx); return err })
	timed("overview: StorageStoredNow", func() error { _, err := qs.StorageStoredNow(ctx); return err })
}
