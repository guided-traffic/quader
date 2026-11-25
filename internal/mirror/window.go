package mirror

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// DeferredError is returned by a [Window] once an upstream asked for a wait longer than [LongWait] (R-REPO-054), or than the window's
// own bound ([Window.EndAfter]): the run holds no slot through it, so it ends and its caller runs again at Until. Cause is the
// rate-limit answer.
type DeferredError struct {
	Until time.Time
	Cause error
}

// Error implements error.
func (e *DeferredError) Error() string {
	return fmt.Sprintf("mirror: the upstream is rate limiting; no request before %s: %v", e.Until.UTC().Format(time.RFC3339), e.Cause)
}

// Unwrap lets discover.AsRateLimited find the answer.
func (e *DeferredError) Unwrap() error { return e.Cause }

// Window is the window and the waits of R-REPO-054 (Q-REPO-138) for the requests of one run outside a mirror pass, such as a
// remote repository's background job (Q-REPO-272): it starts at 1, doubles after as many successes in a row as it is wide until
// its ceiling or the first rate-limit answer, grows by one afterwards and halves once per wait. It is the pass's throttle without
// the pass: it writes no state and no event. Safe for concurrent use.
type Window struct {
	th    *throttle
	clock Clock

	mu       sync.Mutex
	deferred *DeferredError
}

// NewWindow creates the window of one run. The ceiling is the repository's background share (R-REPO-055); a nil clock and a nil
// jitter are the system's.
func NewWindow(c Clock, j Jitter, ceiling int) *Window {
	if c == nil {
		c = SystemClock{}
	}
	if j == nil {
		j = SystemJitter{}
	}
	return &Window{th: newThrottle(c, j, ceiling, nil), clock: c}
}

// EndAfter makes a wait longer than d end the run with a *DeferredError instead of the window's [LongWait], and returns the window. The
// download queue's runs wait out only a short rate limit inside the run (remote-curation.md R-CUR-011, Q-CUR-171): a longer one hands
// the row back to the queue, which holds the repository until the upstream's time. Call it before the first request.
func (w *Window) EndAfter(d time.Duration) *Window {
	w.th.mu.Lock()
	w.th.longWait = max(d, 0)
	w.th.mu.Unlock()
	return w
}

// Size returns the current window.
func (w *Window) Size() int { return w.th.Window() }

// Do sends one request through the window, as a pass does (pass.call): it waits while a wait is in force and while the window is
// full, sends the request again after a rate-limit answer (with the window halved), after a *WaitError and, after PreemptRetry,
// when a client pre-empted it, and returns every other error as it is. A wait longer than LongWait ends the run: Do returns a
// *DeferredError, and every later call returns it too.
func (w *Window) Do(ctx context.Context, fn func(context.Context) error) error {
	for {
		tk, err := w.th.acquire(ctx)
		if err != nil {
			if errors.Is(err, errHalted) {
				w.mu.Lock()
				defer w.mu.Unlock()
				if w.deferred != nil {
					return w.deferred
				}
			}
			return err
		}
		err = fn(ctx)
		if err == nil {
			w.th.success(tk)
			return nil
		}
		if ctx.Err() != nil || errors.Is(err, ErrShuttingDown) {
			w.th.abandoned(tk)
			return err
		}
		var wait *WaitError
		switch {
		case errors.Is(err, ErrPreempted):
			w.th.abandoned(tk)
			if serr := w.clock.Sleep(ctx, PreemptRetry); serr != nil {
				return serr
			}
			continue
		case errors.As(err, &wait):
			w.th.abandoned(tk)
			if serr := w.clock.Sleep(ctx, wait.After); serr != nil {
				return serr
			}
			continue
		}
		if rl, ok := discover.AsRateLimited(err); ok {
			hint, hinted := rl.RetryAfter()
			waitFor, started := w.th.rateLimited(tk, hint, hinted)
			if started && waitFor > w.longWait() {
				d := &DeferredError{Until: w.clock.Now().Add(waitFor), Cause: err}
				w.mu.Lock()
				w.deferred = d
				w.mu.Unlock()
				return d
			}
			continue
		}
		w.th.failed(tk)
		return err
	}
}

// longWait is the wait beyond which the window ends its run.
func (w *Window) longWait() time.Duration {
	w.th.mu.Lock()
	defer w.th.mu.Unlock()
	return w.th.longWait
}
