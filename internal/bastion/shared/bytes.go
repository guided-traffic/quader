package shared

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/cluster"
)

// Bytes are the byte counters of the throughput (R-BAS-097, Q-BAS-332): a counter per name and window index, the index counted
// from the Unix epoch by the caller, so that every replica names the same window. With Valkey there is one counter per name and
// index for the whole Bastion; without, and while Valkey cannot be used, each replica counts its own, and its limit is the
// configured one divided by bastion.replicas, so the replicas together stay near it (Q-BAS-34). No method fails: a counter that
// cannot be asked is counted in memory, as the limiters do (R-BAS-034).
type Bytes interface {
	// Claim adds up to want bytes to the counter while it stays within limit and returns what it added, from 0 to want.
	Claim(ctx context.Context, name string, idx int64, ttl time.Duration, want, limit int64) int64
	// Release gives n bytes of the counter back.
	Release(ctx context.Context, name string, idx int64, ttl time.Duration, n int64)
	// Add adds n bytes (0 only reads) and returns the counter and the one of idx-1, in the units of the configured limit: the
	// local form's counts multiplied by the replicas.
	Add(ctx context.Context, name string, idx int64, ttl time.Duration, n int64) (cur, prev int64)
}

// maxLocalCounters bounds the counters of the local form; the names are the Bastion's own (a bucket, a class), so the bound only
// guards against a defect that would make one per index forever.
const maxLocalCounters = 100_000

// localBytes is [Bytes] in the memory of one replica.
type localBytes struct {
	replicas int
	now      func() time.Time

	mu    sync.Mutex
	m     map[string]localCount
	calls int
}

type localCount struct {
	n       int64
	expires time.Time
}

func newLocalBytes(replicas int, now func() time.Time) *localBytes {
	return &localBytes{replicas: max(replicas, 1), now: now, m: map[string]localCount{}}
}

func bytesKey(name string, idx int64) string { return name + "\x00" + strconv.FormatInt(idx, 10) }

// entry returns the counter of the key and keeps the map bounded; l.mu is held.
func (l *localBytes) entry(key string, ttl time.Duration) localCount {
	now := l.now()
	if l.calls++; l.calls%1024 == 0 || len(l.m) >= maxLocalCounters {
		for k, c := range l.m {
			if !c.expires.After(now) {
				delete(l.m, k)
			}
		}
		if len(l.m) >= maxLocalCounters {
			clear(l.m)
		}
	}
	c, ok := l.m[key]
	if !ok || !c.expires.After(now) {
		// The expiry is set when the counter is made and not extended, as Valkey's EXPIRE NX does: a window, a lease or a
		// block ends at its time whoever reads it meanwhile.
		c = localCount{expires: now.Add(ttl)}
	}
	return c
}

// Claim implements [Bytes] with the replica's share of the limit.
func (l *localBytes) Claim(_ context.Context, name string, idx int64, ttl time.Duration, want, limit int64) int64 {
	if want <= 0 {
		return 0
	}
	share := max(limit/int64(l.replicas), 1)
	l.mu.Lock()
	defer l.mu.Unlock()
	k := bytesKey(name, idx)
	c := l.entry(k, ttl)
	granted := min(want, max(0, share-c.n))
	c.n += granted
	l.m[k] = c
	return granted
}

// Release implements [Bytes].
func (l *localBytes) Release(_ context.Context, name string, idx int64, ttl time.Duration, n int64) {
	if n <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	k := bytesKey(name, idx)
	c := l.entry(k, ttl)
	c.n = max(0, c.n-n)
	l.m[k] = c
}

// Add implements [Bytes], scaled by the replicas.
func (l *localBytes) Add(_ context.Context, name string, idx int64, ttl time.Duration, n int64) (cur, prev int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := bytesKey(name, idx)
	c := l.entry(k, ttl)
	c.n += max(n, 0)
	l.m[k] = c
	p := l.m[bytesKey(name, idx-1)]
	if !p.expires.After(l.now()) {
		p.n = 0
	}
	r := int64(l.replicas)
	return c.n * r, p.n * r
}

// valkeyBytes is [Bytes] in Valkey, with the local form while Valkey cannot be used: the throughput's counters or the
// budgets' (cluster.CounterFamily).
type valkeyBytes struct {
	store  *cluster.BastionStore
	family cluster.CounterFamily
	local  *localBytes
}

// unusable reports a failure after which the replica counts in memory; the caller's own end is none.
func unusable(ctx context.Context, err error) bool {
	return err != nil && ctx.Err() == nil && !errors.Is(err, context.Canceled)
}

// Claim implements [Bytes].
func (v *valkeyBytes) Claim(ctx context.Context, name string, idx int64, ttl time.Duration, want, limit int64) int64 {
	n, err := v.store.Claim(ctx, v.family, name, idx, ttl, want, limit)
	if unusable(ctx, err) {
		return v.local.Claim(ctx, name, idx, ttl, want, limit)
	}
	return n
}

// Release implements [Bytes]. A release that does not reach Valkey leaves the bytes counted until the window ends.
func (v *valkeyBytes) Release(ctx context.Context, name string, idx int64, ttl time.Duration, n int64) {
	if err := v.store.Release(ctx, v.family, name, idx, ttl, n); unusable(ctx, err) {
		v.local.Release(ctx, name, idx, ttl, n)
	}
}

// Add implements [Bytes].
func (v *valkeyBytes) Add(ctx context.Context, name string, idx int64, ttl time.Duration, n int64) (cur, prev int64) {
	cur, prev, err := v.store.Add(ctx, v.family, name, idx, ttl, n)
	if unusable(ctx, err) {
		return v.local.Add(ctx, name, idx, ttl, n)
	}
	return cur, prev
}
