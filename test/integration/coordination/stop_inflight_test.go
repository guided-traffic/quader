//go:build integration

package coordination

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// The close phase of a replica stops its control loops (the change-log poll, the heartbeat, the fetch share's count) while one of
// them may be running its statement on the control connection: in the integration setting the phase begins 0.2 s after the stop,
// at the end of the drain delay, and a poll of 1 s with a statement of a few milliseconds is caught in it about once in a few
// hundred stops (F-136). A stop that cancelled the loop's context cut that statement: the driver closed the control connection,
// the pool logged "a database connection came back closed and is discarded" and the server was sent a cancel request, on a
// database that answered. These tests hold the statement in flight with a table lock, stop the component, and require that the
// stop waits for the statement instead of cutting it (Q-ARCH-503), and that a stop whose deadline ends while the statement still
// waits ends at that deadline.

// loggedDB opens the test's database for one in-process replica with a log the test reads: the pools log a connection that comes
// back closed.
func (e *env) loggedDB(t *testing.T, name string) (*postgres.DB, *testenv.SyncBuffer) {
	t.Helper()
	logs := &testenv.SyncBuffer{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := postgres.Open(ctx, e.cfg, name, "web", slog.New(slog.NewJSONHandler(logs, nil)))
	if err != nil {
		t.Fatalf("Open(%s): %v", name, err)
	}
	t.Cleanup(db.Close)
	return db, logs
}

// holdTable locks a table of the schema against every statement of the other sessions until the returned release is called or the
// test ends.
func (e *env) holdTable(t *testing.T, table string) (release func()) {
	t.Helper()
	conn := e.st.admin(t, e.db)
	ctx := context.Background()
	if _, err := conn.Exec(ctx, "BEGIN"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "LOCK TABLE "+schemaName+"."+table+" IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	done := false
	release = func() {
		if !done {
			done = true
			if _, err := conn.Exec(ctx, "ROLLBACK"); err != nil {
				t.Errorf("release the lock of %s: %v", table, err)
			}
		}
	}
	t.Cleanup(release)
	return release
}

// awaitWaiting returns once a statement of the replica's control connection waits for a lock on the server.
func (e *env) awaitWaiting(t *testing.T, db *postgres.DB, what string) {
	t.Helper()
	watch := e.st.admin(t, e.db)
	deadline := time.Now().Add(10 * time.Second)
	for {
		var n int64
		if err := watch.QueryRow(context.Background(),
			`SELECT count(*) FROM pg_stat_activity WHERE datname = $1 AND wait_event_type = 'Lock' AND state = 'active'`, e.db).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 && db.Stats().Control.Acquired == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never waited on the control connection", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// closedLines are the log lines of connections that came back closed.
func closedLines(logs *testenv.SyncBuffer) string {
	var out []string
	for line := range strings.Lines(logs.String()) {
		if strings.Contains(line, "came back closed") {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return strings.Join(out, "\n")
}

// stopWaitsForTheStatement stops a component whose control statement waits for a lock, and requires that the stop neither returns
// nor cuts the statement while the statement is in flight and its deadline is far; once the lock is released, the statement ends
// normally and the stop returns without an error.
func stopWaitsForTheStatement(t *testing.T, logs *testenv.SyncBuffer, release func(), stop func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- stop(ctx) }()
	// A stop that cuts the statement returns within milliseconds: the cut statement returns at once. A stop that waits does not
	// return before the lock is released, however long this look takes.
	select {
	case err := <-stopped:
		t.Fatalf("the stop returned (%v) while its control statement was in flight and the stop's deadline was 20 s away; the pool logged:\n%s",
			err, closedLines(logs))
	case <-time.After(500 * time.Millisecond):
	}
	if got := closedLines(logs); got != "" {
		t.Fatalf("the stop cut the control statement in flight:\n%s", got)
	}
	release()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("the stop after its statement ended: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the stop did not return after its control statement ended")
	}
	if got := closedLines(logs); got != "" {
		t.Fatalf("a control connection came back closed:\n%s", got)
	}
}

// stopEndsAtItsDeadline stops a component whose control statement waits for a lock that is not released, with a short deadline:
// the stop ends at it, and the statement is cut then.
func stopEndsAtItsDeadline(t *testing.T, logs *testenv.SyncBuffer, stop func(context.Context) error) {
	t.Helper()
	const bound = 300 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), bound)
	defer cancel()
	start := time.Now()
	_ = stop(ctx)
	if took := time.Since(start); took > bound+2*time.Second {
		t.Fatalf("the stop took %s with a deadline of %s", took.Round(time.Millisecond), bound)
	}
	deadline := time.Now().Add(5 * time.Second)
	for closedLines(logs) == "" {
		if time.Now().After(deadline) {
			t.Fatal("the statement still in flight at the stop's deadline was not cut")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// R-ARCH-024 step 6, R-ARCH-018, Q-ARCH-503, F-136: the stop of the change log waits for the poll it has in flight.
func TestR_ARCH_024_TheStopOfTheChangeLogWaitsForItsPollInFlight(t *testing.T) {
	start := func(t *testing.T) (*cluster.Changes, *postgres.DB, *testenv.SyncBuffer, *env) {
		e := newEnv(t)
		db, logs := e.loggedDB(t, "stop-poll")
		clu := defaultCluster()
		clu.EpochPollInterval = time.Hour // only the poll the test asks for runs
		c, err := cluster.NewChanges(cluster.ChangesOptions{DB: db.ControlPool(), Config: clu, WatchdogTimeout: 30 * time.Second, Log: quiet})
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Start(ctxT(t, 30*time.Second)); err != nil {
			t.Fatal(err)
		}
		return c, db, logs, e
	}
	t.Run("a poll in flight ends normally and the stop returns after it", func(t *testing.T) {
		c, db, logs, e := start(t)
		release := e.holdTable(t, "cluster_epoch")
		polled := make(chan error, 1)
		go func() { polled <- c.PollNow(context.Background()) }()
		e.awaitWaiting(t, db, "the poll")
		stopWaitsForTheStatement(t, logs, release, c.Stop)
		if err := <-polled; err != nil {
			t.Fatalf("the poll in flight at the stop: %v", err)
		}
	})
	t.Run("a poll still waiting at the stop's deadline is cut then", func(t *testing.T) {
		c, db, logs, e := start(t)
		e.holdTable(t, "cluster_epoch")
		go func() { _ = c.PollNow(context.Background()) }()
		e.awaitWaiting(t, db, "the poll")
		stopEndsAtItsDeadline(t, logs, c.Stop)
	})
}

// R-ARCH-024 step 6, R-OPS-003, Q-ARCH-503, F-136: the stop of the registry waits for the heartbeat it has in flight, then deletes
// the row.
func TestR_ARCH_024_TheStopOfTheRegistryWaitsForItsHeartbeatInFlight(t *testing.T) {
	start := func(t *testing.T) (*cluster.Registry, *postgres.DB, *testenv.SyncBuffer, *env, string) {
		e := newEnv(t)
		db, logs := e.loggedDB(t, "stop-beat")
		uid := uuidV4()
		sys := defaultSystem()
		sys.HeartbeatInterval = 100 * time.Millisecond
		r, err := cluster.NewRegistry(cluster.RegistryOptions{
			DB: db.ControlPool(), ShutdownDB: db.Main(),
			Identity: cluster.ReplicaIdentity{
				UID: uid, Name: "stop-beat", Version: "0.1.0", Revision: "it", Role: "web", StartedAt: time.Now(),
				SecretKeyIDs: []string{"k1"}, ActiveSecretKeyID: "k1",
			},
			Config: sys, WatchdogTimeout: 30 * time.Second, Log: quiet,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Start(ctxT(t, 30*time.Second)); err != nil {
			t.Fatal(err)
		}
		return r, db, logs, e, uid
	}
	t.Run("a heartbeat in flight ends normally and the row is deleted after it", func(t *testing.T) {
		r, db, logs, e, uid := start(t)
		release := e.holdTable(t, "replica")
		e.awaitWaiting(t, db, "the heartbeat")
		stopWaitsForTheStatement(t, logs, release, r.Stop)
		var n int64
		if err := e.admin.QueryRow(context.Background(), "SELECT count(*) FROM "+schemaName+".replica WHERE instance_uid = $1", uid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatal("the row of the stopped replica is still there")
		}
	})
	t.Run("a heartbeat still waiting at the stop's deadline is cut then", func(t *testing.T) {
		r, db, logs, e, _ := start(t)
		e.holdTable(t, "replica")
		e.awaitWaiting(t, db, "the heartbeat")
		stopEndsAtItsDeadline(t, logs, r.Stop)
	})
}

// R-ARCH-024 step 6, R-REPO-040, Q-ARCH-503, F-136: the stop of the fetch share's count waits for the count it has in flight.
func TestR_ARCH_024_TheStopOfTheFillCountWaitsForItsCountInFlight(t *testing.T) {
	start := func(t *testing.T) (*cluster.FillCount, *postgres.DB, *testenv.SyncBuffer, *env) {
		e := newEnv(t)
		db, logs := e.loggedDB(t, "stop-fill")
		c, err := cluster.NewFillCount(db.ControlPool(), 3*time.Second, 100*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Start(ctxT(t, 30*time.Second)); err != nil {
			t.Fatal(err)
		}
		return c, db, logs, e
	}
	t.Run("a count in flight ends normally and the stop returns after it", func(t *testing.T) {
		c, db, logs, e := start(t)
		release := e.holdTable(t, "replica")
		e.awaitWaiting(t, db, "the count")
		stopWaitsForTheStatement(t, logs, release, c.Stop)
	})
	t.Run("a count still waiting at the stop's deadline is cut then", func(t *testing.T) {
		c, db, logs, e := start(t)
		e.holdTable(t, "replica")
		e.awaitWaiting(t, db, "the count")
		stopEndsAtItsDeadline(t, logs, c.Stop)
	})
}

// ctxT is a context that ends after d or with the test.
func ctxT(t *testing.T, d time.Duration) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}
