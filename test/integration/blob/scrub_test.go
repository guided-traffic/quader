//go:build integration

package blob

import (
	"bytes"
	"context"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/blob/scrub"
)

// The scrub against PostgreSQL of the stack with in-memory stores (storage.md R-STOR-027, R-STOR-063: the statements of the
// ledger, the order of the due copies, the mark, the repair and its record, the list of damaged blobs and the overdue gauge;
// objects of 1 KiB).

func scrubber(tb testing.TB, p *pgEnv, s *blob.Store) *scrub.Scrubber {
	tb.Helper()
	sc, err := scrub.New(scrub.Options{Connections: s.Connections(), Ledger: scrub.PGLedger{DB: p.db, Store: s, Log: quiet}, RunDuration: time.Minute, PartSize: 16 << 20, Log: quiet})
	if err != nil {
		tb.Fatal(err)
	}
	return sc
}

func copyState(tb testing.TB, p *pgEnv, d blob.Digest, conn string) (state string, verified bool) {
	tb.Helper()
	if err := p.admin.QueryRow(context.Background(), `SELECT state, verified_at IS NOT NULL FROM quader.blob_copies WHERE sha256 = $1 AND connection_id = $2`,
		d.Bytes(), conn).Scan(&state, &verified); err != nil {
		tb.Fatal(err)
	}
	return state, verified
}

func TestR_STOR_027_TheScrubAgainstTheDatabase(t *testing.T) {
	ctx := context.Background()

	t.Run("R-STOR-027 a copy whose bytes were replaced behind Quader's back is marked corrupt, the blob stays ok, the copy is replaced from the other connection and is verified", func(t *testing.T) {
		p := newPG(t)
		a, b := blobtest.New(), blobtest.New()
		s, err := memStore(t, p, nil, a, b)
		if err != nil {
			t.Fatal(err)
		}
		good := memData(1024, 21)
		d := commit(t, s, good)
		// the commit wrote the first connection only (min_copies 1): put the second copy as the reconciliation would
		key := blob.Key("quader", d)
		b.Set(key, good, time.Now())
		p.exec(t, `INSERT INTO quader.blob_copies (sha256, connection_id, object_key, stored_size, state) VALUES ($1, 'secondary', $2, 1024, 'present')`, d.Bytes(), key)
		p.exec(t, `DELETE FROM quader.blob_copy_queue`)
		a.Set(key, memData(1024, 99), time.Now())
		p.exec(t, `UPDATE quader.blob_copies SET verified_at = now() - interval '40 days' WHERE connection_id = 'primary'`)

		res, err := scrubber(t, p, s).Run(ctx, scrub.RunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		var primary scrub.ConnectionResult
		for _, r := range res.Connections {
			if r.Connection == "primary" {
				primary = r
			}
		}
		if primary.Corrupt != 1 || primary.Repaired != 1 {
			t.Fatalf("primary = %+v", primary)
		}
		if got, _ := a.Data(key); !bytes.Equal(got, good) {
			t.Error("the object on primary was not replaced with the blob's bytes")
		}
		if st, verified := copyState(t, p, d, "primary"); st != blob.CopyPresent || !verified {
			t.Errorf("the copy on primary is %s, verified %v", st, verified)
		}
		if st, verified := copyState(t, p, d, "secondary"); st != blob.CopyPresent || !verified {
			t.Errorf("the copy on secondary, read whole as never verified, is %s, verified %v", st, verified)
		}
		if _, status, _ := p.state(t, d); status != blob.StatusOK {
			t.Errorf("the blob is %s, want ok", status)
		}
		if n := p.count(t, "blob_copy_queue"); n != 0 {
			t.Errorf("%d queue rows are left, want the repair's row gone", n)
		}
		// nothing is due any more: a second run reads nothing
		res, err = scrubber(t, p, s).Run(ctx, scrub.RunOptions{})
		if err != nil || res.Connections[0].Read+res.Connections[1].Read != 0 {
			t.Errorf("second run = %+v, %v", res, err)
		}
	})

	t.Run("R-STOR-027 with one connection a deleted object marks the copy and the blob missing; the list names the items; an upload of the same content heals both", func(t *testing.T) {
		p := newPG(t)
		a := blobtest.New()
		s, err := memStore(t, p, nil, a)
		if err != nil {
			t.Fatal(err)
		}
		data := memData(1024, 22)
		sp, err := s.Ingest(ctx, bytes.NewReader(data), blob.IngestOptions{ContentLength: int64(len(data))})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Commit(ctx, sp, p.link("pkg/a.bin", sp)); err != nil {
			t.Fatal(err)
		}
		sp.Close()
		d := sp.SHA256
		p.exec(t, `UPDATE quader.blob_copies SET verified_at = NULL`)
		if err := a.Delete(ctx, blob.Key("quader", d)); err != nil {
			t.Fatal(err)
		}
		if _, err := scrubber(t, p, s).Run(ctx, scrub.RunOptions{}); err != nil {
			t.Fatal(err)
		}
		if st, _ := copyState(t, p, d, "primary"); st != blob.CopyMissing {
			t.Errorf("the copy is %s, want missing", st)
		}
		if _, status, _ := p.state(t, d); status != blob.StatusMissing {
			t.Errorf("the blob is %s, want missing", status)
		}
		list, err := scrub.ListDamaged(ctx, p.db, scrub.DamagedQuery{Scope: scrub.ScopeBlobs, Limit: 10})
		if err != nil || len(list) != 1 || list[0].Digest != d || list[0].ItemCount != 1 || list[0].Items[0].Path != "pkg/a.bin" ||
			len(list[0].Copies) != 1 || list[0].Copies[0].State != blob.CopyMissing {
			t.Fatalf("list = %+v, %v", list, err)
		}
		if copies, err := scrub.ListDamaged(ctx, p.db, scrub.DamagedQuery{Scope: scrub.ScopeCopies, Connection: "secondary", Limit: 10}); err != nil || len(copies) != 0 {
			t.Errorf("copies scope narrowed to another connection = %+v, %v", copies, err)
		}
		commit(t, s, data)
		if st, _ := copyState(t, p, d, "primary"); st != blob.CopyPresent {
			t.Errorf("after the upload the copy is %s, want present", st)
		}
		if _, status, _ := p.state(t, d); status != blob.StatusOK {
			t.Errorf("after the upload the blob is %s, want ok", status)
		}
	})

	t.Run("R-STOR-027 the walk reads never-verified copies first, then the oldest verification past the period, and leaves the recent ones", func(t *testing.T) {
		p := newPG(t)
		a := blobtest.New()
		s, err := memStore(t, p, nil, a)
		if err != nil {
			t.Fatal(err)
		}
		var ds []blob.Digest
		for i := range 4 {
			ds = append(ds, commit(t, s, memData(1024, byte(30+i))))
		}
		p.exec(t, `UPDATE quader.blob_copies SET verified_at = now() WHERE sha256 = $1`, ds[0].Bytes())
		p.exec(t, `UPDATE quader.blob_copies SET verified_at = now() - interval '30 days 12 hours' WHERE sha256 = $1`, ds[1].Bytes())
		p.exec(t, `UPDATE quader.blob_copies SET verified_at = now() - interval '45 days' WHERE sha256 = $1`, ds[2].Bytes())
		p.exec(t, `UPDATE quader.blob_copies SET verified_at = NULL WHERE sha256 = $1`, ds[3].Bytes())
		l := scrub.PGLedger{DB: p.db, Store: s}
		var got []blob.Digest
		cur := scrub.Cursor{}
		for {
			page, next, err := l.Due(ctx, "primary", 720*time.Hour, cur, 1)
			if err != nil {
				t.Fatal(err)
			}
			if len(page) == 0 {
				break
			}
			got = append(got, page[0].Digest)
			cur = next
		}
		if len(got) != 3 || got[0] != ds[3] || got[1] != ds[2] || got[2] != ds[1] {
			t.Errorf("due order = %v, want the never verified, then 45 days, then 30.5 days", got)
		}
		// 45 days is past the period and the day of grace: one copy is overdue
		src := scrub.OverdueGauge(p.db, s.Connections())
		ms, err := src.Collect(ctx)
		if err != nil || len(ms) != 1 {
			t.Fatalf("overdue = %v, %v", ms, err)
		}
		var m dto.Metric
		if err := ms[0].Write(&m); err != nil {
			t.Fatal(err)
		}
		if v := m.GetGauge().GetValue(); v != 1 {
			t.Errorf("overdue copies = %v, want 1", v)
		}
	})

	t.Run("R-STOR-027 a copy marked missing a day ago whose object is back is read again by the next run and is present, its blob ok; a fresh mark is left", func(t *testing.T) {
		p := newPG(t)
		a := blobtest.New()
		s, err := memStore(t, p, nil, a)
		if err != nil {
			t.Fatal(err)
		}
		old, fresh := commit(t, s, memData(1024, 50)), commit(t, s, memData(1024, 51))
		p.exec(t, `UPDATE quader.blob_copies SET state = 'missing', verified_at = now()`)
		p.exec(t, `UPDATE quader.blobs SET content_status = 'missing'`)
		p.exec(t, `UPDATE quader.blob_copies SET checked_at = now() - interval '25 hours' WHERE sha256 = $1`, old.Bytes())
		res, err := scrubber(t, p, s).Run(ctx, scrub.RunOptions{})
		if err != nil || res.Connections[0].MissingLooked != 1 || res.Connections[0].MissingFound != 1 {
			t.Fatalf("res = %+v, %v", res, err)
		}
		if st, verified := copyState(t, p, old, "primary"); st != blob.CopyPresent || !verified {
			t.Errorf("the copy marked a day ago is %s, verified %v", st, verified)
		}
		if _, status, _ := p.state(t, old); status != blob.StatusOK {
			t.Errorf("its blob is %s, want ok", status)
		}
		if st, _ := copyState(t, p, fresh, "primary"); st != blob.CopyMissing {
			t.Errorf("the copy marked now is %s, want missing until its look is due", st)
		}
	})

	t.Run("R-STOR-027 the recheck sets a missing copy whose object is back present, and a stale identity records nothing", func(t *testing.T) {
		p := newPG(t)
		a := blobtest.New()
		s, err := memStore(t, p, nil, a)
		if err != nil {
			t.Fatal(err)
		}
		data := memData(1024, 40)
		d := commit(t, s, data)
		p.exec(t, `UPDATE quader.blob_copies SET state = 'missing', state_changed_at = now() - interval '1 hour'`)
		p.exec(t, `UPDATE quader.blobs SET content_status = 'missing'`)
		l := scrub.PGLedger{DB: p.db, Store: s}
		if ok, err := l.Whole(ctx, d, "primary", blob.CopyMissing, 1, 1024); err != nil || ok {
			t.Errorf("a record with a stale identity: %v, %v", ok, err)
		}
		copies, err := scrubber(t, p, s).Recheck(ctx, d)
		if err != nil || len(copies) != 1 || copies[0].State != blob.CopyPresent || copies[0].VerifiedAt.IsZero() {
			t.Fatalf("copies = %+v, %v", copies, err)
		}
		if _, status, _ := p.state(t, d); status != blob.StatusOK {
			t.Errorf("the blob is %s, want ok", status)
		}
	})
}
