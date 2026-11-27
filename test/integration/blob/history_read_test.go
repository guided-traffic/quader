//go:build integration

package blob

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/blob/history"
	"github.com/guided-traffic/quader/internal/blob/scrub"
	"github.com/guided-traffic/quader/internal/meta"
)

// The reads of the history for the storage page against PostgreSQL of the stack (storage.md R-STOR-111): a finding the scrub wrote, an
// interval the recorder wrote and the counts of an hour reach the problems, the cells and the findings' list the operations answer, and
// an acknowledgement is written and refused for a resolved finding. In-memory stores, objects of 1 KiB.

func TestR_STOR_111_AScrubFindingAndAnOutageReachThePage(t *testing.T) {
	ctx := context.Background()
	p := newPG(t)
	a, b := blobtest.New(), blobtest.New()
	s, err := memStore(t, p, nil, a, b)
	if err != nil {
		t.Fatal(err)
	}
	// The scrub finds the primary's copy of one blob corrupt and repairs it from the secondary: a finding resolved as rewritten.
	good := memData(1024, 41)
	d := commit(t, s, good)
	key := blob.Key("quader", d)
	b.Set(key, good, time.Now())
	p.exec(t, `INSERT INTO quader.blob_copies (sha256, connection_id, object_key, stored_size, state) VALUES ($1, 'secondary', $2, 1024, 'present')`, d.Bytes(), key)
	p.exec(t, `DELETE FROM quader.blob_copy_queue`)
	a.Set(key, memData(1024, 99), time.Now())
	p.exec(t, `UPDATE quader.blob_copies SET verified_at = NULL WHERE connection_id = 'primary'`)
	if _, err := scrubber(t, p, s).Run(ctx, scrub.RunOptions{Connection: "primary"}); err != nil {
		t.Fatal(err)
	}
	// A second blob's only copy goes missing on a read, unrepaired: an open finding no other copy held.
	lost := commit(t, s, memData(1024, 42))
	p.exec(t, `UPDATE quader.blob_copies SET state = 'missing', damage_source = 'download', damage_reason = 'not_found' WHERE sha256 = $1`, lost.Bytes())
	// The secondary was unreachable on one replica for a while, and the reads fell over meanwhile.
	rec := history.New(history.Options{Store: history.PG{DB: p.db}, Replica: "web-0"})
	rec.Circuit("secondary", true, &blob.StoreError{Status: 503, Kind: blob.KindTransient})
	rec.Count("secondary", blob.HistoryReadFallOver, "unavailable", 0)
	if err := rec.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	period, _ := history.ParsePeriod("24h")
	from, to := period.Window(now)

	t.Run("R-STOR-111 the problems of the period: the day's findings, the outage that lasts", func(t *testing.T) {
		records, err := history.ReadProblems(ctx, p.db, from, to)
		if err != nil {
			t.Fatal(err)
		}
		var damaged, outage *history.Problem
		problems := history.DeriveProblems(records, to)
		for i := range problems {
			switch problems[i].Kind {
			case history.ProblemCopiesDamaged:
				damaged = &problems[i]
			case history.ProblemUnreachable:
				outage = &problems[i]
			}
		}
		if damaged == nil || damaged.Count != 2 || damaged.Resolved != 1 || damaged.Open != 1 || damaged.UnavailableOpen != 1 || damaged.Tone != history.ToneError ||
			!damaged.Lasting() || damaged.Bytes != 2048 {
			t.Errorf("damaged = %+v", damaged)
		}
		if outage == nil || outage.Connection != "secondary" || !outage.Lasting() || outage.Replicas != 1 || len(outage.Codes) != 1 || outage.Codes[0] != "http_5xx" {
			t.Errorf("outage = %+v", outage)
		}
	})

	t.Run("R-STOR-111 the cells of the period: the findings, the outage and the fall-overs in the last cell", func(t *testing.T) {
		records, err := history.ReadCells(ctx, p.db, period, to)
		if err != nil {
			t.Fatal(err)
		}
		cells := history.DeriveCells(period, to, records, []string{"primary", "secondary"})
		if len(cells) != 2 || len(cells[0].Cells) != 24 {
			t.Fatalf("cells %+v", cells)
		}
		last, sec := cells[0].Cells[23], cells[1].Cells[23]
		if last.DamagedCopies != 2 || last.UnavailableBlobs != 1 || last.Tone != history.ToneError {
			t.Errorf("primary's last cell %+v", last)
		}
		if sec.Replicas != 1 || sec.ReadFallovers != 1 || sec.Tone != history.ToneWarning {
			t.Errorf("secondary's last cell %+v", sec)
		}
		f := cells[0].Findings
		if f.N != 2 || f.ByOutcome["rewritten"] != 1 || f.ByOutcome["open"] != 1 || f.BySource["scrub"] != 1 || f.BySource["download"] != 1 {
			t.Errorf("totals %+v", f)
		}
	})

	t.Run("R-STOR-111 the findings newest first, one per page by the exact cursor, with the items behind them", func(t *testing.T) {
		first, err := history.ListFindings(ctx, p.db, history.FindingQuery{From: from, State: history.FindingAll, Limit: 1})
		if err != nil || len(first) != 1 {
			t.Fatalf("%+v %v", first, err)
		}
		if first[0].Digest != lost || first[0].State() != history.FindingOpen || !first[0].Unavailable || first[0].Source != "download" {
			t.Errorf("first %+v", first[0])
		}
		next, err := history.ListFindings(ctx, p.db, history.FindingQuery{
			From: from, State: history.FindingAll, Limit: 5, AfterDetected: first[0].Detected, AfterID: first[0].ID,
		})
		if err != nil || len(next) != 1 || next[0].Digest != d || next[0].Resolution != "rewritten" || next[0].State() != history.FindingResolved {
			t.Errorf("next %+v %v", next, err)
		}
		open, err := history.ListFindings(ctx, p.db, history.FindingQuery{From: from, State: history.FindingOpen, Connection: "primary", Limit: 5})
		if err != nil || len(open) != 1 || open[0].Digest != lost {
			t.Errorf("open %+v %v", open, err)
		}
	})

	t.Run("R-STOR-111 an acknowledgement records the person and the note, never resolves; a resolved finding is refused", func(t *testing.T) {
		const person = "0193f2c4-0000-7000-8000-0000000000aa"
		p.exec(t, `INSERT INTO quader.principal (id, kind, name) VALUES ($1, 'user', 'alice')`, person)
		all, err := history.ListFindings(ctx, p.db, history.FindingQuery{From: from, State: history.FindingAll, Limit: 5})
		if err != nil || len(all) != 2 {
			t.Fatalf("%+v %v", all, err)
		}
		var got history.Finding
		err = p.db.InTx(ctx, func(tx meta.Tx) error {
			var err error
			got, err = history.Acknowledge(ctx, tx, all[0].ID, person, "The bucket's lifecycle rule removed it.")
			return err
		})
		if err != nil || got.State() != history.FindingAcknowledged || got.ByName != "alice" || got.Note != "The bucket's lifecycle rule removed it." {
			t.Errorf("acknowledged %+v %v", got, err)
		}
		if n := p.count(t, "storage_findings WHERE resolved_at IS NULL AND acknowledged_at IS NOT NULL"); n != 1 {
			t.Errorf("%d unresolved acknowledged findings, want 1", n)
		}
		err = p.db.InTx(ctx, func(tx meta.Tx) error {
			_, err := history.Acknowledge(ctx, tx, all[1].ID, "", "")
			return err
		})
		if !errors.Is(err, history.ErrFindingResolved) {
			t.Errorf("resolved: %v", err)
		}
		err = p.db.InTx(ctx, func(tx meta.Tx) error {
			_, err := history.Acknowledge(ctx, tx, all[0].ID+all[1].ID+100, "", "")
			return err
		})
		if !errors.Is(err, history.ErrNoFinding) {
			t.Errorf("unknown: %v", err)
		}
	})
}
