package settings

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
)

// Store keeps the configuration a replica applied in its cache backend, configuration/current.json (R-BAS-093, Q-BAS-327):
// signed where the backend's entries are signed and in R-BAS-061's stored format with a cache key. cache.Cache implements it.
type Store interface {
	StoreConfiguration(ctx context.Context, payload []byte) error
	// LoadConfiguration returns the stored payload; a missing object, one whose signature fails or one the stored format
	// refuses is an error.
	LoadConfiguration(ctx context.Context) ([]byte, error)
}

// storeTimeout bounds the write of the stored configuration and its read at start: the cache backend gates nothing of the
// configuration, so a slow one costs a replica at most this.
const storeTimeout = 10 * time.Second

// Options configure a [Holder].
type Options struct {
	// Store keeps the applied configuration; nil keeps nothing (tests).
	Store   Store
	Metrics *metrics.Metrics
	Log     *slog.Logger
}

// Holder holds the configuration a replica applies. It starts with [Builtin]; [Holder.LoadStored] replaces that with the stored
// one, [Holder.Apply] with the one of a handshake. It is safe for concurrent use.
type Holder struct {
	o   Options
	log *slog.Logger
	cur atomic.Pointer[Applied]

	// mu serializes the swaps and the watchers they call, so that a watcher sees the swaps in their order.
	mu       sync.Mutex
	watchers []func(*Applied)
}

// New returns a holder that applies [Builtin].
func New(o Options) *Holder {
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	h := &Holder{o: o, log: o.Log.With("component", "configuration")}
	a, err := FromConfiguration(Builtin(), SourceBuiltin)
	if err != nil {
		panic(fmt.Sprintf("the built-in configuration is invalid: %v", err))
	}
	h.cur.Store(a)
	h.export(a)
	return h
}

// Current returns the configuration applied now.
func (h *Holder) Current() *Applied { return h.cur.Load() }

// For returns the configuration a request read at its start ([With]), or the one applied now.
func (h *Holder) For(ctx context.Context) *Applied {
	if a := From(ctx); a != nil {
		return a
	}
	return h.Current()
}

// Watch registers a function called after every swap with the configuration swapped in, and once now with the current one.
// It runs under the holder's lock and must not block: the slot budget resizes in it (R-BAS-025).
func (h *Holder) Watch(f func(*Applied)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.watchers = append(h.watchers, f)
	f(h.Current())
}

// LoadStored applies the stored configuration while the replica still applies the built-in one (Q-BAS-327): a replica that
// starts while the main installation cannot be reached keeps the administrator's figures and switches. A missing object, one
// whose signature fails, one that does not decode and one that Validate refuses leave the built-in configuration; the first
// handshake replaces either. It reports whether the stored one was applied.
func (h *Holder) LoadStored(ctx context.Context) bool {
	if h.o.Store == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, storeTimeout)
	defer cancel()
	raw, err := h.o.Store.LoadConfiguration(ctx)
	if err != nil {
		h.log.Info("no stored configuration applied, the built-in one holds until the first handshake", "error", err)
		return false
	}
	var c iface.Configuration
	if err := iface.DecodeStrict(bytes.NewReader(raw), &c); err != nil {
		h.log.Warn("the stored configuration does not decode, the built-in one holds until the first handshake", "error", err)
		return false
	}
	a, err := FromConfiguration(c, SourceStored)
	if err != nil {
		h.log.Warn("the stored configuration is refused, the built-in one holds until the first handshake", "error", err)
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.Current().Source != SourceBuiltin {
		return false // a handshake came first
	}
	h.swap(a)
	h.log.Info("stored configuration applied until the first handshake", "revision", c.Revision)
	return true
}

// Apply applies the configuration of a handshake whole (R-BAS-093). One that equals the configuration applied now changes
// nothing; another is swapped in, the watchers are called, and it is stored for a restart. It reports whether it swapped.
// A configuration Validate refuses is an error and changes nothing: the link fails the handshake with it.
func (h *Holder) Apply(ctx context.Context, c iface.Configuration) (bool, error) {
	cur := h.Current()
	if cur.Source == SourceMain && reflect.DeepEqual(cur.Configuration, c) {
		return false, nil
	}
	a, err := FromConfiguration(c, SourceMain)
	if err != nil {
		return false, err
	}
	h.mu.Lock()
	h.swap(a)
	h.mu.Unlock()
	h.log.Info("configuration applied", "revision", c.Revision, "previous", cur.Configuration.Revision, "from", string(cur.Source))
	h.store(ctx, c)
	return true, nil
}

// swap stores a, exports it and calls the watchers; h.mu is held.
func (h *Holder) swap(a *Applied) {
	h.cur.Store(a)
	h.export(a)
	for _, w := range slices.Clone(h.watchers) {
		w(a)
	}
}

// store writes the configuration for a restart; a failure is logged and changes nothing of what the replica applies.
func (h *Holder) store(ctx context.Context, c iface.Configuration) {
	if h.o.Store == nil {
		return
	}
	payload, err := iface.Marshal(c)
	if err == nil {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), storeTimeout)
		err = h.o.Store.StoreConfiguration(sctx, payload)
		cancel()
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		h.log.Warn("the applied configuration could not be stored; a restart without the main installation applies an older one or the built-in one", "error", err)
	}
}

func (h *Holder) export(a *Applied) {
	m := h.o.Metrics
	if m == nil {
		return
	}
	m.ConfigRevision.Set(float64(a.Configuration.Revision))
	mode := 0.0
	if a.Enforce {
		mode = 1
	}
	m.LimitsMode.Set(mode)
	for _, s := range []Source{SourceMain, SourceStored, SourceBuiltin} {
		v := 0.0
		if s == a.Source {
			v = 1
		}
		m.ConfigSource.WithLabelValues(string(s)).Set(v)
	}
}
