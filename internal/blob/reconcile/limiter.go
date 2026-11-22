package reconcile

import (
	"context"
	"io"
	"sync"
	"time"
)

// limiter spaces the streamed bytes of a run to storage.reconcile.bytes_per_second (R-STOR-056): every read through it reserves
// its bytes on one virtual clock shared by the run's copies and waits until the clock reaches its reservation. Server-side
// copies move no byte through the worker and do not pass it.
type limiter struct {
	rate  float64 // bytes per second; zero or less means no limit
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error

	mu   sync.Mutex
	next time.Time
}

func newLimiter(bytesPerSecond int64, now func() time.Time) *limiter {
	return &limiter{rate: float64(bytesPerSecond), now: now, sleep: sleepCtx}
}

// wait reserves n bytes and returns when they may move.
func (l *limiter) wait(ctx context.Context, n int) error {
	if l == nil || l.rate <= 0 || n <= 0 {
		return nil
	}
	l.mu.Lock()
	now := l.now()
	if l.next.Before(now) {
		l.next = now
	}
	at := l.next
	l.next = l.next.Add(time.Duration(float64(n) / l.rate * float64(time.Second)))
	l.mu.Unlock()
	if d := at.Sub(now); d > 0 {
		return l.sleep(ctx, d)
	}
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// throttledReader reads through the limiter in chunks of at most throttleChunk bytes.
type throttledReader struct {
	ctx context.Context // the copy's: the reader lives as long as it
	r   io.Reader
	l   *limiter
}

// throttleChunk bounds one reservation, so that one read never holds the run's clock for long.
const throttleChunk = 256 << 10

func (t *throttledReader) Read(p []byte) (int, error) {
	if len(p) > throttleChunk {
		p = p[:throttleChunk]
	}
	n, err := t.r.Read(p)
	if werr := t.l.wait(t.ctx, n); werr != nil && err == nil {
		err = werr
	}
	return n, err
}
