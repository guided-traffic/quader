//go:build integration

package valkey

import (
	"fmt"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_LIM_017_AFlushOfAThousandEndedDownloadsIsOneRoundTrip: the flush of a replica that ended a thousand downloads in
// an interval drops their thousand leases in its one batch; a batch was bounded to 255 operations, and the flush of a
// busy replica failed and left every lease in Valkey for its full minute (R-LIM-017, R-LIM-024).
func TestR_LIM_017_AFlushOfAThousandEndedDownloadsIsOneRoundTrip(t *testing.T) {
	c := newClient(t, stackConfig(t, 2*time.Second))
	waitAvailable(t, c)
	counters := cluster.NewLimitCounters(c.Valkey)
	bucket := cluster.LimitBucket("it:" + randHex(6))
	now := time.Now()
	var take, drop cluster.LimitBatch
	for i := range 1000 {
		member := fmt.Sprintf("r1:%d", i)
		take.Add(cluster.LimitOp{Kind: cluster.OpLeaseTake, Bucket: bucket, Member: member, Until: now.Add(time.Minute), Now: now, TTL: 2 * time.Minute})
		drop.Add(cluster.LimitOp{Kind: cluster.OpLeaseDrop, Bucket: bucket, Member: member, TTL: 2 * time.Minute})
	}
	res, err := counters.Run(t.Context(), &take)
	if err != nil || res[999].Count != 1000 {
		t.Fatalf("a batch of 1,000 lease takes: %v, the last counted %v", err, res)
	}
	if _, err := counters.Run(t.Context(), &drop); err != nil {
		t.Fatalf("a batch of 1,000 lease drops: %v", err)
	}
	var count cluster.LimitBatch
	count.Add(cluster.LimitOp{Kind: cluster.OpLeaseCount, Bucket: bucket, Now: now, TTL: 2 * time.Minute})
	if res, err := counters.Run(t.Context(), &count); err != nil || res[0].Count != 0 {
		t.Fatalf("leases after the drops: %v %v", res, err)
	}
}

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
