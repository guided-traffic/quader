package engine

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/shared"
)

// The large artifacts of R-BAS-054 and the large documents of Q-BAS-173: a blob of 256 KiB that the double of the main
// installation sends in 16 KiB steps, each released by the test.

// steppedBody yields its bytes one step at a time, each step when the test sends on next.
type steppedBody struct {
	data   []byte
	step   int
	off    int
	next   chan struct{}
	closed chan struct{}
	once   sync.Once
}

func newSteppedBody(b []byte, step int) *steppedBody {
	return &steppedBody{data: b, step: step, next: make(chan struct{}, 64), closed: make(chan struct{})}
}

func (s *steppedBody) Read(p []byte) (int, error) {
	if s.off >= len(s.data) {
		return 0, io.EOF
	}
	select {
	case <-s.next:
	case <-s.closed:
		return 0, io.ErrClosedPipe
	}
	n := copy(p[:min(len(p), s.step)], s.data[s.off:min(len(s.data), s.off+s.step)])
	s.off += n
	return n, nil
}

func (s *steppedBody) Close() error { s.once.Do(func() { close(s.closed) }); return nil }

// release lets n steps through.
func (s *steppedBody) release(n int) {
	for range n {
		s.next <- struct{}{}
	}
}

// liveWriter is a response writer whose body a test reads while the handler still writes it.
type liveWriter struct {
	mu     sync.Mutex
	header http.Header
	status int
	body   bytes.Buffer
}

func newLiveWriter() *liveWriter { return &liveWriter{header: http.Header{}} }

func (w *liveWriter) Header() http.Header { return w.header }
func (w *liveWriter) WriteHeader(s int) {
	w.mu.Lock()
	if w.status == 0 {
		w.status = s
	}
	w.mu.Unlock()
}

func (w *liveWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}
func (w *liveWriter) Flush() {}
func (w *liveWriter) got() (int, []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status, append([]byte(nil), w.body.Bytes()...)
}

// serveAsync runs a request through the engine in a goroutine; done reports whether the handler aborted the transfer.
func serveAsync(h *harness, path string, hdr ...string) (*liveWriter, <-chan bool) {
	w := newLiveWriter()
	r := httptest.NewRequest(http.MethodGet, "https://packages.example.com"+path, nil)
	r.RemoteAddr = "203.0.113.7:4711"
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	done := make(chan bool, 1)
	go func() {
		aborted := false
		defer func() {
			if v := recover(); v != nil {
				if err, ok := v.(error); !ok || !errors.Is(err, http.ErrAbortHandler) {
					panic(v)
				}
				aborted = true
			}
			done <- aborted
		}()
		h.e.ServeHTTP(w, r)
	}()
	return w, done
}

func waitDone(t *testing.T, done <-chan bool) bool {
	t.Helper()
	select {
	case a := <-done:
		return a
	case <-time.After(10 * time.Second):
		t.Fatal("the request did not end")
	}
	return false
}

// largeBound ends a wait of TestR_BAS_054_LargeArtifacts for a state the fill reaches; it only ends a hung test.
const largeBound = 30 * time.Second

// TestR_BAS_054_LargeArtifacts: a ranged miss streams from the spool with the range's last 64 KiB withheld until the commit;
// a second caller of the running fill, with a credential of its own that reads part of the repository, is served the whole
// blob from the spool after one conditional request, without a second transfer; a fill that fails its digest cuts both.
func TestR_BAS_054_LargeArtifacts(t *testing.T) {
	big := "/api/npm/web/npm-all/@acme/big/-/big-1.0.0.tgz"
	run := func(t *testing.T, corrupt bool) {
		s := newSiteHarness(t, options{})
		blob := payload(256 << 10)
		s.site.artifacts[big] = blob
		body := newSteppedBody(blob, 16<<10)
		if corrupt {
			bad := append([]byte(nil), blob...)
			bad[len(bad)-1] ^= 0xff
			body = newSteppedBody(bad, 16<<10)
		}
		viaSite := s.main.doFn
		s.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
			r, err := viaSite(f)
			if err == nil && r.Status == http.StatusOK && f.Kind == mainclient.KindForward {
				r.Body = body
			}
			return r, err
		}
		// The first caller asks for the first 128 KiB. Each wait below is for a state the fill reaches while the test holds
		// its body; the bound only ends a hung test.
		a, aDone := serveAsync(s.harness, big, "Authorization", bearer(s.full), "Range", "bytes=0-131071")
		body.release(1)
		eventuallyTrue(t, largeBound, "the ranged caller's first byte before the fill ends", func() bool { _, b := a.got(); return len(b) > 0 })
		// The second caller reads part of the repository: it gets its own permission first.
		b, bDone := serveAsync(s.harness, big, "Authorization", bearer(s.part))
		eventuallyTrue(t, largeBound, "the second caller's conditional request", func() bool { return s.main.count(mainclient.KindConditional) == 1 })
		body.release(11) // 192 KiB written; the fill waits for the next step, so nothing is committed before the test releases it
		eventuallyTrue(t, largeBound, "the second caller to follow the spool", func() bool { _, got := b.got(); return len(got) >= 128<<10 })
		// The ranged caller streams at its own pace: the second caller's progress says nothing about how far it has come, so
		// the test waits until it holds the 64 KiB the spool allows it, and then that it holds no more.
		eventuallyTrue(t, largeBound, "the ranged caller to stream the first 64 KiB of its range", func() bool { _, got := a.got(); return len(got) >= 64<<10 })
		if _, got := a.got(); len(got) != 64<<10 {
			t.Fatalf("the ranged caller holds %d bytes while the fill runs, want 64 KiB: its last 64 KiB wait for the commit", len(got))
		}
		body.release(4)
		aAborted, bAborted := waitDone(t, aDone), waitDone(t, bDone)
		if corrupt {
			if !aAborted || !bAborted {
				t.Fatalf("a fill that fails its digest: the ranged caller aborted %v, the follower aborted %v", aAborted, bAborted)
			}
			if s.cache.entryCount() != 0 {
				t.Fatal("a corrupt fill was kept")
			}
			return
		}
		if st, got := a.got(); aAborted || st != http.StatusPartialContent || !bytes.Equal(got, blob[:128<<10]) || a.header.Get("Content-Range") != "bytes 0-131071/262144" {
			t.Fatalf("the ranged caller: %d, %d bytes, %v", st, len(got), a.header)
		}
		if st, got := b.got(); bAborted || st != http.StatusOK || !bytes.Equal(got, blob) {
			t.Fatalf("the follower: %d, %d bytes", st, len(got))
		}
		if n, c := s.main.count(mainclient.KindForward), s.main.count(mainclient.KindConditional); n != 1 || c != 1 {
			t.Fatalf("%d transfers and %d conditional requests, want one each", n, c)
		}
		// The follower's serve from the spool is reported like a hit: the main installation counted the leader's only.
		if ev := s.dl.list(); len(ev) != 1 || ev[0].TokenID != "part" {
			t.Fatalf("download events %+v", ev)
		}
	}
	t.Run("streamed and followed", func(t *testing.T) { run(t, false) })
	t.Run("a fill that fails its digest cuts both transfers", func(t *testing.T) { run(t, true) })
}

// TestR_BAS_054_AReplicaWaitsForAnotherReplicasFill: with the bucket backend and shared state, a replica that meets
// another replica's fetch lock waits while it is held, sending nothing, and serves the committed entry afterwards; past
// fill_wait_max it fetches itself.
func TestR_BAS_054_AReplicaWaitsForAnotherReplicasFill(t *testing.T) {
	clk := &testClock{t: epoch}
	st := shared.NewLocal(1, clk.now)
	st.Shared = func() bool { return true }
	bucket := func(c *configT) { c.Bastion.Cache.Backend = "bucket" }
	a := newSiteHarness(t, options{state: st, clock: clk, cfg: []func(*configT){bucket}})
	b := newSiteHarness(t, options{state: st, clock: clk, cfg: []func(*configT){bucket}, cache: a.cache})
	b.full, b.part = a.full, a.part
	b.site.addReader(a.full, &reader{id: "full", read: iface.ReadFull})
	blob := payload(80 << 10)
	path := "/api/npm/web/npm-all/@acme/big/-/big-2.0.0.tgz"
	a.site.artifacts[path], b.site.artifacts[path] = blob, blob

	gate := newGatedBody(blob, 1)
	viaSite := a.main.doFn
	a.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
		r, err := viaSite(f)
		if err == nil && r.Status == http.StatusOK {
			r.Body = gate
		}
		return r, err
	}
	_, aDone := serveAsync(a.harness, path, "Authorization", bearer(a.full))
	eventuallyTrue(t, 5*time.Second, "replica A's fill", func() bool { return a.main.count(mainclient.KindForward) == 1 })
	bw, bDone := serveAsync(b.harness, path, "Authorization", bearer(a.full))
	time.Sleep(300 * time.Millisecond)
	if n := b.main.count(mainclient.KindForward); n != 0 {
		t.Fatalf("replica B sent %d requests while A's fill held the lock", n)
	}
	close(gate.gate)
	waitDone(t, aDone)
	if waitDone(t, bDone) {
		t.Fatal("replica B aborted")
	}
	if st, got := bw.got(); st != http.StatusOK || !bytes.Equal(got, blob) || b.main.count(mainclient.KindForward) != 0 {
		t.Fatalf("replica B: %d, %d bytes, %d transfers", st, len(got), b.main.count(mainclient.KindForward))
	}

	// Past fill_wait_max the waiting replica fetches itself.
	b.e.fillWaitMax = 200 * time.Millisecond
	other := strings.Replace(path, "2.0.0", "3.0.0", 2)
	a.site.artifacts[other], b.site.artifacts[other] = blob, blob
	gate2 := newGatedBody(blob, 1)
	a.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
		r, err := viaSite(f)
		if err == nil && r.Status == http.StatusOK {
			r.Body = gate2
		}
		return r, err
	}
	_, aDone = serveAsync(a.harness, other, "Authorization", bearer(a.full))
	eventuallyTrue(t, 5*time.Second, "replica A's second fill", func() bool { return a.main.count(mainclient.KindForward) == 2 })
	bw, bDone = serveAsync(b.harness, other, "Authorization", bearer(a.full))
	if waitDone(t, bDone) {
		t.Fatal("replica B aborted")
	}
	if st, got := bw.got(); st != http.StatusOK || !bytes.Equal(got, blob) || b.main.count(mainclient.KindForward) != 1 {
		t.Fatalf("replica B past fill_wait_max: %d, %d bytes, %d transfers", st, len(got), b.main.count(mainclient.KindForward))
	}
	close(gate2.gate)
	waitDone(t, aDone)
}

// TestR_BAS_021_AShareableDocumentAboveTheMemoryBoundIsSpooled: the memory bound of documents is a parameter (64 KiB here,
// so no test holds 64 MiB); a shared document of 100 KiB whose ETag is a content address is spooled, verified, kept in the
// documents' area and served from the cache, and one without such an ETag is passed through (Q-BAS-173).
func TestR_BAS_021_AShareableDocumentAboveTheMemoryBoundIsSpooled(t *testing.T) {
	s := newSiteHarness(t, options{})
	s.e.docMemory = 64 << 10
	doc := payload(100 << 10)
	addressed, plain := "/api/clamav/upstream/clamav/main.cvd", "/api/clamav/upstream/clamav/daily.cvd"
	s.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
		etag := `"` + sha(doc) + `"`
		if f.Request.URL.Path == plain {
			etag = `"d1"`
		}
		h := http.Header{"Content-Type": {"application/octet-stream"}, "Etag": {etag}}
		return resp(http.StatusOK, marking(iface.ClassShared, iface.ReadFull, "upstream/clamav", iface.Subject{Kind: iface.SubjectAnonymous}), h, string(doc)), nil
	}
	for _, p := range []string{addressed, plain} {
		if w := s.get(p); w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), doc) {
			t.Fatalf("%s cold: %d, %d bytes", p, w.Code, w.Body.Len())
		}
	}
	eventuallyTrue(t, 5*time.Second, "the spooled document's entry", func() bool { return s.cache.entryCount() == 1 })
	if len(s.cache.files("documents")) != 1 {
		t.Fatal("the spooled document is not in the documents' area")
	}
	before := s.main.count()
	if w := s.get(addressed); w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), doc) || s.main.count() != before {
		t.Fatalf("the spooled document warm: %d, %d requests", w.Code, s.main.count()-before)
	}
	if w := s.get(plain); w.Code != http.StatusOK || s.main.count() != before+1 {
		t.Fatalf("the document without a content address was kept: %d, %d requests", w.Code, s.main.count()-before)
	}
}
