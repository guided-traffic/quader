//go:build integration

package bastion

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// Container images through a Bastion (bastion.md R-BAS-051, R-BAS-053, R-BAS-057, R-BAS-059): the requests of a docker login and pull
// and of containerd's pull, replayed by Go's client through an in-process Bastion against the real main installation. An image is a
// config and one layer of 1 KiB each, pushed through the main installation's /v2/.

const (
	ociLocal     = testenv.DefaultProject + "/docker-local"
	manifestType = "application/vnd.oci.image.manifest.v1+json"
	accept       = "application/vnd.oci.image.manifest.v1+json, application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json"
	challenge    = `Bearer realm="` + baseURL + `/v2/_token",service="` + baseHost + `"`
)

func digestOf(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

// ociImage is an image of one config and one layer.
type ociImage struct {
	config, layer, manifest []byte
}

func newOCIImage(seed string) ociImage {
	cfg := []byte(`{"architecture":"amd64","os":"linux","seed":"` + seed + `","pad":"` + strings.Repeat("c", 900) + `"}`)
	layer := []byte("layer of " + seed + strings.Repeat(".", 1024))
	m, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": manifestType,
		"config": map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": digestOf(cfg), "size": len(cfg)},
		"layers": []any{map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar", "digest": digestOf(layer), "size": len(layer)}},
	})
	return ociImage{config: cfg, layer: layer, manifest: m}
}

// ociWorld is the world with the local OCI repository web/docker-local, which acme-full reads.
func ociWorld(t *testing.T, o worldOptions) *world {
	t.Helper()
	seed := o.Seed
	o.Seed = func(w *world) {
		w.st.Repository(ociLocal, "local", "oci", "{}")
		w.st.Policy("oci-read", []string{"read"}, []string{ociLocal}, testenv.Person{ID: w.full.pid, Name: "acme-full", Kind: "service_account"})
		if seed != nil {
			seed(w)
		}
	}
	return newWorld(t, o)
}

// pushImage pushes the image's blobs and its manifest with the tags to web/docker-local/<name> through the main installation's /v2/ as
// the administrator.
func (w *world) pushImage(name string, img ociImage, tags ...string) {
	w.t.Helper()
	base := "/v2/" + ociLocal + "/" + name
	for _, b := range [][]byte{img.config, img.layer} {
		if a := w.api("POST", base+"/blobs/uploads/?digest="+digestOf(b), w.admin, b, "Content-Type", "application/octet-stream"); a.Status != http.StatusCreated {
			w.t.Fatalf("blob upload: %d %s", a.Status, a.Body)
		}
	}
	for _, tag := range tags {
		if a := w.api("PUT", base+"/manifests/"+tag, w.admin, img.manifest, "Content-Type", manifestType); a.Status != http.StatusCreated {
			w.t.Fatalf("manifest push %s: %d %s", tag, a.Status, a.Body)
		}
	}
	w.setUpOlderThanTheFeed()
}

// pull is what one pull through the Bastion saw.
type pull struct {
	failed []string
	bytes  map[string][]byte
}

func (p *pull) want(t *testing.T, step string, r reply, status int) reply {
	t.Helper()
	if r.Status != status {
		p.failed = append(p.failed, step)
		t.Errorf("%s: %d, want %d: %s %v", step, r.Status, status, r.Body, r.Header)
	}
	return r
}

// registryToken reads the token of a token service's answer.
func registryToken(t *testing.T, r reply) string {
	t.Helper()
	var a struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(r.Body, &a); err != nil || !strings.HasPrefix(a.Token, "qdr_rgt_") {
		t.Fatalf("no registry token in %d %s", r.Status, r.Body)
	}
	return a.Token
}

// dockerPull is `docker login` and `docker pull <bastion>/web/docker-local/<name>:<tag>` as the requests go: the version check without
// a credential, the token by GET with Basic, the manifest by tag, the manifest by digest, the config and the layer.
func dockerPull(t *testing.T, r *replica, quaderToken, name, tag string, img ociImage) pull {
	t.Helper()
	p := pull{bytes: map[string][]byte{}}
	ping := p.want(t, "ping", r.do(t, "GET", "/v2/", callOpts{}), http.StatusUnauthorized)
	if got := ping.Header.Get("Www-Authenticate"); got != challenge || ping.Header.Get("Docker-Distribution-Api-Version") != "registry/2.0" {
		t.Errorf("the challenge of the version check: %q %v", got, ping.Header)
	}
	scope := url.QueryEscape("repository:" + ociLocal + "/" + name + ":pull")
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("acme-full:"+quaderToken))
	tok := registryToken(t, p.want(t, "token", r.do(t, "GET", "/v2/_token?scope="+scope+"&service="+baseHost, callOpts{header: []string{"Authorization", basic}}), http.StatusOK))
	base := "/v2/" + ociLocal + "/" + name
	bearer := []string{"Authorization", "Bearer " + tok, "Accept", accept}
	m := p.want(t, "manifest by tag", r.do(t, "GET", base+"/manifests/"+tag, callOpts{header: bearer}), http.StatusOK)
	if m.Header.Get("Docker-Content-Digest") != digestOf(img.manifest) {
		t.Errorf("the manifest by tag carries Docker-Content-Digest %q", m.Header.Get("Docker-Content-Digest"))
	}
	p.bytes["tag"] = m.Body
	p.bytes["manifest"] = p.want(t, "manifest by digest", r.do(t, "GET", base+"/manifests/"+digestOf(img.manifest), callOpts{header: bearer}), http.StatusOK).Body
	p.bytes["config"] = p.want(t, "config", r.do(t, "GET", base+"/blobs/"+digestOf(img.config), callOpts{header: bearer}), http.StatusOK).Body
	p.bytes["layer"] = p.want(t, "layer", r.do(t, "GET", base+"/blobs/"+digestOf(img.layer), callOpts{header: bearer}), http.StatusOK).Body
	return p
}

// containerdPull is containerd's pull as the requests go: HEAD of the tag without a credential (the challenge), the token by POST with
// the form, HEAD of the tag with the token for its digest, the manifest by digest, the config and the layer.
func containerdPull(t *testing.T, r *replica, quaderToken, name, tag string, img ociImage) pull {
	t.Helper()
	p := pull{bytes: map[string][]byte{}}
	base := "/v2/" + ociLocal + "/" + name
	head := p.want(t, "HEAD of the tag without a credential", r.do(t, "HEAD", base+"/manifests/"+tag, callOpts{header: []string{"Accept", accept}}), http.StatusUnauthorized)
	if !strings.HasPrefix(head.Header.Get("Www-Authenticate"), `Bearer realm="`+baseURL+`/v2/_token"`) {
		t.Errorf("the challenge of the HEAD: %q", head.Header.Get("Www-Authenticate"))
	}
	form := url.Values{
		"grant_type": {"password"}, "username": {"acme-full"}, "password": {quaderToken}, "service": {baseHost},
		"scope": {"repository:" + ociLocal + "/" + name + ":pull"}, "client_id": {"containerd-client"},
	}
	tok := registryToken(t, p.want(t, "token by POST", r.do(t, "POST", "/v2/_token", callOpts{
		body:   []byte(form.Encode()),
		header: []string{"Content-Type", "application/x-www-form-urlencoded"},
	}), http.StatusOK))
	bearer := []string{"Authorization", "Bearer " + tok, "Accept", accept}
	h := p.want(t, "HEAD of the tag", r.do(t, "HEAD", base+"/manifests/"+tag, callOpts{header: bearer}), http.StatusOK)
	if h.Header.Get("Docker-Content-Digest") != digestOf(img.manifest) {
		t.Errorf("the HEAD of the tag carries Docker-Content-Digest %q", h.Header.Get("Docker-Content-Digest"))
	}
	p.bytes["manifest"] = p.want(t, "manifest by digest", r.do(t, "GET", base+"/manifests/"+digestOf(img.manifest), callOpts{header: bearer}), http.StatusOK).Body
	p.bytes["config"] = p.want(t, "config", r.do(t, "GET", base+"/blobs/"+digestOf(img.config), callOpts{header: bearer}), http.StatusOK).Body
	p.bytes["layer"] = p.want(t, "layer", r.do(t, "GET", base+"/blobs/"+digestOf(img.layer), callOpts{header: bearer}), http.StatusOK).Body
	return p
}

func (p pull) check(t *testing.T, img ociImage) {
	t.Helper()
	for name, want := range map[string][]byte{"manifest": img.manifest, "config": img.config, "layer": img.layer} {
		if got := p.bytes[name]; string(got) != string(want) {
			t.Errorf("%s: %d bytes, want the pushed %d", name, len(got), len(want))
		}
	}
}

// conditionals are the forwarded reads of an artifact after a mark that carried the Bastion's own If-None-Match.
func conditionals(f *front, mark int) []rec {
	var out []rec
	for _, r := range f.Forwards(mark, "/v2/") {
		if r.Header.Get("If-None-Match") != "" && isArtifactPath(r.Path) {
			out = append(out, r)
		}
	}
	return out
}

// besidesTheToken returns what a pull cost the main installation after a mark besides its token step (the token request): the forwarded reads and the checks. The Bastion's own polling (the feed, the handshake, the downloads) is
// no cost of a pull.
func besidesTheToken(f *front, mark int) []rec {
	var out []rec
	for _, r := range f.Since(mark) {
		if (r.Kind == "forward" || r.Kind == "check") && r.Path != "/v2/_token" {
			out = append(out, r)
		}
	}
	return out
}

// TestR_BAS_051_R_BAS_053_ADockerAndAContainerdPullThroughABastion: docker's and containerd's pulls succeed through a Bastion cold and
// warm. The cold pull fills the manifest by digest and both blobs; a warm pull with a new registry token costs the main installation one
// check besides its token step and no conditional request: the token's package answer on the image, which the check gives, serves the
// tag's document, the manifest and both blobs from the cache (R-BAS-085). A HEAD of the tag answered from the cache for a Quader token
// presented directly carries Docker-Content-Digest, and a tags list of three tags with n=2 served from the cache carries its Link. A write
// below /v2/ is refused in the distribution's shape with the main installation's image prefix. About 4 s, an image of two blobs of 1 KiB.
func TestR_BAS_051_R_BAS_053_ADockerAndAContainerdPullThroughABastion(t *testing.T) {
	w := ociWorld(t, worldOptions{})
	img := newOCIImage("one")
	w.pushImage("app", img, "1", "2", "3")
	b := w.newBastion(t, bastionOptions{Configure: relaxed}).wait()
	r := b.reps[0]

	m := w.front.Mark()
	dockerPull(t, r, w.full.token, "app", "1", img).check(t, img)
	if got := len(w.front.Artifacts(m)); got != 3 {
		t.Errorf("the cold docker pull transferred %d artifacts, want the manifest by digest and two blobs", got)
	}
	for _, kind := range []string{"docker", "containerd"} {
		// A registry token names the second it was issued in: a pull in the same second gets the same token, whose URL answers the
		// Bastion holds. A new token is a new caller.
		time.Sleep(1100 * time.Millisecond)
		m = w.front.Mark()
		var p pull
		if kind == "docker" {
			p = dockerPull(t, r, w.full.token, "app", "1", img)
		} else {
			p = containerdPull(t, r, w.full.token, "app", "1", img)
		}
		p.check(t, img)
		if got := w.front.Artifacts(m); len(got) != 0 {
			t.Errorf("a warm %s pull transferred %d artifacts: %+v", kind, len(got), got)
		}
		if got := len(conditionals(w.front, m)); got != 0 {
			t.Errorf("a warm %s pull sent %d conditional requests, want none", kind, got)
		}
		if got := besidesTheToken(w.front, m); len(got) != 1 || got[0].Kind != "check" {
			t.Errorf("a warm %s pull cost the main installation %d requests besides the token step, want one check: %+v", kind, len(got), got)
		}
	}

	direct := callOpts{header: []string{"Authorization", "Bearer " + w.full.token, "Accept", accept}}
	tag := "/v2/" + ociLocal + "/app/manifests/2"
	if got := r.do(t, "GET", tag, direct); got.Status != http.StatusOK {
		t.Fatalf("the tag with a Quader token: %d %s", got.Status, got.Body)
	}
	tags := "/v2/" + ociLocal + "/app/tags/list?n=2"
	if got := r.do(t, "GET", tags, direct); got.Status != http.StatusOK || !strings.Contains(got.Header.Get("Link"), `rel="next"`) {
		t.Fatalf("the tags list cold: %d %v", got.Status, got.Header)
	}
	m = w.front.Mark()
	h := r.do(t, "HEAD", tag, direct)
	if h.Status != http.StatusOK || h.Header.Get("Docker-Content-Digest") != digestOf(img.manifest) {
		t.Errorf("HEAD of the tag from the cache: %d %v", h.Status, h.Header)
	}
	l := r.do(t, "GET", tags, direct)
	if l.Status != http.StatusOK || !strings.Contains(l.Header.Get("Link"), `rel="next"`) {
		t.Errorf("the tags list from the cache: %d %v", l.Status, l.Header)
	}
	if c := w.front.Cost(m); c != 0 {
		t.Errorf("the warm HEAD and tags list cost the main installation %d requests", c)
	}

	wr := r.do(t, "POST", "/v2/"+ociLocal+"/app/blobs/uploads/", callOpts{header: []string{"Authorization", "Bearer " + w.full.token}})
	if wr.Status != http.StatusForbidden || !strings.Contains(string(wr.Body), `"DENIED"`) || !strings.Contains(string(wr.Body), readOnlyText) {
		t.Errorf("a push through the Bastion: %d %s", wr.Status, wr.Body)
	}
}

// TestR_BAS_053_AWithdrawnImageIsRefusedWithinTheFeedsBound: an image served warm through a Bastion is refused there within the feed's
// bound once its manifest is deleted at the main installation (oci.manifest.deleted is a content entry, Q-BAS-175). The block of the
// vulnerability scanning reaches the Bastion through the same entry kind (R-BAS-015's table); its own case needs the scanning's
// findings, which the stack of this suite does not hold.
func TestR_BAS_053_AWithdrawnImageIsRefusedWithinTheFeedsBound(t *testing.T) {
	w := ociWorld(t, worldOptions{})
	img := newOCIImage("withdrawn")
	w.pushImage("gone", img, "1")
	b := w.newBastion(t, bastionOptions{Configure: relaxed}).wait()
	r := b.reps[0]
	direct := callOpts{header: []string{"Authorization", "Bearer " + w.full.token, "Accept", accept}}
	byDigest := "/v2/" + ociLocal + "/gone/manifests/" + digestOf(img.manifest)
	for i := 0; i < 2; i++ {
		if got := r.do(t, "GET", byDigest, direct); got.Status != http.StatusOK {
			t.Fatalf("read %d: %d", i, got.Status)
		}
	}
	if a := w.api("DELETE", byDigest, w.admin, nil); a.Status != http.StatusAccepted {
		t.Fatalf("delete the manifest: %d %s", a.Status, a.Body)
	}
	start := time.Now()
	testWait(t, "the deleted manifest refused through the Bastion", func() bool {
		return r.do(t, "GET", byDigest, direct).Status == http.StatusNotFound
	})
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("refused after %v, beyond the bound of 5 s", d)
	}
}

// TestR_BAS_057_ABoundHostThroughABastion: a Bastion whose host the main installation's default endpoint binds to web/docker-local serves
// /v2/<name>/… from that repository, and a path that names a project and a key is an image name of the bound repository too, as at the
// main installation (R-OCI-029, Q-BAS-195). The handshake names the binding. About 2 s.
func TestR_BAS_057_ABoundHostThroughABastion(t *testing.T) {
	// The Bastion's address among its registration's networks, as the manual sets it up (R-BAS-017, Q-BAS-334): the re-derivation of
	// a recognized Bastion request reads the binding of the Bastion's host.
	w := ociWorld(t, worldOptions{
		Endpoints: `{"default":{"host_roots":{"` + baseHost + `":"` + ociLocal + `"}}}`,
		Egress:    []string{"127.0.0.1/32"},
	})
	img := newOCIImage("bound")
	w.pushImage("library/alpine", img, "3")
	b := w.newBastion(t, bastionOptions{Configure: relaxed}).wait()
	r := b.reps[0]
	direct := callOpts{header: []string{"Authorization", "Bearer " + w.full.token, "Accept", accept}}
	got := r.do(t, "GET", "/v2/library/alpine/manifests/3", direct)
	if got.Status != http.StatusOK || string(got.Body) != string(img.manifest) {
		t.Fatalf("the bound name: %d %s", got.Status, got.Body)
	}
	if got := r.do(t, "GET", "/v2/library/alpine/blobs/"+digestOf(img.layer), direct); got.Status != http.StatusOK || string(got.Body) != string(img.layer) {
		t.Fatalf("a blob of the bound name: %d", got.Status)
	}
	// /v2/web/docker-local/library/alpine/… names the image web/docker-local/library/alpine of the bound repository, which it does not hold.
	if got := r.do(t, "GET", "/v2/"+ociLocal+"/library/alpine/manifests/3", direct); got.Status != http.StatusNotFound {
		t.Fatalf("a project and key on the bound host: %d %s", got.Status, got.Body)
	}
}

// TestR_BAS_059_AHitIsReportedWhenTheMainInstallationCountedItsFill: a warm pull by tag through the Bastion is reported as the main
// installation's own audit trail would record it: the tag's document and the two blobs, nothing for a HEAD of the tag; a warm npm read
// still reports its tarball and no package document. About 4 s.
func TestR_BAS_059_AHitIsReportedWhenTheMainInstallationCountedItsFill(t *testing.T) {
	w := ociWorld(t, worldOptions{})
	img := newOCIImage("counted")
	w.pushImage("app", img, "1")
	w.publishNPM("counted-lib", "1.0.0")
	b := w.newBastion(t, bastionOptions{Configure: relaxed}).wait()
	r := b.reps[0]
	const addr = "203.0.113.59"
	direct := callOpts{addr: addr, header: []string{"Authorization", "Bearer " + w.full.token, "Accept", accept}}
	base := "/v2/" + ociLocal + "/app"
	paths := []string{base + "/manifests/1", base + "/blobs/" + digestOf(img.config), base + "/blobs/" + digestOf(img.layer)}
	npm := callOpts{addr: addr, token: w.full.token}
	for round := 0; round < 2; round++ {
		for _, p := range paths {
			if got := r.do(t, "GET", p, direct); got.Status != http.StatusOK {
				t.Fatalf("round %d %s: %d", round, p, got.Status)
			}
		}
		if s := npmRun(t, r, npmAll, npm, "counted-lib"); !s.ok() {
			t.Fatalf("npm round %d: %v", round, s.failed)
		}
	}
	r.do(t, "HEAD", paths[0], direct)
	reported := func() []string {
		var out []string
		w.st.Query(`SELECT path FROM audit_event WHERE action = 'artifact.downloaded' AND details->>'origin' = 'bastion' AND host(source_ip) = $1 ORDER BY path`,
			func(row pgx.Rows) error {
				var p string
				if err := row.Scan(&p); err != nil {
					return err
				}
				out = append(out, p)
				return nil
			}, addr)
		return out
	}
	deadline := time.Now().Add(20 * time.Second)
	for len(reported()) < 4 && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
	}
	time.Sleep(2 * time.Second) // two flush intervals more: nothing else arrives
	got := reported()
	want := map[string]bool{paths[0]: true, paths[1]: true, paths[2]: true}
	tgz := 0
	for _, p := range got {
		switch {
		case want[p]:
			delete(want, p)
		case strings.HasSuffix(p, ".tgz"):
			tgz++
		default:
			t.Errorf("reported %s, which the main installation does not count", p)
		}
	}
	if len(want) != 0 || tgz != 1 || len(got) != 4 {
		t.Errorf("reported %v, want the tag, the two blobs and the tarball once each", got)
		w.st.Query(`SELECT coalesce(details->>'origin', '-'), path, coalesce(host(source_ip), '-') FROM audit_event WHERE action = 'artifact.downloaded' ORDER BY occurred_at`,
			func(row pgx.Rows) error {
				var o, p, ip string
				if err := row.Scan(&o, &p, &ip); err != nil {
					return err
				}
				t.Logf("download event: origin %s path %s address %s", o, p, ip)
				return nil
			})
	}
}
