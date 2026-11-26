//go:build integration

package bastion

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/engine"
	"github.com/guided-traffic/quader/internal/bastion/handover"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/tlsfiles/tlstest"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// storeRelay is the object store's public address in the joined case: a TLS listener with a certificate of the stack's CA
// that passes every request to the stack's SeaweedFS with its Host unchanged (the signature of a link covers the host it
// names), counts the bytes the store sends through it, and can be told to refuse one object or to cut its body after some
// bytes. It stands for the store's address as a Bastion's network reaches it, the place where a store's refusal or a cut
// transfer happens.
type storeRelay struct {
	srv *httptest.Server

	mu     sync.Mutex
	sent   map[string]int64 // bytes sent per object, by the digest (or the keyed name) its key ends with
	refuse map[string]int   // a status for an object
	cut    map[string]int64 // break the body of an object after n bytes
	// swap answers an object with other bytes, 200 with their length, as a store whose object was replaced would: the body of
	// another object of the same stored size, which a handed data key does not open.
	swap map[string][]byte
}

func newStoreRelay(t *testing.T) *storeRelay {
	t.Helper()
	target, err := url.Parse(testenv.S3Config(t).Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	caPEM, err := os.ReadFile(testenv.CAFile(t))
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)
	ca := tlstest.LoadCA(t, testenv.CAFile(t), filepath.Join(testenv.CertDir(t), "ca.key"))
	leaf := ca.Issue(t, tlstest.Options{DNSNames: []string{"localhost"}, IPs: []net.IP{net.IPv4(127, 0, 0, 1)}, NotAfter: time.Now().Add(6 * time.Hour)})
	r := &storeRelay{sent: map[string]int64{}, refuse: map[string]int{}, cut: map[string]int64{}, swap: map[string][]byte{}}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = pr.In.Host
		},
		ModifyResponse: func(res *http.Response) error {
			key := r.keyOf(res.Request.URL.Path)
			r.mu.Lock()
			limit, cutting := r.cut[key]
			r.mu.Unlock()
			res.Body = &relayBody{ReadCloser: res.Body, r: r, key: key, limit: limit, cutting: cutting}
			return nil
		},
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, DisableCompression: true},
		// Every write reaches the Bastion before a cut, so the bytes it spooled are the bytes the relay let through.
		FlushInterval: -1,
		ErrorLog:      log.New(io.Discard, "", 0), // a cut is what the test asks for
	}
	r.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		key := r.keyOf(req.URL.Path)
		r.mu.Lock()
		status, other := r.refuse[key], r.swap[key]
		r.mu.Unlock()
		switch {
		case status != 0:
			w.WriteHeader(status)
		case other != nil:
			w.Header().Set("Content-Length", strconv.Itoa(len(other)))
			w.WriteHeader(http.StatusOK)
			n, _ := w.Write(other)
			r.mu.Lock()
			r.sent[key] += int64(n)
			r.mu.Unlock()
		default:
			proxy.ServeHTTP(w, req)
		}
	}))
	r.srv.TLS = tlsServerConfig(leaf)
	r.srv.StartTLS()
	t.Cleanup(r.srv.Close)
	return r
}

// address is the public address the main installation signs its links for: localhost, which is not the host of the
// Bastion's bastion.main.url (the front on 127.0.0.1), so the Bastion's check of the link's host lets it pass (R-BAS-074).
func (r *storeRelay) address() string {
	u, _ := url.Parse(r.srv.URL)
	return "https://localhost:" + u.Port()
}

// keyOf is the digest an object's key ends with, or the path.
func (r *storeRelay) keyOf(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 && len(p)-i-1 == 64 {
		return p[i+1:]
	}
	return p
}

func (r *storeRelay) sentFor(sum string) int64 { r.mu.Lock(); defer r.mu.Unlock(); return r.sent[sum] }

type relayBody struct {
	io.ReadCloser
	r       *storeRelay
	key     string
	n       int64
	limit   int64
	cutting bool
}

func (b *relayBody) Read(p []byte) (int, error) {
	if b.cutting {
		if b.n >= b.limit {
			return 0, errors.New("relay: the transfer is cut")
		}
		if left := b.limit - b.n; int64(len(p)) > left {
			p = p[:left]
		}
	}
	n, err := b.ReadCloser.Read(p)
	b.n += int64(n)
	b.r.mu.Lock()
	b.r.sent[b.key] += int64(n)
	b.r.mu.Unlock()
	return n, err
}

// handedFile is a file of the joined case: its path below /files/ and its bytes.
type handedFile struct {
	path string
	data []byte
	sum  string
}

// TestR_BAS_070_ALargeFileIsReadFromTheObjectStore joins the two sides of the handover per blob: the suite's main installation
// (the web role of the stack, its links' minSize 0 and the default connection's public address on a relay to the stack's store)
// with the registration's handover switched on through the API, and the Bastion role, which offers, is answered with the
// file's 200 without its bytes and the class handover, reads the file from the store and serves it. What only the join shows:
// the caller gets the file's bytes and the headers the main installation serves; the main installation answered the class
// handover with no body (the front's record, its request log's delivery handover, its metric); the Bastion counts a read and
// names it in its request log; the download is counted once at the main installation and the Bastion reports the hit that
// follows, which asks the main installation nothing; a direct read cut after its first segment is continued with the Bastion's
// own Range and If-Range against the real serve point, which answers 206 and counts no second download; a link the store
// refuses falls back to the bytes through the main installation, which counts the download again (H-1342); and with the switch
// off the handshake tells the Bastion, which then offers nothing and receives the bytes. Files of 64 KiB, one of 160 KiB where a
// segment's boundary is the assertion.
func TestR_BAS_070_ALargeFileIsReadFromTheObjectStore(t *testing.T) {
	const repo = "generic-handover"
	relay := newStoreRelay(t)
	w := newWorld(t, worldOptions{Seed: func(w *world) {
		w.st.Repository(repo, "local", "generic", `{}`)
		w.st.Exec(`UPDATE download_link_settings SET min_size = 0, public_addresses = jsonb_build_object('default', $1::text), version = version + 1`,
			relay.address())
	}})
	file := func(name string, data []byte) handedFile {
		t.Helper()
		f := handedFile{path: "/files/" + testenv.DefaultProject + "/" + repo + "/dist/" + name, data: data}
		s := sha256.Sum256(data)
		f.sum = hex.EncodeToString(s[:])
		w.mustAPI("PUT", f.path, http.StatusCreated, data, "Content-Type", "application/octet-stream")
		// The malware scanning is on by default: a clean verdict makes the gate pass on a decided state (R-STOR-095 step 3).
		w.st.Exec(`INSERT INTO malware_scan (sha256, state, holder_repo_id, size, due, scanned_at)
			SELECT i.blob_sha256, 'clean', i.repo_id, i.size, false, now() FROM items i JOIN repositories r ON r.id = i.repo_id
			 WHERE r.key = $1 AND i.path = $2
			ON CONFLICT (sha256) DO UPDATE SET state = 'clean', due = false, scanned_at = now()`, repo, "dist/"+name)
		return f
	}
	random := func(n int) []byte {
		b := make([]byte, n)
		if _, err := rand.Read(b); err != nil {
			t.Fatal(err)
		}
		return b
	}
	small := random(64 << 10)
	handed, again := file("handed.bin", small), file("again.bin", small) // one blob at two paths
	continued, refused := file("continued.bin", random(160<<10)), file("refused.bin", random(64<<10))
	// A format's artifact, whose answers name its package (R-BAS-082): a tarball of npm-local, where acme-part reads the packages @acme/* only (a partial shape).
	w.publishNPM("@acme/handed", "1.0.0")
	w.st.Exec(`INSERT INTO malware_scan (sha256, state, holder_repo_id, size, due, scanned_at)
		SELECT DISTINCT ON (i.blob_sha256) i.blob_sha256, 'clean', i.repo_id, i.size, false, now() FROM items i JOIN repositories r ON r.id = i.repo_id
		 WHERE r.key = 'npm-local' AND i.blob_sha256 IS NOT NULL
		ON CONFLICT (sha256) DO UPDATE SET state = 'clean', due = false, scanned_at = now()`)
	tarball := tarballPath(npmLocal, "@acme/handed", "1.0.0")

	// The main installation's answers to the Bastion's forwards, as the front sees them.
	type answer struct {
		class, length, handover string
		status                  int
	}
	var (
		amu     sync.Mutex
		answers = map[string][]answer{}
	)
	w.front.SetHold(func(req *http.Request, res *http.Response) {
		amu.Lock()
		answers[req.URL.Path] = append(answers[req.URL.Path], answer{
			class: res.Header.Get(iface.HeaderClass), length: res.Header.Get("Content-Length"),
			handover: res.Header.Get(iface.HeaderHandover), status: res.StatusCode,
		})
		amu.Unlock()
	})
	answersOf := func(p string) []answer { amu.Lock(); defer amu.Unlock(); return append([]answer(nil), answers[p]...) }

	w.mustAPI("PATCH", "/api/v1/bastions/partners", http.StatusOK, map[string]any{"handover": true},
		"Content-Type", "application/merge-patch+json", "If-Match", "*")
	bundle := filepath.Join(w.dir, "bastion-ca-bundle.pem")
	front, err := os.ReadFile(w.front.caFile)
	if err != nil {
		t.Fatal(err)
	}
	stack, err := os.ReadFile(testenv.CAFile(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle, append(append(front, '\n'), stack...), 0o600); err != nil {
		t.Fatal(err)
	}
	b := w.newBastion(t, bastionOptions{CAFile: bundle}).wait()
	r := b.reps[0]
	testWait(t, "the Bastion told the handover is on", func() bool {
		return metricValue(t, r.metrics(t), "quader_bastion_handover_enabled") == 1
	})
	const addr = "203.0.113.70"
	call := callOpts{token: w.admin, addr: addr}
	downloads := func(f handedFile, origin string) int64 {
		return w.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'artifact.downloaded' AND host(source_ip) = $1 AND path LIKE $2 AND `+origin,
			addr, "%"+f.path[strings.LastIndexByte(f.path, '/'):])
	}
	const viaBastion, direct = "details->>'origin' = 'bastion'", "details->>'origin' IS NULL"
	mainHandovers := func() float64 {
		// The series exists once the registration's first handover is counted.
		return max(0, metricValue(t, w.main.Metrics(t), `quader_storage_download_handovers_total{bastion="partners",kind="link"}`))
	}
	bastionMetric := func(series string) float64 { return metricValue(t, r.metrics(t), series) }
	mainLine := func(f handedFile, fragments ...string) bool {
		for _, line := range strings.Split(w.main.Logs.String(), "\n") {
			ok := strings.Contains(line, `"request completed"`) && strings.Contains(line, f.path)
			for _, frag := range fragments {
				ok = ok && strings.Contains(line, frag)
			}
			if ok {
				return true
			}
		}
		return false
	}

	t.Run("a cold read is handed over and read from the store", func(t *testing.T) {
		before := mainHandovers()
		got := r.do(t, "GET", handed.path, call)
		if got.Status != http.StatusOK || !bytes.Equal(got.Body, handed.data) {
			t.Fatalf("%d, %d bytes\n%s", got.Status, len(got.Body), testenv.Tail(r.logs.String(), 20))
		}
		fromMain := w.api("GET", handed.path, w.admin, nil)
		for _, k := range []string{"Content-Type", "Etag", "Last-Modified", "Content-Disposition", "X-Content-Type-Options", "Cache-Control"} {
			if want := fromMain.Header.Get(k); want != "" && got.Header.Get(k) != want {
				t.Errorf("%s: the Bastion served %q, the main installation serves %q", k, got.Header.Get(k), want)
			}
		}
		if got.Header.Get("Content-Length") != strconv.Itoa(len(handed.data)) {
			t.Errorf("Content-Length %q", got.Header.Get("Content-Length"))
		}
		for k := range got.Header {
			if strings.HasPrefix(http.CanonicalHeaderKey(k), iface.HeaderPrefix) {
				t.Errorf("the caller received %s", k)
			}
		}
		if a := answersOf(handed.path); len(a) != 1 || a[0].status != http.StatusOK || a[0].class != string(iface.ClassHandover) || a[0].length != "0" || a[0].handover == "" {
			t.Fatalf("the main installation's answers: %+v", a)
		}
		for _, rc := range w.front.Records() {
			if rc.Path == handed.path && (rc.Bytes != 0 || rc.Header.Get(iface.HeaderHandover) == "") {
				t.Fatalf("the forward offered %q and the answer carried %d bytes", rc.Header.Get(iface.HeaderHandover), rc.Bytes)
			}
		}
		if relay.sentFor(handed.sum) != int64(len(handed.data)) {
			t.Fatalf("the store sent %d bytes through its public address", relay.sentFor(handed.sum))
		}
		if mainHandovers() != before+1 {
			t.Fatalf("quader_storage_download_handovers_total rose by %v", mainHandovers()-before)
		}
		testWait(t, "the main installation's request line", func() bool { return mainLine(handed, `"delivery":"handover"`, `"response_bytes":0`) })
		if bastionMetric(`quader_bastion_handovers_total{outcome="read"}`) != 1 || bastionMetric(`quader_bastion_fill_bytes_total{class="authenticated",source="store"}`) != float64(len(handed.data)) ||
			bastionMetric(`quader_bastion_fill_bytes_total{class="authenticated",source="main"}`) != 0 {
			t.Fatalf("the Bastion's counts:\n%s", grepLines(r.metrics(t), "quader_bastion_handovers_total", "quader_bastion_fill_bytes_total"))
		}
		testWait(t, "the Bastion's request line", func() bool { return strings.Contains(r.logs.String(), `"bastion_handover":"read"`) })
		testWait(t, "the download counted at the main installation", func() bool { return downloads(handed, direct) == 1 })
	})

	t.Run("a second read is a hit that asks the main installation nothing and is reported once", func(t *testing.T) {
		m := w.front.Mark()
		got := r.do(t, "GET", handed.path, call)
		if got.Status != http.StatusOK || !bytes.Equal(got.Body, handed.data) {
			t.Fatalf("%d, %d bytes", got.Status, len(got.Body))
		}
		if fw := w.front.Forwards(m, "/files/"); len(fw) != 0 {
			t.Fatalf("the hit sent %d forwards", len(fw))
		}
		testWait(t, "the Bastion's report of the hit", func() bool { return downloads(handed, viaBastion) == 1 })
		if n := downloads(handed, direct); n != 1 {
			t.Fatalf("%d downloads counted at the main installation, want the handover's one", n)
		}
	})

	t.Run("a handed artifact of a format is kept with the package its handover answer named", func(t *testing.T) {
		part := callOpts{token: w.part.token, addr: addr}
		reads := bastionMetric(`quader_bastion_handovers_total{outcome="read"}`)
		got := r.do(t, "GET", tarball, part)
		if got.Status != http.StatusOK || len(got.Body) == 0 {
			t.Fatalf("%d, %d bytes\n%s", got.Status, len(got.Body), testenv.Tail(r.logs.String(), 20))
		}
		if a := answersOf(tarball); len(a) != 1 || a[0].class != string(iface.ClassHandover) {
			t.Fatalf("the main installation's answers: %+v", a)
		}
		if bastionMetric(`quader_bastion_handovers_total{outcome="read"}`) != reads+1 {
			t.Fatal("the tarball was not read from the store")
		}
		// The entry's package is what R-BAS-085 serves a reader narrowed by package on; the volume holds the signed records.
		var kept bool
		testWait(t, "the tarball's entry", func() bool {
			_ = filepath.WalkDir(filepath.Join(b.sh.volume, "entries"), func(p string, d os.DirEntry, err error) error {
				if err == nil && d.Type().IsRegular() {
					rec, _ := os.ReadFile(p)
					kept = kept || (bytes.Contains(rec, []byte(`"package":"@acme/handed"`)) && bytes.Contains(rec, []byte(`"repository":"`+npmLocal+`"`)))
				}
				return nil
			})
			return kept
		})
	})

	t.Run("a read cut after its first segment is continued with Range and If-Range at the serve point", func(t *testing.T) {
		const cutAt = 96 << 10
		relay.mu.Lock()
		relay.cut[continued.sum] = cutAt
		relay.mu.Unlock()
		got := r.do(t, "GET", continued.path, call)
		if got.Status != http.StatusOK || !bytes.Equal(got.Body, continued.data) {
			t.Fatalf("%d, %d bytes\n%s", got.Status, len(got.Body), testenv.Tail(r.logs.String(), 20))
		}
		a := answersOf(continued.path)
		var rng []rec
		for _, rc := range w.front.Records() {
			if rc.Path == continued.path {
				rng = append(rng, rc)
			}
		}
		if len(a) != 2 || a[0].class != string(iface.ClassHandover) || a[1].status != http.StatusPartialContent || len(rng) != 2 {
			t.Fatalf("the main installation's answers %+v", a)
		}
		// The spooled length is what arrived before the cut, at least the first segment Open verified (Q-BAS-291).
		cont := rng[1].Header
		var from int64
		if _, err := fmt.Sscanf(cont.Get("Range"), "bytes=%d-", &from); err != nil || from < 64<<10 || from > cutAt ||
			cont.Get("If-Range") != `"`+continued.sum+`"` || cont.Get(iface.HeaderHandover) != "" || rng[1].Bytes != int64(len(continued.data))-from {
			t.Fatalf("the continuation: Range %q, If-Range %q, offer %q, %d bytes", cont.Get("Range"), cont.Get("If-Range"), cont.Get(iface.HeaderHandover), rng[1].Bytes)
		}
		if from != cutAt {
			t.Logf("the Bastion spooled %d bytes before the cut at %d", from, cutAt)
		}
		if bastionMetric(`quader_bastion_handovers_total{outcome="continued"}`) != 1 || bastionMetric(`quader_bastion_fill_bytes_total{class="authenticated",source="main"}`) != float64(int64(len(continued.data))-from) {
			t.Fatalf("the Bastion's counts:\n%s", grepLines(r.metrics(t), "quader_bastion_handovers_total", "quader_bastion_fill_bytes_total"))
		}
		testWait(t, "the download counted at the main installation", func() bool { return downloads(continued, direct) >= 1 })
		time.Sleep(200 * time.Millisecond) // a second event would be written with the 206's request
		if n := downloads(continued, direct); n != 1 {
			t.Fatalf("%d downloads counted for a continued read, want 1 (a 206 after byte 0 counts none, Q-BAS-186)", n)
		}
	})

	t.Run("a link the store refuses falls back to the bytes through the main installation, which counts again", func(t *testing.T) {
		relay.mu.Lock()
		relay.refuse[refused.sum] = http.StatusForbidden
		relay.mu.Unlock()
		got := r.do(t, "GET", refused.path, call)
		if got.Status != http.StatusOK || !bytes.Equal(got.Body, refused.data) {
			t.Fatalf("%d, %d bytes", got.Status, len(got.Body))
		}
		a := answersOf(refused.path)
		if len(a) != 2 || a[0].class != string(iface.ClassHandover) || a[1].class != string(iface.ClassArtifact) || a[1].length != strconv.Itoa(len(refused.data)) {
			t.Fatalf("the main installation's answers %+v", a)
		}
		if bastionMetric(`quader_bastion_handovers_total{outcome="fallback"}`) != 1 || bastionMetric("quader_bastion_handover_cooldown") != 1 {
			t.Fatalf("the Bastion's counts:\n%s", grepLines(r.metrics(t), "quader_bastion_handover"))
		}
		testWait(t, "the two downloads of H-1342", func() bool { return downloads(refused, direct) == 2 })
	})

	t.Run("R-BAS-073 an ordinary request that sends the offer gets the file and no marking", func(t *testing.T) {
		a := w.api("GET", handed.path, w.admin, nil, iface.HeaderHandover, iface.HandoverOffer{MaxSize: 1 << 20}.String())
		if a.Status != http.StatusOK || !bytes.Equal(a.Body, handed.data) {
			t.Fatalf("%d, %d bytes", a.Status, len(a.Body))
		}
		for k := range a.Header {
			if strings.HasPrefix(http.CanonicalHeaderKey(k), iface.HeaderPrefix) {
				t.Errorf("the answer carries %s", k)
			}
		}
	})

	t.Run("with the switch off the main installation streams an offered forward, the handshake tells the Bastion, and the read comes as bytes from the main installation", func(t *testing.T) {
		w.mustAPI("PATCH", "/api/v1/bastions/partners", http.StatusOK, map[string]any{"handover": false},
			"Content-Type", "application/merge-patch+json", "If-Match", "*")
		// The main installation decides from its own snapshot of the switch, whatever a Bastion's last handshake said: a recognized
		// forward that offers, sent as the Bastion sends it, is streamed once the snapshot followed (R-BAS-071, within 2 s).
		testWait(t, "an offered forward streamed with the switch off", func() bool {
			a := w.api("GET", handed.path, w.admin, nil,
				iface.HeaderAuthorization, "Bearer "+w.bst, iface.HeaderInterface, strconv.Itoa(iface.InterfaceVersion),
				iface.HeaderHandover, iface.HandoverOffer{MaxSize: 1 << 20}.String())
			return a.Status == http.StatusOK && bytes.Equal(a.Body, handed.data) && a.Header.Get(iface.HeaderClass) == string(iface.ClassArtifact) &&
				a.Header.Get(iface.HeaderHandover) == ""
		})
		testWait(t, "the Bastion told the handover is off", func() bool {
			return metricValue(t, r.metrics(t), "quader_bastion_handover_enabled") == 0
		})
		mainBefore := bastionMetric(`quader_bastion_fill_bytes_total{class="authenticated",source="main"}`)
		got := r.do(t, "GET", again.path, call)
		if got.Status != http.StatusOK || !bytes.Equal(got.Body, again.data) {
			t.Fatalf("%d, %d bytes", got.Status, len(got.Body))
		}
		a := answersOf(again.path)
		if len(a) != 1 || a[0].class != string(iface.ClassArtifact) || a[0].length != strconv.Itoa(len(again.data)) {
			t.Fatalf("the main installation's answers %+v", a)
		}
		for _, rc := range w.front.Records() {
			if rc.Path == again.path && rc.Header.Get(iface.HeaderHandover) != "" {
				t.Fatal("the Bastion offered while the handshake said the handover is off")
			}
		}
		if d := bastionMetric(`quader_bastion_fill_bytes_total{class="authenticated",source="main"}`) - mainBefore; d != float64(len(again.data)) {
			t.Fatalf("the Bastion filled %v bytes from the main installation, want %d", d, len(again.data))
		}
	})

	t.Run("the Bastion's request log names each outcome", func(t *testing.T) {
		for _, outcome := range []string{"read", "continued", "fallback"} {
			if !strings.Contains(r.logs.String(), `"bastion_handover":"`+outcome+`"`) {
				t.Errorf("no request line with bastion_handover %s", outcome)
			}
		}
	})

	t.Run("no log line of either side holds a usable link", func(t *testing.T) {
		// The main installation's upstream log names the probe objects of its link check with the signature's members redacted
		// (R-OPS-013); a link whose credential or signature stands in a log line is a bearer for its life (H-12).
		secret := regexp.MustCompile(`X-Amz-(Signature|Credential|Security-Token)=([^&"\s\\]*)`)
		for who, text := range map[string]string{"the Bastion": r.logs.String(), "the main installation": w.main.Logs.String()} {
			for _, m := range secret.FindAllStringSubmatch(text, -1) {
				if m[2] != "[REDACTED]" && m[2] != "%5BREDACTED%5D" {
					t.Errorf("a log line of %s holds X-Amz-%s unredacted", who, m[1])
				}
			}
			for _, f := range []handedFile{handed, continued, refused} {
				if strings.Contains(text, f.sum+"?X-Amz-") {
					t.Errorf("a log line of %s holds the link of %s", who, f.path)
				}
			}
		}
	})
}

// encryptedDefault is QUADER_STORAGE_CONNECTIONS of the main replica with the stack's one connection, default, encrypted (R-STOR-075,
// keyed names by default), below the stack's prefix: its first start draws the storage key and the name key, sealed with the stack's
// key ring (R-STOR-083).
func encryptedDefault(w *world) []string {
	w.t.Helper()
	c := testenv.Connection(w.t, w.dir, w.st.ObjectPrefix())
	b, err := json.Marshal([]map[string]any{{
		"id": c.ID, "endpoint": c.Endpoint, "region": c.Region, "bucket": c.Bucket, "prefix": w.st.ObjectPrefix(), "force_path_style": c.ForcePathStyle,
		"ca_file": c.CAFile, "credentials_source": c.CredentialsSource, "access_key_id_file": c.AccessKeyIDFile,
		"secret_access_key_file": c.SecretAccessKeyFile, "encrypted": true,
	}})
	if err != nil {
		w.t.Fatal(err)
	}
	return []string{"QUADER_STORAGE_CONNECTIONS=" + string(b)}
}

// TestR_BAS_070_AnEncryptedFileIsReadFromTheObjectStoreWithItsDataKey is the joined case on an encrypted connection (storage.md
// R-STOR-095 steps 8 and 10, R-STOR-096; R-BAS-073, R-BAS-074, R-BAS-076): the suite's main installation with its one connection
// encrypted, the registration's handover on through the API, the links' minSize 0 and the connection's public address on the relay,
// and the Bastion role with the stack's CA in its bundle. A cold read of an encrypted file is answered with the file's 200 without its
// bytes and a value that carries the copy's data key and its stored size S(size); the main installation wrote
// bastion.data_key.handed with its members and without the key or the link; the store sent the stored size through its public
// address; the Bastion opened the segments with the handed key, served the plaintext and holds no key afterwards. A second file of
// the same size whose object the relay answers with the first one's body, under another data key, falls back to the bytes through the
// main installation without the cooldown, and its trace names a key whose read fell back (H-1342). No log line of either side holds
// a key or a usable link. Two files of one segment and 1 KiB, so that a read crosses a segment's boundary.
func TestR_BAS_070_AnEncryptedFileIsReadFromTheObjectStoreWithItsDataKey(t *testing.T) {
	const repo = "generic-sealed"
	relay := newStoreRelay(t)
	w := newWorld(t, worldOptions{
		Seed: func(w *world) {
			w.st.Repository(repo, "local", "generic", `{}`)
			w.st.Exec(`UPDATE download_link_settings SET min_size = 0, public_addresses = jsonb_build_object('default', $1::text), version = version + 1`,
				relay.address())
		},
		MainEnv: encryptedDefault,
	})
	const size = 64<<10 + 1<<10
	type sealedFile struct {
		path, name, sum, object, keyID string
		data                           []byte
	}
	file := func(name string) sealedFile {
		t.Helper()
		f := sealedFile{path: "/files/" + testenv.DefaultProject + "/" + repo + "/dist/" + name, name: name, data: make([]byte, size)}
		if _, err := rand.Read(f.data); err != nil {
			t.Fatal(err)
		}
		s := sha256.Sum256(f.data)
		f.sum = hex.EncodeToString(s[:])
		w.mustAPI("PUT", f.path, http.StatusCreated, f.data, "Content-Type", "application/octet-stream")
		w.st.Exec(`INSERT INTO malware_scan (sha256, state, holder_repo_id, size, due, scanned_at)
			SELECT i.blob_sha256, 'clean', i.repo_id, i.size, false, now() FROM items i JOIN repositories r ON r.id = i.repo_id
			 WHERE r.key = $1 AND i.path = $2
			ON CONFLICT (sha256) DO UPDATE SET state = 'clean', due = false, scanned_at = now()`, repo, "dist/"+name)
		f.object = w.st.Text(`SELECT object_key FROM blob_copies WHERE sha256 = decode($1, 'hex') AND connection_id = 'default'`, f.sum)
		f.keyID = w.st.Text(`SELECT coalesce(storage_key_id, '') FROM blob_copies WHERE sha256 = decode($1, 'hex') AND connection_id = 'default'`, f.sum)
		if !strings.Contains(f.object, "/keyed/") || f.keyID == "" {
			t.Fatalf("the copy of %s is not an encrypted object with keyed names: %q, key %q", name, f.object, f.keyID)
		}
		return f
	}
	sealedA, sealedB := file("sealed.bin"), file("other.bin")
	stored, err := blobcrypt.StoredSize(size)
	if err != nil {
		t.Fatal(err)
	}

	var (
		amu    sync.Mutex
		values = map[string][]string{} // the handover values the main installation answered, by path
	)
	w.front.SetHold(func(req *http.Request, res *http.Response) {
		if v := res.Header.Get(iface.HeaderHandover); v != "" {
			amu.Lock()
			values[req.URL.Path] = append(values[req.URL.Path], v)
			amu.Unlock()
		}
	})
	valuesOf := func(p string) []string { amu.Lock(); defer amu.Unlock(); return append([]string(nil), values[p]...) }

	w.mustAPI("PATCH", "/api/v1/bastions/partners", http.StatusOK, map[string]any{"handover": true},
		"Content-Type", "application/merge-patch+json", "If-Match", "*")
	bundle := filepath.Join(w.dir, "bastion-ca-bundle.pem")
	front, err := os.ReadFile(w.front.caFile)
	if err != nil {
		t.Fatal(err)
	}
	stack, err := os.ReadFile(testenv.CAFile(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle, append(append(front, '\n'), stack...), 0o600); err != nil {
		t.Fatal(err)
	}
	var links *handover.Client
	b := w.newBastion(t, bastionOptions{CAFile: bundle, Deps: func(d *engine.Deps) { links = d.Handover }}).wait()
	r := b.reps[0]
	testWait(t, "the Bastion told the handover is on", func() bool {
		return metricValue(t, r.metrics(t), "quader_bastion_handover_enabled") == 1
	})
	const addr = "203.0.113.71"
	call := callOpts{token: w.admin, addr: addr}
	traces := func(f sealedFile) int64 {
		return w.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'bastion.data_key.handed' AND path = $1`, "dist/"+f.name)
	}
	bastionMetric := func(series string) float64 { return metricValue(t, r.metrics(t), series) }
	var handed []byte // the data key the first read was handed, for the search of the logs

	t.Run("R-STOR-095 R-BAS-074 a cold read of an encrypted file is handed over with its data key, traced, and read from the store", func(t *testing.T) {
		got := r.do(t, "GET", sealedA.path, call)
		if got.Status != http.StatusOK || !bytes.Equal(got.Body, sealedA.data) {
			t.Fatalf("%d, %d bytes\n%s", got.Status, len(got.Body), testenv.Tail(r.logs.String(), 20))
		}
		vs := valuesOf(sealedA.path)
		if len(vs) != 1 {
			t.Fatalf("%d handover values for the file, want one\n%s", len(vs), testenv.Tail(w.main.Logs.String(), 20))
		}
		v, err := iface.DecodeHandover(vs[0])
		if err != nil {
			t.Fatal(err)
		}
		handed = v.DataKey.Bytes()
		if !v.Encrypted || !v.DataKey.Present() || v.StoredSize != stored || v.Size != size || v.SHA256 != sealedA.sum || v.Connection != "default" {
			t.Fatalf("the value %v", v)
		}
		for _, rc := range w.front.Records() {
			if rc.Path == sealedA.path && rc.Bytes != 0 {
				t.Fatalf("the main installation's answer to the handed forward carried %d bytes", rc.Bytes)
			}
		}
		object := sealedA.object[strings.LastIndexByte(sealedA.object, '/')+1:]
		if n := relay.sentFor(object); n != stored {
			t.Fatalf("the store sent %d bytes through its public address, want the stored size %d", n, stored)
		}
		if got := metricValue(t, w.main.Metrics(t), `quader_storage_download_handovers_total{bastion="partners",kind="link_and_key"}`); got != 1 {
			t.Fatalf("quader_storage_download_handovers_total{kind=link_and_key} = %v", got)
		}
		if n := traces(sealedA); n != 1 {
			t.Fatalf("%d traces of the data key, want one", n)
		}
		row := w.st.Text(`SELECT concat_ws('|', resource_type, resource_id::text, actor_name, host(source_ip), repository, project, endpoint,
				details->>'bastion', details->>'replica', details->>'sha256', details->>'connection', details->>'storageKeyId',
				(details->>'linkExpiresAt')::timestamptz = $2::timestamptz, details::text)
			FROM audit_event WHERE action = 'bastion.data_key.handed' AND path = $1`, "dist/"+sealedA.name, v.ExpiresAt.UTC().Format(time.RFC3339))
		want := []string{
			"bastion", w.st.Text(`SELECT id::text FROM principal WHERE name = 'partners'`), "ops", addr, testenv.DefaultProject + "/" + repo,
			testenv.DefaultProject, "default", "partners", r.name, sealedA.sum, "default", sealedA.keyID, "t",
		}
		if parts := strings.SplitN(row, "|", len(want)+1); len(parts) != len(want)+1 || strings.Join(parts[:len(want)], "|") != strings.Join(want, "|") {
			t.Fatalf("the trace %q, want %q", row, strings.Join(want, "|"))
		}
		for _, form := range []string{hex.EncodeToString(handed), base64.RawURLEncoding.EncodeToString(handed), base64.StdEncoding.EncodeToString(handed), "X-Amz-"} {
			if strings.Contains(row, form) {
				t.Fatalf("the trace holds the key or the link (%.8s…)", form)
			}
		}
		if bastionMetric(`quader_bastion_handovers_total{outcome="read"}`) != 1 || bastionMetric(`quader_bastion_fill_bytes_total{class="authenticated",source="main"}`) != 0 {
			t.Fatalf("the Bastion's counts:\n%s", grepLines(r.metrics(t), "quader_bastion_handovers_total", "quader_bastion_fill_bytes_total"))
		}
		if links == nil || links.KeysHeld() != 0 {
			t.Fatal("the Bastion holds a data key after the read")
		}
		testWait(t, "the main installation's request line", func() bool {
			for _, line := range strings.Split(w.main.Logs.String(), "\n") {
				if strings.Contains(line, `"request completed"`) && strings.Contains(line, sealedA.path) && strings.Contains(line, `"delivery":"handover"`) &&
					strings.Contains(line, `"response_bytes":0`) {
					return true
				}
			}
			return false
		})
	})

	t.Run("R-BAS-075 R-BAS-076 a body under another data key falls back to the bytes through the main installation, and the key was traced", func(t *testing.T) {
		// The relay answers the second file's object with the first file's stored bytes: the same length, sealed under another key.
		c, _ := w.st.ObjectClient(t)
		body, _, err := c.Get(t.Context(), sealedA.object, nil)
		if err != nil {
			t.Fatal(err)
		}
		other, err := io.ReadAll(body)
		_ = body.Close()
		if err != nil || int64(len(other)) != stored {
			t.Fatalf("the first file's object: %d bytes, %v", len(other), err)
		}
		relay.mu.Lock()
		relay.swap[sealedB.object[strings.LastIndexByte(sealedB.object, '/')+1:]] = other
		relay.mu.Unlock()
		got := r.do(t, "GET", sealedB.path, call)
		if got.Status != http.StatusOK || !bytes.Equal(got.Body, sealedB.data) {
			t.Fatalf("%d, %d bytes\n%s", got.Status, len(got.Body), testenv.Tail(r.logs.String(), 20))
		}
		if vs := valuesOf(sealedB.path); len(vs) != 1 {
			t.Fatalf("%d handover values, want the one before the fallback", len(vs))
		}
		if bastionMetric(`quader_bastion_handovers_total{outcome="fallback"}`) != 1 || bastionMetric(`quader_bastion_fill_bytes_total{class="authenticated",source="main"}`) != size ||
			bastionMetric("quader_bastion_handover_cooldown") != 0 {
			t.Fatalf("the Bastion's counts:\n%s", grepLines(r.metrics(t), "quader_bastion_handover", "quader_bastion_fill_bytes_total"))
		}
		if n := traces(sealedB); n != 1 {
			t.Fatalf("%d traces of the second file's key, want one: the trail names a key whose read fell back", n)
		}
		if links.KeysHeld() != 0 {
			t.Fatal("the Bastion holds a data key after a read that fell back")
		}
	})

	t.Run("R-BAS-076 R-STOR-096 no log line of either side holds a handed key or a usable link", func(t *testing.T) {
		if len(handed) != 32 {
			t.Skip("no key was handed")
		}
		secret := regexp.MustCompile(`X-Amz-(Signature|Credential|Security-Token)=([^&"\s\\]*)`)
		for who, text := range map[string]string{"the Bastion": r.logs.String(), "the main installation": w.main.Logs.String()} {
			for _, form := range []string{hex.EncodeToString(handed), base64.RawURLEncoding.EncodeToString(handed), base64.StdEncoding.EncodeToString(handed)} {
				if strings.Contains(text, form) {
					t.Errorf("a log line of %s holds the handed data key", who)
				}
			}
			for _, m := range secret.FindAllStringSubmatch(text, -1) {
				if m[2] != "[REDACTED]" && m[2] != "%5BREDACTED%5D" {
					t.Errorf("a log line of %s holds X-Amz-%s unredacted", who, m[1])
				}
			}
		}
	})
}

// grepLines returns the lines of a metrics text that start with one of the names.
func grepLines(text string, names ...string) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		for _, n := range names {
			if strings.HasPrefix(line, n) {
				out = append(out, line)
			}
		}
	}
	return strings.Join(out, "\n")
}
