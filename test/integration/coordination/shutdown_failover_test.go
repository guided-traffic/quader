//go:build integration

package coordination

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/jobs"
)

// switchover makes the test's database refuse new connections and ends every connection to it except the test's own, as a
// switchover of the PostgreSQL primary does for the seconds it takes (F-146): the replicas' pools find their connections closed
// and cannot open new ones. The returned function lets connections in again.
func (e *env) switchover(t *testing.T) (back func()) {
	t.Helper()
	ctx := context.Background()
	maint := e.st.admin(t, "postgres")
	var own int
	if err := e.admin.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&own); err != nil {
		t.Fatal(err)
	}
	if _, err := maint.Exec(ctx, "ALTER DATABASE "+e.db+" WITH ALLOW_CONNECTIONS false"); err != nil {
		t.Fatal(err)
	}
	if _, err := maint.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> $2", e.db, own); err != nil {
		t.Fatal(err)
	}
	var once atomic.Bool
	back = func() {
		if once.CompareAndSwap(false, true) {
			if _, err := maint.Exec(context.Background(), "ALTER DATABASE "+e.db+" WITH ALLOW_CONNECTIONS true"); err != nil {
				t.Error(err)
			}
		}
	}
	t.Cleanup(back)
	return back
}

func (e *env) replicaState(t *testing.T, uid string) string {
	t.Helper()
	var state string
	if err := e.admin.QueryRow(context.Background(), "SELECT coalesce((SELECT state FROM replica WHERE instance_uid = $1::uuid), 'gone')", uid).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

// TestShutdownThroughASwitchover puts a worker replica through what the worker of F-146 went through: its database connections
// are ended by the server and new ones are refused for some seconds, around the steps of its stop. The steps that write to its
// registry row retry within their step, so the row is draining from the drain step on and gone at the end whenever the
// database answers again inside the step (R-ARCH-024, R-OPS-003, Q-HA-307). F-137's shape is the first subtest: a job that only
// waits when the drain begins is released at once and finishes on the other worker (Q-ARCH-501).
func TestShutdownThroughASwitchover(t *testing.T) {
	e := newEnv(t)
	var first atomic.Value // the replica that ran the job first
	started := make(chan string, 4)
	kinds := func(r *jobs.Registry, rep *replica) {
		r.RegisterKind("it.wait", jobs.KindSpec{MaxAttempts: 3, Handler: func(ctx context.Context, run *jobs.Run) error {
			started <- rep.name
			if !first.CompareAndSwap(nil, rep.name) {
				return nil // the second worker finishes the job at once
			}
			select {
			case <-run.Draining():
				return fmt.Errorf("%w: the job only waited", jobs.ErrReleased)
			case <-ctx.Done():
				return context.Cause(ctx)
			case <-time.After(time.Minute):
				return nil
			}
		}})
	}
	ws := map[string]*replica{
		"fo-a": e.newReplica("fo-a", replicaOpts{role: "worker", kinds: kinds}),
		"fo-b": e.newReplica("fo-b", replicaOpts{role: "worker", kinds: kinds}),
	}

	t.Run("R-ARCH-024 R-ARCH-016 R-STOR-046 F-137 a job that only waits when the drain begins is released at once and finishes on the other worker", func(t *testing.T) {
		res, err := ws["fo-a"].enqueueTx(context.Background(), jobs.EnqueueSpec{Kind: "it.wait"})
		if err != nil {
			t.Fatal(err)
		}
		var claimer string
		select {
		case claimer = <-started:
		case <-time.After(15 * time.Second):
			t.Fatal("the job did not start")
		}
		other := "fo-a"
		if claimer == "fo-a" {
			other = "fo-b"
		}
		drainedAt := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		ws[claimer].worker.Drain(ctx)
		cancel()
		var second string
		select {
		case second = <-started:
		// The release is one statement and the other worker claims at its next poll (1 s) or at the wake-up signal; 10 s are far
		// below the minute the handler would otherwise wait and leave room for a loaded machine.
		case <-time.After(10 * time.Second):
			t.Fatalf("the job was not released at the drain of %s", claimer)
		}
		if second != other {
			t.Fatalf("the job started again on %s, want %s", second, other)
		}
		j := e.waitState(ws[other], res.Job.ID, jobs.StateSucceeded, 15*time.Second)
		if j.Attempt != 1 {
			t.Fatalf("attempt %d after the release at the drain, want 1: a release does not count an attempt", j.Attempt)
		}
		t.Logf("released at the drain of %s and finished on %s %s after the drain", claimer, other, time.Since(drainedAt).Round(10*time.Millisecond))
		ws[claimer].stop()
	})

	rep := e.newReplica("fo-c", replicaOpts{role: "worker", kinds: kinds})
	waitFor(t, "the row of fo-c", 10*time.Second, func() bool { return e.replicaState(t, rep.uid) == "ready" })

	t.Run("R-ARCH-024 R-OPS-003 F-146 the mark draining waits through a switchover inside the drain step: the row is draining when the step returns", func(t *testing.T) {
		back := e.switchover(t)
		// The database is away for 2 s of a drain step of 8 s (a worker's step is 2 s in production; the switchover of F-146
		// took 14 s, which the heartbeat covers after the step).
		go func() { time.Sleep(2 * time.Second); back() }()
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		rep.registry.Drain(ctx)
		cancel()
		if got := e.replicaState(t, rep.uid); got != "draining" {
			t.Fatalf("the row is %q when the drain step returns, want draining", got)
		}
	})

	t.Run("R-ARCH-024 R-HA-011 R-OPS-003 F-146 the final deletion waits through a switchover inside the close phase: the row is gone", func(t *testing.T) {
		back := e.switchover(t)
		go func() { time.Sleep(2 * time.Second); back() }()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := rep.registry.Stop(ctx); err != nil {
			t.Fatalf("Stop: %v", err)
		}
		if got := e.replicaState(t, rep.uid); got != "gone" {
			t.Fatalf("the row is %q after the stop, want it deleted", got)
		}
	})
}
