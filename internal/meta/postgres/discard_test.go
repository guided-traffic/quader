package postgres

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// TestR_ARCH_019_TheCancelRequestOfADiscardedConnectionLeavesThePool (Q-ARCH-498): the dials of one pool connection are told apart.
// The first is the connection, whose socket discard closes; a later one of the driver is its cancel request, which discard ends even
// while it dials; after discard the driver's dial is refused and only the cancel request that discard sends itself goes out.
func TestR_ARCH_019_TheCancelRequestOfADiscardedConnectionLeavesThePool(t *testing.T) {
	var dialed []net.Conn
	block := make(chan struct{})
	base := func(ctx context.Context, _, _ string) (net.Conn, error) {
		if ctx.Value(blockDial{}) != nil {
			select {
			case <-block:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		a, b := net.Pipe()
		t.Cleanup(func() { _ = b.Close() })
		dialed = append(dialed, a)
		return a, nil
	}
	st := &connLife{cancelBound: time.Second, inFlight: map[*abortable]struct{}{}}
	conn, err := st.dial(context.Background(), base, "tcp", "db:5432")
	if err != nil || st.raw != conn {
		t.Fatalf("the connection's dial = %v, %v: want the socket recorded", conn, err)
	}
	st.connected = true

	// The driver's cancel request, dialed: tracked until it closes.
	c1, err := st.dial(context.Background(), base, "tcp", "db:5432")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.inFlight) != 1 {
		t.Fatalf("%d cancel requests in flight, want 1", len(st.inFlight))
	}
	// A second one, still dialing when the connection is let go.
	dialing := make(chan error, 1)
	go func() {
		_, err := st.dial(context.WithValue(context.Background(), blockDial{}, true), base, "tcp", "db:5432")
		dialing <- err
	}()
	waitUntil(t, func() bool { st.mu.Lock(); defer st.mu.Unlock(); return len(st.inFlight) == 2 })

	st.mu.Lock()
	st.discarded = true
	var all []*abortable
	for a := range st.inFlight {
		all = append(all, a)
	}
	st.mu.Unlock()
	for _, a := range all {
		a.abort()
	}
	if err := <-dialing; !errors.Is(err, context.Canceled) {
		t.Errorf("the cancel request that was dialing = %v, want its dial ended", err)
	}
	if _, err := c1.Write([]byte("x")); err == nil {
		t.Error("the dialed cancel request is still open")
	}
	_ = c1.Close()
	waitUntil(t, func() bool { st.mu.Lock(); defer st.mu.Unlock(); return len(st.inFlight) == 0 })

	if _, err := st.dial(context.Background(), base, "tcp", "db:5432"); !errors.Is(err, errDiscarded) {
		t.Errorf("the driver's dial after the discard = %v, want errDiscarded", err)
	}
	if _, err := st.dial(context.WithValue(context.Background(), ownCancel{}, true), base, "tcp", "db:5432"); err != nil {
		t.Errorf("discard's own cancel request = %v, want it dialed", err)
	}
}

type blockDial struct{}

// TestR_ARCH_024_AClosePhaseDoesNotWaitForTheDriversOwnWork (Q-ARCH-499): a pool that stays open only for a closed connection's
// cleanup or a connection being opened, with no borrower, does not hold the close: it returns without an error long before its
// deadline; a pool with a borrower still holds it to the deadline and is named.
func TestR_ARCH_024_AClosePhaseDoesNotWaitForTheDriversOwnWork(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	err := closePools(ctx, []closingPool{
		{name: "control", close: func() { <-release }, held: func() int32 { return 1 }, borrowed: func() int { return 0 }},
		{name: "main", close: func() {}, held: func() int32 { return 0 }, borrowed: func() int { return 0 }},
	})
	if err != nil || time.Since(start) > time.Second {
		t.Fatalf("closePools = %v after %s, want nil at once", err, time.Since(start))
	}
	bctx, bcancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer bcancel()
	err = closePools(bctx, []closingPool{
		{name: "control", close: func() { <-release }, held: func() int32 { return 1 }, borrowed: func() int { return 1 }},
	})
	if !errors.Is(err, ErrPoolsOpen) {
		t.Fatalf("closePools with a borrower = %v, want ErrPoolsOpen", err)
	}
}
