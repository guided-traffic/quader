package remotedescribe

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
)

type runner struct{}

func (runner) InTx(_ context.Context, fn func(meta.Tx) error) error { return fn(nil) }

type queue struct{ specs []jobs.EnqueueSpec }

func (q *queue) Enqueue(_ context.Context, _ meta.Tx, e jobs.EnqueueSpec) (jobs.Result, error) {
	q.specs = append(q.specs, e)
	return jobs.Result{Created: true}, nil
}

// port holds the ids of the packages left to describe; a batch queues those after the cursor, at most the limit, and records the
// priority it was asked for. A package queued once is described and left out of the next selection, as the data port's statement does.
type port struct {
	left       []int64
	priorities []int
}

func (p *port) QueueUndescribed(_ context.Context, after int64, limit, priority int) (int64, int, bool, error) {
	p.priorities = append(p.priorities, priority)
	next, n := after, 0
	for _, id := range p.left {
		if id > after && n < limit {
			next, n = id, n+1
		}
	}
	return next, n, n < limit, nil
}

func (p *port) HasUndescribed(context.Context) (bool, error) { return len(p.left) > 0, nil }

func ids(n int) []int64 {
	out := make([]int64, n)
	for i := range out {
		out[i] = int64(i + 1)
	}
	return out
}

// TestR_NPM_050_TheTaskQueuesWhatIsLeftInBatchesAndStopsAtItsBounds: a pass walks both formats from the cursor in batches of Batch at
// the low Priority and ends; it stops at MaxPerRun jobs, or at once while MaxPending describe jobs wait, and then queues the run after it
// with its cursor and the pause; a store without a package left queues no run at the start. Unit, microseconds.
func TestR_NPM_050_TheTaskQueuesWhatIsLeftInBatchesAndStopsAtItsBounds(t *testing.T) {
	ctx := context.Background()
	run := func(npm, deb *port, pending int64, cur Cursor) (Result, Cursor, *queue, int) {
		t.Helper()
		q := &queue{}
		task, err := New(Options{
			DB: runner{}, Jobs: q, Formats: []Format{{Kind: "npm-remote-describe", Port: npm}, {Kind: "deb-remote-describe", Port: deb}},
			Pending: func(context.Context, []string) (int64, error) { return pending, nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		saves := 0
		res, err := task.Pass(ctx, &cur, func(Cursor) error { saves++; return nil })
		if err != nil {
			t.Fatal(err)
		}
		return res, cur, q, saves
	}

	npm, deb := &port{left: ids(250)}, &port{left: ids(30)}
	res, cur, q, saves := run(npm, deb, 0, Cursor{})
	if res != (Result{State: "done", Queued: 280}) || cur != (Cursor{Format: 2}) || len(q.specs) != 0 || saves != 4 {
		t.Errorf("a whole pass: %+v, cursor %+v, %d continuations, %d saves", res, cur, len(q.specs), saves)
	}
	for _, p := range append(npm.priorities, deb.priorities...) {
		if p != Priority {
			t.Errorf("a batch was queued at priority %d, want %d (after a fill's jobs)", p, Priority)
		}
	}

	// The cap of a run: it continues with its cursor after the pause.
	res, cur, q, _ = run(&port{left: ids(MaxPerRun + 500)}, &port{}, 0, Cursor{})
	if res.State != "continued" || res.Queued != MaxPerRun || len(q.specs) != 1 || q.specs[0].Delay != ContinueAfter || q.specs[0].Kind != TaskName {
		t.Fatalf("at the cap: %+v, %+v", res, q.specs)
	}
	var args Cursor
	if b, _ := json.Marshal(q.specs[0].Args); json.Unmarshal(b, &args) != nil || args != (Cursor{Format: 0, After: MaxPerRun}) || cur != args {
		t.Errorf("the run after it starts at %+v, the cursor is %+v", args, cur)
	}

	// Many describe jobs wait: the run queues nothing and continues later.
	res, _, q, _ = run(&port{left: ids(10)}, &port{}, MaxPending, Cursor{})
	if res != (Result{State: "continued"}) || len(q.specs) != 1 {
		t.Errorf("with %d jobs waiting: %+v, %d continuations", MaxPending, res, len(q.specs))
	}

	// The start: a run is queued only while a package is left.
	for _, c := range []struct {
		npm, deb *port
		want     bool
	}{{&port{}, &port{}, false}, {&port{}, &port{left: ids(1)}, true}} {
		q := &queue{}
		task, _ := New(Options{DB: runner{}, Jobs: q, Formats: []Format{{Port: c.npm}, {Port: c.deb}}})
		if got, err := task.EnqueueIfNeeded(ctx); err != nil || got != c.want || len(q.specs) != map[bool]int{false: 0, true: 1}[c.want] {
			t.Errorf("start with left %v/%v: queued %v (%v), %d jobs", len(c.npm.left), len(c.deb.left), got, err, len(q.specs))
		}
	}
}
