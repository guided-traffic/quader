package answers

import (
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// TestR_BAS_026_AnAppliedIDIsKeptWhileTheFeedSendsItsEntry: the ID of an applied entry is not forgotten by time. A
// read that returns the entry again keeps it, however long after the first receipt (a quiet feed re-sends the same
// window on every read, F-116); a read from a cursor after the entry that does not return it ends it, and an entry
// at or after the cursor of a read stays known whether or not that read returned it (Q-BAS-152).
func TestR_BAS_026_AnAppliedIDIsKeptWhileTheFeedSendsItsEntry(t *testing.T) {
	const period, grace = 30 * time.Second, 90 * time.Second
	log := NewFeedLog()
	grants := iface.FeedEntry{ID: "g1", At: t0, Kind: iface.FeedGrants}
	later := iface.FeedEntry{ID: "g2", At: t0.Add(time.Minute), Kind: iface.FeedGrants}
	if !log.Record(grants, t0) || !log.Record(later, t0.Add(time.Minute)) {
		t.Fatal("not applied")
	}
	// An hour of a quiet feed: the cursor stands at the later entry and every read returns both.
	now := t0.Add(time.Hour)
	log.Settle(later.At, []iface.FeedEntry{grants, later})
	log.Prune(PruneCutoff(now, period, grace))
	if log.Record(grants, now) || log.Record(later, now) {
		t.Fatal("an entry the feed still sends was applied again")
	}
	s := State{Now: now, Period: period, HistoryStart: t0.Add(-time.Hour), Log: log}
	if got := s.Check(Answer{Kind: Shape, ObtainedAt: now.Add(-time.Second)}); got != Usable {
		t.Fatalf("an answer obtained after the entries' margin: %q", got)
	}
	// The cursor moved on: a read returns the later entry but not the first, which has left the window.
	log.Settle(t0.Add(6*time.Minute), []iface.FeedEntry{later})
	if log.Known() != 1 {
		t.Fatalf("known IDs = %d, want the one the feed still sends", log.Known())
	}
	// A read whose cursor lies before an entry and does not return it says nothing about it.
	log.Settle(t0, nil)
	if log.Known() != 1 {
		t.Fatalf("an entry at or after the cursor was forgotten: %d known", log.Known())
	}
	if !log.Record(grants, now) {
		t.Fatal("the ID of an entry that left the window was kept")
	}
}
