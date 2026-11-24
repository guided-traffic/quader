package catalogue

import (
	"context"
	"sync"
	"time"
)

// Window is a [Budget] of this process: at most limit units per bucket in a fixed window. It is the budget of an engine that has no
// cluster limiter (a test, a process without the limiter set); the composition root wires the cluster limiter, whose fallback is the
// share of a replica (R-CUR-008, R-ARCH-030).
type Window struct {
	limit  func() int
	period time.Duration
	now    func() time.Time
	mu     sync.Mutex
	start  time.Time
	counts map[string]int
}

// NewWindow builds a window of the period whose limit is read at every take.
func NewWindow(limit func() int, period time.Duration, now func() time.Time) *Window {
	if now == nil {
		now = time.Now
	}
	return &Window{limit: limit, period: period, now: now, counts: map[string]int{}}
}

// Take implements [Budget].
func (w *Window) Take(_ context.Context, bucket string) (bool, time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	if w.start.IsZero() || now.Sub(w.start) >= w.period {
		w.start, w.counts = now, map[string]int{}
	}
	if w.counts[bucket] >= max(w.limit(), 1) {
		return false, w.start.Add(w.period).Sub(now)
	}
	w.counts[bucket]++
	return true, 0
}
