package httpserver

import (
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoryGuard(t *testing.T) {
	t.Run("R-ARCH-005 the memory slots are high-water times the Go memory limit over the request bytes, and no known limit means no guard", func(t *testing.T) {
		g := NewMemoryGuard(MemoryGuardConfig{Limit: 100 << 20, RequestBytes: 1 << 20, Read: func() (uint64, uint64) { return 0, 0 }})
		if g.Slots() != 80 {
			t.Fatalf("slots = %d, want 80", g.Slots())
		}
		held := func(n int64) *MemoryGuard {
			g := NewMemoryGuard(MemoryGuardConfig{Limit: 100 << 20, RequestBytes: 1 << 20, Held: func() int64 { return n }, Read: func() (uint64, uint64) { return 0, 0 }})
			g.Sample()
			return g
		}
		if g := held(30 << 20); g.Slots() != 50 {
			t.Fatalf("with 30 MiB in the caches the slots are %d, want (80 - 30) MiB / 1 MiB = 50 (Q-ARCH-328)", g.Slots())
		}
		if g := held(90 << 20); g.Slots() != 1 {
			t.Fatalf("caches above the high-water mark give %d slots, want 1", g.Slots())
		}
		if g := NewMemoryGuard(MemoryGuardConfig{Limit: 1 << 20, RequestBytes: 4 << 20}); g.Slots() != 1 {
			t.Fatalf("a limit below one request gives %d slots, want 1", g.Slots())
		}
		for _, l := range []int64{0, math.MaxInt64} {
			g := NewMemoryGuard(MemoryGuardConfig{Limit: l, RequestBytes: 1 << 20})
			if g != nil || g.Slots() != 0 {
				t.Fatalf("limit %d: a guard without a known limit", l)
			}
			_, release, reason := g.Admit()
			if reason != "" {
				t.Fatalf("limit %d: a nil guard refused with %q", l, reason)
			}
			release()
			g.Run(t.Context(), MemorySampleInterval) // returns at once
		}
	})

	t.Run("R-ARCH-005 InUse counts the slots admitted requests hold, and a full guard sheds", func(t *testing.T) {
		g := NewMemoryGuard(MemoryGuardConfig{Limit: 5 << 20, RequestBytes: 2 << 20, Read: func() (uint64, uint64) { return 0, 0 }})
		_, r1, _ := g.Admit()
		_, r2, _ := g.Admit()
		if g.Slots() != 2 || g.InUse() != 2 {
			t.Fatalf("slots %d, in use %d, want 2 and 2", g.Slots(), g.InUse())
		}
		if _, _, reason := g.Admit(); reason != ShedMemorySlots {
			t.Fatalf("a full guard answered %q, want %q", reason, ShedMemorySlots)
		}
		r1()
		if g.InUse() != 1 {
			t.Fatalf("in use %d after one release, want 1", g.InUse())
		}
		r2()
		if (*MemoryGuard)(nil).InUse() != 0 {
			t.Fatal("a nil guard reports slots in use")
		}
	})

	t.Run("R-ARCH-005 the heap gate closes at the high-water mark, stays closed above the low-water mark and opens below it", func(t *testing.T) {
		var heap atomic.Uint64
		var changes []bool
		cycles := uint64(0)
		g := NewMemoryGuard(MemoryGuardConfig{
			Limit: 1000, RequestBytes: 1, Read: func() (uint64, uint64) { cycles++; return heap.Load(), cycles },
			OnChange: func(_ string, s bool, _ uint64) { changes = append(changes, s) },
		})
		steps := []struct {
			heap   uint64
			reason string
		}{
			{100, ""},
			{799, ""},
			{800, ShedHeap}, // 80 % of the limit
			{750, ShedHeap},
			{700, ShedHeap},
			{699, ""}, // below 70 %
			{790, ""},
		}
		for _, s := range steps {
			heap.Store(s.heap)
			g.Sample()
			_, release, reason := g.Admit()
			if reason != s.reason {
				t.Fatalf("heap %d: reason %q, want %q", s.heap, reason, s.reason)
			}
			if release != nil {
				release()
			}
		}
		if len(changes) != 2 || !changes[0] || changes[1] {
			t.Fatalf("OnChange calls %v, want [true false]", changes)
		}
	})

	t.Run("R-ARCH-005 the memory slots bound the requests of every endpoint together: beyond them a request gets 503 server_busy with Retry-After 1 and the reason is counted", func(t *testing.T) {
		g := NewMemoryGuard(MemoryGuardConfig{Limit: 10 << 20, RequestBytes: 4 << 20, Read: func() (uint64, uint64) { return 0, 0 }})
		if g.Slots() != 2 {
			t.Fatalf("slots = %d", g.Slots())
		}
		started, release := make(chan struct{}, 2), make(chan struct{})
		slow := func(w http.ResponseWriter, r *http.Request) {
			started <- struct{}{}
			<-release
		}
		var mu sync.Mutex
		shed := map[string]int{}
		chain := func() http.Handler {
			return Chain{Router: newTestRouter("GET /slow", slow), MaxInFlight: 10, Memory: g, OnShed: func(r string) {
				mu.Lock()
				shed[r]++
				mu.Unlock()
			}}.Handler()
		}
		a, b := chain(), chain() // two endpoints of one replica
		var wg sync.WaitGroup
		for _, h := range []http.Handler{a, b} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/slow", nil))
			}()
		}
		<-started
		<-started
		w := httptest.NewRecorder()
		a.ServeHTTP(w, httptest.NewRequest("GET", "/slow", nil))
		if w.Code != 503 || w.Header().Get("Retry-After") != "1" || problemOf(t, w).Code != "server_busy" {
			t.Fatalf("status %d, Retry-After %q, body %s", w.Code, w.Header().Get("Retry-After"), w.Body)
		}
		if shed[ShedMemorySlots] != 1 || len(shed) != 1 {
			t.Fatalf("shed = %v", shed)
		}
		close(release)
		wg.Wait()
		w = httptest.NewRecorder()
		router := newTestRouter("GET /fast", func(http.ResponseWriter, *http.Request) {})
		Chain{Router: router, Memory: g}.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/fast", nil))
		if w.Code != 200 {
			t.Fatalf("slots not released: %d", w.Code)
		}
	})

	t.Run("R-ARCH-005 the endpoint's max_in_flight is asked first and counted as in_flight", func(t *testing.T) {
		g := NewMemoryGuard(MemoryGuardConfig{Limit: 100 << 20, RequestBytes: 1 << 20, Read: func() (uint64, uint64) { return 0, 0 }})
		started, release := make(chan struct{}), make(chan struct{})
		var reasons []string
		h := Chain{Router: newTestRouter("GET /slow", func(http.ResponseWriter, *http.Request) {
			close(started)
			<-release
		}), MaxInFlight: 1, Memory: g, OnShed: func(r string) { reasons = append(reasons, r) }}.Handler()
		done := make(chan struct{})
		go func() {
			defer close(done)
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/slow", nil))
		}()
		<-started
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/slow", nil))
		close(release)
		<-done
		if w.Code != 503 || len(reasons) != 1 || reasons[0] != ShedInFlight {
			t.Fatalf("status %d, reasons %v", w.Code, reasons)
		}
		if g.used.Load() != 0 {
			t.Fatalf("%d bytes still charged", g.used.Load())
		}
	})

	t.Run("R-ARCH-005 while the heap gate is closed a request is refused with the reason heap", func(t *testing.T) {
		g := NewMemoryGuard(MemoryGuardConfig{Limit: 1000, RequestBytes: 1, Read: func() (uint64, uint64) { return 900, 1 }})
		g.Sample()
		var reasons []string
		w := httptest.NewRecorder()
		Chain{
			Router: newTestRouter("GET /x", func(http.ResponseWriter, *http.Request) { t.Error("the handler ran") }),
			Memory: g, OnShed: func(r string) { reasons = append(reasons, r) },
		}.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
		if w.Code != 503 || w.Header().Get("Retry-After") != "1" || len(reasons) != 1 || reasons[0] != ShedHeap {
			t.Fatalf("status %d, reasons %v", w.Code, reasons)
		}
	})

	t.Run("R-ARCH-005 a gate closed without a GC cycle for a second runs one, so it opens once the load is gone", func(t *testing.T) {
		heap, cycles, collected := uint64(900), uint64(7), 0
		g := NewMemoryGuard(MemoryGuardConfig{Limit: 1000, RequestBytes: 1, Read: func() (uint64, uint64) { return heap, cycles }})
		clock := time.Unix(0, 0)
		g.now = func() time.Time { return clock }
		g.collect = func() { collected++; cycles++; heap = 100 }
		g.Sample()
		if _, _, r := g.Admit(); r != ShedHeap {
			t.Fatalf("reason %q, want heap", r)
		}
		clock = clock.Add(memoryRecheck / 2)
		g.Sample()
		if collected != 0 {
			t.Fatal("a GC ran before the recheck interval")
		}
		clock = clock.Add(memoryRecheck / 2)
		g.Sample()
		if collected != 1 {
			t.Fatalf("%d GC runs, want 1", collected)
		}
		_, release, r := g.Admit()
		if r != "" {
			t.Fatalf("the gate stayed closed after the GC showed the heap free: %q", r)
		}
		release()
		clock = clock.Add(10 * memoryRecheck)
		g.Sample()
		if collected != 1 {
			t.Fatal("an open gate ran a GC")
		}
	})

	t.Run("Q-ARCH-500 the kernel gate closes at 80 % of the kernel's share, stays closed above 70 % and opens below it, whatever the heap and the charges say", func(t *testing.T) {
		// The forced reproduction of Q-ARCH-500: 860 downloads whose clients stopped reading held 383 MiB of send queues
		// (sock) beside 619 MiB of Go memory, the container was OOM-killed at 1 GiB, and the heap gate and the slots
		// never refused: the live heap stayed below its mark and the charges below the slots.
		var kern atomic.Int64
		type change struct {
			gate   string
			closed bool
			bytes  uint64
		}
		var changes []change
		var readErr error
		g := NewMemoryGuard(MemoryGuardConfig{
			Limit: 1000, RequestBytes: 1, Read: func() (uint64, uint64) { return 100, 1 },
			Kernel:       func() (int64, error) { return kern.Load(), readErr },
			KernelBudget: 200,
			OnChange:     func(gate string, closed bool, n uint64) { changes = append(changes, change{gate, closed, n}) },
		})
		steps := []struct {
			kernel int64
			err    error
			reason string
		}{
			{0, nil, ""},
			{159, nil, ""},
			{160, nil, ShedKernel}, // 80 % of the 200 bytes the Go limit leaves
			{150, nil, ShedKernel},
			{999, errors.New("memory.stat vanished"), ShedKernel}, // a failed reading keeps the state
			{140, nil, ShedKernel},
			{139, nil, ""}, // below 70 %
			{0, errors.New("memory.stat vanished"), ""},
		}
		for _, s := range steps {
			kern.Store(s.kernel)
			readErr = s.err
			g.Sample()
			_, release, reason := g.Admit()
			if reason != s.reason {
				t.Fatalf("kernel %d (err %v): reason %q, want %q", s.kernel, s.err, reason, s.reason)
			}
			if release != nil {
				release()
			}
		}
		want := []change{{ShedKernel, true, 160}, {ShedKernel, false, 139}}
		if len(changes) != len(want) || changes[0] != want[0] || changes[1] != want[1] {
			t.Fatalf("OnChange calls %v, want %v", changes, want)
		}
	})

	t.Run("Q-ARCH-500 every admitted request is charged its send buffer against the kernel's share, beside what the kernel holds beyond the charges, so a burst between two samples cannot overfill it", func(t *testing.T) {
		// Without the charge the gate alone let 640 downloads in during the 300 ms after it opened, and their send
		// queues took the kernel's memory from 112 to 237 MiB against a mark of 123 MiB.
		var kern atomic.Int64
		g := NewMemoryGuard(MemoryGuardConfig{
			Limit: 1 << 40, RequestBytes: 1, Read: func() (uint64, uint64) { return 0, 1 },
			Kernel: func() (int64, error) { return kern.Load(), nil }, KernelBudget: 1000, KernelRequestBytes: 100,
		})
		g.Sample()
		if g.Slots() != 8 {
			t.Fatalf("slots %d, want 0.8 × 1000 / 100 = 8", g.Slots())
		}
		admit := func(n int) (releases []func(), reason string) {
			for range n {
				_, release, r := g.Admit()
				if r != "" {
					return releases, r
				}
				releases = append(releases, release)
			}
			return releases, ""
		}
		held, reason := admit(9)
		if len(held) != 8 || reason != ShedKernel {
			t.Fatalf("admitted %d before %q, want 8 before %q: the samples saw nothing, the charges alone bound the burst", len(held), reason, ShedKernel)
		}
		kern.Store(700) // the eight send queues filled: what the kernel holds is what the charges said
		g.Sample()
		if _, _, r := g.Admit(); r != ShedKernel {
			t.Fatalf("reason %q: the charges are not counted twice, but nothing more fits", r)
		}
		for _, r := range held {
			r()
		}
		kern.Store(300) // finished responses whose bytes the clients have not read yet: no charge holds them
		g.Sample()
		if held, reason = admit(6); len(held) != 5 || reason != ShedKernel || g.Slots() != 5 {
			t.Fatalf("admitted %d before %q with %d slots, want 5: (800 - 300 held beyond the charges) / 100", len(held), reason, g.Slots())
		}
		for _, r := range held {
			r()
		}
	})

	t.Run("Q-ARCH-500 without a reader or a share for the kernel there is no kernel gate", func(t *testing.T) {
		for _, c := range []MemoryGuardConfig{
			{Limit: 1000, RequestBytes: 1, Read: func() (uint64, uint64) { return 0, 1 }},
			{Limit: 1000, RequestBytes: 1, Read: func() (uint64, uint64) { return 0, 1 }, Kernel: func() (int64, error) { return 1 << 40, nil }},
		} {
			g := NewMemoryGuard(c)
			g.Sample()
			_, release, reason := g.Admit()
			if reason != "" {
				t.Fatalf("refused with %q", reason)
			}
			release()
		}
	})

	t.Run("R-ARCH-005 the heap read from runtime/metrics is the live heap plus the stacks, with the GC cycles", func(t *testing.T) {
		runtime.GC()
		if heap, cycles := readHeap(); heap == 0 || cycles == 0 {
			t.Fatalf("runtime/metrics gave heap %d after %d cycles", heap, cycles)
		}
	})
}
