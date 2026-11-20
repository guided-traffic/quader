//go:build integration

package valkey

import (
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_LIM_017_TheTakeLoadsItsScriptAgainAfterTheServerLostIt: a primary that lost its script cache (a restart, a
// failover) answers the take NOSCRIPT; the take is sent once more with the script's body, counts once, and the next one
// finds the script again (R-LIM-017, R-ARCH-034).
func TestR_LIM_017_TheTakeLoadsItsScriptAgainAfterTheServerLostIt(t *testing.T) {
	c := newClient(t, stackConfig(t, 2*time.Second))
	waitAvailable(t, c)
	v := testenv.ValkeyConfig(t)
	admin := adminAt(t, v, sentinelPrimary(t.Context(), t, v))
	if err := admin.Do(t.Context(), admin.B().ScriptFlush().Build()).Error(); err != nil {
		t.Fatalf("SCRIPT FLUSH: %v", err)
	}
	ls, err := cluster.NewLimiterSet(cluster.LimiterOptions{Valkey: c.Valkey})
	if err != nil {
		t.Fatal(err)
	}
	l := ls.NewLimiter("it.take_"+randHex(4), cluster.ScopeCluster, 1, 2)
	for i, want := range []bool{true, true, false} {
		if v := l.Take(t.Context(), "a"); v.Allowed != want || v.Mode != cluster.ModeCluster {
			t.Fatalf("take %d after the script cache was flushed: %+v, want allowed %v in the cluster", i+1, v, want)
		}
	}
}

// TestR_BAS_016_ABatchIsChargedItsEventsInValkey: TakeNAt counts n units in one take, with the least-privilege ACL of
// the stack, against the shared window, so two replicas of the main installation together admit a registration's events
// up to the burst and refuse the batch that does not fit, with a Retry-After (R-ARCH-030, R-ARCH-034, Q-BAS-117).
func TestR_BAS_016_ABatchIsChargedItsEventsInValkey(t *testing.T) {
	ks := randHex(8)
	a, b := newClientIn(t, stackConfig(t, 2*time.Second), ks), newClientIn(t, stackConfig(t, 2*time.Second), ks)
	waitAvailable(t, a)
	waitAvailable(t, b)
	limiter := func(c *client) *cluster.Limiter {
		ls, err := cluster.NewLimiterSet(cluster.LimiterOptions{Valkey: c.Valkey, LiveReplicas: func() int { return 2 }})
		if err != nil {
			t.Fatal(err)
		}
		return ls.NewLimiter("bastion.download_events", cluster.ScopeCluster, 2, 20)
	}
	la, lb := limiter(a), limiter(b)
	bucket := "bastion:" + randHex(6)
	if v := la.TakeNAt(t.Context(), bucket, 10, time.Time{}); !v.Allowed || v.Mode != cluster.ModeCluster {
		t.Fatalf("the first batch on one replica: %+v", v)
	}
	if v := lb.TakeNAt(t.Context(), bucket, 10, time.Time{}); !v.Allowed || v.Mode != cluster.ModeCluster || v.Remaining > 0 {
		t.Fatalf("the second batch on the other replica: %+v", v)
	}
	v := la.TakeNAt(t.Context(), bucket, 10, time.Time{})
	if v.Allowed || v.Mode != cluster.ModeCluster || v.RetryAfter < time.Second {
		t.Fatalf("a third batch: %+v", v)
	}
	if v := lb.TakeNAt(t.Context(), "bastion:"+randHex(6), 10, time.Time{}); !v.Allowed {
		t.Fatalf("another registration: %+v", v)
	}
}
