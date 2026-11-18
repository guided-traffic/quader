package cluster

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/guided-traffic/quader/internal/config"
)

// stuckDB is a control pool whose every call waits for a connection that never frees up, as a saturated pool does under host
// load: it returns only when the context of the call ends, with that context's error.
type stuckDB struct{}

func (stuckDB) Exec(ctx context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	<-ctx.Done()
	return pgconn.CommandTag{}, ctx.Err()
}

func (stuckDB) Query(ctx context.Context, _ string, _ ...any) (pgx.Rows, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (stuckDB) QueryRow(ctx context.Context, _ string, _ ...any) pgx.Row {
	<-ctx.Done()
	return nil
}

// okDB is a pool that deletes at once.
type okDB struct{}

func (okDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("DELETE 1"), nil
}

func (okDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }

func (okDB) QueryRow(context.Context, string, ...any) pgx.Row { return nil }

func stopAfter(t *testing.T, r *Registry, deadline time.Duration) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Stop(ctx) }()
	select {
	case err := <-done:
		return err
	case <-time.After(deadline + 500*time.Millisecond):
		t.Fatal("Stop outlived the deadline of the close phase")
		return nil
	}
}

// R-HA-011: the deletion of the replica's own row is a close step, so it ends with the close phase and the process exits
// inside the pod's grace period. Behind a pool that cannot hand out a connection it used to wait its own five seconds, past
// the close deadline, and the phase was abandoned (F-94).
func TestR_HA_011_RegistryStopEndsWithTheClosePhase(t *testing.T) {
	r, err := NewRegistry(RegistryOptions{DB: stuckDB{}, Identity: ReplicaIdentity{UID: "uid", Name: "a", Role: "web"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := stopAfter(t, r, 200*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop = %v, want the deadline of the close phase", err)
	}
}

// R-HA-011, R-ARCH-024 step 6, R-OPS-003: the final deletion does not queue on the control connection, which the change-log
// poll and the readiness checks still use during the close phase; it runs on the idle request pool and ends without an error
// (Q-HA-202).
func TestR_HA_011_RegistryStopDoesNotWaitForTheControlConnection(t *testing.T) {
	r, err := NewRegistry(RegistryOptions{DB: stuckDB{}, ShutdownDB: okDB{}, Identity: ReplicaIdentity{UID: "uid", Name: "a", Role: "web"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := stopAfter(t, r, 200*time.Millisecond); err != nil {
		t.Fatalf("Stop = %v, want the row deleted on the request pool", err)
	}
}

// R-ARCH-024 step 1, R-OPS-003 (Q-HA-283): marking the row draining is the first thing a stopping replica does, and it runs on
// the request pool like the deletion. On the control connection it queued behind the poll and the readiness checks for the
// whole call timeout (10 s) when the database stalled, and that time came out of the budget of every later phase.
func TestR_ARCH_024_RegistryDrainDoesNotWaitForTheControlConnection(t *testing.T) {
	r, err := NewRegistry(RegistryOptions{DB: stuckDB{}, ShutdownDB: okDB{}, Identity: ReplicaIdentity{UID: "uid", Name: "a", Role: "web"}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { r.Drain(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Drain waited for the control connection")
	}
}

// flakyDB fails the first `fail` statements as a server that ended its connections does (a switchover of the primary), then
// executes every statement; it records the statements it executed.
type flakyDB struct {
	mu   sync.Mutex
	fail int
	ran  []string
}

func (f *flakyDB) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail > 0 {
		f.fail--
		return pgconn.CommandTag{}, &pgconn.PgError{Code: "57P01", Message: "terminating connection due to administrator command"}
	}
	f.ran = append(f.ran, sql)
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (f *flakyDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (f *flakyDB) QueryRow(context.Context, string, ...any) pgx.Row        { return nil }

func (f *flakyDB) executed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ran...)
}

func logTo(buf *syncBuf) *slog.Logger { return slog.New(slog.NewTextHandler(buf, nil)) }

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }

func (s *syncBuf) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// R-ARCH-024 step 1, R-OPS-003 (F-146, Q-HA-307): the mark draining is retried within the drain step. A switchover of the
// primary ends the replica's connections for some seconds, and one attempt that failed then was the only one.
func TestR_ARCH_024_RegistryDrainRetriesTheMarkWithinTheDrainStep(t *testing.T) {
	db := &flakyDB{fail: 2}
	var logs syncBuf
	r, err := NewRegistry(RegistryOptions{DB: stuckDB{}, ShutdownDB: db, Identity: ReplicaIdentity{UID: "uid", Name: "a", Role: "worker"}, Log: logTo(&logs)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r.Drain(ctx)
	if n := len(db.executed()); n != 1 {
		t.Fatalf("the mark draining reached the row %d times after two failed attempts, want once", n)
	}
	if !strings.Contains(logs.String(), "replica row: write failed; retrying") || !strings.Contains(logs.String(), "replica row marked draining") {
		t.Errorf("the log does not say that the mark waited for the database:\n%s", logs.String())
	}
}

// R-ARCH-024 step 1, R-HA-011 (Q-ARCH-460): the retries end with the drain step, whatever the database does.
func TestR_ARCH_024_RegistryDrainRetriesEndWithTheDrainStep(t *testing.T) {
	var logs syncBuf
	r, err := NewRegistry(RegistryOptions{DB: stuckDB{}, ShutdownDB: &flakyDB{fail: 1 << 30}, Identity: ReplicaIdentity{UID: "uid", Name: "a", Role: "worker"}, Log: logTo(&logs)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	begin := time.Now()
	r.Drain(ctx)
	// The step is 600 ms; the 2 s leave room for a loaded machine, and a loop that ignored the step would run on for good.
	if took := time.Since(begin); took > 2*time.Second {
		t.Errorf("Drain took %s, past the end of its step", took)
	}
	if !strings.Contains(logs.String(), "replica row not marked draining in the drain step") {
		t.Errorf("the failed mark is not logged:\n%s", logs.String())
	}
}

// R-HA-011, R-OPS-003 (Q-HA-307): the final deletion is retried within the close phase, so a switchover of a few seconds during
// the stop does not leave the row behind.
func TestR_HA_011_RegistryStopRetriesTheDeletion(t *testing.T) {
	db := &flakyDB{fail: 3}
	var logs syncBuf
	r, err := NewRegistry(RegistryOptions{DB: stuckDB{}, ShutdownDB: db, Identity: ReplicaIdentity{UID: "uid", Name: "a", Role: "worker"}, Log: logTo(&logs)})
	if err != nil {
		t.Fatal(err)
	}
	if err := stopAfter(t, r, 5*time.Second); err != nil {
		t.Fatalf("Stop = %v, want the row deleted on a later attempt", err)
	}
	if ran := db.executed(); len(ran) != 1 || !strings.Contains(ran[0], "DELETE FROM replica") {
		t.Fatalf("statements that reached the database: %v, want the deletion once", ran)
	}
	if !strings.Contains(logs.String(), "replica row deleted") {
		t.Errorf("the log does not say that the deletion waited for the database:\n%s", logs.String())
	}
}

// R-OPS-003 (Q-OPS-210, Q-HA-307): a row that cannot be deleted is named in the log with what it will be listed as: signed off
// when the mark draining reached it, lost when the database answered neither.
func TestR_OPS_003_RegistryStopNamesWhatTheRowLeftBehindIsListedAs(t *testing.T) {
	for _, c := range []struct {
		name   string
		marked bool
		want   string
	}{
		{"marked", true, "it was marked draining and counts as signed off"},
		{"not marked", false, "replica row neither marked draining nor deleted"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var logs syncBuf
			r, err := NewRegistry(RegistryOptions{DB: stuckDB{}, ShutdownDB: stuckDB{}, Identity: ReplicaIdentity{UID: "uid", Name: "a", Role: "worker"}, Log: logTo(&logs)})
			if err != nil {
				t.Fatal(err)
			}
			r.marked = c.marked
			if err := stopAfter(t, r, 300*time.Millisecond); err == nil {
				t.Fatal("Stop reported a deletion that did not happen")
			}
			if !strings.Contains(logs.String(), c.want) {
				t.Errorf("log does not contain %q:\n%s", c.want, logs.String())
			}
		})
	}
}

// R-OPS-003 (Q-HA-309): a heartbeat that has not reached the row for longer than system.heartbeat_stale_after is logged once as
// what it means to the other replicas, and its return is logged.
func TestR_OPS_003_RegistryLogsAHeartbeatThatWentStale(t *testing.T) {
	var logs syncBuf
	r, err := NewRegistry(RegistryOptions{
		DB: stuckDB{}, Identity: ReplicaIdentity{UID: "uid", Name: "a", Role: "worker"},
		Config: config.System{HeartbeatInterval: time.Second, HeartbeatStaleAfter: 3 * time.Second}, Log: logTo(&logs),
	})
	if err != nil {
		t.Fatal(err)
	}
	r.lastWritten = time.Now().Add(-2 * time.Second)
	fail := errors.New("dial error")
	r.noteHeartbeat(fail)
	if strings.Contains(logs.String(), "level=ERROR") {
		t.Fatalf("a heartbeat failed 2 s after the last write is reported as stale:\n%s", logs.String())
	}
	r.lastWritten = time.Now().Add(-4 * time.Second)
	r.noteHeartbeat(fail)
	r.noteHeartbeat(fail)
	if n := strings.Count(logs.String(), "the other replicas list this replica as lost"); n != 1 {
		t.Fatalf("the stale heartbeat is logged %d times, want once:\n%s", n, logs.String())
	}
	r.noteHeartbeat(nil)
	if !strings.Contains(logs.String(), "replica heartbeat written again") {
		t.Errorf("the heartbeat's return is not logged:\n%s", logs.String())
	}
}
