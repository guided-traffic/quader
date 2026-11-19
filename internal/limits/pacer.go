package limits

import (
	"context"
	"io"
	"sync"
	"time"
)

// Pacing of a download (R-LIM-023, Q-LIM-13). Each replica keeps a token bucket per budget key and figure with the rate
// figure × local ÷ max(local, cluster): local is the key's downloads in progress on the replica, cluster the key's parallel
// leases, which the flush counts every second. The burst is one second of that rate. The downloads of the key take turns on
// the bucket in chunks of 16 KiB. Every download proceeds at least at limits.pacing.min_bytes_per_second, and no write waits
// longer than one second, so no idle timeout of a client fires. While Valkey is unreachable the rate is the figure ÷ n.

// PaceChunk is the size of the chunks a paced body is written or read in.
const PaceChunk = 16 << 10

// pacer is the bucket of one budget key and figure on this replica.
type pacer struct {
	mu      sync.Mutex
	figure  int64
	local   int
	cluster int64
	tokens  float64
	last    time.Time
}

func pacerKey(key string, uncached bool) string {
	if uncached {
		return key + "\x00u"
	}
	return key + "\x00c"
}

// Pace is one download's turn on the bucket of its budget key. A nil Pace paces nothing.
type Pace struct {
	e        *Engine
	c        *Charge
	p        *pacer
	key      string
	uncached bool
	closed   bool
}

// Pacer returns the pace of a download for its cached (uncached false) or uncached bytes, nil when the figure is unlimited,
// the charge is a background download's, or there is no charge. Close it when the download ends.
func (e *Engine) Pacer(c *Charge, uncached bool) *Pace {
	if e == nil || c == nil || c.bg || c.Subject.Kind == KindSystem {
		return nil
	}
	fig := CachedBytesPerSecond
	if uncached {
		fig = UncachedBytesPerSecond
	}
	rate := c.Figures()[fig]
	if rate <= 0 {
		return nil
	}
	k := pacerKey(c.Key, uncached)
	e.mu.Lock()
	p := e.pacers[k]
	if p == nil {
		p = &pacer{last: e.now(), tokens: float64(rate)}
		e.pacers[k] = p
	}
	e.mu.Unlock()
	p.mu.Lock()
	p.figure = rate
	p.local++
	p.mu.Unlock()
	return &Pace{e: e, c: c, p: p, key: k, uncached: uncached}
}

// Close ends the download's turn; the bucket is dropped with its last download.
func (pc *Pace) Close() {
	if pc == nil || pc.closed {
		return
	}
	pc.closed = true
	pc.e.mu.Lock()
	defer pc.e.mu.Unlock()
	pc.p.mu.Lock()
	pc.p.local--
	empty := pc.p.local <= 0
	pc.p.mu.Unlock()
	if empty && pc.e.pacers[pc.key] == pc.p {
		delete(pc.e.pacers, pc.key)
	}
}

// clusterLeases updates the cluster's count of a key's downloads after a flush.
func (e *Engine) clusterLeases(key string, n int64) {
	e.mu.Lock()
	ps := []*pacer{e.pacers[pacerKey(key, false)], e.pacers[pacerKey(key, true)]}
	e.mu.Unlock()
	for _, p := range ps {
		if p != nil {
			p.mu.Lock()
			p.cluster = n
			p.mu.Unlock()
		}
	}
}

// reserve takes n bytes from the bucket and returns how long the download waits before it writes them.
func (pc *Pace) reserve(n int) time.Duration {
	p := pc.p
	now := pc.e.now()
	p.mu.Lock()
	rate := float64(p.figure)
	if pc.e.Fallback() {
		rate /= float64(pc.e.share())
	} else if local := int64(max(p.local, 1)); p.cluster > local {
		rate = rate * float64(local) / float64(p.cluster)
	}
	rate = max(rate, 1)
	if dt := now.Sub(p.last).Seconds(); dt > 0 {
		p.tokens = min(rate, p.tokens+dt*rate)
		p.last = now
	}
	p.tokens -= float64(n)
	var wait time.Duration
	if p.tokens < 0 {
		wait = time.Duration(-p.tokens / rate * float64(time.Second))
	}
	if p.tokens < -rate { // the debt of downloads that ran at the floor stays bounded
		p.tokens = -rate
	}
	p.mu.Unlock()
	floor := time.Duration(float64(n) / float64(pc.e.o.MinPacing) * float64(time.Second))
	return min(wait, floor, time.Second)
}

// Wait paces n bytes: in enforce it sleeps for the bucket's wait, in audit it records the download that would have waited
// once as would_limit and sleeps nothing (R-LIM-023).
func (pc *Pace) Wait(ctx context.Context, n int) error {
	if pc == nil {
		return nil
	}
	wait := pc.reserve(n)
	if wait <= 0 {
		return nil
	}
	fig := CachedBytesPerSecond
	slot := 0
	if pc.uncached {
		fig, slot = UncachedBytesPerSecond, 1
	}
	if pc.c.Mode != ModeEnforce {
		pc.c.mu.Lock()
		first := !pc.c.pacedWould[slot]
		pc.c.pacedWould[slot] = true
		pc.c.mu.Unlock()
		if first {
			pc.e.decided(pc.c, fig.String(), false)
		}
		return nil
	}
	pc.c.mu.Lock()
	first := !pc.c.pacedWould[slot]
	pc.c.pacedWould[slot] = true
	if pc.c.outcome == "" {
		pc.c.outcome = "paced"
	}
	pc.c.mu.Unlock()
	if first {
		pc.e.m.decision(fig.String(), ModeEnforce, "paced")
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Reader paces and counts the uncached bytes of an upstream body the request leads (R-LIM-006, R-LIM-023): every byte read is
// charged to the charge as uncached, and with a pace the read waits for its turn. The caller calls Done when the body ends.
func (e *Engine) Reader(ctx context.Context, c *Charge, r io.Reader) *CountingReader {
	return &CountingReader{ctx: ctx, c: c, r: r, pace: e.Pacer(c, true)}
}

// CountingReader is the reader of [Engine.Reader].
type CountingReader struct {
	ctx  context.Context
	c    *Charge
	r    io.Reader
	pace *Pace
}

func (cr *CountingReader) Read(p []byte) (int, error) {
	if cr.pace != nil && len(p) > PaceChunk {
		p = p[:PaceChunk]
	}
	n, err := cr.r.Read(p)
	if n > 0 {
		cr.c.AddUncached(n)
		if cr.pace != nil {
			// A detached fill keeps the leader's rate after its client left (R-LIM-023): the wait does not end with the request.
			_ = cr.pace.Wait(context.WithoutCancel(cr.ctx), n)
		}
	}
	if err != nil {
		cr.Done()
	}
	return n, err
}

// Done ends the pace of the body; it does not close the body.
func (cr *CountingReader) Done() {
	if cr != nil && cr.pace != nil {
		cr.pace.Close()
		cr.pace = nil
	}
}
