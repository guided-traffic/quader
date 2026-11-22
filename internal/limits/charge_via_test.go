package limits

import (
	"context"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/qname"
)

// usageRow is what the usage store learnt of one row: subject, serving repository and virtual repository.
type usageRow struct {
	subject   string
	repo, via int64
}

type usageCounts struct{ requests, refused, cached, uncached, events int64 }

// rows sums the deltas the usage store received per row.
func (u *usageRecorder) rows() map[usageRow]usageCounts {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := map[usageRow]usageCounts{}
	for _, d := range u.d {
		k := usageRow{subject: d.Subject.String(), repo: d.RepoID, via: d.ViaRepoID}
		s := out[k]
		s.requests += d.Requests
		s.refused += d.Refused
		s.cached += d.Cached
		s.uncached += d.Uncached
		for _, ev := range d.Events {
			s.events += ev.Count
		}
		out[k] = s
	}
	return out
}

// activeCharges is the number of charges the flush still walks.
func (e *Engine) activeCharges() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.active)
}

// TestR_LIM_027_ARequestIsRecordedInTheRowOfTheRepositoryThatServedIt: a flush of the charge buffer that falls while a request of a
// team's virtual repository still walks its members (after the dispatcher recorded the addressed repository, before a member of the
// project upstream fetched) records nothing of the request under the addressed repository: the request and the limits it met are
// recorded once it has ended, in the row of the repository that served it and the virtual repository it was reached through, beside
// its bytes (R-LIM-027, Q-LIM-63, Q-LIM-75).
func TestR_LIM_027_ARequestIsRecordedInTheRowOfTheRepositoryThatServedIt(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, ModeAudit, BuiltinProfiles()[ProfileAnonymous])
	c := r.e.Begin(Caller{Kind: CallerUser, PrincipalID: "u-reader", TokenID: "tok-r"}, qname.Repo{Project: "team", Key: "npm-all"})
	if st := r.e.Check(ctx, c, time.Time{}, nil); st.Err != nil {
		t.Fatal(st.Err)
	}
	c.Served(5)                      // the dispatcher records the addressed virtual repository
	c.note("requestsPerHour", false) // a limit the request met at stage 7, in Audit
	r.e.Flush(ctx)                   // the flush falls while the walk is at the members
	c.ServedThrough(6, 5)
	c.AddUncached(1000) // the member's fetch from its upstream
	c.Served(6)         // the item store serves what the fetch stored
	c.AddCached(1000)
	r.e.Finish(c)
	r.e.Flush(ctx)
	rows := r.usage.rows()
	want := usageRow{subject: c.Subject.String(), repo: 6, via: 5}
	if got := rows[want]; got != (usageCounts{requests: 1, cached: 1000, uncached: 1000, events: 1}) {
		t.Fatalf("the row of the member through the virtual repository holds %+v, want the request, its limit and its bytes; all rows %+v", got, rows)
	}
	if len(rows) != 1 {
		t.Fatalf("the request was recorded in more rows than the member's: %+v", rows)
	}
	if n := r.e.activeCharges(); n != 0 {
		t.Fatalf("%d charges left in the buffer after the request ended", n)
	}
}

// TestR_LIM_027_TheVirtualRepositoryAWellKnownMemberServedThroughNamesTheRowWhileItServes: a request of a team's virtual repository that a
// member of the project upstream serves records the virtual repository beside the serving one (Q-LIM-63); a later serving repository,
// or a member recorded without one, leaves it out of the row.
func TestR_LIM_027_TheVirtualRepositoryAWellKnownMemberServedThroughNamesTheRowWhileItServes(t *testing.T) {
	c := &Charge{}
	c.Served(5) // the dispatcher records the addressed virtual repository first
	if got := c.viaOf(c.ServedRepo()); got != 0 {
		t.Fatalf("via of the addressed repository = %d", got)
	}
	c.ServedThrough(6, 5)
	if c.ServedRepo() != 6 || c.viaOf(6) != 5 {
		t.Fatalf("served %d via %d, want 6 via 5", c.ServedRepo(), c.viaOf(6))
	}
	c.Served(6) // the item store records the same member again
	if c.viaOf(c.ServedRepo()) != 5 {
		t.Fatal("recording the same member again dropped the virtual repository")
	}
	c.Served(5) // a fetch of the member found nothing: the addressed repository is put back
	if c.viaOf(c.ServedRepo()) != 0 {
		t.Fatal("the virtual repository names a row the member did not serve")
	}
	var none *Charge
	none.ServedThrough(6, 5) // a request without the limits records nothing
}
