package scanner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/vuln/imagescan"
)

const testToken = "qdr_rgt_eyJzaWQiOiJzaWQtb2YtdGhlLXRlc3QifQ.AAAA" // a registry token's shape; the stub registry checks it

func digestOf(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

func tarOf(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// image is an image of bytes: its manifest and its blobs by digest.
type image struct {
	manifest []byte
	blobs    map[string][]byte
}

func imageOf(t *testing.T, layers ...[]byte) image {
	t.Helper()
	cfg := []byte(`{"architecture":"amd64","os":"linux"}`)
	img := image{blobs: map[string][]byte{digestOf(cfg): cfg}}
	var ls []string
	for _, l := range layers {
		img.blobs[digestOf(l)] = l
		ls = append(ls, fmt.Sprintf(`{"mediaType":"application/vnd.oci.image.layer.v1.tar+gzip","digest":%q,"size":%d}`, digestOf(l), len(l)))
	}
	img.manifest = []byte(fmt.Sprintf(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":%q,"size":%d},"layers":[%s]}`,
		digestOf(cfg), len(cfg), strings.Join(ls, ",")))
	return img
}

// registry is a stand-in of the scan listener: it serves the images of web/docker-local by digest for the token of the test, and
// answers as the test tells it to.
type registry struct {
	mu       sync.Mutex
	images   map[string]image // by manifest digest
	answer   func(w http.ResponseWriter, r *http.Request) bool
	requests atomic.Int64
}

func (g *registry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.requests.Add(1)
	if g.answer != nil && g.answer(w, r) {
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+testToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/v2/web/docker-local/app/")
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	kind, digest, _ := strings.Cut(rest, "/")
	g.mu.Lock()
	defer g.mu.Unlock()
	for md, img := range g.images {
		switch {
		case kind == "manifests" && digest == md:
			_, _ = w.Write(img.manifest)
			return
		case kind == "blobs":
			if b, ok := img.blobs[digest]; ok {
				_, _ = w.Write(b)
				return
			}
		}
	}
	w.WriteHeader(http.StatusNotFound)
}

func (g *registry) add(img image) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.images == nil {
		g.images = map[string]image{}
	}
	d := digestOf(img.manifest)
	g.images[d] = img
	return d
}

// fakeAnalyser writes a CycloneDX document for the layout it is given, or a report for the analysis it is given, or waits until its
// call ends.
type fakeAnalyser struct {
	block   chan struct{}
	started chan struct{}
	runs    atomic.Int64
	fail    error
	// detected records the arguments of the last detection: the analysis it read, its working directory, the database's directory and
	// the environment the scanner would give the process (the working directory's HOME and TMPDIR).
	mu       sync.Mutex
	detected struct{ analysis, scratch, database string }
}

func (a *fakeAnalyser) Version() string { return "0.76.2" }

func (a *fakeAnalyser) Analyse(ctx context.Context, layout, sbom, _ string, _ imagescan.Limits) error {
	a.runs.Add(1)
	if _, err := os.Stat(filepath.Join(layout, "index.json")); err != nil {
		return errors.New("no OCI layout")
	}
	if a.started != nil {
		a.started <- struct{}{}
	}
	if a.block != nil {
		select {
		case <-a.block:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
	if a.fail != nil {
		return a.fail
	}
	return os.WriteFile(sbom, []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","components":[{"type":"library","name":"xz"}]}`), 0o600)
}

func (a *fakeAnalyser) Detect(ctx context.Context, sbom, out, scratch, database string, _ imagescan.Limits) error {
	a.runs.Add(1)
	doc, err := os.ReadFile(sbom) //nolint:gosec // the test's own directory
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.detected.analysis, a.detected.scratch, a.detected.database = string(doc), scratch, database
	a.mu.Unlock()
	if a.started != nil {
		a.started <- struct{}{}
	}
	if a.block != nil {
		select {
		case <-a.block:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
	if a.fail != nil {
		return a.fail
	}
	return os.WriteFile(out, []byte(`{"SchemaVersion":2,"Results":[]}`), 0o600)
}

type space struct {
	mu     sync.Mutex
	budget int64
	held   int64
}

func (s *space) Hold(n int64) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held+n > s.budget {
		return nil, imagescan.ErrNoSpace
	}
	s.held += n
	var once sync.Once
	return func() { once.Do(func() { s.mu.Lock(); s.held -= n; s.mu.Unlock() }) }, nil
}
func (s *space) Adopt(n int64) func() { return func() {} }
func (s *space) Budget() int64        { return s.budget }
func (s *space) Held() int64          { s.mu.Lock(); defer s.mu.Unlock(); return s.held }

type rig struct {
	t   *testing.T
	reg *registry
	an  *fakeAnalyser
	sp  *space
	svc *Service
	srv *httptest.Server
	dir string
	// db is the pod's database directory, which the loader writes and the scanner reads.
	db string
}

func newRig(t *testing.T, slots int) *rig {
	t.Helper()
	r := &rig{t: t, reg: &registry{}, an: &fakeAnalyser{}, sp: &space{budget: 1 << 30}, dir: t.TempDir(), db: t.TempDir()}
	listener := httptest.NewServer(r.reg)
	t.Cleanup(listener.Close)
	svc, err := New(Options{
		Analyser: r.an, Registry: Registry{URL: listener.URL}, Space: r.sp, ScanDir: r.dir, DatabaseDir: r.db, Slots: slots, PullTimeout: 5 * time.Second,
		Content: imagescan.ContentLimits{MaxLayers: 8, MaxImageBytes: 1 << 20, MaxUncompressedBytes: 1 << 20, MaxEntries: 100},
		Run:     imagescan.Limits{Timeout: 10 * time.Second, OutputBytes: 1 << 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.svc = svc
	mux := http.NewServeMux()
	mux.Handle(imagescan.AnalysesPath, svc.Handler())
	mux.Handle(imagescan.DetectionsPath, svc.DetectHandler())
	r.srv = httptest.NewServer(mux)
	t.Cleanup(r.srv.Close)
	return r
}

func callBody(repo, name, digest, token string) string {
	b, _ := json.Marshal(imagescan.CallRequest{Repository: repo, Name: name, Digest: digest, Token: token})
	return string(b)
}

// post sends a call and returns the status, the code of a problem and the decoded analysis of a 200.
func (r *rig) post(ctx context.Context, body string) (int, imagescan.CallProblem, []byte) {
	r.t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.srv.URL+imagescan.AnalysesPath, strings.NewReader(body))
	if err != nil {
		r.t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, imagescan.CallProblem{}, nil
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var p imagescan.CallProblem
	_ = json.Unmarshal(raw, &p)
	return res.StatusCode, p, raw
}

func TestR_VULN_041_TheScannerAnswersTheAnalysisOfTheManifestOfACall(t *testing.T) {
	r := newRig(t, 1)
	d := r.reg.add(imageOf(t, tarOf(t, map[string]string{"etc/os-release": "ID=alpine"})))
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, r.srv.URL+imagescan.AnalysesPath, strings.NewReader(callBody("web/docker-local", "app", d, testToken)))
	req.Header.Set("Accept-Encoding", "gzip")
	res, err := (&http.Client{Transport: &http.Transport{DisableCompression: true}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Encoding") != "gzip" || res.Header.Get(imagescan.AnalyserVersionHeader) != "0.76.2" ||
		res.Header.Get("Content-Type") != imagescan.AnalysisMediaType {
		t.Fatalf("%d %v", res.StatusCode, res.Header)
	}
	zr, err := gzip.NewReader(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := io.ReadAll(zr)
	if !strings.Contains(string(doc), `"bomFormat":"CycloneDX"`) {
		t.Fatalf("%s", doc)
	}
	if r.sp.Held() != 0 || r.svc.Busy() != 0 {
		t.Fatalf("held %d, busy %d after the call", r.sp.Held(), r.svc.Busy())
	}
	if ents, _ := os.ReadDir(r.dir); len(ents) != 0 {
		t.Fatalf("%d working directories left", len(ents))
	}
}

func TestR_VULN_041_AMalformedCallIsRefusedWith400(t *testing.T) {
	r := newRig(t, 1)
	good := "sha256:" + strings.Repeat("ab", 32)
	for name, body := range map[string]string{
		"an unknown member":          `{"repository":"web/docker-local","name":"app","digest":"` + good + `","token":"` + testToken + `","registry":"https://evil.example.com"}`,
		"a digest of 63 hex digits":  callBody("web/docker-local", "app", good[:len(good)-1], testToken),
		"a tag as digest":            callBody("web/docker-local", "app", "latest", testToken),
		"a repository of no project": callBody("docker-local", "app", good, testToken),
		"a name outside the grammar": callBody("web/docker-local", "../app", good, testToken),
		"no scan token":              callBody("web/docker-local", "app", good, "qdr_pat_x"),
		"trailing data":              callBody("web/docker-local", "app", good, testToken) + "{}",
		"a body above 16 KiB":        `{"repository":"web/docker-local","name":"` + strings.Repeat("a", 17<<10) + `"}`,
	} {
		if code, p, _ := r.post(t.Context(), body); code != http.StatusBadRequest || p.Code != imagescan.CodeMalformedRequest {
			t.Errorf("%s: %d %+v", name, code, p)
		}
	}
	if r.reg.requests.Load() != 0 {
		t.Fatal("a malformed call reached the registry")
	}
}

func TestR_VULN_041_ACallWhileEverySlotHoldsAnAnalysisIsAnsweredBusy(t *testing.T) {
	r := newRig(t, 1)
	r.an.block, r.an.started = make(chan struct{}), make(chan struct{}, 1)
	d := r.reg.add(imageOf(t, tarOf(t, map[string]string{"a": "1"})))
	first := make(chan int, 1)
	go func() {
		code, _, _ := r.post(context.Background(), callBody("web/docker-local", "app", d, testToken))
		first <- code
	}()
	<-r.an.started
	code, p, _ := r.post(t.Context(), callBody("web/docker-local", "app", d, testToken))
	if code != http.StatusServiceUnavailable || p.Code != imagescan.CodeScannerBusy {
		t.Fatalf("%d %+v", code, p)
	}
	close(r.an.block)
	if code := <-first; code != http.StatusOK {
		t.Fatalf("the first call: %d", code)
	}
}

func TestR_VULN_041_ACallWhoseClientGoesAwayKillsTheAnalysersProcessGroupAndFreesItsSlot(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	script := filepath.Join(dir, "analyser.sh")
	// The analyser starts a child of its own in its process group and waits; the kill of the group must end both.
	body := "#!/bin/sh\n/bin/sleep 60 &\necho $! > " + pidFile + "\nwait\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, 1)
	r.svc.o.Analyser = &imagescan.Trivy{Binary: script, AnalyserVersion: "0.76.2"}
	d := r.reg.add(imageOf(t, tarOf(t, map[string]string{"a": "1"})))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); r.post(ctx, callBody("web/docker-local", "app", d, testToken)) }()
	var pid int
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(pidFile); err == nil && len(b) > 0 {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
			if pid > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("the analyser did not start")
	}
	cancel()
	<-done
	deadline = time.Now().Add(10 * time.Second)
	for r.svc.Busy() != 0 || r.sp.Held() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("busy %d, held %d", r.svc.Busy(), r.sp.Held())
		}
		time.Sleep(10 * time.Millisecond)
	}
	for running(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("the analyser's child %d outlived the call", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// running reports whether a process still runs. A process that ended but was not reaped yet is a zombie and has ended: its parent, the
// analyser, was killed with it, so it is reparented to the first process of the test's environment, which reaps it in a pod (the
// scanner's reaper, Q-ARCH-530) and on a host, but not when that is a `go test` in a container. Where /proc exists its state says so;
// elsewhere the signal 0 does, on systems whose first process reaps.
func running(pid int) bool {
	if _, err := os.Stat("/proc/self"); err != nil {
		return syscall.Kill(pid, 0) == nil
	}
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false // a system with /proc that has no entry for the pid: it is gone
	}
	// The state follows the command name in parentheses, which may itself hold spaces.
	if i := strings.LastIndexByte(string(raw), ')'); i > 0 && i+2 < len(raw) {
		return raw[i+2] != 'Z' && raw[i+2] != 'X'
	}
	return true
}

func TestR_VULN_036_TheChecksRunOnWhatThePullBrings(t *testing.T) {
	r := newRig(t, 1)
	cases := []struct {
		name string
		img  func() image
		code string
	}{
		{"a path that climbs out of the root", func() image { return imageOf(t, tarOf(t, map[string]string{"../../etc/shadow": "x"})) }, imagescan.CodePathTraversal},
		{"a layer that decompresses past the limit", func() image {
			return imageOf(t, tarOf(t, map[string]string{"bomb": strings.Repeat("0", 2<<20)}))
		}, imagescan.CodeDecompressedSize},
		{"a blob whose bytes are not its digest", func() image {
			img := imageOf(t, tarOf(t, map[string]string{"a": "1"}))
			for d, b := range img.blobs {
				if len(b) > 60 {
					img.blobs[d] = append([]byte{b[0] ^ 0xff}, b[1:]...)
				}
			}
			return img
		}, imagescan.CodeDigestMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := r.reg.add(tc.img())
			code, p, _ := r.post(t.Context(), callBody("web/docker-local", "app", d, testToken))
			if code != http.StatusUnprocessableEntity || p.Code != tc.code || !p.Terminal {
				t.Fatalf("%d %+v", code, p)
			}
			if r.sp.Held() != 0 {
				t.Fatalf("held %d after the refusal", r.sp.Held())
			}
		})
	}
	t.Run("a manifest whose bytes are not the call's digest", func(t *testing.T) {
		img := imageOf(t, tarOf(t, map[string]string{"m": "1"}))
		d := r.reg.add(img)
		r.reg.mu.Lock()
		r.reg.images[d] = image{manifest: append(append([]byte{}, img.manifest...), ' '), blobs: img.blobs}
		r.reg.mu.Unlock()
		code, p, _ := r.post(t.Context(), callBody("web/docker-local", "app", d, testToken))
		if code != http.StatusUnprocessableEntity || p.Code != imagescan.CodeDigestMismatch || !p.Terminal {
			t.Fatalf("%d %+v", code, p)
		}
	})
}

func TestR_VULN_042_TheAnswersOfTheScanListenerMapToTheirCodes(t *testing.T) {
	r := newRig(t, 1)
	img := imageOf(t, tarOf(t, map[string]string{"a": "1"}))
	d := r.reg.add(img)
	var layer string
	for dg, b := range img.blobs {
		if len(b) > 60 {
			layer = dg
		}
	}
	cases := []struct {
		name   string
		answer func(w http.ResponseWriter, r *http.Request) bool
		status int
		code   string
	}{
		{"an uncached layer of a remote is layer_missing", func(w http.ResponseWriter, r *http.Request) bool {
			if strings.HasSuffix(r.URL.Path, layer) {
				w.Header().Set(ErrorCodeHeader, "populate_not_permitted")
				w.WriteHeader(http.StatusForbidden)
				return true
			}
			return false
		}, http.StatusUnprocessableEntity, imagescan.CodeLayerMissing},
		{"a manifest that went is layer_missing", func(w http.ResponseWriter, _ *http.Request) bool {
			w.WriteHeader(http.StatusNotFound)
			return true
		}, http.StatusUnprocessableEntity, imagescan.CodeLayerMissing},
		{"a repository the listener holds for disabled is repository_disabled, not the image's", func(w http.ResponseWriter, _ *http.Request) bool {
			w.Header().Set(ErrorCodeHeader, "repository_disabled")
			w.WriteHeader(http.StatusNotFound)
			return true
		}, http.StatusConflict, imagescan.CodeRepositoryDisabled},
		{"a layer of a repository the listener holds for disabled is repository_disabled too", func(w http.ResponseWriter, r *http.Request) bool {
			if strings.HasSuffix(r.URL.Path, layer) {
				w.Header().Set(ErrorCodeHeader, "repository_disabled")
				w.WriteHeader(http.StatusNotFound)
				return true
			}
			return false
		}, http.StatusConflict, imagescan.CodeRepositoryDisabled},
		{"a refused token is pull_refused", func(w http.ResponseWriter, _ *http.Request) bool {
			w.WriteHeader(http.StatusUnauthorized)
			return true
		}, http.StatusForbidden, imagescan.CodePullRefused},
		{"another refusal is pull_refused", func(w http.ResponseWriter, _ *http.Request) bool {
			w.WriteHeader(http.StatusForbidden)
			return true
		}, http.StatusForbidden, imagescan.CodePullRefused},
		{"a failing scan listener is registry_unavailable", func(w http.ResponseWriter, _ *http.Request) bool {
			w.WriteHeader(http.StatusServiceUnavailable)
			return true
		}, http.StatusBadGateway, imagescan.CodeRegistryUnavailable},
		{"a redirect is not followed", func(w http.ResponseWriter, _ *http.Request) bool {
			w.Header().Set("Location", "https://elsewhere.example.com/")
			w.WriteHeader(http.StatusFound)
			return true
		}, http.StatusBadGateway, imagescan.CodeRegistryUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r.reg.answer = tc.answer
			code, p, _ := r.post(t.Context(), callBody("web/docker-local", "app", d, testToken))
			if code != tc.status || p.Code != tc.code || p.Terminal {
				t.Fatalf("%d %+v", code, p)
			}
		})
	}
	r.reg.answer = nil
}

func TestR_VULN_036_AnAnalysisHoldsTheRoomOfItsImageAndANewOneThatDoesNotFitIsPutOff(t *testing.T) {
	r := newRig(t, 2)
	d := r.reg.add(imageOf(t, tarOf(t, map[string]string{"a": "1"})))
	t.Run("one that cannot fit an empty spool is spool_too_small", func(t *testing.T) {
		r.sp.budget = analysisMargin
		code, p, _ := r.post(t.Context(), callBody("web/docker-local", "app", d, testToken))
		if code != http.StatusUnprocessableEntity || p.Code != imagescan.CodeSpoolTooSmall || p.Terminal {
			t.Fatalf("%d %+v", code, p)
		}
	})
	t.Run("one that does not fit next to the analysis in flight is scanner_busy", func(t *testing.T) {
		r.sp.budget = analysisMargin + analysisMargin/2
		r.an.block, r.an.started = make(chan struct{}), make(chan struct{}, 1)
		first := make(chan int, 1)
		go func() {
			code, _, _ := r.post(context.Background(), callBody("web/docker-local", "app", d, testToken))
			first <- code
		}()
		<-r.an.started
		code, p, _ := r.post(t.Context(), callBody("web/docker-local", "app", d, testToken))
		if code != http.StatusServiceUnavailable || p.Code != imagescan.CodeScannerBusy {
			t.Fatalf("%d %+v", code, p)
		}
		close(r.an.block)
		<-first
		if r.sp.Held() != 0 {
			t.Fatalf("held %d after both calls", r.sp.Held())
		}
	})
}

func TestR_HA_065_ADrainingScannerAnswersDraining(t *testing.T) {
	r := newRig(t, 1)
	d := r.reg.add(imageOf(t, tarOf(t, map[string]string{"a": "1"})))
	r.an.block, r.an.started = make(chan struct{}), make(chan struct{}, 1)
	first := make(chan imagescan.CallProblem, 1)
	go func() {
		_, p, _ := r.post(context.Background(), callBody("web/docker-local", "app", d, testToken))
		first <- p
	}()
	<-r.an.started
	r.svc.Drain()
	if code, p, _ := r.post(t.Context(), callBody("web/docker-local", "app", d, testToken)); code != http.StatusServiceUnavailable || p.Code != imagescan.CodeScannerDraining {
		t.Fatalf("a new call while draining: %d %+v", code, p)
	}
	r.svc.Stop()
	if p := <-first; p.Code != imagescan.CodeScannerDraining {
		t.Fatalf("the analysis in flight at the shutdown timeout: %+v", p)
	}
}

func TestR_VULN_036_TheReaperActsOnlyAsTheContainersFirstProcess(t *testing.T) {
	var killed []int
	waits := []int{41, 42, 0}
	r := &Reaper{
		log: nil, kill: func(pid int, _ syscall.Signal) error { killed = append(killed, pid); return nil },
		wait: func() (int, error) { p := waits[0]; waits = waits[1:]; return p, nil },
	}
	r.Reap()
	if len(killed) != 0 {
		t.Fatal("a reaper that is not PID 1 sent a signal")
	}
	r.pid1, r.log = true, NewReaper(nil).log
	r.Reap()
	if len(killed) != 1 || killed[0] != -1 || len(waits) != 0 {
		t.Fatalf("killed %v, waits left %v", killed, waits)
	}
}
