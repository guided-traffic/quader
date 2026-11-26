package usage

import (
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// The usage buffer of a Bastion replica (R-LIM-045, Q-LIM-308): rows of bytes, no other data.

var at = time.Date(2026, 11, 26, 12, 7, 30, 0, time.UTC)

// TestR_LIM_045_TheUsageReportIsBoundedAndFoldsTheRestIntoAnOverflowRow: a buffer of two rows keeps the first two keys, folds the
// requests of three more subjects (one of them twice) into one overflow row per repository and kind that counts three subjects, and
// the report it drains passes the interface's bounds.
func TestR_LIM_045_TheUsageReportIsBoundedAndFoldsTheRestIntoAnOverflowRow(t *testing.T) {
	b := New(2)
	for _, s := range []string{"anonymous:203.0.113.1", "user:u1", "anonymous:203.0.113.2", "anonymous:203.0.113.3", "anonymous:203.0.113.2", "anonymous:203.0.113.4"} {
		b.Add(Delta{At: at, Subject: s, Repository: "web/npm-all", Rule: "r1", Requests: 1, Cached: 10})
	}
	b.Add(Delta{At: at, Subject: "user:u1", Repository: "web/npm-all", Rule: "r1", Limited: "cachedBytesPerHour"})
	b.Add(Delta{At: at, Subject: "user:u1", Requests: 1}) // no repository: counted in the budgets, no row
	rep := b.Drain()
	if err := rep.Validate(); err != nil {
		t.Fatal(err)
	}
	if rep.BucketSeconds != 300 || len(rep.Rows) != 2 || len(rep.Overflow) != 1 {
		t.Fatalf("report %+v", rep)
	}
	o := rep.Overflow[0]
	if o.SubjectKind != "anonymous" || o.Subjects != 3 || o.Requests != 4 || o.CachedBytes != 40 || !o.BucketStart.Equal(time.Date(2026, 11, 26, 12, 5, 0, 0, time.UTC)) {
		t.Fatalf("overflow %+v", o)
	}
	for _, r := range rep.Rows {
		if r.Subject == "user:u1" && r.Limited["cachedBytesPerHour"] != 1 {
			t.Fatalf("the user's row %+v", r)
		}
	}
	if b.Len() != 0 {
		t.Fatal("a drain leaves the buffer empty")
	}
}

// TestR_LIM_045_AReportThatCouldNotBeSentIsRestored: what a failed send hands back is added to what was counted meanwhile.
func TestR_LIM_045_AReportThatCouldNotBeSentIsRestored(t *testing.T) {
	b := New(0)
	b.Add(Delta{At: at, Subject: "user:u1", Repository: "web/npm-all", Rule: "r1", Requests: 2, Limited: "requestsPerHour"})
	rep := b.Drain()
	b.Add(Delta{At: at, Subject: "user:u1", Repository: "web/npm-all", Rule: "r1", Requests: 1})
	b.Restore(rep)
	b.Restore(iface.UsageReport{Overflow: []iface.UsageOverflow{{BucketStart: at, SubjectKind: "anonymous", Repository: "web/npm-all", Subjects: 5, Requests: 7}}})
	got := b.Drain()
	if len(got.Rows) != 1 || got.Rows[0].Requests != 3 || got.Rows[0].Limited["requestsPerHour"] != 1 || len(got.Overflow) != 1 || got.Overflow[0].Subjects != 5 {
		t.Fatalf("report %+v", got)
	}
}
