package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/cache"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta/postgres"
)

// The trigger of the public catalogue's builder (bastion.md R-BAS-105) without a database or a queue: what it enqueues when its replica
// starts and after a change. Milliseconds, no data.

// fakeTrigger is a trigger whose answers the test sets and whose enqueues it counts.
func fakeTrigger(active, drift bool) (*catalogueTrigger, *atomic.Int64) {
	var started atomic.Int64
	t := &catalogueTrigger{
		log:     slog.New(slog.DiscardHandler),
		active:  func(context.Context) bool { return active },
		enqueue: func(context.Context) error { started.Add(1); return nil },
		drift:   func(context.Context) (bool, error) { return drift, nil },
	}
	return t, &started
}

// TestR_BAS_104_TheBuilderTakesTheDefaultEndpointsAnonymousSetting (H-2330): the builder builds every catalogue empty exactly while
// the default endpoint admits no request without a credential, as the handshake's anonymousReads reads the endpoint.
func TestR_BAS_104_TheBuilderTakesTheDefaultEndpointsAnonymousSetting(t *testing.T) {
	cfg := &config.Config{}
	if anonymousRefused(cfg) {
		t.Fatal("the default endpoint admits anonymous requests by default, but the builder refuses")
	}
	no := false
	cfg.Server.Endpoints = map[string]config.Endpoint{config.DefaultEndpoint: {Auth: config.EndpointAuth{Anonymous: &no}}}
	if !anonymousRefused(cfg) {
		t.Fatal("auth.anonymous false on the default endpoint, but the builder builds")
	}
}

// TestR_BAS_102_SwitchingTheWebInterfaceOnStartsTheBuilder: a change of a registration's configuration (its switches, the change area
// bastion_configuration) starts a run within the debounce, also right after the trigger answered for another change that no
// registration had its web interface on: the switch is what made it on, so the trigger asks again instead of its cached answer.
func TestR_BAS_102_SwitchingTheWebInterfaceOnStartsTheBuilder(t *testing.T) {
	var on atomic.Bool
	tr, started := fakeTrigger(false, false)
	tr.active = func(context.Context) bool { return on.Load() }
	caches, err := cache.NewRegistry(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	tr.register(caches)
	if err := tr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Stop(context.Background()) })
	wait := func(want int64, within time.Duration) bool {
		deadline := time.Now().Add(within)
		for time.Now().Before(deadline) {
			if started.Load() >= want {
				return true
			}
			time.Sleep(5 * time.Millisecond)
		}
		return false
	}

	// A repository's settings changed while no web interface is on: nothing starts, and the trigger keeps that answer for a while.
	caches.ApplyChange(cluster.AreaRepoConfig, 1, json.RawMessage(`{}`))
	if wait(1, 100*time.Millisecond) {
		t.Fatal("a run started while no registration has its web interface on")
	}
	// The web interface switched on (PATCH /api/v1/bastions/{name} notifies bastion_configuration): a run starts at once.
	on.Store(true)
	caches.ApplyChange(cluster.AreaBastionConfiguration, 1, json.RawMessage(`{"bastion":"0192f1c4-0000-7000-8000-0000000000b1"}`))
	if !wait(1, 2*time.Second) {
		t.Fatal("switching the web interface on started no run")
	}
}

// TestR_BAS_105_AContactWithANewBaseURLStartsTheBuilder: a Bastion that connects for the first time, or moves, after its web interface
// was switched on raises bastion_base_url at its contact (Q-BAS-499), and the trigger starts a run within the debounce instead of
// leaving the web interface empty until the hourly run.
func TestR_BAS_105_AContactWithANewBaseURLStartsTheBuilder(t *testing.T) {
	signals, err := cluster.NewSignals(cluster.SignalsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tr, started := fakeTrigger(true, false)
	tr.signals = signals
	if err := tr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Stop(context.Background()) })
	signals.Publish(cluster.AreaBastionBaseURL, "0192f1c4-0000-7000-8000-0000000000b1")
	deadline := time.Now().Add(2 * time.Second)
	for started.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if started.Load() == 0 {
		t.Fatal("a contact with a new base URL started no run")
	}
}

// TestR_BAS_105_AReplicaThatStartsWithAChangedConfigurationStartsARun: a change of the configuration a run reads (the default
// endpoint's auth.anonymous, server.additional_base_urls) applies only with a restart, so the trigger starts a run when its replica
// starts while the store does not hold what a run would leave, and none while it does.
func TestR_BAS_105_AReplicaThatStartsWithAChangedConfigurationStartsARun(t *testing.T) {
	for _, drift := range []bool{true, false} {
		tr, started := fakeTrigger(false, drift)
		if err := tr.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for drift && started.Load() == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		_ = tr.Stop(context.Background())
		if want := map[bool]int64{true: 1, false: 0}[drift]; started.Load() != want {
			t.Errorf("drift %v: %d runs started, want %d", drift, started.Load(), want)
		}
	}
}

// comparedHandler is a log handler that closes compared at the trigger's warning that its first comparison failed.
type comparedHandler struct {
	once     *sync.Once
	compared chan struct{}
}

func (h comparedHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h comparedHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h comparedHandler) WithGroup(string) slog.Handler            { return h }
func (h comparedHandler) Handle(_ context.Context, r slog.Record) error {
	if strings.Contains(r.Message, "could not compare") {
		h.once.Do(func() { close(h.compared) })
	}
	return nil
}

// TestR_BAS_105_TheTriggerReadsNothingTheWiringStillWrites: the trigger's first comparison runs on its own goroutine as soon as its
// step has run, while the wiring goes on and puts the results of the later steps into the replica's parts. It compares over what its
// step was handed and reads nothing of those parts (a replica crashed at start-up with a concurrent map read and map write). The test
// goes on putting parts until the comparison has run; run with -race. Milliseconds, no data, no database: the step is handed no token
// service, so the comparison ends before its first query.
func TestR_BAS_105_TheTriggerReadsNothingTheWiringStillWrites(t *testing.T) {
	compared := make(chan struct{})
	a := appFor(t, "worker")
	a.log = slog.New(comparedHandler{once: &sync.Once{}, compared: compared})
	out, err := a.wireBastionCatalogue(t.Context(), infra{db: &postgres.DB{}}, coordArea{queue: &jobs.Queue{}}, identityArea{}, packagesAPIArea{}, vulnAPIArea{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = out.trigger.Stop(context.Background()) })
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-compared:
			return
		case <-deadline:
			t.Fatal("the trigger's first comparison did not run")
		default:
			// A later step of the wiring puts its result.
			a.parts.put(reflect.ValueOf(sessionsArea{}))
		}
	}
}
