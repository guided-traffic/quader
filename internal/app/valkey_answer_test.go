package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/observability/health"
)

// fakeValkey is the part of the Valkey client that the start reads: the first answer, the start-up error and the
// status. The test moves it through what a real client reports (internal/cluster's TestValkeyFirstAnswer shows the
// client reporting them against a server).
type fakeValkey struct {
	mu       sync.Mutex
	answered chan struct{}
	once     sync.Once
	err      error
	st       cluster.ValkeyStatus
}

func newFakeValkey() *fakeValkey {
	return &fakeValkey{answered: make(chan struct{}), st: cluster.ValkeyStatus{ReasonCode: cluster.ReasonTimeout, DegradedReason: "not connected: i/o timeout"}}
}

func (f *fakeValkey) Answered() <-chan struct{} { return f.answered }

func (f *fakeValkey) Status() cluster.ValkeyStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.st
}

func (f *fakeValkey) startupError() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

// answer moves the fake to an answer: accepted (err nil) or refused.
func (f *fakeValkey) answer(err error) {
	f.mu.Lock()
	if err == nil {
		f.st = cluster.ValkeyStatus{Available: true}
	} else {
		f.err, f.st = err, cluster.ValkeyStatus{ReasonCode: cluster.ReasonAuthFailed, DegradedReason: "not connected: WRONGPASS"}
	}
	f.mu.Unlock()
	f.once.Do(func() { close(f.answered) })
}

// lose makes a connected client unavailable, as a primary that went away after the first readiness.
func (f *fakeValkey) lose() {
	f.mu.Lock()
	f.st = cluster.ValkeyStatus{ReasonCode: cluster.ReasonCircuitOpen, DegradedReason: "circuit open"}
	f.mu.Unlock()
}

// startRig is the start of a replica reduced to what decides its first readiness: the startup check and the valkey
// check of the composition root on a registry, and step 10's wait.
type startRig struct {
	h        *health.Health
	vk       *fakeValkey
	started  atomic.Bool
	awaiting atomic.Bool
	logs     bytes.Buffer
	logMu    sync.Mutex
	done     chan error
}

func newStartRig(t *testing.T) *startRig {
	t.Helper()
	r := &startRig{
		h:    health.New(config.Health{CheckInterval: time.Hour, CheckTimeout: time.Second, FailureThreshold: 1, SuccessThreshold: 1}),
		vk:   newFakeValkey(),
		done: make(chan error, 1),
	}
	r.h.AddReadiness("startup", startupCheck(&r.started, &r.awaiting), false)
	r.h.AddReported("valkey", func(context.Context) error { return valkeyCheck(r.vk.startupError(), r.vk.Status()) })
	return r
}

func (r *startRig) Write(p []byte) (int, error) {
	r.logMu.Lock()
	defer r.logMu.Unlock()
	return r.logs.Write(p)
}

func (r *startRig) log() string {
	r.logMu.Lock()
	defer r.logMu.Unlock()
	return r.logs.String()
}

// step10 runs the wait of step 10 in the background and then passes the startup check, as bootstrap does.
func (r *startRig) step10(ctx context.Context, bound time.Duration) {
	log := slog.New(slog.NewJSONHandler(r, nil))
	since := time.Now()
	go func() {
		err := awaitValkeyAnswer(ctx, r.vk, since, bound, &r.awaiting, log)
		if err == nil {
			r.started.Store(true)
		}
		r.done <- err
	}()
}

func (r *startRig) wait(t *testing.T, within time.Duration) time.Duration {
	t.Helper()
	start := time.Now()
	select {
	case err := <-r.done:
		if err != nil {
			t.Fatalf("step 10: %v", err)
		}
	case <-time.After(within):
		t.Fatalf("step 10 did not end within %s", within)
	}
	return time.Since(start)
}

func (r *startRig) snapshot(t *testing.T) health.Snapshot {
	t.Helper()
	r.h.Refresh(t.Context())
	return r.h.Snapshot()
}

func checkOf(s health.Snapshot, name string) health.CheckResult {
	for _, c := range s.Checks {
		if c.Name == name {
			return c
		}
	}
	return health.CheckResult{}
}

func TestFirstReadinessWaitsForValkey(t *testing.T) {
	t.Run("R-HA-008 R-HA-056 silence that ends in an answer inside the bound: not ready while it waits, ready at the answer", func(t *testing.T) {
		r := newStartRig(t)
		r.step10(t.Context(), time.Minute)
		waitFor := func(cond func() bool) {
			deadline := time.Now().Add(5 * time.Second)
			for !cond() && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}
		}
		waitFor(r.awaiting.Load)
		s := r.snapshot(t)
		if s.State != health.StateStarting || !slices.Equal(s.Failing, []string{"startup"}) || checkOf(s, "startup").Reason != "waiting_for_valkey" {
			t.Fatalf("while waiting: state %s, failing %v, startup reason %q", s.State, s.Failing, checkOf(s, "startup").Reason)
		}
		if c := checkOf(s, "valkey"); c.Status != health.Degraded || c.Reason != string(cluster.ReasonTimeout) {
			t.Fatalf("while waiting the valkey check is %+v, want degraded with the client's reason", c)
		}
		r.vk.answer(nil)
		if took := r.wait(t, 5*time.Second); took > 2*time.Second {
			t.Fatalf("the answer ended the wait after %s, not at once", took)
		}
		s = r.snapshot(t)
		if s.State != health.StateReady || len(s.Degraded) != 0 {
			t.Fatalf("after the answer: state %s, degraded %v", s.State, s.Degraded)
		}
		if !strings.Contains(r.log(), "waiting for Valkey's first answer") || !strings.Contains(r.log(), "valkey answered") {
			t.Fatalf("the log does not say that the start waited and why it ended:\n%s", r.log())
		}
	})

	t.Run("R-HA-008 R-HA-056 a refusal inside the bound ends the wait and holds the first readiness with the cause", func(t *testing.T) {
		r := newStartRig(t)
		r.step10(t.Context(), time.Minute)
		r.vk.answer(errors.New("valkey configuration error: WRONGPASS invalid username-password pair or user is disabled."))
		r.wait(t, 5*time.Second)
		s := r.snapshot(t)
		c := checkOf(s, "valkey")
		if s.State != health.StateStarting || !slices.Equal(s.Failing, []string{"valkey"}) || c.Reason != string(cluster.ReasonAuthFailed) || !c.ConfigError {
			t.Fatalf("after a refusal: state %s, failing %v, valkey %+v", s.State, s.Failing, c)
		}
		if strings.Contains(r.log(), "no answer within") {
			t.Fatalf("a refusal is an answer, the log says otherwise:\n%s", r.log())
		}
	})

	t.Run("R-HA-008 R-HA-057 silence past the bound: ready, valkey degraded, and the log names the bound and the reason", func(t *testing.T) {
		r := newStartRig(t)
		const bound = 50 * time.Millisecond
		r.step10(t.Context(), bound)
		if took := r.wait(t, 5*time.Second); took < bound/2 {
			t.Fatalf("step 10 ended after %s, before the bound", took)
		}
		s := r.snapshot(t)
		if s.State != health.StateReady || !slices.Equal(s.Degraded, []string{"valkey"}) || checkOf(s, "valkey").Reason != string(cluster.ReasonTimeout) {
			t.Fatalf("after the bound: state %s, degraded %v, valkey %+v", s.State, s.Degraded, checkOf(s, "valkey"))
		}
		line := r.log()
		for _, want := range []string{"no answer within valkey.startup_answer_timeout", `"bound":"50ms"`, `"reason_code":"timeout"`} {
			if !strings.Contains(line, want) {
				t.Fatalf("the log lacks %q:\n%s", want, line)
			}
		}
	})

	t.Run("R-HA-008 R-HA-057 a client that has answered does not wait, and a loss after the first readiness keeps the replica ready", func(t *testing.T) {
		r := newStartRig(t)
		r.vk.answer(nil)
		r.step10(t.Context(), time.Hour)
		r.wait(t, time.Second)
		if s := r.snapshot(t); s.State != health.StateReady {
			t.Fatalf("state %s", s.State)
		}
		if r.log() != "" {
			t.Fatalf("a client that had answered logged a wait:\n%s", r.log())
		}
		r.vk.lose()
		r.vk.mu.Lock()
		r.vk.err = errors.New("valkey configuration error: WRONGPASS") // a credential rotated wrongly at run time
		r.vk.mu.Unlock()
		s := r.snapshot(t)
		if s.State != health.StateReady || !slices.Equal(s.Degraded, []string{"valkey"}) {
			t.Fatalf("after the loss: state %s, degraded %v, failing %v", s.State, s.Degraded, s.Failing)
		}
	})

	t.Run("R-ARCH-024 a replica stopped while it waits ends step 10 with the context's error", func(t *testing.T) {
		r := newStartRig(t)
		ctx, cancel := context.WithCancel(t.Context())
		r.step10(ctx, time.Hour)
		cancel()
		select {
		case err := <-r.done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the wait did not end with its context")
		}
		if r.started.Load() || r.awaiting.Load() {
			t.Fatalf("started %v, awaiting %v after the stop", r.started.Load(), r.awaiting.Load())
		}
	})
}
