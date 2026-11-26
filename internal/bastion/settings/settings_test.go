package settings

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
)

var ctx = context.Background()

// memStore is a [Store] in memory; err, when set, is what LoadConfiguration answers (a forged or a missing object).
type memStore struct {
	mu      sync.Mutex
	payload []byte
	err     error
	writes  int
}

func (s *memStore) StoreConfiguration(_ context.Context, p []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.payload, s.writes = append([]byte(nil), p...), s.writes+1
	return nil
}

func (s *memStore) LoadConfiguration(context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	if s.payload == nil {
		return nil, errors.New("not found")
	}
	return s.payload, nil
}

func gauge(t *testing.T, g interface{ Write(*dto.Metric) error }) float64 {
	t.Helper()
	var m dto.Metric
	if err := g.Write(&m); err != nil {
		t.Fatal(err)
	}
	return m.GetGauge().GetValue()
}

// with returns the built-in configuration at revision rev with every figure the replica reads scaled by n, so that a reader
// can tell from any one figure which configuration it holds.
func with(rev int64, n int) iface.Configuration {
	c := Builtin()
	c.Revision = rev
	f := &c.Limits.Flood
	f.Anonymous.RequestsPerSecond, f.Authenticated.RequestsPerSecond, f.GUI.RequestsPerSecond = n, 2*n, n
	f.Blocking.Threshold = 100 * n
	c.Limits.Slots.MaxConcurrent = 10 * n
	perSecond := int64(n)
	c.Limits.AnonymousCeiling.RequestsPerSecond = &perSecond
	return c
}

// TestR_BAS_093_ABuiltinConfigurationFailsClosed: before its first handshake, without a stored configuration, a replica applies
// the built-in one (Q-BAS-327): every switch off, the Internet preset in Enforce with its flood figures, the default slots and
// anonymous ceiling, the default throughput of 200 MB/s with a quarter for anonymous callers and no hourly volume (Q-BAS-455),
// and a revision no registration has.
func TestR_BAS_093_ABuiltinConfigurationFailsClosed(t *testing.T) {
	m, err := metrics.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	a := New(Options{Metrics: m}).Current()
	c := a.Configuration
	switch {
	case a.Source != SourceBuiltin, c.Revision != 0, c.Switches != (iface.Switches{}):
		t.Fatalf("source %s, revision %d, switches %+v", a.Source, c.Revision, c.Switches)
	case c.Limits.Mode != iface.ModeEnforce || c.Limits.Preset != iface.PresetInternet:
		t.Fatalf("mode %s, preset %s", c.Limits.Mode, c.Limits.Preset)
	case a.Flood.Anonymous != Class{Rate: Rate{50, 1000}, MissesPerMinute: 1000} || a.Flood.GUI != (Rate{20, 100}):
		t.Fatalf("flood %+v", a.Flood)
	case a.Slots != (Slots{MaxConcurrent: 64, AnonymousShare: 0.25, QueueTimeout: 5 * time.Second}):
		t.Fatalf("slots %+v", a.Slots)
	case a.Ceiling != (Ceiling{PerSecond: 100, Burst: 2000, PerHour: 36000}):
		t.Fatalf("ceiling %+v", a.Ceiling)
	case a.Throughput != (Throughput{BytesPerSecond: 200_000_000, AnonymousShare: 0.25}) || a.Throughput.AnonymousBytesPerSecond() != 50_000_000:
		t.Fatalf("throughput %+v", a.Throughput)
	}
	if gauge(t, m.ConfigSource.WithLabelValues("builtin")) != 1 || gauge(t, m.ConfigSource.WithLabelValues("main")) != 0 {
		t.Fatal("quader_bastion_configuration_source does not say builtin")
	}
}

// TestR_BAS_093_ANewConfigurationIsAppliedWhole: a configuration of a handshake is applied at once and whole: every reader
// sees all figures of the old or all of the new one, never a mix; the watchers (the slot budget) see each swap; an equal
// configuration swaps and stores nothing; one Validate refuses is an error and changes nothing.
func TestR_BAS_093_ANewConfigurationIsAppliedWhole(t *testing.T) {
	m, err := metrics.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	st := &memStore{}
	h := New(Options{Store: st, Metrics: m})
	var seen []int
	h.Watch(func(a *Applied) { seen = append(seen, a.Slots.MaxConcurrent) })
	if _, err := h.Apply(ctx, with(1, 1)); err != nil {
		t.Fatal(err)
	}
	var mixed atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				a := h.Current()
				n := a.Flood.Anonymous.RequestsPerSecond
				if a.Flood.Authenticated.RequestsPerSecond != 2*n || a.Flood.Blocking.Threshold != 100*n || a.Slots.MaxConcurrent != 10*n ||
					a.Ceiling.PerSecond != int64(n) || a.Configuration.Revision != int64(n) {
					mixed.Add(1)
				}
			}
		}()
	}
	for i := 2; i <= 200; i++ {
		if _, err := h.Apply(ctx, with(int64(i), i)); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	if n := mixed.Load(); n != 0 {
		t.Fatalf("%d reads saw figures of two configurations", n)
	}
	if len(seen) != 201 || seen[0] != 64 || seen[1] != 10 || seen[200] != 2000 {
		t.Fatalf("the watcher saw %d swaps, first %d, last %d", len(seen), seen[0], seen[len(seen)-1])
	}
	if gauge(t, m.ConfigRevision) != 200 || gauge(t, m.ConfigSource.WithLabelValues("main")) != 1 {
		t.Fatal("quader_bastion_configuration_revision or _source does not follow")
	}
	writes := st.writes
	if swapped, err := h.Apply(ctx, with(200, 200)); swapped || err != nil || st.writes != writes {
		t.Fatalf("an equal configuration: swapped %v, %v, %d writes", swapped, err, st.writes-writes)
	}
	bad := with(201, 201)
	bad.Limits.Slots.AnonymousShare = 2
	if _, err := h.Apply(ctx, bad); err == nil || h.Current().Configuration.Revision != 200 {
		t.Fatalf("a refused configuration: %v, revision %d applied", err, h.Current().Configuration.Revision)
	}
}

// TestR_BAS_093_AReplicaStartedWithoutTheMainInstallationAppliesTheStoredConfiguration: what a replica applied is stored; a
// replica that starts while the main installation cannot be reached applies it (Q-BAS-327), and with none, or one whose
// signature fails, the built-in one. A handshake replaces either, and a stored configuration read after a handshake is not
// applied.
func TestR_BAS_093_AReplicaStartedWithoutTheMainInstallationAppliesTheStoredConfiguration(t *testing.T) {
	st := &memStore{}
	if _, err := New(Options{Store: st}).Apply(ctx, with(42, 3)); err != nil {
		t.Fatal(err)
	}
	restarted := New(Options{Store: st})
	if !restarted.LoadStored(ctx) {
		t.Fatal("the stored configuration was not applied")
	}
	if a := restarted.Current(); a.Source != SourceStored || a.Configuration.Revision != 42 || a.Flood.Anonymous.RequestsPerSecond != 3 {
		t.Fatalf("applied %s revision %d", a.Source, a.Configuration.Revision)
	}
	if _, err := restarted.Apply(ctx, with(42, 3)); err != nil || restarted.Current().Source != SourceMain {
		t.Fatalf("the first handshake of the same revision: %v, source %s", err, restarted.Current().Source)
	}
	if restarted.LoadStored(ctx) {
		t.Fatal("a stored configuration replaced the main installation's")
	}
	for name, s := range map[string]*memStore{
		"none":   {},
		"forged": {payload: []byte(`{}`), err: errors.New("cache: the record's signature does not verify")},
		"refused by Validate": {payload: func() []byte {
			b, _ := iface.Marshal(iface.Configuration{Revision: 3})
			return b
		}()},
		"not a configuration": {payload: []byte(`{"revision": "x"}`)},
	} {
		h := New(Options{Store: s})
		if h.LoadStored(ctx) || h.Current().Source != SourceBuiltin {
			t.Errorf("%s: applied %s", name, h.Current().Source)
		}
	}
}
