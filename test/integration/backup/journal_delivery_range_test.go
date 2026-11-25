//go:build integration

package backup

import (
	"context"
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/qname"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules/store"
)

// rangesOf lists a repository's package rules as "<name>:<effect> <from>..<to>[ below]" in name order, "-" for no bound.
func (s *twinSide) rangesOf(key string) string {
	s.t.Helper()
	return s.text(`SELECT COALESCE(string_agg(name || ':' || effect || ' ' || COALESCE(from_version, '-') || '..' || COALESCE(to_version, '-')
		|| CASE WHEN to_inclusive THEN '' ELSE ' below' END, ',' ORDER BY name), '')
		FROM remote_delivery_rules WHERE kind = 'package' AND repo_id = (SELECT id FROM repositories WHERE key = $1)`, key)
}

// TestR_CUR_026_TheReplayKeepsTheNarrowerRange: the ranges of a package's allow written after t are replayed into the copy made at t
// (remote-curation.md R-CUR-026, Q-CUR-225, R-REPO-061), in mode allExceptBlocked: a range narrowed on an allow that had one keeps the
// narrower of the two; a range set where the package had no rule of its own gives it the allow with the range; a removed allow with a
// range is not removed where the mode delivers the package without it (that would widen); a range that shares no version with the
// restored one blocks the package. A second replay applies nothing again. Data: four rule rows, no artifact; about 0.5 s.
func TestR_CUR_026_TheReplayKeepsTheNarrowerRange(t *testing.T) {
	w := newNarrowWorld(t)
	ctx := context.Background()
	w.create("remote", "npm-ranged", "npm", "")
	tg := store.Target{ID: w.src.repoID("npm-ranged"), Name: qname.Repo{Project: "web", Key: "npm-ranged"}, Format: "npm"}
	put := func(rs *store.Store, name string, r rc.Range) string {
		t.Helper()
		wr, err := rs.Put(ctx, tg, ruleCaller, store.Input{Kind: rc.KindPackage, Name: name, Effect: rc.Allow, Range: r})
		if err != nil {
			t.Fatalf("the range of %s: %v", name, err)
		}
		return wr.ID
	}
	limit := func(rs *store.Store, name string, r rc.Range) {
		t.Helper()
		if err := w.src.db.InTx(ctx, func(tx meta.Tx) error {
			_, err := rs.SwitchTx(ctx, tx, tg, ruleCaller, store.SwitchInput{Name: name, Delivered: true, Range: &r})
			return err
		}); err != nil {
			t.Fatalf("the switch of %s: %v", name, err)
		}
	}
	rs := rulesStore(t, w.src)
	put(rs, "lodash", rc.Range{From: "1.0.0"})
	react := put(rs, "react", rc.Range{From: "2.0.0"})
	put(rs, "vue", rc.Range{From: "1.0.0", To: "3.0.0", ToInclusive: true})
	w.snapshot()

	rs = rulesStore(t, w.src)
	limit(rs, "lodash", rc.Range{From: "2.0.0", To: "4.0.0"})
	limit(rs, "express", rc.Range{From: "1.5.0"})
	if err := rs.Delete(ctx, tg, ruleCaller, react); err != nil {
		t.Fatal(err)
	}
	limit(rs, "vue", rc.Range{From: "5.0.0"})
	js := w.ship(4)
	w.src.close()

	rst := w.tw.open(w.tw.restored)
	if got := rst.rangesOf("npm-ranged"); got != "lodash:allow 1.0.0..-,react:allow 2.0.0..-,vue:allow 1.0.0..3.0.0" {
		t.Fatalf("the rules at t: %s", got)
	}
	sum, _ := rst.replay(js, w.tw.at, rst.changes)
	if o := outcomes(sum)["repository.narrowed web/npm-ranged"]; len(o) != 4 || slices.ContainsFunc(o, func(x journal.Outcome) bool { return x == journal.Suspended }) {
		t.Errorf("repository.narrowed web/npm-ranged: %v, want four, none suspended", o)
	}
	want := "express:allow 1.5.0..-,lodash:allow 2.0.0..4.0.0 below,react:allow 2.0.0..-,vue:block -..-"
	if got := rst.rangesOf("npm-ranged"); got != want {
		t.Errorf("the restored rules: %s, want %s", got, want)
	}
	if rst.disabledRepo("npm-ranged") {
		t.Error("npm-ranged was disabled")
	}
	secondPass(t, rst, js, w.tw.at, 4)
}
