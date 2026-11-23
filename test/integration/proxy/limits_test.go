//go:build integration

package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/proxy"
	"github.com/guided-traffic/quader/internal/proxy/proxytest"
	"github.com/guided-traffic/quader/internal/repository"
)

// TestSizeLimit is R-REPO-040: an item above the effective limit is refused, stored
// nowhere and negatively cached nowhere, so raising the limit takes effect at once.
func TestSizeLimit(t *testing.T) {
	e := newEnv(t)
	up := e.upstream()
	two := proxytest.Body("two", 2<<20)
	up.Set("/pkg/huge/-/huge-1.tgz", proxytest.Route{Body: two, Type: "application/octet-stream"})
	up.Set("/pkg/chunked/-/chunked-1.tgz", proxytest.Route{Type: "application/octet-stream", Handler: func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		for range 40 { // no Content-Length: the limit is found by counting
			_, _ = w.Write(two[:64<<10])
			w.(http.Flusher).Flush()
		}
	}})
	small := e.remote("limit", up, func(s *repository.RemoteSettings) {
		m := int64(1 << 20)
		s.Limits.MaxArtifactBytes = &m
	})
	r := e.newReplica("a", replicaOpts{})

	t.Run("R-REPO-040 a 2 MiB tarball over a 1 MiB limit answers 502 upstream_item_too_large and leaves no item", func(t *testing.T) {
		s := r.artifact(small, "pkg/huge/-/huge-1.tgz")
		if s.status != 502 || !strings.Contains(string(s.body), "upstream_item_too_large") {
			t.Fatalf("%d %s", s.status, s.body)
		}
		if n := e.number("SELECT count(*) FROM items WHERE kind = 'file'"); n != 0 {
			t.Fatalf("items = %d", n)
		}
	})
	t.Run("R-REPO-040 a body that is longer than announced, or announces no length, is cut at the limit", func(t *testing.T) {
		s := r.artifact(small, "pkg/chunked/-/chunked-1.tgz")
		if s.status != 502 || !strings.Contains(string(s.body), "upstream_item_too_large") {
			t.Fatalf("%d %s", s.status, s.body)
		}
		if n := e.number("SELECT count(*) FROM remote_cache_entries"); n != 0 {
			t.Fatalf("entries = %d", n)
		}
	})
	t.Run("R-REPO-040 nothing is negatively cached, so raising the limit succeeds at once", func(t *testing.T) {
		raised := small
		m := int64(4 << 20)
		raised.Settings.Limits.MaxArtifactBytes = &m
		s := r.artifact(raised, "pkg/huge/-/huge-1.tgz")
		if s.status != 200 || !bytes.Equal(s.body, two) {
			t.Fatalf("%d (%d bytes)", s.status, len(s.body))
		}
	})
	t.Run("R-REPO-040 the system limit applies where the repository sets none", func(t *testing.T) {
		e2 := newEnv(t)
		up2 := e2.upstream()
		up2.Set("/pkg/huge/-/huge-1.tgz", proxytest.Route{Body: two, Type: "application/octet-stream"})
		rem := e2.remote("limit-system", up2)
		r2 := e2.newReplica("a", replicaOpts{proxy: func(p *config.Proxy) { p.MaxArtifactBytes = 1 << 20 }})
		if s := r2.artifact(rem, "pkg/huge/-/huge-1.tgz"); s.status != 502 || !strings.Contains(string(s.body), "upstream_item_too_large") {
			t.Fatalf("%d %s", s.status, s.body)
		}
	})
}

// concurrency tracks how many requests the upstream serves at once. With a target set (expect), a request that enters waits until
// the target is in the upstream at once, so that a peak the slots allow is reached however slowly the machine brings the requests
// there; slotsBound ends the wait of a hung test.
type concurrency struct {
	cur, peak atomic.Int64
	mu        sync.Mutex
	target    int64
	reached   chan struct{}
	deadline  time.Time
}

// slotsBound is the upper bound of every wait of TestFetchSlots: it only ends a hung test. The replicas' queue timeout and the
// upstream's read timeout of the test are longer, so that neither ends a request the test still waits for.
const slotsBound = 60 * time.Second

// expect makes the requests that enter from now on wait until n of them are in the upstream at once; 0 lets them through.
func (c *concurrency) expect(n int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.target, c.reached, c.deadline = n, make(chan struct{}), time.Now().Add(slotsBound)
	if n <= 0 {
		close(c.reached)
	}
}

func (c *concurrency) enter() {
	n := c.cur.Add(1)
	for {
		p := c.peak.Load()
		if n <= p || c.peak.CompareAndSwap(p, n) {
			break
		}
	}
	c.mu.Lock()
	reached, deadline := c.reached, c.deadline
	if reached != nil && c.target > 0 && n >= c.target {
		select {
		case <-reached:
		default:
			close(reached)
		}
	}
	c.mu.Unlock()
	if reached == nil {
		return
	}
	select {
	case <-reached:
	case <-time.After(time.Until(deadline)):
	}
}

func (c *concurrency) leave() { c.cur.Add(-1) }

// gate holds the upstream's answers of a route until the test opens it, or the request ends; held counts the requests it holds.
type gate struct {
	open chan struct{}
	once sync.Once
	held atomic.Int64
}

func newGate(t *testing.T) *gate {
	g := &gate{open: make(chan struct{})}
	t.Cleanup(g.release)
	return g
}

func (g *gate) release() { g.once.Do(func() { close(g.open) }) }

// route answers "x" once the gate is open.
func (g *gate) route() proxytest.Route {
	return proxytest.Route{Handler: func(w http.ResponseWriter, r *http.Request) {
		g.held.Add(1)
		defer g.held.Add(-1)
		select {
		case <-g.open:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("x"))
	}}
}

// patientRead is the upstream's read timeout of the remotes of TestFetchSlots, longer than slotsBound.
var patientRead = 2 * int(slotsBound/time.Millisecond)

// patient is the replicas' queue timeout of TestFetchSlots, longer than slotsBound.
func patient(p *config.Proxy) { p.QueueTimeoutSeconds = 2 * int(slotsBound/time.Second) }

// TestFetchSlots is R-REPO-040: the budget of limits.maxConcurrentFetches is shared out
// per replica as ceil(budget / n), and a waiter for another replica's flight takes no
// slot. No wait of it is a guess about the machine's speed: a peak the slots allow is
// waited for (concurrency.expect), a slow upstream is a gate the test opens, and the
// replicas' queue timeout and the upstream's read timeout are longer than slotsBound.
func TestFetchSlots(t *testing.T) {
	setup := func(t *testing.T, budget int) (*env, *proxytest.Upstream, proxy.Remote, *concurrency) {
		e := newEnv(t)
		up := e.upstream()
		c := &concurrency{}
		for i := range 20 {
			up.Set("/pkg/s/-/s-"+strconv.Itoa(i)+".tgz", proxytest.Route{Handler: func(w http.ResponseWriter, r *http.Request) {
				c.enter()
				defer c.leave()
				// The hold in which a connection above the share would be seen beside the others.
				time.Sleep(400 * time.Millisecond)
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write([]byte("x"))
			}})
		}
		rem := e.remote("slots", up, func(s *repository.RemoteSettings) {
			s.Limits.MaxConcurrentFetches, s.ReadTimeoutMs = budget, patientRead
		})
		return e, up, rem, c
	}
	run := func(reps []*replica, rem proxy.Remote, n int) {
		var wg sync.WaitGroup
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if s := reps[i%len(reps)].artifact(rem, "pkg/s/-/s-"+strconv.Itoa(i)+".tgz"); s.status != 200 {
					t.Errorf("request %d: %d %s", i, s.status, s.body)
				}
			}()
		}
		wg.Wait()
	}

	t.Run("R-REPO-040 a budget of 4 on two replicas keeps at most 4 connections open and 2 per replica", func(t *testing.T) {
		e, _, rem, c := setup(t, 4)
		two := func() int { return 2 }
		a, b := e.newReplica("a", replicaOpts{replicas: two, proxy: patient}), e.newReplica("b", replicaOpts{replicas: two, proxy: patient})
		c.expect(0)
		run([]*replica{a}, rem, 10) // one replica alone: its share is 2
		if p := c.peak.Load(); p > 2 {
			t.Fatalf("one replica held %d connections open, its share is ceil(4 / 2) = 2", p)
		}
		c.peak.Store(0)
		c.expect(4) // the first requests wait at the upstream until both replicas' shares of 2 are in it at once
		run([]*replica{a, b}, rem, 20)
		if p := c.peak.Load(); p != 4 {
			t.Fatalf("two replicas held %d connections open, want the budget of 4", p)
		}
	})
	t.Run("R-REPO-040 after a third replica registers the share is ceil(4 / 3) = 2 and the total at most 6", func(t *testing.T) {
		e, _, rem, c := setup(t, 4)
		n := atomic.Int64{}
		n.Store(2)
		count := func() int { return int(n.Load()) }
		a, b := e.newReplica("a", replicaOpts{replicas: count, proxy: patient}), e.newReplica("b", replicaOpts{replicas: count, proxy: patient})
		d := e.newReplica("c", replicaOpts{replicas: count, proxy: patient})
		n.Store(3)
		c.expect(0)
		run([]*replica{a, b, d}, rem, 20)
		if p := c.peak.Load(); p > 6 {
			t.Fatalf("three replicas held %d connections open, at most 3 * 2 = 6", p)
		}
		c.peak.Store(0)
		run([]*replica{a}, rem, 10)
		if p := c.peak.Load(); p > 2 {
			t.Fatalf("one of three replicas held %d connections open, its share is 2", p)
		}
	})
	t.Run("R-REPO-040 a replica that waits for another replica's flights fetches other items at once", func(t *testing.T) {
		e := newEnv(t)
		up := e.upstream()
		// a's two flights are held at the upstream until b's own fetches have reached it; each of b's own fetches records how
		// many of a's flights were still held when it arrived.
		slow := newGate(t)
		var mu sync.Mutex
		var heldAtOwn []int64
		for i := range 4 {
			path := "/pkg/w/-/w-" + strconv.Itoa(i) + ".tgz"
			if i < 2 {
				up.Set(path, slow.route())
				continue
			}
			up.Set(path, proxytest.Route{Handler: func(w http.ResponseWriter, _ *http.Request) {
				mu.Lock()
				heldAtOwn = append(heldAtOwn, slow.held.Load())
				mu.Unlock()
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write([]byte("x"))
			}})
		}
		rem := e.remote("slots-wait", up, func(s *repository.RemoteSettings) { s.Limits.MaxConcurrentFetches, s.ReadTimeoutMs = 4, patientRead })
		two := func() int { return 2 }
		a, b := e.newReplica("a", replicaOpts{replicas: two, proxy: patient}), e.newReplica("b", replicaOpts{replicas: two, proxy: patient})
		for i := range 2 {
			go a.artifact(rem, "pkg/w/-/w-"+strconv.Itoa(i)+".tgz")
		}
		waitFor(t, "a to lead two slow fetches", slotsBound, func() bool { return slow.held.Load() == 2 })
		for i := range 4 { // b waits on a's two flights and fetches two others
			go b.artifact(rem, "pkg/w/-/w-"+strconv.Itoa(i)+".tgz")
		}
		waitFor(t, "b to fetch the two other items", slotsBound, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return len(heldAtOwn) == 2
		})
		slow.release()
		if heldAtOwn[0] != 2 || heldAtOwn[1] != 2 {
			t.Fatalf("b's own fetches reached the upstream while %v of a's two flights were held, want 2 each: the waiters took its slots", heldAtOwn)
		}
	})
	t.Run("R-REPO-040 a fetch that finds no free slot answers 503 with Retry-After 5 after the queue timeout", func(t *testing.T) {
		e := newEnv(t)
		up := e.upstream()
		slow := newGate(t) // the one slot stays taken until the subtest ends
		up.Set("/pkg/q/-/q-0.tgz", slow.route())
		up.Set("/pkg/q/-/q-1.tgz", proxytest.Route{Body: []byte("x"), Type: "application/octet-stream"})
		rem := e.remote("slots-queue", up, func(s *repository.RemoteSettings) { s.Limits.MaxConcurrentFetches, s.ReadTimeoutMs = 1, patientRead })
		a := e.newReplica("a", replicaOpts{proxy: func(p *config.Proxy) { p.QueueTimeoutSeconds = 1 }})
		go a.artifact(rem, "pkg/q/-/q-0.tgz")
		waitFor(t, "the slot to be taken", slotsBound, func() bool { return slow.held.Load() == 1 })
		s := a.artifact(rem, "pkg/q/-/q-1.tgz")
		if s.status != 503 || s.header.Get("Retry-After") != "5" || !strings.Contains(string(s.body), "fetch_queue_timeout") {
			t.Fatalf("%d %v %s", s.status, s.header, s.body)
		}
		if a.metric("quader_remote_fetch_queue_wait_seconds", "repo", "slots-queue") < 1 {
			t.Fatal("the queue wait was not observed")
		}
	})
}

// TestIntegrity is R-REPO-041: the announced digests, the withheld tail and the
// immutability of a cached artifact.
func TestIntegrity(t *testing.T) {
	body := proxytest.Body("pkg", 400_000)
	wrong := proxytest.SHA512([]byte("other bytes"))
	right := proxytest.SHA512(body)

	t.Run("R-REPO-041 a mismatch stores nothing, counts, writes the security event and answers 502", func(t *testing.T) {
		e := newEnv(t)
		up := e.upstream()
		up.Set("/pkg/m/-/m-1.tgz", proxytest.Route{Body: body, Type: "application/octet-stream"})
		rem := e.remote("integrity", up)
		r := e.newReplica("a", replicaOpts{})
		s := r.artifact(rem, "pkg/m/-/m-1.tgz", func(rq *proxy.Request) { rq.Expected = proxy.Digests{SHA512: wrong} })
		if s.status != 502 && !s.aborted {
			t.Fatalf("%d", s.status)
		}
		if s.status == 502 && !strings.Contains(string(s.body), "upstream_integrity_mismatch") {
			t.Fatalf("%s", s.body)
		}
		if n := e.number("SELECT count(*) FROM items WHERE kind = 'file'") + e.number("SELECT count(*) FROM remote_cache_entries"); n != 0 {
			t.Fatalf("a refused artifact left %d rows", n)
		}
		if got := r.metric("quader_remote_integrity_failures_total", "repo", "integrity", "reason", "mismatch"); got != 1 {
			t.Fatalf("quader_remote_integrity_failures_total{mismatch} = %v", got)
		}
		if n := len(r.rec.ofType(audit.EventProxyIntegrityMismatch)); n != 1 {
			t.Fatalf("proxy.integrity_mismatch events = %d", n)
		}
		// Nothing is negatively cached, so the correct digest succeeds at once.
		if s := r.artifact(rem, "pkg/m/-/m-1.tgz", func(rq *proxy.Request) { rq.Expected = proxy.Digests{SHA512: right} }); s.status != 200 {
			t.Fatalf("%d", s.status)
		}
	})

	t.Run("R-REPO-041 the withheld tail: a client never receives a complete response with unverified content", func(t *testing.T) {
		e := newEnv(t)
		up := e.upstream()
		up.Set("/pkg/w/-/w-1.tgz", proxytest.Route{Body: body, Type: "application/octet-stream", Chunk: 50_000, ChunkDelay: 20 * time.Millisecond})
		rem := e.remote("integrity-tail", up)
		r := e.newReplica("a", replicaOpts{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			rq := r.request(rem, "pkg/w/-/w-1.tgz", proxy.Artifact, func(rq *proxy.Request) { rq.Expected = proxy.Digests{SHA512: wrong} })
			rq.Serve = r.serveFunc()
			if err := r.engine.Artifact(req.Context(), rq, w, req); err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
			}
		}))
		t.Cleanup(srv.Close)
		resp, err := http.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		got, rerr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if rerr == nil {
			t.Fatalf("the client completed a download of %d bytes whose digest does not match", len(got))
		}
		if len(got) >= len(body) {
			t.Fatalf("the client received the whole body (%d bytes)", len(got))
		}
	})

	t.Run("R-REPO-041 a verified transfer streams to the leader's client, with the tail released after the commit", func(t *testing.T) {
		e := newEnv(t)
		up := e.upstream()
		up.Set("/pkg/ok/-/ok-1.tgz", proxytest.Route{Body: body, Type: "application/octet-stream", Chunk: 50_000, ChunkDelay: 20 * time.Millisecond})
		rem := e.remote("integrity-ok", up)
		r := e.newReplica("a", replicaOpts{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			rq := r.request(rem, "pkg/ok/-/ok-1.tgz", proxy.Artifact, func(rq *proxy.Request) { rq.Expected = proxy.Digests{SHA512: right} })
			rq.Serve = r.serveFunc()
			if err := r.engine.Artifact(req.Context(), rq, w, req); err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
			}
		}))
		t.Cleanup(srv.Close)
		resp, err := http.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		first := make([]byte, 1000)
		started := time.Now()
		if _, err := io.ReadFull(resp.Body, first); err != nil {
			t.Fatal(err)
		}
		if took := time.Since(started); took > 150*time.Millisecond {
			t.Logf("the first bytes arrived after %s", took)
		}
		rest, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil || !bytes.Equal(append(first, rest...), body) {
			t.Fatalf("%v, %d bytes", err, len(rest)+len(first))
		}
		if resp.Header.Get("Cache-Status") != "quader; fwd=uri-miss; fwd-status=200; stored" {
			t.Fatalf("Cache-Status = %q", resp.Header.Get("Cache-Status"))
		}
		if n := e.number("SELECT count(*) FROM remote_cache_entries WHERE kind = 'artifact'"); n != 1 {
			t.Fatalf("entries = %d", n)
		}
	})

	t.Run("R-REPO-041 the policy require refuses an artifact whose metadata announces only SHA-1, without asking the upstream", func(t *testing.T) {
		e := newEnv(t)
		up := e.upstream()
		up.Set("/pkg/r/-/r-1.tgz", proxytest.Route{Body: body, Type: "application/octet-stream"})
		rem := e.remote("integrity-require", up, func(s *repository.RemoteSettings) { s.Integrity.ChecksumPolicy = repository.ChecksumRequire })
		r := e.newReplica("a", replicaOpts{})
		s := r.artifact(rem, "pkg/r/-/r-1.tgz", func(rq *proxy.Request) { rq.Expected = proxy.Digests{SHA1: []byte("12345678901234567890")} })
		if s.status != 502 || !strings.Contains(string(s.body), "upstream_digest_missing") {
			t.Fatalf("%d %s", s.status, s.body)
		}
		if up.Total() != 0 {
			t.Fatalf("the upstream was asked %d times", up.Total())
		}
		if s := r.artifact(rem, "pkg/r/-/r-1.tgz", func(rq *proxy.Request) { rq.Expected = proxy.Digests{SHA512: right} }); s.status != 200 {
			t.Fatalf("with a SHA-512 announced: %d", s.status)
		}
	})

	t.Run("R-REPO-041 the policy ignore does not compare, a format that always verifies still does", func(t *testing.T) {
		e := newEnv(t)
		up := e.upstream()
		up.Set("/pkg/i/-/i-1.tgz", proxytest.Route{Body: body, Type: "application/octet-stream"})
		up.Set("/pkg/j/-/j-1.tgz", proxytest.Route{Body: body, Type: "application/octet-stream"})
		rem := e.remote("integrity-ignore", up, func(s *repository.RemoteSettings) { s.Integrity.ChecksumPolicy = repository.ChecksumIgnore })
		r := e.newReplica("a", replicaOpts{})
		if s := r.artifact(rem, "pkg/i/-/i-1.tgz", func(rq *proxy.Request) { rq.Expected = proxy.Digests{SHA512: wrong} }); s.status != 200 {
			t.Fatalf("ignore compared: %d", s.status)
		}
		s := r.artifact(rem, "pkg/j/-/j-1.tgz", func(rq *proxy.Request) {
			rq.Expected, rq.AlwaysVerify = proxy.Digests{SHA512: wrong}, true
		})
		if s.status == 200 {
			t.Fatal("a format that always verifies was served a mismatching artifact")
		}
	})

	t.Run("R-REPO-041 refreshed metadata that announces another digest keeps the cached artifact and writes proxy.upstream_digest_changed", func(t *testing.T) {
		e := newEnv(t)
		up := e.upstream()
		up.Set("/pkg/c/-/c-1.tgz", proxytest.Route{Body: body, Type: "application/octet-stream"})
		rem := e.remote("integrity-changed", up)
		r := e.newReplica("a", replicaOpts{})
		if s := r.artifact(rem, "pkg/c/-/c-1.tgz", func(rq *proxy.Request) { rq.Expected = proxy.Digests{SHA512: right} }); s.status != 200 {
			t.Fatalf("%d", s.status)
		}
		before := up.Total()
		s := r.artifact(rem, "pkg/c/-/c-1.tgz", func(rq *proxy.Request) { rq.Expected = proxy.Digests{SHA512: wrong} })
		if s.status != 200 || !bytes.Equal(s.body, body) {
			t.Fatalf("the cached artifact was not served unchanged: %d", s.status)
		}
		if up.Total() != before {
			t.Fatal("the cached artifact was fetched again")
		}
		if n := len(r.rec.ofType(audit.EventProxyUpstreamDigestChanged)); n != 1 {
			t.Fatalf("proxy.upstream_digest_changed events = %d", n)
		}
		if got := r.metric("quader_remote_integrity_failures_total", "repo", "integrity-changed", "reason", "digest_changed"); got != 1 {
			t.Fatalf("digest_changed = %v", got)
		}
		_ = r.artifact(rem, "pkg/c/-/c-1.tgz", func(rq *proxy.Request) { rq.Expected = proxy.Digests{SHA512: wrong} })
		if n := len(r.rec.ofType(audit.EventProxyUpstreamDigestChanged)); n != 1 {
			t.Fatalf("a second request wrote another event: %d", n)
		}
	})
}

// TestContentTypes is R-REPO-042: an HTML page in place of a package is refused, and
// the refusal counts toward the circuit.
func TestContentTypes(t *testing.T) {
	t.Run("R-REPO-042 a tarball answered as text/html is refused with 502 upstream_content_type_blocked and not stored", func(t *testing.T) {
		e := newEnv(t)
		up := e.upstream()
		up.Set("/pkg/h/-/h-1.tgz", proxytest.Route{Body: []byte("<html>login</html>"), Type: "text/html; charset=utf-8"})
		rem := e.remote("ctype", up)
		r := e.newReplica("a", replicaOpts{})
		s := r.artifact(rem, "pkg/h/-/h-1.tgz")
		if s.status != 502 || !strings.Contains(string(s.body), "upstream_content_type_blocked") {
			t.Fatalf("%d %s", s.status, s.body)
		}
		if n := e.number("SELECT count(*) FROM items WHERE kind = 'file'"); n != 0 {
			t.Fatalf("items = %d", n)
		}
		if n := e.number("SELECT consecutive_failures FROM remote_upstream_state"); n != 1 {
			t.Fatalf("the refusal counted %d toward the circuit, want 1", n)
		}
	})
	t.Run("R-REPO-042 allowedContentTypes of the repository and a type the format expects are exempt", func(t *testing.T) {
		e := newEnv(t)
		up := e.upstream()
		up.Set("/pkg/a/-/a-1.tgz", proxytest.Route{Body: []byte("<html>ok</html>"), Type: "text/html"})
		up.Set("/pkg/f/-/f-1.tgz", proxytest.Route{Body: []byte("<html>ok</html>"), Type: "text/html"})
		rem := e.remote("ctype-allowed", up, func(s *repository.RemoteSettings) { s.Integrity.AllowedContentTypes = []string{"text/html"} })
		r := e.newReplica("a", replicaOpts{})
		if s := r.artifact(rem, "pkg/a/-/a-1.tgz"); s.status != 200 {
			t.Fatalf("allowed: %d %s", s.status, s.body)
		}
		strict := e.remote("ctype-strict", up)
		s := r.artifact(strict, "pkg/f/-/f-1.tgz", func(rq *proxy.Request) {
			rq.Adapter = &proxytest.Adapter{Expected: map[string]bool{"pkg/f/-/f-1.tgz text/html": true}}
		})
		if s.status != 200 {
			t.Fatalf("expected by the format: %d %s", s.status, s.body)
		}
	})
	t.Run("R-REPO-042 blockUnexpectedContentTypes false turns the check off", func(t *testing.T) {
		e := newEnv(t)
		up := e.upstream()
		up.Set("/pkg/o/-/o-1.tgz", proxytest.Route{Body: []byte("<html>"), Type: "text/html"})
		rem := e.remote("ctype-off", up, func(s *repository.RemoteSettings) { s.Integrity.BlockUnexpectedContentTypes = false })
		r := e.newReplica("a", replicaOpts{})
		if s := r.artifact(rem, "pkg/o/-/o-1.tgz"); s.status != 200 {
			t.Fatalf("%d", s.status)
		}
	})
}
