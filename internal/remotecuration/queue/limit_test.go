package queue

import (
	"context"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/remotecuration"
)

// An upstream's rate limit holds the repository's queue (remote-curation.md R-CUR-011 "An upstream's rate limit", Q-CUR-170 to
// Q-CUR-172), over the store in memory and a fixed clock. Unit tier, milliseconds, no data.

func limited(retryAfter int, at time.Time) Result {
	return Result{Failed: true, Failure: &remotecuration.Failure{
		Class: remotecuration.ClassUpstreamRateLimited, Step: remotecuration.StepStatus, Code: errs.CodeUpstreamRateLimited, Status: 429,
		RetryAfter: retryAfter, At: at, Headers: map[string]string{"RateLimit-Remaining": "0;w=3600"},
	}}
}

func (f *fixture) state(r Row) remotecuration.State { return f.store.byID(r.ID).State }

func (f *fixture) dispatch() {
	f.t.Helper()
	if _, err := f.svc.Dispatch(context.Background(), nil); err != nil {
		f.t.Fatal(err)
	}
}

func TestR_CUR_011_AnUpstreamsRateLimitHoldsTheQueueWithoutCountingTheAttempt(t *testing.T) {
	f := newFixture(t, Settings{PerRepository: 2, MaxWaiting: 100, MaxAttempts: 5})
	a, b := f.add("a", remotecuration.OriginBulk), f.add("b", remotecuration.OriginBulk)
	c, d := f.add("c", remotecuration.OriginBulk), f.add("d", remotecuration.OriginBulk)

	// a meets the limit with the upstream's time of 40 minutes: it waits again, its attempt does not count, and nothing takes its place.
	s := f.finish(a, limited(2400, f.now))
	if s.State != remotecuration.StateWaiting || !s.Limited || !s.NotBefore.Equal(f.now.Add(40*time.Minute)) {
		t.Fatalf("the limited attempt: %+v", s)
	}
	if got := f.store.byID(a.ID); got.Attempts != 0 || got.Failure == nil || got.Failure.Headers["RateLimit-Remaining"] != "0;w=3600" {
		t.Fatalf("the row: attempts %d, failure %+v", got.Attempts, got.Failure)
	}
	if f.state(c) != remotecuration.StateWaiting || f.state(d) != remotecuration.StateWaiting {
		t.Fatal("a row started into the limit")
	}
	l, found, _ := f.svc.Limit(context.Background(), 1)
	if !found || !l.Hinted || l.Waits != 1 || !l.Since.Equal(f.now) || !l.Until.Equal(f.now.Add(40*time.Minute)) || l.Failure.Status != 429 {
		t.Fatalf("the limit: %+v", l)
	}

	// b, running when the limit began, meets it a second later: the same wait, not a second one, and the later time stays.
	f.now = f.now.Add(time.Second)
	f.finish(b, limited(60, f.now))
	if l2, _, _ := f.svc.Limit(context.Background(), 1); l2.Waits != 1 || !l2.Until.Equal(l.Until) {
		t.Fatalf("an answer of a row in flight: %+v", l2)
	}

	// While the limit holds, the dispatch starts nothing; a new row waits.
	e := f.add("e", remotecuration.OriginPerson)
	if res, err := f.svc.Dispatch(context.Background(), nil); err != nil || res.Started != 0 || f.state(e) != remotecuration.StateWaiting {
		t.Fatalf("the dispatch under the limit: %+v, %v, e %s", res, err, f.state(e))
	}

	// After its time one row asks first: e, a person's.
	f.now = l.Until.Add(time.Second)
	if res, _ := f.svc.Dispatch(context.Background(), nil); res.Started != 1 || f.state(e) != remotecuration.StateRunning {
		t.Fatalf("after the limit: started %d, e %s", res.Started, f.state(e))
	}
	// It meets the limit again, without a time: the second wait in a row, the queue's back-off of 1 minute (jitter 0.5).
	f.now = f.now.Add(time.Second)
	f.finish(e, limited(0, f.now))
	l3, _, _ := f.svc.Limit(context.Background(), 1)
	if l3.Waits != 2 || l3.Hinted || !l3.Until.Equal(f.now.Add(time.Minute)) || !l3.Since.Equal(l.Since) {
		t.Fatalf("the second wait: %+v", l3)
	}

	// After it, the asking row completes: the limit ends and the free places are taken at once.
	f.now = l3.Until.Add(time.Second)
	if res, _ := f.svc.Dispatch(context.Background(), nil); res.Started != 1 {
		t.Fatalf("after the second wait: started %d", res.Started)
	}
	probe := f.store.byID(e.ID)
	f.now = f.now.Add(time.Second)
	f.finish(probe, Result{Outcome: remotecuration.OutcomeCompleted})
	if _, found, _ := f.svc.Limit(context.Background(), 1); found {
		t.Fatal("the limit stays after an answer without it")
	}
	running := 0
	for _, r := range []Row{a, b, c, d} {
		if f.state(r) == remotecuration.StateRunning {
			running++
		}
	}
	if running != 2 || f.store.byID(e.ID).Attempts != 1 {
		t.Fatalf("after the limit ended: %d running, e's attempts %d", running, f.store.byID(e.ID).Attempts)
	}
}

func TestR_CUR_011_ARowThatRanBeforeTheLimitDoesNotEndIt(t *testing.T) {
	f := newFixture(t, Settings{PerRepository: 2, MaxWaiting: 100, MaxAttempts: 5})
	a, b := f.add("a", remotecuration.OriginBulk), f.add("b", remotecuration.OriginBulk)
	f.now = f.now.Add(time.Second)
	f.finish(a, limited(600, f.now))
	// b began before the limit's answer: its end proves nothing about the limit.
	f.now = f.now.Add(time.Second)
	f.finish(b, Result{Outcome: remotecuration.OutcomeCompleted})
	if l, found, _ := f.svc.Limit(context.Background(), 1); !found || !l.Holds(f.now) {
		t.Fatalf("a row that ran before the limit ended it: %+v", l)
	}
	// A client's pre-emption shares the class and is no limit.
	f.now = f.now.Add(time.Hour)
	f.dispatch()
	cur := f.store.byID(a.ID)
	if cur.State != remotecuration.StateRunning {
		t.Fatalf("a is %s after the limit", cur.State)
	}
	s := f.finish(cur, Result{Failed: true, Failure: &remotecuration.Failure{
		Class: remotecuration.ClassUpstreamRateLimited, Step: remotecuration.StepRequest, Code: "preempted",
	}})
	if s.Limited {
		t.Fatal("a pre-emption holds the queue")
	}
	// A limit that ended, in a repository with nothing left that waits or runs, is removed by the dispatch.
	f.now = f.now.Add(time.Hour)
	f.dispatch()
	f.finish(f.store.byID(a.ID), Result{Outcome: remotecuration.OutcomeCompleted})
	f.dispatch()
	if _, found, _ := f.svc.Limit(context.Background(), 1); found {
		t.Fatal("the limit of an empty queue stays")
	}
}

func TestR_CUR_011_TheLimitsTime(t *testing.T) {
	now := time.Date(2026, 11, 25, 5, 30, 51, 0, time.UTC)
	f := remotecuration.Failure{Class: remotecuration.ClassUpstreamRateLimited, Code: errs.CodeUpstreamRateLimited}
	for _, c := range []struct {
		name       string
		prev       *Limit
		retryAfter int
		waits      int
		wait       time.Duration
	}{
		{"the first, without a time: 30 s", nil, 0, 1, 30 * time.Second},
		{"the first, with the upstream's time", nil, 90, 1, 90 * time.Second},
		{"the upstream's time is capped at six hours", nil, 86400, 1, 6 * time.Hour},
		{"the fourth in a row: 4 minutes", &Limit{Since: now.Add(-time.Hour), Until: now.Add(-time.Second), Waits: 3}, 0, 4, 4 * time.Minute},
		{"the back-off is capped at an hour", &Limit{Since: now.Add(-9 * time.Hour), Until: now, Waits: 12}, 0, 13, time.Hour},
		{"an answer while it holds keeps the later time", &Limit{Since: now, Until: now.Add(time.Hour), Waits: 2, Hinted: true}, 5, 2, time.Hour},
	} {
		fc := f
		fc.RetryAfter = c.retryAfter
		var prev Limit
		if c.prev != nil {
			prev = *c.prev
		}
		got := nextLimit(prev, c.prev != nil, 7, fc, now, 0.5)
		if got.Waits != c.waits || !got.Until.Equal(now.Add(c.wait)) || got.RepoID != 7 {
			t.Errorf("%s: waits %d until %s, want %d and +%s", c.name, got.Waits, got.Until.Sub(now), c.waits, c.wait)
		}
	}
}
