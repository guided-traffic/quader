package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/observability/health"
)

// connectionsOf builds two connections, one and two, on in-memory stores behind guards whose circuit opens at the first
// transient failure; drainTwo sets drain: true on the second.
func connectionsOf(t *testing.T, one, two *blobtest.Store, drainTwo bool) *blob.Connections {
	t.Helper()
	p := "quader"
	breaker := config.StorageS3Breaker{FailureThreshold: 1, OpenDuration: time.Minute}
	a := config.StorageConnection{ID: "one", Bucket: "a", Prefix: &p}
	b := config.StorageConnection{ID: "two", Bucket: "b", Prefix: &p, Drain: drainTwo}
	cs, err := blob.NewConnections(
		blob.NewConnection(a, blob.NewGuard(one, breaker, blob.WithGuardConnection("one"))),
		blob.NewConnection(b, blob.NewGuard(two, breaker, blob.WithGuardConnection("two"))))
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func transient(op, key string) error {
	return &blob.StoreError{Op: op, Key: key, Code: "SlowDown", Status: 503, Kind: blob.KindTransient}
}

func denied(op, key string) error {
	return &blob.StoreError{Op: op, Key: key, Code: "AccessDenied", Status: 403, Kind: blob.KindConfig}
}

func reasonOf(err error) string {
	var r interface{ HealthReason() string }
	if errors.As(err, &r) {
		return r.HealthReason()
	}
	return ""
}

func TestR_STOR_060_ReadinessPerConnection(t *testing.T) {
	opts := blob.StartCheckOptions{Replica: "unit", Await: []blob.AwaitOption{blob.WithAwaitBackoff(time.Millisecond, 5*time.Millisecond)}}
	t.Run("R-STOR-060 started with the second store failing transiently, the replica becomes ready and objectStore is degraded naming the second", func(t *testing.T) {
		two := blobtest.New()
		two.SetFault(transient)
		check, err := startConnections(context.Background(), connectionsOf(t, blobtest.New(), two, false), true, opts, false)
		if err != nil {
			t.Fatalf("not ready: %v", err)
		}
		err = check(context.Background())
		if err == nil || reasonOf(err) != "unavailable: two" || health.IsConfigError(err) {
			t.Fatalf("objectStore = %v (reason %q), want degraded naming two", err, reasonOf(err))
		}
		two.SetFault(nil)
	})
	t.Run("R-STOR-060 started with both stores failing it stays not ready", func(t *testing.T) {
		one, two := blobtest.New(), blobtest.New()
		one.SetFault(transient)
		two.SetFault(transient)
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		if _, err := startConnections(ctx, connectionsOf(t, one, two, false), true, opts, false); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("got %v, want the start waiting until its context ends", err)
		}
	})
	t.Run("R-STOR-060 started with a configuration error (AccessDenied) on the second it exits naming the connection", func(t *testing.T) {
		two := blobtest.New()
		two.SetFault(denied)
		_, err := startConnections(context.Background(), connectionsOf(t, blobtest.New(), two, false), true, opts, false)
		if err == nil || !strings.Contains(err.Error(), "connection two") || !strings.Contains(err.Error(), "AccessDenied") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("R-STOR-060 with drain: true on the second its configuration error does not hold the start", func(t *testing.T) {
		two := blobtest.New()
		two.SetFault(denied)
		check, err := startConnections(context.Background(), connectionsOf(t, blobtest.New(), two, true), true, opts, false)
		if err != nil {
			t.Fatalf("got %v", err)
		}
		if err := check(context.Background()); health.IsConfigError(err) {
			t.Fatalf("a draining connection's configuration error holds back the readiness: %v", err)
		}
	})
	t.Run("R-STOR-060 with require_object_store the check gates only while no connection without drain answers", func(t *testing.T) {
		one, two := blobtest.New(), blobtest.New()
		check, err := startConnections(context.Background(), connectionsOf(t, one, two, false), false, opts, true)
		if err != nil {
			t.Fatal(err)
		}
		two.SetFault(transient)
		if err := check(context.Background()); err != nil {
			t.Fatalf("one of two down gates: %v", err)
		}
		one.SetFault(transient)
		if err := check(context.Background()); err == nil || reasonOf(err) != "unavailable: one, two" {
			t.Fatalf("both down: %v", err)
		}
	})
	t.Run("R-OPS-003 the object-store view lists the connections in order with their circuit and drain flag, and nothing else", func(t *testing.T) {
		two := blobtest.New()
		cs := connectionsOf(t, blobtest.New(), two, true)
		two.SetFault(transient)
		c, _ := cs.Get("two")
		_, _ = c.Objects().Head(context.Background(), "k")
		var view struct {
			Connections []map[string]any `json:"connections"`
		}
		raw := objectStoreViewOf(cs)
		if err := json.Unmarshal(raw, &view); err != nil {
			t.Fatal(err)
		}
		if len(view.Connections) != 2 || view.Connections[0]["id"] != "one" || view.Connections[0]["circuit"] != "closed" ||
			view.Connections[1]["circuit"] != "open" || view.Connections[1]["drain"] != true || len(view.Connections[0]) != 3 {
			t.Fatalf("%s", raw)
		}
	})
}
