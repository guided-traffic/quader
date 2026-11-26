package mainclient_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/maintest"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
)

// TestR_BAS_025_ANewConfigurationResizesTheBudget: the slots are the configuration's (R-BAS-093) and change without a restart. A
// larger budget hands its new slots to the waiters at once; a smaller one takes no slot from a request in flight and admits
// nobody new until the requests in flight are below it; the gauges follow.
func TestR_BAS_025_ANewConfigurationResizesTheBudget(t *testing.T) {
	m, _ := metrics.New(nil)
	b := mainclient.NewBudget(mainclient.BudgetOptions{MaxConcurrent: 2, Replicas: 1, AnonymousShare: 0.5, QueueTimeout: 5 * time.Second, Metrics: m})
	ctx := context.Background()
	r1, _ := b.Acquire(ctx, mainclient.Authenticated)
	r2, _ := b.Acquire(ctx, mainclient.Authenticated)
	got := make(chan error, 1)
	go func() {
		rel, err := b.Acquire(ctx, mainclient.Authenticated)
		if err == nil {
			defer rel()
		}
		got <- err
	}()
	time.Sleep(20 * time.Millisecond)
	b.Resize(4, 0.25, time.Second)
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("the waiter after the budget grew: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the waiter did not get a slot the budget grew by")
	}
	if total, anon := b.Slots(); total != 4 || anon != 1 || maintest.Value(m.MainBudget.WithLabelValues("authenticated")) != 4 {
		t.Fatalf("slots %d/%d after the resize, want 4/1", total, anon)
	}
	b.Resize(1, 0.25, 50*time.Millisecond)
	if _, err := b.Acquire(ctx, mainclient.Authenticated); !errors.Is(err, mainclient.ErrBusy) {
		t.Fatalf("a request while two are in flight on a budget of one: %v", err)
	}
	r1()
	r2()
	rel, err := b.Acquire(ctx, mainclient.Authenticated)
	if err != nil {
		t.Fatalf("a request once the budget of one is free: %v", err)
	}
	rel()
}

// ceiling is a mainclient.Ceiling double: it admits while open.
type ceiling struct {
	open  bool
	asked int
}

func (c *ceiling) Take(context.Context) (bool, time.Duration) {
	c.asked++
	return c.open, 7 * time.Second
}

// TestR_BAS_094_ARefusalOfTheCeilingGivesTheSlotBack: only a request of the anonymous class asks the anonymous ceiling, after
// it got its slot; one the ceiling refuses is a CeilingError, which is ErrBusy with the ceiling's wait, and its slot is free at
// once (R-BAS-094, Q-BAS-445).
func TestR_BAS_094_ARefusalOfTheCeilingGivesTheSlotBack(t *testing.T) {
	m, _ := metrics.New(nil)
	c := &ceiling{}
	b := mainclient.NewBudget(mainclient.BudgetOptions{MaxConcurrent: 4, Replicas: 1, AnonymousShare: 0.25, QueueTimeout: time.Second, Ceiling: c, Metrics: m})
	ctx := context.Background()
	_, err := b.Acquire(ctx, mainclient.Anonymous)
	var ce *mainclient.CeilingError
	if !errors.As(err, &ce) || ce.RetryAfter != 7*time.Second || !errors.Is(err, mainclient.ErrBusy) {
		t.Fatalf("a request above the ceiling: %v", err)
	}
	if maintest.Value(m.MainInFlight.WithLabelValues("anonymous")) != 0 {
		t.Fatal("the refused request kept its slot")
	}
	rel, err := b.Acquire(ctx, mainclient.Authenticated)
	if err != nil || c.asked != 1 {
		t.Fatalf("a signed-in request: %v, the ceiling asked %d times", err, c.asked)
	}
	rel()
	c.open = true
	if rel, err := b.Acquire(ctx, mainclient.Anonymous); err != nil {
		t.Fatalf("an anonymous request below the ceiling: %v", err)
	} else {
		rel()
	}
}

// TestR_BAS_093_TheHandshakeReportsTheEdgeFigure: every handshake carries the edge figure of the configuration file in
// Quader-Bastion-Edge-Limit (R-BAS-093), "off" for an edge that limits nothing, and no such header without one.
func TestR_BAS_093_TheHandshakeReportsTheEdgeFigure(t *testing.T) {
	main := maintest.New(t)
	dir := t.TempDir()
	m, _ := metrics.New(nil)
	for _, c := range []struct {
		edge *iface.EdgeLimit
		want string
	}{
		{&iface.EdgeLimit{RequestsPerSecond: 1000, Connections: 256}, "rps=1000; connections=256"},
		{&iface.EdgeLimit{}, "off"},
		{nil, ""},
	} {
		cl, err := mainclient.New(mainclient.Options{
			URL: main.URL(), BaseURL: "https://packages.example.com", Replica: "r", Release: "test",
			Tokens: mainclient.NewHolder(mainclient.Token{Value: main.NewToken()}), EdgeLimit: c.edge,
			ConnectTimeout: time.Second, ResponseHeaderTimeout: time.Second, CAFile: main.CAFile(dir), Metrics: m,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cl.Handshake(context.Background()); err != nil {
			t.Fatal(err)
		}
		cl.Close()
		h := main.Headers("handshake")
		if got := h.Get(iface.HeaderEdgeLimit); got != c.want || (c.want == "" && len(h.Values(http.CanonicalHeaderKey(iface.HeaderEdgeLimit))) != 0) {
			t.Errorf("%s = %q, want %q", iface.HeaderEdgeLimit, got, c.want)
		}
	}
}
