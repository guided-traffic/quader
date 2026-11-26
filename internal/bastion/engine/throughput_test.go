package engine

import (
	"bytes"
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/bastion/settings"
	"github.com/guided-traffic/quader/internal/bastion/shared"
	"github.com/guided-traffic/quader/internal/bastion/throughput"
)

// TestR_BAS_097_AMissTheThroughputRefusesIsBusyOrUsedUpAndAHitIsServed: a miss the throughput does not admit (the client of
// the main installation returns its refusal) answers 503 busy with the wait at the cap, and 429 upstream_limit_reached with the
// wait and the figure when its class's hour is used up; both count as refused for the throughput, and a hit is served at once.
// Payloads are bytes.
func TestR_BAS_097_AMissTheThroughputRefusesIsBusyOrUsedUpAndAHitIsServed(t *testing.T) {
	s := newSiteHarness(t, options{})
	s.site.artifacts[tgz] = payload(64)
	if w := s.get(tgz); w.Code != http.StatusOK {
		t.Fatalf("the fill: %d", w.Code)
	}
	site := s.main.doFn
	var refusal error
	s.main.mu.Lock()
	s.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
		if refusal != nil {
			return nil, refusal
		}
		return site(f)
	}
	s.main.mu.Unlock()
	s.site.artifacts["/api/npm/web/npm-all/other.tgz"] = payload(64)

	refusal = &mainclient.ThroughputError{RetryAfter: 300 * time.Millisecond}
	w := s.get("/api/npm/web/npm-all/other.tgz")
	if w.Code != http.StatusServiceUnavailable || problemCode(w) != "busy" || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("a miss at the cap: %d %v %s", w.Code, w.Header(), w.Body)
	}
	refusal = &mainclient.ThroughputError{RetryAfter: 90 * time.Minute, Hour: true, HourLimit: 1_000_000_000}
	w = s.get("/api/npm/web/npm-all/other.tgz")
	if w.Code != http.StatusTooManyRequests || problemCode(w) != codeUpstreamLimitReached || w.Header().Get("Retry-After") == "" {
		t.Fatalf("a miss after its class's hour: %d %v %s", w.Code, w.Header(), w.Body)
	}
	if got := counterValue(t, s.harness, "refused", metrics.RefusedThroughput); got != 2 {
		t.Fatalf("quader_bastion_refused_total{reason=throughput} = %v, want 2", got)
	}
	if w := s.get(tgz); w.Code != http.StatusOK {
		t.Fatalf("a hit while the throughput refuses misses: %d", w.Code)
	}
}

// TestR_BAS_097_AHandoversDirectReadIsPaced: the direct read of a handover from the object store takes its bytes from the
// throughput like a fill from the main installation (Q-BAS-331): at 1 MB/s a blob of 192 KiB waits for later slices, and is
// read whole and kept. The pacer's sleep only records the waits, so nothing waits for real time.
func TestR_BAS_097_AHandoversDirectReadIsPaced(t *testing.T) {
	bytesPerSecond := int64(1_000_000)
	c := settings.Builtin()
	c.Limits.Throughput.BytesPerSecond = &bytesPerSecond
	a, err := settings.FromConfiguration(c, settings.SourceMain)
	if err != nil {
		t.Fatal(err)
	}
	var slept atomic.Int64
	p := throughput.New(throughput.Options{
		Bytes: shared.NewLocal(1, nil).Bytes, Figures: func() *settings.Applied { return a },
		Sleep: func(_ context.Context, d time.Duration) error { slept.Add(int64(d)); return nil },
	})
	x := newHandoverRig(t, options{throughput: p})
	blob := payload(192 << 10)
	x.hand(t, hoTgz, blob, false)
	if w := x.get(hoTgz, "Authorization", bearer(x.full)); w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), blob) {
		t.Fatalf("the read: %d, %d bytes", w.Code, w.Body.Len())
	}
	if x.handovers(metrics.HandoverRead) != 1 || x.fillBytes(metrics.SourceStore) != float64(len(blob)) {
		t.Fatalf("handovers read %v, fill bytes from the store %v", x.handovers(metrics.HandoverRead), x.fillBytes(metrics.SourceStore))
	}
	if slept.Load() <= 0 {
		t.Fatal("the direct read never waited for the throughput's cap")
	}
}
