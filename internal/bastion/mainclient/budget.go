package mainclient

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/metrics"
)

// BudgetOptions are the figures of R-BAS-025 and the anonymous ceiling of R-BAS-094.
type BudgetOptions struct {
	// MaxConcurrent is the configuration's limits.slots.maxConcurrent: the slots across all replicas.
	MaxConcurrent int
	// Replicas is bastion.replicas, the divisor (Q-BAS-34).
	Replicas int
	// AnonymousShare is limits.slots.anonymousShare, 0 to 1.
	AnonymousShare float64
	// QueueTimeout is limits.slots.queueTimeoutMillis.
	QueueTimeout time.Duration
	// Ceiling is the anonymous ceiling (R-BAS-094): every request of the class [Anonymous] that got a slot takes from it; nil
	// is no ceiling.
	Ceiling Ceiling
	// Metrics receives the gauges and the exhausted counter; nil counts nothing.
	Metrics *metrics.Metrics
}

// Ceiling is the anonymous ceiling of a Bastion (R-BAS-094): the requests to the main installation on behalf of all anonymous
// callers together. Take admits one request or says how long until the ceiling admits one; a refused request takes nothing
// (Q-BAS-127's rule). guard.Ceiling implements it over the shared state.
type Ceiling interface {
	Take(ctx context.Context) (ok bool, retryAfter time.Duration)
}

// SlotBudget is the concurrency budget of one replica towards the main installation (R-BAS-025, Q-BAS-35): at
// most ceil(max_concurrent / replicas) requests in flight, of which requests on behalf of anonymous callers may
// hold at most the anonymous share. A request that finds no slot waits in arrival order for the queue timeout
// and then gets [ErrBusy]. A request of the anonymous class that got its slot takes from the anonymous ceiling as
// well, and above it gives the slot back and gets a [*CeilingError]. A new configuration resizes the budget without
// a restart ([SlotBudget.Resize]). It is safe for concurrent use and implements [Budget].
type SlotBudget struct {
	replicas int
	ceiling  Ceiling
	m        *metrics.Metrics

	mu      sync.Mutex
	total   int
	anon    int
	timeout time.Duration
	inUse   int
	anonUse int
	queue   []*waiter
}

var _ Budget = (*SlotBudget)(nil)

type waiter struct {
	class   Class
	granted bool
	ch      chan struct{}
}

// NewBudget returns the budget of one replica and exports its size as quader_bastion_main_budget.
func NewBudget(o BudgetOptions) *SlotBudget {
	b := &SlotBudget{replicas: max(o.Replicas, 1), ceiling: o.Ceiling, m: o.Metrics}
	b.mu.Lock()
	b.size(o.MaxConcurrent, o.AnonymousShare, o.QueueTimeout)
	b.mu.Unlock()
	return b
}

// size sets the slots of this replica from the figures of the whole Bastion and exports them; b.mu is held.
func (b *SlotBudget) size(maxConcurrent int, share float64, timeout time.Duration) {
	total := max(int(math.Ceil(float64(max(maxConcurrent, 1))/float64(b.replicas))), 1)
	// The anonymous cap is the share of the total, rounded down, and at least one slot while the share is
	// above zero, so that a small budget does not lock anonymous callers out altogether (Q-BAS-75).
	anon := 0
	if share > 0 {
		anon = min(max(int(math.Floor(float64(total)*share+1e-9)), 1), total)
	}
	b.total, b.anon, b.timeout = total, anon, timeout
	if b.m != nil {
		b.m.MainBudget.WithLabelValues(metrics.BudgetAuthenticated).Set(float64(total))
		b.m.MainBudget.WithLabelValues(metrics.BudgetAnonymous).Set(float64(anon))
	}
}

// Resize applies new figures of the configuration (R-BAS-025, R-BAS-093) without a restart. A larger budget hands its new
// slots to the waiters at once; a smaller one takes no slot away from a request in flight: it admits nobody new until the
// requests in flight are below it. A waiter keeps the wait it began with.
func (b *SlotBudget) Resize(maxConcurrent int, share float64, timeout time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.size(maxConcurrent, share, timeout)
	b.grant()
	b.gauge()
}

// Slots returns the slots of this replica: in all, and for anonymous callers.
func (b *SlotBudget) Slots() (total, anonymous int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.total, b.anon
}

// Acquire implements [Budget].
func (b *SlotBudget) Acquire(ctx context.Context, class Class) (func(), error) {
	release, err := b.slot(ctx, class)
	if err != nil || class != Anonymous || b.ceiling == nil {
		return release, err
	}
	// The ceiling is taken after the slot, so that a request the slots refuse takes nothing from it, and a request it refuses
	// gives its slot back at once (Q-BAS-445).
	if ok, wait := b.ceiling.Take(ctx); !ok {
		release()
		return nil, &CeilingError{RetryAfter: wait}
	}
	return release, nil
}

// slot takes a slot of the class, waiting for the queue timeout.
func (b *SlotBudget) slot(ctx context.Context, class Class) (func(), error) {
	b.mu.Lock()
	// After every grant no waiter fits, so a newcomer that fits takes a slot without overtaking anyone.
	if b.fits(class) {
		b.take(class)
		b.mu.Unlock()
		return b.releaser(class), nil
	}
	if class == Anonymous && b.anon == 0 {
		b.mu.Unlock()
		b.exhausted(class)
		return nil, ErrBusy
	}
	w := &waiter{class: class, ch: make(chan struct{})}
	b.queue = append(b.queue, w)
	timeout := b.timeout
	b.mu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-w.ch:
		return b.releaser(class), nil
	case <-timer.C:
	case <-ctx.Done():
		if !b.abandon(w) {
			// The slot was granted in the same instant: give it back.
			b.releaser(class)()
		}
		return nil, ctx.Err()
	}
	if b.abandon(w) {
		b.exhausted(class)
		return nil, ErrBusy
	}
	return b.releaser(class), nil
}

// abandon removes a waiter that was not granted and reports whether it was still waiting.
func (b *SlotBudget) abandon(w *waiter) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if w.granted {
		return false
	}
	for i, q := range b.queue {
		if q == w {
			b.queue = append(b.queue[:i], b.queue[i+1:]...)
			break
		}
	}
	return true
}

func (b *SlotBudget) exhausted(class Class) {
	if b.m != nil {
		b.m.BudgetExhausted.WithLabelValues(string(class)).Inc()
	}
}

// fits reports whether a request of the class may take a slot now; b.mu is held.
func (b *SlotBudget) fits(class Class) bool {
	if b.inUse >= b.total {
		return false
	}
	return class != Anonymous || b.anonUse < b.anon
}

// take counts a slot; b.mu is held.
func (b *SlotBudget) take(class Class) {
	b.inUse++
	if class == Anonymous {
		b.anonUse++
	}
	b.gauge()
}

func (b *SlotBudget) gauge() {
	if b.m == nil {
		return
	}
	b.m.MainInFlight.WithLabelValues(metrics.BudgetAuthenticated).Set(float64(b.inUse - b.anonUse))
	b.m.MainInFlight.WithLabelValues(metrics.BudgetAnonymous).Set(float64(b.anonUse))
}

// releaser returns the function that gives the slot back exactly once and hands it to the next waiter that fits.
func (b *SlotBudget) releaser(class Class) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.inUse--
			if class == Anonymous {
				b.anonUse--
			}
			b.grant()
			b.gauge()
		})
	}
}

// grant hands free slots to waiters in arrival order; a waiter whose class does not fit is skipped, so an
// anonymous request at its cap never blocks an authenticated one behind it. b.mu is held.
func (b *SlotBudget) grant() {
	kept := b.queue[:0]
	for _, w := range b.queue {
		if b.fits(w.class) {
			b.take(w.class)
			w.granted = true
			close(w.ch)
			continue
		}
		kept = append(kept, w)
	}
	for i := len(kept); i < len(b.queue); i++ {
		b.queue[i] = nil
	}
	b.queue = kept
}
