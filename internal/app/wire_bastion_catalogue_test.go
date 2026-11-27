package app

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/config"
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
