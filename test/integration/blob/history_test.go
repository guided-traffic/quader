//go:build integration

package blob

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/blob/history"
	"github.com/guided-traffic/quader/internal/blob/scrub"
)

// The history of the stored content against PostgreSQL of the stack (storage.md R-STOR-108 to R-STOR-110): the triggers that open and
// resolve a finding, the daily cap, the backfill of the migration, the recorder's statements and the task storage-history. In-memory
// stores, objects of 1 KiB; the cap's 1,001 copy rows have no object.

// finding is a row of storage_findings as the tests compare it.
type finding struct {
	status, source, reason, resolution string
	unavailable                        bool
}

func findings(tb testing.TB, p *pgEnv, d blob.Digest, conn string) []finding {
	tb.Helper()
	rows, err := p.admin.Query(context.Background(), `SELECT status, source, reason, COALESCE(resolution, ''), unavailable FROM quader.storage_findings
		WHERE sha256 = $1 AND connection_id = $2 ORDER BY id`, d.Bytes(), conn)
	if err != nil {
		tb.Fatal(err)
	}
	defer rows.Close()
	var out []finding
	for rows.Next() {
		var f finding
		if err := rows.Scan(&f.status, &f.source, &f.reason, &f.resolution, &f.unavailable); err != nil {
			tb.Fatal(err)
		}
		out = append(out, f)
	}
	return out
}

func randomDigest(tb testing.TB) blob.Digest {
	tb.Helper()
	var d blob.Digest
	if _, err := rand.Read(d[:]); err != nil {
		tb.Fatal(err)
	}
	return d
}

func TestR_STOR_108_AFindingFromMarkToRepair(t *testing.T) {
	ctx := context.Background()

	t.Run("R-STOR-108 the scrub's mark opens a finding with source scrub and reason digest, its repair resolves it rewritten and clears the copy's mark", func(t *testing.T) {
		p := newPG(t)
		a, b := blobtest.New(), blobtest.New()
		s, err := memStore(t, p, nil, a, b)
		if err != nil {
			t.Fatal(err)
		}
		good := memData(1024, 31)
		d := commit(t, s, good)
		key := blob.Key("quader", d)
		b.Set(key, good, time.Now())
		p.exec(t, `INSERT INTO quader.blob_copies (sha256, connection_id, object_key, stored_size, state) VALUES ($1, 'secondary', $2, 1024, 'present')`, d.Bytes(), key)
		p.exec(t, `DELETE FROM quader.blob_copy_queue`)
		a.Set(key, memData(1024, 98), time.Now())
		p.exec(t, `UPDATE quader.blob_copies SET verified_at = NULL WHERE connection_id = 'primary'`)
		if _, err := scrubber(t, p, s).Run(ctx, scrub.RunOptions{Connection: "primary"}); err != nil {
			t.Fatal(err)
		}
		got := findings(t, p, d, "primary")
		if len(got) != 1 || got[0] != (finding{status: "corrupt", source: "scrub", reason: "digest", resolution: "rewritten"}) {
			t.Fatalf("findings %+v", got)
		}
		if n := p.count(t, "blob_copies WHERE damage_source IS NOT NULL OR damage_reason IS NOT NULL"); n != 0 {
			t.Errorf("%d copies still carry a mark", n)
		}
	})

	t.Run("R-STOR-108 found again, deleted, superseded and unavailable follow from the copy row, whatever statement changed it", func(t *testing.T) {
		p := newPG(t)
		a := blobtest.New()
		s, err := memStore(t, p, nil, a)
		if err != nil {
			t.Fatal(err)
		}
		d := commit(t, s, memData(1024, 32))
		set := func(sql string) { p.exec(t, sql, d.Bytes()) }
		set(`UPDATE quader.blob_copies SET state = 'missing', damage_source = 'download', damage_reason = 'not_found' WHERE sha256 = $1`)
		set(`UPDATE quader.blob_copies SET state = 'present' WHERE sha256 = $1`)                                                    // the object is there again
		set(`UPDATE quader.blob_copies SET state = 'missing' WHERE sha256 = $1`)                                                    // a writer that names nothing
		set(`UPDATE quader.blob_copies SET state = 'corrupt', damage_source = 'recheck', damage_reason = 'size' WHERE sha256 = $1`) // missing to corrupt
		set(`DELETE FROM quader.blob_copies WHERE sha256 = $1`)                                                                     // collection
		want := []finding{
			{status: "missing", source: "download", reason: "not_found", resolution: "found_again", unavailable: true},
			{status: "missing", source: "other", reason: "missing", resolution: "superseded", unavailable: true},
			{status: "corrupt", source: "recheck", reason: "size", resolution: "deleted", unavailable: true},
		}
		got := findings(t, p, d, "primary")
		if len(got) != len(want) {
			t.Fatalf("findings %+v, want %+v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("finding %d is %+v, want %+v", i, got[i], want[i])
			}
		}
	})

	t.Run("R-STOR-108 1,001 copies damaged in one day list 1,000 findings and count one as findings_unlisted with its bytes", func(t *testing.T) {
		p := newPG(t)
		if _, err := memStore(t, p, nil, blobtest.New()); err != nil {
			t.Fatal(err)
		}
		digests := make([][]byte, 1001)
		for i := range digests {
			d := randomDigest(t)
			digests[i] = d.Bytes()
		}
		p.exec(t, `INSERT INTO quader.blob_copies (sha256, connection_id, object_key, stored_size, state, damage_source, damage_reason)
			SELECT d, 'primary', encode(d, 'hex'), 7, 'corrupt', 'scrub', 'digest' FROM unnest($1::bytea[]) AS d`, digests)
		if n := p.count(t, "storage_findings"); n != 1000 {
			t.Errorf("%d findings, want 1000", n)
		}
		var count, bytes int64
		if err := p.admin.QueryRow(ctx, `SELECT count, bytes FROM quader.storage_counts WHERE kind = 'findings_unlisted' AND connection_id = 'primary' AND reason = 'digest'`).
			Scan(&count, &bytes); err != nil || count != 1 || bytes != 7 {
			t.Errorf("findings_unlisted %d with %d bytes (%v), want 1 with 7", count, bytes, err)
		}
	})

	t.Run("R-STOR-108 the migration's backfill opens a finding for a copy damaged before the history existed", func(t *testing.T) {
		p := newPG(t)
		a := blobtest.New()
		s, err := memStore(t, p, nil, a)
		if err != nil {
			t.Fatal(err)
		}
		d := commit(t, s, memData(1024, 33))
		p.exec(t, `UPDATE quader.blob_copies SET state = 'corrupt', state_changed_at = now() - interval '3 days' WHERE sha256 = $1`, d.Bytes())
		p.exec(t, `UPDATE quader.blobs SET content_status = 'corrupt' WHERE sha256 = $1`, d.Bytes()) // its only copy: no connection holds it whole
		p.exec(t, `DELETE FROM quader.storage_findings`)                                             // as before the history existed
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "internal", "meta", "postgres", "migrations", "00151_storage_history.sql"))
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		i := strings.Index(text, "INSERT INTO storage_findings (sha256, connection_id, status, source, reason, stored_size, blob_size, unavailable, detected_at)")
		if i < 0 {
			t.Fatal("the backfill of 00151 is gone; update the test")
		}
		p.exec(t, text[i:])
		got := findings(t, p, d, "primary")
		if len(got) != 1 || got[0] != (finding{status: "corrupt", source: "other", reason: "corrupt", unavailable: true}) {
			t.Fatalf("findings %+v", got)
		}
		if n := p.count(t, "storage_findings WHERE detected_at < now() - interval '2 days'"); n != 1 {
			t.Errorf("the backfilled finding is not dated when its copy was marked")
		}
	})
}

type jobResult struct{ v any }

func (j *jobResult) SetResult(v any) error { j.v = v; return nil }

func TestR_STOR_110_TheRecordAgainstTheDatabase(t *testing.T) {
	ctx := context.Background()
	t.Run("R-STOR-110 two replicas' counts of one hour add up, and an interval is inserted, updated and ended", func(t *testing.T) {
		p := newPG(t)
		hour := time.Now().UTC().Truncate(time.Hour)
		for _, replica := range []string{"web-0", "web-1"} {
			store := history.PG{DB: p.db}
			if err := store.AddCounts(ctx, []history.Count{{Hour: hour, Connection: "primary", Kind: blob.HistoryReadFallOver, Reason: "not_found", N: 2, Bytes: 0}}); err != nil {
				t.Fatalf("%s: %v", replica, err)
			}
		}
		if n := p.scalarInt(t, `SELECT count FROM quader.storage_counts WHERE kind = 'read_fallover'`); n != 4 {
			t.Errorf("the hour counts %d, want 4", n)
		}
		rec := history.New(history.Options{Store: history.PG{DB: p.db}, Replica: "web-0"})
		rec.Circuit("primary", true, &blob.StoreError{Status: 503, Kind: blob.KindTransient})
		if err := rec.Flush(ctx); err != nil {
			t.Fatal(err)
		}
		if n := p.count(t, "storage_events WHERE kind = 'unreachable' AND code = 'http_5xx' AND replica = 'web-0' AND ended_at IS NULL"); n != 1 {
			t.Fatalf("%d open intervals, want 1", n)
		}
		if err := rec.Stop(ctx); err != nil { // never started: nothing to stop
			t.Fatal(err)
		}
		if err := (history.PG{DB: p.db}).EndReplicaEvents(ctx, "web-0"); err != nil {
			t.Fatal(err)
		}
		if n := p.count(t, "storage_events WHERE ended_at IS NOT NULL"); n != 1 {
			t.Errorf("%d ended intervals, want the one a new life of web-0 ended", n)
		}
	})
	t.Run("R-STOR-110 beyond 100 intervals of a connection and replica in a day the last one counts occurrences", func(t *testing.T) {
		p := newPG(t)
		p.exec(t, `INSERT INTO quader.storage_events (connection_id, kind, replica, started_at, ended_at, last_seen_at, code)
			SELECT 'primary', 'unreachable', 'web-0', now() - make_interval(secs => g), now() - make_interval(secs => g), now(), 'timeout'
			  FROM generate_series(1, 100) AS g`)
		id, capped, err := history.PG{DB: p.db}.OpenEvent(ctx, "web-0", history.Event{
			Connection: "primary", Kind: history.KindUnreachable,
			Started: time.Now(), Code: "timeout",
		}, history.EventsPerDay)
		if err != nil || !capped || id != 0 {
			t.Fatalf("id %d capped %v: %v", id, capped, err)
		}
		if n := p.count(t, "storage_events"); n != 100 {
			t.Errorf("%d intervals, want 100", n)
		}
		if n := p.count(t, "storage_events WHERE occurrences = 2"); n != 1 {
			t.Errorf("%d intervals count a second occurrence, want the last one", n)
		}
	})
}

func TestR_STOR_110_TheTaskKeepsTheHistoryBounded(t *testing.T) {
	ctx := context.Background()
	p := newPG(t)
	a := blobtest.New()
	s, err := memStore(t, p, nil, a)
	if err != nil {
		t.Fatal(err)
	}
	d := commit(t, s, memData(1024, 34))
	old, stale := randomDigest(t), randomDigest(t)
	// a resolved finding and an open one older than the retention, a recent resolved one, and an open finding whose copy is present
	p.exec(t, `INSERT INTO quader.storage_findings (sha256, connection_id, status, source, reason, stored_size, blob_size, unavailable, detected_at,
			resolved_at, resolution) VALUES
		($1, 'primary', 'corrupt', 'scrub', 'digest', 1, 1, false, now() - interval '500 days', now() - interval '499 days', 'rewritten'),
		($2, 'primary', 'missing', 'other', 'missing', 1, 1, true, now() - interval '500 days', NULL, NULL),
		($1, 'primary', 'corrupt', 'scrub', 'digest', 1, 1, false, now() - interval '1 day', now(), 'rewritten'),
		($3, 'primary', 'corrupt', 'download', 'digest', 1024, 1024, true, now() - interval '1 hour', NULL, NULL)`,
		old.Bytes(), stale.Bytes(), d.Bytes())
	p.exec(t, `INSERT INTO quader.blob_copies (sha256, connection_id, object_key, stored_size, state) VALUES ($1, 'primary', 'x', 1, 'missing')`, stale.Bytes())
	p.exec(t, `DELETE FROM quader.storage_findings WHERE sha256 = $1 AND detected_at > now() - interval '1 day'`, stale.Bytes()) // the trigger's
	p.exec(t, `INSERT INTO quader.storage_events (connection_id, kind, replica, started_at, ended_at, last_seen_at, code) VALUES
		('primary', 'unreachable', 'web-0', now() - interval '500 days', now() - interval '499 days', now() - interval '499 days', 'timeout'),
		('primary', 'unreachable', 'web-gone', now() - interval '2 hours', NULL, now() - interval '1 hour', 'timeout')`)
	p.exec(t, `INSERT INTO quader.storage_counts (hour, connection_id, kind, reason, count) VALUES
		(date_trunc('hour', now() - interval '500 days', 'UTC'), 'primary', 'read_fallover', 'not_found', 3),
		(date_trunc('hour', now(), 'UTC'), 'primary', 'read_fallover', 'not_found', 1)`)

	var res jobResult
	if err := history.RunTask(ctx, p.db, history.TaskOptions{Retention: 9600 * time.Hour}, &res); err != nil {
		t.Fatal(err)
	}
	r := res.v.(history.Result)
	if r.FindingsDeleted != 1 || r.EventsDeleted != 1 || r.CountsDeleted != 1 || r.EventsEnded != 1 || r.FindingsResolved != 1 {
		t.Fatalf("result %+v", r)
	}
	if n := p.count(t, "storage_findings WHERE sha256 = $1 AND resolved_at IS NULL", stale.Bytes()); n != 1 {
		t.Error("the open finding older than the retention was deleted or resolved")
	}
	if n := p.count(t, "storage_findings WHERE sha256 = $1 AND resolution = 'reconciled'", d.Bytes()); n != 1 {
		t.Error("the open finding whose copy is present was not resolved as reconciled")
	}
	if n := p.count(t, "storage_events WHERE replica = 'web-gone' AND ended_at = last_seen_at"); n != 1 {
		t.Error("the open interval of a replica that is gone was not ended at the time it last saw it")
	}
	if n := p.count(t, "storage_counts"); n != 1 {
		t.Errorf("%d counts left, want the recent one", n)
	}
}
