//go:build integration

package bastion

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/catalogue"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// The public catalogue at the Bastion (R-BAS-109 to R-BAS-112; W-35's package P7) against the main installation of the suite with
// the role all, which builds the catalogue (P6): two Bastions, one on a bucket and one on a volume, mirror it; a publish reaches
// both within the builder's debounce and a poll; with the link cut they serve on with their last confirmation, a replica of the
// bucket restarted during the cut serves the stored state, one whose volume was removed answers not ready; a level lowered at the
// main installation is hidden at the Bastion soon after the builder's run. Two npm packages of bytes.

// listed reports whether the replica's catalogue lists a package of the name.
func listed(t *testing.T, r *replica, name string) bool {
	t.Helper()
	page, err := r.srv.Catalogue().Search(catalogue.Query{Q: name})
	if err != nil {
		return false
	}
	for _, e := range page.Items {
		if e.Name == name {
			return true
		}
	}
	return false
}

// TestR_BAS_109_TheBastionMirrorsTheCatalogueAndServesOnWhileTheLinkIsCut: the scenario of the package P7 in one world.
func TestR_BAS_109_TheBastionMirrorsTheCatalogueAndServesOnWhileTheLinkIsCut(t *testing.T) {
	w := newWorld(t, worldOptions{
		Role: "all", Egress: []string{"127.0.0.1/32"},
		Switches: func(string) iface.Switches { return iface.Switches{GUI: true, Docs: true} },
	})
	w.anonymousPolicy()
	// A poll of 2 s (the default is 10 s), so the case waits for the builder's debounce more than for the poll.
	gui := "  gui:\n    poll_interval: 2s\n"
	bucket := w.newBastion(t, bastionOptions{Backend: "bucket", CutsLink: true, Settings: gui}).wait()
	volume := w.newBastion(t, bastionOptions{CutsLink: true, Settings: gui}).wait()
	b, v := bucket.reps[0], volume.reps[0]

	// A publish reaches both within the builder's debounce (5 s), its run and a poll.
	start := time.Now()
	w.publishNPM("plain-lib", "1.0.0")
	testenv.WaitFor(t, "plain-lib at both Bastions", 30*time.Second, func() bool { return listed(t, b, "plain-lib") && listed(t, v, "plain-lib") })
	t.Logf("the publish reached both Bastions after %s", time.Since(start).Round(100*time.Millisecond))
	entry, ok, err := b.srv.Catalogue().Package(mustFind(t, b, "plain-lib"))
	if err != nil || !ok {
		t.Fatalf("the package: %v %v", ok, err)
	}
	if d, ok, err := b.srv.Catalogue().Document(context.Background(), iface.CataloguePath{Kind: iface.CatalogueVersion, Package: entry.ID, Version: "1.0.0"}.String()); err != nil || !ok || d.Size == 0 {
		t.Fatalf("the version's document: %v %v", ok, err)
	}
	if st := b.srv.Catalogue().State(); !st.Ready || !st.Kept || v.srv.Catalogue().State().Kept {
		t.Fatalf("the states: bucket %+v, volume %+v", st, v.srv.Catalogue().State())
	}
	walkWebInterface(t, b, entry.ID)
	crawlWebInterface(t, w, b)

	// The link cut: both serve on with the time of their last confirmation.
	w.front.SetDown(true)
	failed := func(r *replica) bool {
		return sumMetric(r.metrics(t), metrics.CatalogueSyncTotal, `outcome="failed"`) > 0
	}
	testenv.WaitFor(t, "a failed round at both Bastions", 20*time.Second, func() bool { return failed(b) && failed(v) })
	confirmed := b.srv.Catalogue().ConfirmedAt()
	time.Sleep(2500 * time.Millisecond) // more than a poll
	if got := b.srv.Catalogue().ConfirmedAt(); !got.Equal(confirmed) || !listed(t, b, "plain-lib") || !listed(t, v, "plain-lib") {
		t.Fatalf("with the link cut: confirmedAt %s (was %s), listed %v %v", got, confirmed, listed(t, b, "plain-lib"), listed(t, v, "plain-lib"))
	}

	// A replica of the bucket restarted during the cut serves the stored state; one whose volume was removed is not ready.
	b.stop(t)
	nb := bucket.start(t, bucket.dir, bucket.o, 1)
	if st := nb.srv.Catalogue().State(); !st.Ready || st.ConfirmedAt.IsZero() || st.ConfirmedAt.After(confirmed) || !listed(t, nb, "plain-lib") {
		t.Fatalf("the bucket's replica restarted during the cut: %+v", st)
	}
	v.stop(t)
	if err := os.RemoveAll(volume.sh.volume); err != nil {
		t.Fatal(err)
	}
	nv := volume.start(t, volume.dir, volume.o, 1)
	if _, _, err := nv.srv.Catalogue().Document(context.Background(), "state"); !errors.Is(err, catalogue.ErrNotReady) {
		t.Fatalf("the volume's replica restarted without its volume: %v", err)
	}

	// The link back: the new replicas confirm; a level lowered to private is hidden soon after the builder's run.
	w.front.SetDown(false)
	testenv.WaitFor(t, "the restarted replicas confirmed", 30*time.Second, func() bool {
		return nb.srv.Catalogue().ConfirmedAt().After(confirmed) && nv.srv.Catalogue().State().Ready && listed(t, nv, "plain-lib")
	})
	start = time.Now()
	w.mustAPI("PUT", "/api/v1/projects/"+testenv.DefaultProject+"/visibility", 200, map[string]any{"level": "private"}, "If-Match", "*")
	testenv.WaitFor(t, "plain-lib hidden at both Bastions", 30*time.Second, func() bool { return !listed(t, nb, "plain-lib") && !listed(t, nv, "plain-lib") })
	t.Logf("the lowered level was hidden at both Bastions after %s", time.Since(start).Round(100*time.Millisecond))
}

// walkWebInterface reads what a visitor's browser reads of the package plain-lib through the web interface's own addresses
// (R-BAS-118, W-35's P9): its page, the search, the lookup, its versions, its version and the state, each answered 200, then the
// version's file through the download link its page carries (R-BAS-107), and scans
// every status line, header and body for the main installation (W-35's B.13): its host, the address the Bastion reaches it at, and
// a path of its management API. The web interface's own `/ui/` paths are the Bastion's and are not scanned for.
func walkWebInterface(t *testing.T, r *replica, id string) {
	t.Helper()
	var names []string
	for _, n := range r.mainNames {
		if !strings.Contains(n, "/ui/") {
			names = append(names, n)
		}
	}
	version := "/ui/data/v1/packages/" + id + "/versions/1.0.0"
	var versionBody []byte
	for _, p := range []string{
		"/ui/en/projects/" + testenv.DefaultProject + "/packages/npm/plain-lib",
		"/ui/data/v1/state", "/ui/data/v1/legal?locale=de",
		"/ui/data/v1/packages?q=plain-lib", "/ui/data/v1/packages/lookup?format=npm&name=plain-lib",
		"/ui/data/v1/packages/" + id, "/ui/data/v1/packages/" + id + "/versions", version,
	} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, r.client+p, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = baseHost
		res, err := clientHTTP.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", p, err)
		}
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d %s", p, res.StatusCode, body)
		}
		if p == version {
			versionBody = body
		}
		var head strings.Builder
		for k, vs := range res.Header {
			head.WriteString(k + ": " + strings.Join(vs, ", ") + "\n")
		}
		for _, n := range names {
			if strings.Contains(head.String(), n) || bytes.Contains(body, []byte(n)) {
				t.Errorf("GET %s: the answer of the web interface names the main installation (%q)", p, n)
			}
		}
	}
	downloadFromVersionPage(t, r, versionBody)
}

// crawlWebInterface reads the Bastion as a crawler that runs no script does (R-BAS-121, R-BAS-122, W-35's B.11): robots.txt names the
// sitemap on the Bastion's base URL, the sitemap lists the package's page, and the page's own answer carries its name, its README and
// its canonical address; none of it names the main installation, and none of it reaches the main installation: the double in front of
// it records nothing but the Bastion's own interface operations (its polling and the catalogue's sync) meanwhile.
func crawlWebInterface(t *testing.T, w *world, r *replica) {
	t.Helper()
	var names []string
	for _, n := range r.mainNames {
		if !strings.Contains(n, "/ui/") {
			names = append(names, n)
		}
	}
	page := "/ui/en/projects/" + testenv.DefaultProject + "/packages/npm/plain-lib"
	mark := w.front.Mark()
	for p, want := range map[string][]string{
		"/robots.txt":    {"Disallow: /api/\n", "Sitemap: " + baseURL + "/sitemap.xml\n"},
		"/sitemap.xml":   {"<loc>" + baseURL + "/sitemap/0.xml</loc>"},
		"/sitemap/0.xml": {"<loc>" + baseURL + page + "</loc>"},
		page: {
			`<title>plain-lib – Quader</title>`, `<link rel="canonical" href="` + baseURL + page + `">`, `<h1>plain-lib</h1>`,
			`<article><h1`, `"@type":"SoftwareApplication"`,
		},
	} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, r.client+p, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = baseHost
		res, err := clientHTTP.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", p, err)
		}
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d %s", p, res.StatusCode, body)
		}
		for _, s := range want {
			if !bytes.Contains(body, []byte(s)) {
				t.Errorf("GET %s lacks %q:\n%s", p, s, body)
			}
		}
		for _, n := range names {
			if bytes.Contains(body, []byte(n)) {
				t.Errorf("GET %s: the answer names the main installation (%q)", p, n)
			}
		}
	}
	for _, rec := range w.front.Since(mark) {
		if !strings.HasPrefix(rec.Path, "/api/v1/bastion/") {
			t.Errorf("a crawler's read reached the main installation: %s %s", rec.Method, rec.Path)
		}
	}
}

// downloadFromVersionPage follows a version page's download link as a visitor's browser does (R-BAS-107, Q-BAS-398): the file's
// contentUrl is the Bastion's package route, never a path of the management API, and it answers the tarball without a credential.
func downloadFromVersionPage(t *testing.T, r *replica, versionDoc []byte) {
	t.Helper()
	var v struct {
		Files []struct {
			ContentURL *string `json:"contentUrl"`
			SHA256     string  `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal(versionDoc, &v); err != nil || len(v.Files) == 0 || v.Files[0].ContentURL == nil {
		t.Fatalf("the version page's files: %v %s", err, versionDoc)
	}
	link := *v.Files[0].ContentURL
	if !strings.HasPrefix(link, "/api/npm/"+testenv.DefaultProject+"/") {
		t.Fatalf("the version page's download link is %q, not the Bastion's npm route", link)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, r.client+link, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = baseHost
	res, err := clientHTTP.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", link, err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if sum := sha256.Sum256(body); res.StatusCode != http.StatusOK || hex.EncodeToString(sum[:]) != v.Files[0].SHA256 {
		t.Fatalf("GET %s = %d, %d bytes of SHA-256 %x, want the file of SHA-256 %s", link, res.StatusCode, len(body), sum, v.Files[0].SHA256)
	}
}

// mustFind is the catalogue ID of a listed package.
func mustFind(t *testing.T, r *replica, name string) string {
	t.Helper()
	page, err := r.srv.Catalogue().Search(catalogue.Query{Q: name})
	if err != nil || len(page.Items) == 0 {
		t.Fatalf("%s is not listed: %v", name, err)
	}
	return page.Items[0].ID
}
