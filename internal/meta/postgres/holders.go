package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

// holders records who has a connection of a pool checked out, so that a close that cannot finish names the borrowers instead of
// only counting them (R-HA-011, R-ARCH-024 step 6). The pool tells the tracer about every acquire and every release, whoever
// borrows (an explicit Acquire, a statement run on the pool, a transaction), so no call site is wrapped. A borrower may name itself
// with [WithHolder]; any other is named by the first function of the call stack outside the driver and this package, read from the
// recorded program counters only when the close reports.
//
// A connection that comes back closed (a statement cut by its context, a broken connection) has no borrower any more, but the pool
// still counts it as acquired and keeps its place until the driver has cleaned it up: the cancel request on a new connection, the
// terminate message and the wait for the server to close, up to 15 s. A pool of one connection, the control pool, hands out nothing
// in that time. holders counts those connections and logs a cleanup that takes longer than cleanupWarnAfter (Q-ARCH-497).
type holders struct {
	m sync.Map // *pgx.Conn -> hold

	pool     string       // the db_pool label, for the log
	log      *slog.Logger // nil logs nothing
	cleaning atomic.Int32 // closed connections whose cleanup has not ended
	warnAt   time.Duration
}

// cleanupWarnAfter is how long the cleanup of a closed connection may take before it is logged: on a quiet host it takes 15 to
// 25 ms (H-822), so a second is a stall of the database, its network path or the cancel request.
const cleanupWarnAfter = time.Second

func newHolders(pool string, log *slog.Logger) *holders {
	return &holders{pool: pool, log: log, warnAt: cleanupWarnAfter}
}

type hold struct {
	label string
	pcs   [12]uintptr
	n     int
	since time.Time
}

type holderKey struct{}

// WithHolder names the borrower of the connections acquired with the returned context; the name appears in the error of a close
// that finds a connection still held.
func WithHolder(ctx context.Context, label string) context.Context {
	return context.WithValue(ctx, holderKey{}, label)
}

func (h *holders) acquired(ctx context.Context, c *pgx.Conn) {
	if h == nil || c == nil {
		return
	}
	v := hold{since: time.Now()}
	if l, ok := ctx.Value(holderKey{}).(string); ok {
		v.label = l
	} else {
		v.n = runtime.Callers(2, v.pcs[:])
	}
	h.m.Store(c, v)
}

func (h *holders) released(c *pgx.Conn) {
	if h == nil || c == nil {
		return
	}
	v, held := h.m.LoadAndDelete(c)
	if pc := c.PgConn(); pc != nil && pc.IsClosed() {
		// A connection comes back closed when a statement on it was cut by its context or the connection broke: the pool discards
		// it (Q-ARCH-498). Who had it says which caller's context ended, which a stall alone does not tell (H-822).
		if h.log != nil {
			borrower := "unknown"
			if held {
				borrower = v.(hold).name()
			}
			h.log.Warn("a database connection came back closed and is discarded", "db_pool", h.pool, "borrower", borrower)
		}
		h.watchCleanup(pc.CleanupDone())
	}
}

// watchCleanup counts a closed connection until done, the end of its cleanup, and logs a cleanup that takes longer than warnAt: once
// when warnAt has passed, so that a process that stops before the cleanup ends still says so, and once with the whole time.
func (h *holders) watchCleanup(done <-chan struct{}) {
	h.cleaning.Add(1)
	start := time.Now()
	go func() {
		defer h.cleaning.Add(-1)
		t := time.NewTimer(h.warnAt)
		defer t.Stop()
		select {
		case <-done:
			return
		case <-t.C:
		}
		if h.log != nil {
			h.log.Warn("a closed database connection is still being cleaned up; its pool cannot open another in its place until it is",
				"db_pool", h.pool, "after", h.warnAt.String())
		}
		<-done // the driver ends the cleanup within 15 s
		if h.log != nil {
			h.log.Warn("a closed database connection was cleaned up", "db_pool", h.pool, "took", time.Since(start).Round(time.Millisecond).String())
		}
	}()
}

// describeHeld names every connection a close waits for: the borrowers, the closed connections still being cleaned up and the
// connections under construction (constructing, from the pool's counts).
func (h *holders) describeHeld(now time.Time, constructing int32) []string {
	who := h.describe(now)
	if h != nil {
		if n := h.cleaning.Load(); n == 1 {
			who = append(who, "1 closed connection still being cleaned up")
		} else if n > 1 {
			who = append(who, fmt.Sprintf("%d closed connections still being cleaned up", n))
		}
	}
	if constructing == 1 {
		who = append(who, "1 connection under construction")
	} else if constructing > 1 {
		who = append(who, fmt.Sprintf("%d connections under construction", constructing))
	}
	return who
}

// describe lists the connections still checked out, longest held first: "label for 8.7s".
func (h *holders) describe(now time.Time) []string {
	if h == nil {
		return nil
	}
	type row struct {
		text string
		for_ time.Duration
	}
	var rows []row
	h.m.Range(func(_, v any) bool {
		e := v.(hold)
		rows = append(rows, row{text: e.name(), for_: now.Sub(e.since)})
		return true
	})
	sort.Slice(rows, func(i, j int) bool { return rows[i].for_ > rows[j].for_ })
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = fmt.Sprintf("%s for %s", r.text, r.for_.Round(10*time.Millisecond))
	}
	return out
}

// name is the label, or the first function of the stack that is neither the driver nor this package.
func (e hold) name() string {
	if e.label != "" {
		return e.label
	}
	frames := runtime.CallersFrames(e.pcs[:e.n])
	for {
		f, more := frames.Next()
		fn := f.Function
		if fn != "" && !strings.HasPrefix(fn, "github.com/jackc/") && !strings.HasPrefix(fn, "github.com/guided-traffic/quader/internal/meta/postgres.") &&
			!strings.HasPrefix(fn, "runtime.") {
			return strings.TrimPrefix(fn, "github.com/guided-traffic/quader/internal/")
		}
		if !more {
			return "unknown borrower"
		}
	}
}
