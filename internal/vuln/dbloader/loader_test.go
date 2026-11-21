package dbloader

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/vuln/imagescan"
)

// archive is a database layer of bytes: metadata.json with the build time and trivy.db of size bytes.
func archive(t *testing.T, built string, size int, extra ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := [][2]string{}
	for i := 0; i+1 < len(extra); i += 2 {
		files = append(files, [2]string{extra[i], extra[i+1]})
	}
	files = append(files, [2]string{"metadata.json", `{"Version":2,"UpdatedAt":"` + built + `"}`}, [2]string{"trivy.db", strings.Repeat("d", size)})
	for _, f := range files {
		if err := tw.WriteHeader(&tar.Header{Name: f[0], Mode: 0o644, Size: int64(len(f[1])), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f[1])); err != nil {
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

func digestOf(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

func hexOf(d string) string { return strings.TrimPrefix(d, "sha256:") }

// listener is the scan listener's database routes: the database in use and the layers by digest; a layer may be served under another
// digest than its own (a tampered store), and a read may be held at its middle.
type listener struct {
	mu     sync.Mutex
	inUse  string
	layers map[string][]byte
	// hold, while set, makes the layer route write half of the body and wait for it to be closed.
	hold chan struct{}
	half chan struct{}
}

func (l *listener) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	inUse, layers, hold, half := l.inUse, l.layers, l.hold, l.half
	l.mu.Unlock()
	switch {
	case r.URL.Path == registrytoken.TrivyDatabasePath:
		_ = json.NewEncoder(w).Encode(imagescan.DatabaseListing{Digest: inUse})
	case strings.HasPrefix(r.URL.Path, registrytoken.TrivyDatabasePath+"/"):
		b, ok := layers[strings.TrimPrefix(r.URL.Path, registrytoken.TrivyDatabasePath+"/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if hold != nil {
			_, _ = w.Write(b[:len(b)/2])
			w.(http.Flusher).Flush()
			close(half)
			<-hold
		}
		_, _ = w.Write(b[len(b)/2*boolInt(hold != nil):])
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (l *listener) use(digest string, layer []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.layers == nil {
		l.layers = map[string][]byte{}
	}
	l.inUse, l.layers[digest] = digest, layer
}

func newLoader(t *testing.T, l *listener, dir string, budget int64) *Loader {
	t.Helper()
	srv := httptest.NewServer(l)
	t.Cleanup(srv.Close)
	ld, err := New(Options{Listener: Listener{URL: srv.URL}, Dir: dir, Budget: budget, HTTP: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return ld
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	slices.Sort(out)
	return out
}

// R-VULN-049: the loader keeps the newest database and the one before it, and removes the older ones before it unpacks a new one; a
// directory appears under its digest only when it is complete.
func TestR_VULN_049_TheLoaderKeepsTheNewestAndThePreviousDatabase(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	l := &listener{}
	ld := newLoader(t, l, dir, 1<<20)
	if err := ld.Once(ctx); err != nil || len(entries(t, dir)) != 0 {
		t.Fatalf("before the first database: %v %v", err, entries(t, dir))
	}
	a1, a2, a3 := archive(t, "2026-11-19T07:00:00Z", 100), archive(t, "2026-11-20T07:00:00Z", 200), archive(t, "2026-11-21T07:00:00Z", 300)
	// The newest database is the one loaded last (Q-VULN-213); each load is stamped a minute after the one before, so that the order
	// does not hang on the file system's clock resolution.
	stamp := time.Now().Add(-time.Hour)
	load := func(name string, a []byte) {
		t.Helper()
		l.use(digestOf(a), a)
		if err := ld.Once(ctx); err != nil {
			t.Fatalf("database %s: %v", name, err)
		}
		if _, ok := imagescan.DatabaseIn(filepath.Join(dir, hexOf(digestOf(a)))); !ok {
			t.Fatalf("database %s is not complete in %v", name, entries(t, dir))
		}
		stamp = stamp.Add(time.Minute)
		if err := os.Chtimes(filepath.Join(dir, hexOf(digestOf(a)), imagescan.DatabaseDir, "metadata.json"), stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	for i, a := range [][]byte{a1, a2, a3} {
		load(strconv.Itoa(i+1), a)
	}
	want := []string{hexOf(digestOf(a2)), hexOf(digestOf(a3))}
	slices.Sort(want)
	if got := entries(t, dir); !slices.Equal(got, want) {
		t.Fatalf("the directory holds %v, want the newest and the one before it %v", got, want)
	}
	if got := ld.Held(); len(got) != 2 || got[0] != hexOf(digestOf(a3)) {
		t.Fatalf("held newest first: %v", got)
	}
	meta := int64(len(`{"Version":2,"UpdatedAt":"2026-11-21T07:00:00Z"}`))
	if ld.Bytes() != 200+300+2*meta {
		t.Fatalf("the room held is %d", ld.Bytes())
	}
	// A loader that starts finds both and loads nothing again.
	again := newLoader(t, l, dir, 1<<20)
	if again.Bytes() != ld.Bytes() || again.Once(ctx) != nil || len(entries(t, dir)) != 2 {
		t.Fatalf("a restarted loader: %d bytes, %v", again.Bytes(), entries(t, dir))
	}

	t.Run("a directory appears under its digest only when it is complete", func(t *testing.T) {
		a4 := archive(t, "2026-11-22T07:00:00Z", 4000)
		l.use(digestOf(a4), a4)
		l.mu.Lock()
		l.hold, l.half = make(chan struct{}), make(chan struct{})
		half := l.half
		l.mu.Unlock()
		done := make(chan error, 1)
		go func() { done <- ld.Once(ctx) }()
		<-half
		for _, name := range entries(t, dir) {
			if name == hexOf(digestOf(a4)) {
				t.Fatal("the directory is under its digest while its layer is half read")
			}
		}
		l.mu.Lock()
		close(l.hold)
		l.hold = nil
		l.mu.Unlock()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if _, ok := imagescan.DatabaseIn(filepath.Join(dir, hexOf(digestOf(a4)))); !ok {
			t.Fatal("the database is not complete after the read")
		}
		stamp = stamp.Add(time.Minute)
		if err := os.Chtimes(filepath.Join(dir, hexOf(digestOf(a4)), imagescan.DatabaseDir, "metadata.json"), stamp, stamp); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("a database built before the one it follows is the newest once loaded, and stays while the next one loads", func(t *testing.T) {
		a4 := hexOf(digestOf(archive(t, "2026-11-22T07:00:00Z", 4000)))
		a0 := archive(t, "2026-11-18T07:00:00Z", 50) // the repository's tag moved back
		load("built before", a0)
		if got := ld.Held(); len(got) != 2 || got[0] != hexOf(digestOf(a0)) || got[1] != a4 {
			t.Fatalf("held %v, want the database loaded last %s first, then %s", got, hexOf(digestOf(a0)), a4)
		}
		a5 := archive(t, "2026-11-23T07:00:00Z", 60)
		load("after it", a5)
		want := []string{hexOf(digestOf(a5)), hexOf(digestOf(a0))}
		if got := ld.Held(); !slices.Equal(got, want) {
			t.Fatalf("held %v, want %v: the database in use before the load stays as the previous one", got, want)
		}
	})
}

// R-VULN-049, R-VULN-035: a layer whose hash is not its digest leaves no directory and holds no room; an archive with an entry
// ../escape writes nothing outside the directory; the leftovers of an interrupted load go at the next start.
func TestR_VULN_049_ALayerThatIsNotItsDigestLeavesNothing(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dir := filepath.Join(root, "trivy-db")
	l := &listener{}
	ld := newLoader(t, l, dir, 1<<20)
	good, other := archive(t, "2026-11-21T07:00:00Z", 100), archive(t, "2026-11-21T07:00:00Z", 101)
	l.use(digestOf(good), other) // the store serves other bytes under the digest in use
	if err := ld.Once(ctx); !imagescan.ErrLayerDigest(err) {
		t.Fatalf("%v", err)
	}
	if got := entries(t, dir); len(got) != 0 || ld.Bytes() != 0 {
		t.Fatalf("left %v, held %d", got, ld.Bytes())
	}
	escape := archive(t, "2026-11-21T07:00:00Z", 100, "../escape", "x")
	l.use(digestOf(escape), escape)
	if err := ld.Once(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "escape")); err == nil {
		t.Fatal("the entry ../escape was written")
	}
	if err := os.MkdirAll(filepath.Join(dir, unpackPrefix+"7", imagescan.DatabaseDir), 0o700); err != nil {
		t.Fatal(err)
	}
	newLoader(t, l, dir, 1<<20)
	if got := entries(t, dir); len(got) != 1 || got[0] != hexOf(digestOf(escape)) {
		t.Fatalf("after a restart the directory holds %v", got)
	}
}

// R-VULN-049, Q-HA-380: a database that does not fit beside the newest replaces it: the newest goes first, and the new one is loaded.
func TestR_VULN_049_ADatabaseThatDoesNotFitBesideTheNewestReplacesIt(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	l := &listener{}
	a1, a2 := archive(t, "2026-11-20T07:00:00Z", 600), archive(t, "2026-11-21T07:00:00Z", 700)
	ld := newLoader(t, l, dir, 1000)
	l.use(digestOf(a1), a1)
	if err := ld.Once(ctx); err != nil {
		t.Fatal(err)
	}
	l.use(digestOf(a2), a2)
	if err := ld.Once(ctx); err != nil {
		t.Fatal(err)
	}
	if got := entries(t, dir); len(got) != 1 || got[0] != hexOf(digestOf(a2)) {
		t.Fatalf("the directory holds %v, want the new database alone", got)
	}
	if ld.Bytes() > 1000 {
		t.Fatalf("held %d of a budget of 1000", ld.Bytes())
	}
	t.Run("a database larger than the whole budget is refused and the newest stays", func(t *testing.T) {
		big := archive(t, "2026-11-22T07:00:00Z", 5000)
		l.use(digestOf(big), big)
		if err := ld.Once(ctx); err == nil {
			t.Fatal("a database larger than the budget was loaded")
		}
		// The newest went to make room and the new one did not fit either: detections answer database_loading until the volume is raised;
		// the worker names the new database only, so the old one would answer nothing.
		if got := entries(t, dir); len(got) != 0 {
			t.Fatalf("the directory holds %v", got)
		}
		if ld.Bytes() != 0 {
			t.Fatalf("held %d after the refusal", ld.Bytes())
		}
	})
	if _, err := New(Options{Listener: Listener{URL: "https://x"}, Dir: dir}); err == nil {
		t.Fatal("a loader without a budget was built")
	}
}
