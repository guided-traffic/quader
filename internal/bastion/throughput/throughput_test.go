package throughput

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/bastion/settings"
	"github.com/guided-traffic/quader/internal/bastion/shared"
)

// The throughput of R-BAS-097 over the local counters, with a clock the tests drive: nothing waits for real time, and the
// payloads are endless streams of zeros of which the tests read kilobytes.

// fakeClock is a clock whose sleepers wake when the driver moves it: run advances it to the earliest wake once every reader
// that still runs sleeps, so the readers interleave as they would in real time.
type fakeClock struct {
	mu       sync.Mutex
	t        time.Time
	sleepers []*sleeper
}

type sleeper struct {
	until time.Time
	ch    chan struct{}
}

// newFakeClock starts at the beginning of a slice, so a second is ten whole slices.
func newFakeClock() *fakeClock { return &fakeClock{t: time.Unix(1_800_000_000, 0)} }

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	c.mu.Lock()
	s := &sleeper{until: c.t.Add(d), ch: make(chan struct{})}
	c.sleepers = append(c.sleepers, s)
	c.mu.Unlock()
	select {
	case <-s.ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// advance moves the clock by d at once, waking every sleeper on the way (a test with one reader in its own goroutine).
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.wake()
	c.mu.Unlock()
}

// wake releases the sleepers whose time has come; c.mu is held.
func (c *fakeClock) wake() {
	kept := c.sleepers[:0]
	for _, s := range c.sleepers {
		if !s.until.After(c.t) {
			close(s.ch)
			continue
		}
		kept = append(kept, s)
	}
	c.sleepers = kept
}

// run drives the clock while readers run: once as many sleep as run, it moves to the earliest wake.
func (c *fakeClock) run(t *testing.T, running *atomic.Int32) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for running.Load() > 0 {
		if time.Now().After(deadline) {
			t.Fatal("the readers neither sleep nor end")
		}
		c.mu.Lock()
		if int32(len(c.sleepers)) < running.Load() || len(c.sleepers) == 0 {
			c.mu.Unlock()
			time.Sleep(50 * time.Microsecond)
			continue
		}
		next := c.sleepers[0].until
		for _, s := range c.sleepers[1:] {
			if s.until.Before(next) {
				next = s.until
			}
		}
		c.t = next
		c.wake()
		c.mu.Unlock()
	}
}

// configuration applies a throughput of bytesPerSecond with the anonymous share and the queue time.
func configuration(t *testing.T, bytesPerSecond int64, share float64, queue time.Duration, hour func(*settings.Throughput)) func() *settings.Applied {
	t.Helper()
	c := settings.Builtin()
	c.Limits.Throughput.BytesPerSecond, c.Limits.Throughput.AnonymousShare = &bytesPerSecond, share
	c.Limits.Slots.QueueTimeoutMillis = int(queue / time.Millisecond)
	a, err := settings.FromConfiguration(c, settings.SourceMain)
	if err != nil {
		t.Fatal(err)
	}
	if hour != nil {
		hour(&a.Throughput)
	}
	return func() *settings.Applied { return a }
}

func newPacer(t *testing.T, clk *fakeClock, figures func() *settings.Applied, replicas int) (*Pacer, *metrics.Metrics) {
	t.Helper()
	m, err := metrics.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	st := shared.NewLocal(replicas, clk.now)
	return New(Options{Bytes: st.Bytes, Figures: figures, Metrics: m, Now: clk.now, Sleep: clk.sleep}), m
}

// zeros is an endless body.
type zeros struct{ closed atomic.Bool }

func (*zeros) Read(b []byte) (int, error) { clear(b); return len(b), nil }
func (z *zeros) Close() error             { z.closed.Store(true); return nil }

// value reads a counter's series.
func value(c prometheus.Counter) float64 {
	var m dto.Metric
	_ = c.Write(&m)
	return m.GetCounter().GetValue()
}

// TestR_BAS_097_AnAnonymousFillReadsAtMostItsShareWhileASignedInFillTakesTheRest: at 1 MB/s with an anonymous share of 25 %, an
// anonymous fill and a signed-in fill that read as fast as they can for one second read at most 250 KB and the rest of the
// 1 MB: the anonymous bucket holds the anonymous read, the total holds both, and what the anonymous read leaves is the other's.
func TestR_BAS_097_AnAnonymousFillReadsAtMostItsShareWhileASignedInFillTakesTheRest(t *testing.T) {
	clk := newFakeClock()
	p, m := newPacer(t, clk, configuration(t, 1_000_000, 0.25, 5*time.Second, nil), 1)
	start := clk.now()
	end := start.Add(time.Second)
	var running atomic.Int32
	var anonymous, signedIn atomic.Int64
	read := func(class mainclient.Class, into *atomic.Int64) {
		defer running.Add(-1)
		adm, err := p.Admit(t.Context(), class, start)
		if err != nil {
			t.Errorf("%s: admitted %v", class, err)
			return
		}
		body := adm.Pace(&zeros{})
		defer body.Close()
		buf := make([]byte, 32<<10)
		for {
			n, err := body.Read(buf)
			if err != nil || !clk.now().Before(end) {
				return
			}
			into.Add(int64(n))
		}
	}
	running.Store(2)
	go read(mainclient.Anonymous, &anonymous)
	go read(mainclient.Authenticated, &signedIn)
	clk.run(t, &running)
	a, s := anonymous.Load(), signedIn.Load()
	t.Logf("in one second at 1 MB/s: anonymous %d bytes, signed-in %d bytes", a, s)
	if a > 250_000 || a == 0 {
		t.Errorf("the anonymous fill read %d bytes in a second, want some and at most the share of 250,000", a)
	}
	if a+s > 1_000_000 || a+s < 950_000 || s < 750_000 {
		t.Errorf("the signed-in fill read %d bytes beside the anonymous %d, want the rest of 1,000,000", s, a)
	}
	if w := value(m.ThroughputWait.WithLabelValues(metrics.ThroughputAnonymous)); w <= 0 {
		t.Errorf("quader_bastion_throughput_wait_seconds_total{class=anonymous} = %v, want the time the cap held it", w)
	}
}

// TestR_BAS_097_ANewMissAtTheCapIsBusyWhileARunningTransferSlowsAndCompletes: at 1 MB/s with a queue time of 200 ms, the
// running transfers have reserved the slices of the next 300 ms; a new miss of either class finds no room in a slice that
// begins within its queue time and is refused at once with the wait until the first slice with room, while a transfer that
// runs reads its whole body at the cap's pace.
func TestR_BAS_097_ANewMissAtTheCapIsBusyWhileARunningTransferSlowsAndCompletes(t *testing.T) {
	clk := newFakeClock()
	p, m := newPacer(t, clk, configuration(t, 1_000_000, 0.25, 200*time.Millisecond, nil), 1)
	now := clk.now()
	// Twelve admitted signed-in misses take the first 25 KB each of slices 0 to 2: four of them fill a slice of 100 KB.
	var held []mainclient.Admission
	for range 12 {
		adm, err := p.Admit(t.Context(), mainclient.Authenticated, now)
		if err != nil {
			t.Fatalf("a miss before the cap: %v", err)
		}
		held = append(held, adm)
	}
	for _, class := range []mainclient.Class{mainclient.Authenticated, mainclient.Anonymous} {
		_, err := p.Admit(t.Context(), class, now)
		var te *mainclient.ThroughputError
		if !errors.As(err, &te) || te.Hour || !errors.Is(err, mainclient.ErrBusy) {
			t.Fatalf("a %s miss at the cap: %v, want busy", class, err)
		}
		if te.RetryAfter != 300*time.Millisecond {
			t.Fatalf("a %s miss at the cap waits %v, want the 300 ms until slice 3", class, te.RetryAfter)
		}
	}
	if got := value(m.ThroughputRefused.WithLabelValues(metrics.ThroughputSignedIn, metrics.ThroughputBusy)); got != 1 {
		t.Fatalf("quader_bastion_throughput_refused_total{class=signed_in,reason=busy} = %v, want 1", got)
	}
	// A running transfer of 300 KB (a fill's continuation) is never refused: it queues behind the reservations and reads all.
	body := p.Running(t.Context(), mainclient.Authenticated).Pace(io.NopCloser(io.LimitReader(&zeros{}, 300_000)))
	done := make(chan int64)
	go func() {
		n, err := io.Copy(io.Discard, body)
		if err != nil {
			t.Errorf("the running transfer: %v", err)
		}
		done <- n
	}()
	var got int64
	for got == 0 {
		select {
		case got = <-done:
		default:
			clk.advance(10 * time.Millisecond)
			time.Sleep(100 * time.Microsecond)
		}
	}
	if got != 300_000 {
		t.Fatalf("the running transfer read %d bytes, want all 300,000", got)
	}
	if el := clk.now().Sub(now); el < 500*time.Millisecond {
		t.Fatalf("the running transfer of 300 KB behind 300 KB of reservations ended after %v, want it slowed to the cap", el)
	}
	if body.Waited() <= 0 {
		t.Fatal("the running transfer reports no wait")
	}
	for _, a := range held {
		a.Cancel()
	}
}

// TestR_BAS_097_AnUnreadReservationIsGivenBack: a miss whose answer has no body to read (a 304) gives its first bytes back,
// so the next miss finds them in the same slice.
func TestR_BAS_097_AnUnreadReservationIsGivenBack(t *testing.T) {
	clk := newFakeClock()
	p, _ := newPacer(t, clk, configuration(t, 1_000_000, 0.25, 0, nil), 1)
	for i := range 20 {
		adm, err := p.Admit(t.Context(), mainclient.Authenticated, clk.now())
		if err != nil {
			t.Fatalf("miss %d with a queue time of 0 in one slice: %v", i, err)
		}
		if i%2 == 0 {
			adm.Cancel()
			continue
		}
		b := adm.Pace(io.NopCloser(io.LimitReader(&zeros{}, 10)))
		if _, err := io.Copy(io.Discard, b); err != nil {
			t.Fatal(err)
		}
		_ = b.Close()
	}
}

// TestR_BAS_097_AClassWhoseHourIsUsedUpIsRefusedItsNextMiss: with an anonymous hourly volume of 100 KB, an anonymous transfer
// that reads 120 KB completes (a running transfer is never refused), and the next anonymous miss answers the hour with the
// wait, while a signed-in miss is admitted.
func TestR_BAS_097_AClassWhoseHourIsUsedUpIsRefusedItsNextMiss(t *testing.T) {
	clk := newFakeClock()
	p, m := newPacer(t, clk, configuration(t, 100_000_000, 0.25, time.Second, func(t *settings.Throughput) { t.Hour.Anonymous = 100_000 }), 1)
	adm, err := p.Admit(t.Context(), mainclient.Anonymous, clk.now())
	if err != nil {
		t.Fatal(err)
	}
	body := adm.Pace(io.NopCloser(io.LimitReader(&zeros{}, 120_000)))
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, body)
		done <- errors.Join(err, body.Close())
	}()
	for finished := false; !finished; {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			finished = true
		default:
			clk.advance(10 * time.Millisecond)
			time.Sleep(100 * time.Microsecond)
		}
	}
	_, err = p.Admit(t.Context(), mainclient.Anonymous, clk.now())
	var te *mainclient.ThroughputError
	if !errors.As(err, &te) || !te.Hour || te.HourLimit != 100_000 || te.RetryAfter <= 0 || errors.Is(err, mainclient.ErrBusy) {
		t.Fatalf("the anonymous miss after its hour: %v (%+v)", err, te)
	}
	if got := value(m.ThroughputRefused.WithLabelValues(metrics.ThroughputAnonymous, metrics.ThroughputHour)); got != 1 {
		t.Fatalf("quader_bastion_throughput_refused_total{class=anonymous,reason=hour} = %v, want 1", got)
	}
	signed, err := p.Admit(t.Context(), mainclient.Authenticated, clk.now())
	if err != nil {
		t.Fatalf("a signed-in miss beside the anonymous hour: %v", err)
	}
	signed.Cancel()
}

// TestR_BAS_097_WithoutValkeyEachReplicaHoldsItsShare: without Valkey a replica of a Bastion of two reads at most half the
// figure (bytesPerSecond ÷ bastion.replicas), so the two together stay at it.
func TestR_BAS_097_WithoutValkeyEachReplicaHoldsItsShare(t *testing.T) {
	clk := newFakeClock()
	p, _ := newPacer(t, clk, configuration(t, 1_000_000, 0.25, 5*time.Second, nil), 2)
	start := clk.now()
	end := start.Add(time.Second)
	var running atomic.Int32
	var read atomic.Int64
	running.Store(1)
	go func() {
		defer running.Add(-1)
		body := p.Running(t.Context(), mainclient.Authenticated).Pace(&zeros{})
		buf := make([]byte, 32<<10)
		for {
			n, err := body.Read(buf)
			if err != nil || !clk.now().Before(end) {
				return
			}
			read.Add(int64(n))
		}
	}()
	clk.run(t, &running)
	if got := read.Load(); got > 500_000 || got < 450_000 {
		t.Fatalf("one replica of two read %d bytes in a second, want its share of 500,000", got)
	}
}

// TestR_BAS_097_AnUnlimitedThroughputPacesNothing: without a figure a body is read as it comes and nothing waits.
func TestR_BAS_097_AnUnlimitedThroughputPacesNothing(t *testing.T) {
	clk := newFakeClock()
	c := settings.Builtin()
	c.Limits.Throughput.BytesPerSecond = nil
	a, err := settings.FromConfiguration(c, settings.SourceMain)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := newPacer(t, clk, func() *settings.Applied { return a }, 1)
	adm, err := p.Admit(t.Context(), mainclient.Anonymous, clk.now())
	if err != nil {
		t.Fatal(err)
	}
	body := adm.Pace(io.NopCloser(io.LimitReader(&zeros{}, 10<<20)))
	if n, err := io.Copy(io.Discard, body); err != nil || n != 10<<20 || body.Waited() != 0 {
		t.Fatalf("read %d bytes, %v, waited %v", n, err, body.Waited())
	}
}

// TestR_BAS_097_TheCatalogueWaitsFirstAtTheCap: the catalogue's pull reads from a slice only while it is less than half used,
// so when the callers' misses take a slice beyond half, the catalogue reads in a later one.
func TestR_BAS_097_TheCatalogueWaitsFirstAtTheCap(t *testing.T) {
	clk := newFakeClock()
	p, _ := newPacer(t, clk, configuration(t, 1_000_000, 0.25, 5*time.Second, nil), 1)
	// The callers take 75 KB of slice 0.
	for range 3 {
		if _, err := p.Admit(t.Context(), mainclient.Authenticated, clk.now()); err != nil {
			t.Fatal(err)
		}
	}
	pl := p.planOf(Catalogue)
	r, _, ok := p.reserve(t.Context(), pl, pl.chunk, -1)
	if !ok || r.idx == p.sliceOf(clk.now()) {
		t.Fatalf("the catalogue reserved %+v in the slice the callers hold three quarters of", r)
	}
	// A caller still finds the last quarter of slice 0.
	adm, err := p.Admit(t.Context(), mainclient.Authenticated, clk.now())
	if err != nil {
		t.Fatal(err)
	}
	if got := adm.(*admission).first.idx; got != p.sliceOf(clk.now()) {
		t.Fatalf("the caller's miss reserved in slice %d, want the current one", got)
	}
}
