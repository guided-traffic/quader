//go:build integration

package postgres

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// R-HA-011, R-ARCH-024 step 6: a connection still acquired from a pool must not hold the close of the pools past the close
// phase; the error names the pool (F-97).
func TestR_HA_011_CloseContextEndsWithTheClosePhaseWhileAConnectionIsHeld(t *testing.T) {
	st := getStack(t)
	cfg, _, _ := migrated(t, st)
	db, err := postgres.Open(ctxT(t, 30*time.Second), cfg, "pod-close-held", meta.RoleWeb, quiet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	held, err := db.Main().Acquire(ctxT(t, 10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = db.CloseContext(ctx)
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("CloseContext took %s with a connection held, want the 300 ms deadline", took)
	}
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "main pool (1 connection held)") {
		t.Fatalf("CloseContext = %v, want the deadline and the held main pool", err)
	}
	// The error names the borrower: the function that acquired the connection and how long it has held it.
	if !strings.Contains(err.Error(), "by ") || !strings.Contains(err.Error(), "TestR_HA_011_CloseContextEndsWithTheClosePhaseWhileAConnectionIsHeld for ") {
		t.Fatalf("CloseContext = %v, want the borrower named", err)
	}
	held.Release()
}

// R-HA-011, R-ARCH-024 step 6: a readiness probe that is running a statement holds the control connection, and no check deadline cuts
// its statement (Q-HA-284); closing the pools ends the statement, so the control pool is back before the close deadline and a replica
// that stops while a probe is stuck does not report "control pool (1 connection held)". The connection the close ended is logged as
// that, not as a connection that came back closed, which names a fault (Q-ARCH-503, F-136).
func TestR_HA_011_CloseContextEndsAProbeStatementThatIsStillRunning(t *testing.T) {
	st := getStack(t)
	cfg, _, admin := migrated(t, st)
	logs := &testenv.SyncBuffer{}
	db, err := postgres.Open(ctxT(t, 30*time.Second), cfg, "pod-close-probe", meta.RoleWeb, slog.New(slog.NewJSONHandler(logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	// The probe reads schema_state; a lock held by another session keeps that statement waiting.
	mustExec(t, admin, "BEGIN")
	mustExec(t, admin, "LOCK TABLE quader.schema_state IN ACCESS EXCLUSIVE MODE")
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "ROLLBACK") })
	probed := make(chan error, 1)
	go func() { probed <- db.CheckSchema(context.Background(), "0.0.0") }()
	deadline := time.Now().Add(10 * time.Second)
	for db.Stats().Control.Acquired == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the probe never took the control connection")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.CloseContext(ctx); err != nil {
		t.Fatalf("CloseContext = %v, want the pools closed with the probe's statement ended", err)
	}
	select {
	case err := <-probed:
		if err == nil {
			t.Error("the probe's statement finished normally")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the probe is still running after the pools closed")
	}
	deadline = time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), "still running when the database pools closed was ended") {
		if time.Now().After(deadline) {
			t.Fatalf("the connection the close ended was not logged as that:\n%s", logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(logs.String(), `"borrower":"readiness probe: schema"`) {
		t.Errorf("the probe is not named as the borrower:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), "came back closed") {
		t.Errorf("the connection the close ended was logged as a fault:\n%s", logs.String())
	}
}
