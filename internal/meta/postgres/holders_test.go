package postgres

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
)

func borrow(h *holders, c *pgx.Conn) { h.acquired(context.Background(), c) }

// TestR_HA_011_AHeldConnectionIsNamedByItsBorrower: a connection that is checked out is listed with the function that acquired it, or
// with the label the borrower gave, and for how long; a returned one is gone (R-HA-011, R-ARCH-024 step 6).
func TestR_HA_011_AHeldConnectionIsNamedByItsBorrower(t *testing.T) {
	h := &holders{}
	c1, c2, c3 := &pgx.Conn{}, &pgx.Conn{}, &pgx.Conn{}
	borrow(h, c1)
	h.acquired(WithHolder(context.Background(), "readiness probe: database"), c2)
	borrow(h, c3)
	h.released(c3)
	got := h.describe(time.Now().Add(8700 * time.Millisecond))
	if len(got) != 2 {
		t.Fatalf("%v: want the two connections that are still held", got)
	}
	joined := strings.Join(got, "; ")
	if !strings.Contains(joined, "readiness probe: database for 8.7s") {
		t.Errorf("the label is missing: %s", joined)
	}
	// The first function outside the driver and this package: in a test of this package that is the test runner.
	if !strings.Contains(joined, "testing.tRunner for 8.7s") {
		t.Errorf("the calling function is missing: %s", joined)
	}
	h.released(c1)
	h.released(c2)
	if left := h.describe(time.Now()); len(left) != 0 {
		t.Errorf("released connections are still listed: %v", left)
	}
	var none *holders
	none.acquired(context.Background(), c1)
	if none.describe(time.Now()) != nil {
		t.Error("a nil table names a borrower")
	}
}

// TestR_HA_011_TheCloseErrorNamesTheBorrowers: the error of a close that cannot finish keeps the count and adds who holds the
// connections.
func TestR_HA_011_TheCloseErrorNamesTheBorrowers(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	pools := []closingPool{{
		name: "control", close: func() { <-release }, held: func() int32 { return 1 },
		holders: func() []string { return []string{"cluster.(*changeLog).poll for 8.7s"} },
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := closePools(ctx, pools)
	if err == nil || !strings.Contains(err.Error(), "control pool (1 connection held) by cluster.(*changeLog).poll for 8.7s") {
		t.Fatalf("closePools = %v", err)
	}
	if !errors.Is(err, ErrPoolsOpen) || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("closePools = %v: want ErrPoolsOpen and the deadline", err)
	}
}

// TestR_HA_011_AClosedConnectionIsNamedUntilItsCleanupEnds: a connection that came back closed has no borrower, and the pool keeps
// its place until the driver has cleaned it up; the close names it as such and not as a connection under construction, the
// connections under construction are named apart, and a cleanup slower than the warning bound is logged when the bound passes and
// when it ends (Q-ARCH-497, H-822).
func TestR_HA_011_AClosedConnectionIsNamedUntilItsCleanupEnds(t *testing.T) {
	var logs syncBuffer
	h := newHolders(poolControl, slog.New(slog.NewTextHandler(&logs, nil)))
	h.warnAt = 20 * time.Millisecond
	c := &pgx.Conn{}
	borrow(h, c)
	h.released(c) // a connection without a driver connection is not a closed one
	if got := h.describeHeld(time.Now(), 0); len(got) != 0 {
		t.Fatalf("%v: a returned connection is still listed", got)
	}
	quick, slow := make(chan struct{}), make(chan struct{})
	h.watchCleanup(quick)
	h.watchCleanup(slow)
	got := strings.Join(h.describeHeld(time.Now(), 1), ", ")
	if got != "2 closed connections still being cleaned up, 1 connection under construction" {
		t.Fatalf("describeHeld = %q", got)
	}
	close(quick)
	waitUntil(t, func() bool { return h.cleaning.Load() == 1 })
	if got := strings.Join(h.describeHeld(time.Now(), 0), ", "); got != "1 closed connection still being cleaned up" {
		t.Fatalf("describeHeld = %q", got)
	}
	waitUntil(t, func() bool { return strings.Contains(logs.String(), "still being cleaned up") })
	if strings.Contains(logs.String(), "was cleaned up") {
		t.Fatal("the end of a cleanup that has not ended is logged")
	}
	close(slow)
	waitUntil(t, func() bool { return h.cleaning.Load() == 0 && strings.Contains(logs.String(), "was cleaned up") })
	if !strings.Contains(logs.String(), "db_pool=control") {
		t.Errorf("the pool is not named: %s", logs.String())
	}
	if n := strings.Count(logs.String(), "still being cleaned up"); n != 1 {
		t.Errorf("%d warnings for the one slow cleanup, want 1 (the quick one is not logged): %s", n, logs.String())
	}
}

// syncBuffer is a log sink the test reads while the watcher writes.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("the condition did not hold within 5 s")
		}
		time.Sleep(time.Millisecond)
	}
}
