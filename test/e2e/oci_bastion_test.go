//go:build e2e

package e2e

// The client case of R-BAS-060 in the set oci: an image of a few KiB pulled through a Bastion by the clients the set has already, the Docker
// Engine and Podman of the client pods and the kubelet's containerd of the control-plane node, configured with the snippets the client setup
// renders for the Bastion. The integration tier replays the clients' requests (test/integration/bastion); this case shows the real clients:
// Docker's sign-in through the token service, the challenge of the Bastion's own host, a pod's image pull secret, and that a warm pull takes no
// artifact bytes from the main installation. The Bastion is one release of the chart in front of the main installation of the profile oci
// (deploy/dev/bastion.sh install oci: one replica, the volume backend, no Valkey), installed by the case and removed again at its end.
// docs/developer/e2e.md#the-bastion-case-of-the-set-oci describes it, with what it costs.

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const (
	ociBastionReg     = "partners-oci"                 // the registration of the set's Bastion
	ociBastionRelease = "bastion-oci"                  // the release and the fullname of the chart (oci-bastion-chart.yaml)
	ociBastionHost    = "bastion-oci.quader.localhost" // the host of its base URL: the profile oci lists it, the clients and the node resolve it
)

// ociBastionSite is the set's Bastion as the harness of the set bastion describes a Bastion (bastion_env_test.go), so that the registration,
// the token Secret, the readiness and the contacts are written and read the same way.
func ociBastionSite() *basSite {
	return &basSite{variant: "oci", reg: ociBastionReg, release: ociBastionRelease, host: ociBastionHost}
}

// startOCIBastion registers the Bastion at the main installation, writes its token into the Secret the chart reads and starts the install of
// the release in the background; the returned function waits for it, then for its replica to be ready and to have made contact, and for the
// edge to route the Bastion's host to it. A release left by an earlier run (QUADER_E2E_BASTION_KEEP=1) is restarted, so that the new token is
// read at once and no answer of the earlier run is kept. Only the install runs beside the test's goroutine; every failure is reported from
// the test's own.
func startOCIBastion(t *testing.T, fx *ociFixture, s *basSite) func() {
	t.Helper()
	env := &basEnv{adm: fx.adm}
	basScript(t, "infra", "oci")
	_, err := kbTry("get", "deployment", s.release)
	existed := err == nil
	env.registerBastion(t, s)
	done := make(chan error, 1)
	go func() {
		out, err := basScriptTry("install", s.variant)
		if err != nil {
			err = fmt.Errorf("bastion.sh install %s: %w\n%s", s.variant, err, tail(out, 1500))
		}
		done <- err
	}()
	return func() {
		t.Helper()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if existed {
			kb(t, "rollout", "restart", "deployment/"+s.release)
			kb(t, "rollout", "status", "deployment/"+s.release, "--timeout=5m")
		}
		waitBastionReady(t, s, 1)
		env.waitContacts(t, s, 1)
		// HAProxy Ingress takes the new Ingress into its configuration after the pod is ready: until then the host's TLS is the edge's
		// default certificate, which the clients refuse (a first run met it). The version check without a credential is the Bastion's own
		// 401 with the challenge of its own host (R-BAS-051), so it says that the edge reaches the Bastion.
		var last reply
		waitFor(t, "the edge to route "+s.host+" to the Bastion", 2*time.Minute, 500*time.Millisecond, func() bool {
			r, err := s.api(t, "").send(http.MethodGet, "/v2/", nil)
			if err != nil {
				return false
			}
			last = r
			return r.Status == 401 && strings.Contains(r.Header.Get("WWW-Authenticate"), `realm="`+s.baseURL(t)+`/v2/_token"`) &&
				r.Header.Get("Docker-Distribution-API-Version") == "registry/2.0"
		})
		t.Logf("the Bastion answers through the edge: %d, WWW-Authenticate %s", last.Status, last.Header.Get("WWW-Authenticate"))
	}
}

// removeOCIBastion uninstalls the release at the end of the case, without waiting for the pod: in CI the cluster goes with the leg, locally the
// next run installs a release of its own. QUADER_E2E_BASTION_KEEP=1 keeps it.
func removeOCIBastion(t testing.TB, s *basSite) {
	if keepBastions() {
		return
	}
	out, err := exec.Command("helm", "--kube-context", cfg.kubeContext, "-n", basNS(), "uninstall", s.release).CombinedOutput()
	if err != nil {
		t.Logf("helm uninstall %s: %v\n%s", s.release, err, out)
	}
}

// ociBastionSetup returns the snippets the client setup renders for a client of the repository on the Bastion's address, by id, and fails when
// one of them names another address than the Bastion's image prefix or when a publish snippet is among them (a Bastion is read-only, R-BAS-007).
func (fx *ociFixture) ociBastionSetup(t testing.TB, key, client, name, tag, prefix string) map[string]string {
	t.Helper()
	q := url.Values{"client": {client}, "credentials": {"env"}, "package": {name}, "version": {tag}, "bastion": {ociBastionReg}}
	r := fx.adm.get(repoAPI(key)+"/client-setup?"+q.Encode()).want(t, "client setup of "+key+" for "+client+" through the Bastion", 200)
	var setup struct {
		RegistryURL string `json:"registryUrl"`
		Snippets    []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
		} `json:"snippets"`
	}
	r.JSON(t, &setup)
	out := map[string]string{}
	for _, sn := range setup.Snippets {
		if strings.Contains(sn.ID, "publish") {
			t.Errorf("the client setup for the Bastion holds the publish snippet %s:\n%s", sn.ID, sn.Content)
		}
		out[sn.ID] = sn.Content
	}
	if want := "https://" + prefix[:strings.Index(prefix, "/")] + "/v2/" + repoRef(key); strings.TrimSuffix(setup.RegistryURL, "/") != want {
		t.Errorf("the client setup for the Bastion names the registry %q, want %q", setup.RegistryURL, want)
	}
	return out
}

// ociBastionReqs keeps the requests of the main installation's request log that a pod of the Bastion sent after the snapshot before, the
// interface's own calls left out. The pods' addresses are remembered by basPods.
func ociBastionReqs(t testing.TB, s *basSite, all, before map[string]mainReq) []mainReq {
	t.Helper()
	basPods(t, s)
	var out []mainReq
	for id, r := range all {
		if _, old := before[id]; old {
			continue
		}
		if _, ok := basIPs.Load(r.Peer); ok && !strings.HasPrefix(r.Path, "/api/v1/bastion") {
			out = append(out, r)
		}
	}
	return out
}

// ociArtifact says whether a request of the main installation's log is for an artifact of the registry: a blob or a manifest by its digest,
// the `Download` routes a Bastion keeps under their content address (R-BAS-053). A manifest by tag is a document.
func ociArtifact(r mainReq) bool {
	p := r.Path
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	return strings.HasPrefix(p, "/v2/") && (strings.Contains(p, "/blobs/sha256:") || strings.Contains(p, "/manifests/sha256:"))
}

// ociBastionWindow is what the Bastion asked the main installation during one step of the case.
type ociBastionWindow struct {
	t      testing.TB
	s      *basSite
	before map[string]mainReq
}

func openOCIBastionWindow(t testing.TB, s *basSite) *ociBastionWindow {
	t.Helper()
	return &ociBastionWindow{t: t, s: s, before: mainLog(t)}
}

// done returns the Bastion's requests since the window opened; the log lines of the last requests are written within the pause.
func (w *ociBastionWindow) done() []mainReq {
	w.t.Helper()
	time.Sleep(1500 * time.Millisecond)
	return ociBastionReqs(w.t, w.s, mainLog(w.t), w.before)
}

// noArtifactBytes fails when the main installation sent the Bastion an artifact's bytes in the window: a warm pull is served from the
// Bastion's cache (R-BAS-053). It logs the conditional requests answered 304, which a registry token's package answer spares (R-BAS-085).
func noArtifactBytes(t testing.TB, what string, reqs []mainReq) {
	t.Helper()
	conditional := 0
	for _, r := range reqs {
		if !ociArtifact(r) {
			continue
		}
		if r.Status == 200 || r.Status == 206 || (r.Method == "GET" && r.RespBytes > 0) {
			t.Errorf("%s: the main installation sent the Bastion an artifact: %s %s %d, %d bytes", what, r.Method, r.Path, r.Status, r.RespBytes)
		}
		if r.Status == 304 {
			conditional++
		}
	}
	if t.Failed() {
		t.Logf("what the Bastion asked the main installation:\n%s", describeReqs(reqs))
	}
	t.Logf("%s: %d requests of the Bastion at the main installation, %d of them conditional requests for an artifact answered 304", what, len(reqs), conditional)
}

// noArtifactRequest fails when a warm pull asked the main installation anything about an artifact, a manifest by digest or a blob: the
// registry token's package answer on the image, which the marking of its first forwarded read or one check gives, serves every artifact of
// the image from the Bastion's cache (R-BAS-085, Q-BAS-271); before, each was a conditional request answered 304. What a client still
// sends is logged: the token step, the tag's HEAD of a client that never GETs the tag (a HEAD miss keeps no entry, R-BAS-021), its first
// HEAD without a credential while the main installation admits anonymous reads (H-1420), the referrers list (Volatile, R-BAS-053) and a
// version check sent with the token.
func noArtifactRequest(t testing.TB, what string, reqs []mainReq) {
	t.Helper()
	var artifacts []mainReq
	for _, r := range reqs {
		if ociArtifact(r) {
			artifacts = append(artifacts, r)
		}
	}
	if len(artifacts) > 0 {
		t.Errorf("%s asked the main installation %d times about an artifact, want never:\n%s", what, len(artifacts), describeReqs(artifacts))
	}
}

// ociFeedQuiet is how long after the Bastion received the set-up's last feed entry (a push's claim is a content entry, a policy a grants
// entry) the cold pull starts: an entry received less than the skew margin of R-BAS-020 (15 s) before a cached entry was validated makes it
// stale, and the warm pulls would revalidate what they read whatever answers they hold. A replica that starts reads the feed's lookback at
// once, so the set-up's entries are received when the replica is ready, not when they were written; the second beyond the margin is for
// that first read after the replica's contact.
const ociFeedQuiet = 16 * time.Second

// ociBastionCacheBytes is the Bastion's quader_bastion_bytes_served_total{source="cache"} over its ready replicas.
func ociBastionCacheBytes(t testing.TB, s *basSite) float64 {
	t.Helper()
	return basSum(t, s, "quader_bastion_bytes_served_total", `source="cache"`)
}

// TestOCI_R_BAS_051_ThroughABastion: an image of two layers of a few KiB, pushed to the main installation, is pulled through the Bastion with
// the snippets of the client setup for the Bastion: Docker signs in and pulls it cold, the main installation sending each artifact once, and
// warm, the main installation sending no artifact's bytes and being asked nothing about an artifact, since the registry token's package
// answer serves them; Podman and a Kubernetes pod with the image pull secret of the snippet pull it warm the same way (R-BAS-050 to
// R-BAS-053, R-BAS-060, R-BAS-085, Q-BAS-271).
func TestOCI_R_BAS_051_ThroughABastion(t *testing.T) {
	fx := ociFx(t)
	start := time.Now()
	s := ociBastionSite()
	t.Cleanup(func() { removeOCIBastion(t, s) })

	// The image is pushed before the release's install begins: the install adds the Bastion's Ingress, at which HAProxy Ingress reloads, and
	// the old worker closes the connections it keeps alive. A push running beside the install reused one of them for its manifest and failed
	// with "use of closed network connection" (Test and Release 45222735716: the reload at 17:29:30.645, the PUT of the manifest after it),
	// and the client did not repeat the PUT. The overlap saved the push's few seconds.
	pub := fx.client(t, 0, ociDocker, "bastion-push")
	pub.login(ociRegistryHost(t))
	name := fx.name("bastion")
	ref := fx.image(t, ociLocalKey, name, "1")
	payload := func(n int) []byte {
		b := make([]byte, n/2)
		_, _ = rand.Read(b)
		return []byte(hex.EncodeToString(b))
	}
	pub.write("ctx-bastion/a", payload(3072))
	pub.write("ctx-bastion/b", payload(5120))
	pub.write("ctx-bastion/Dockerfile", []byte("FROM scratch\nCOPY a /a\nCOPY b /b\n"))
	pub.exec("build", "--tag", ref, "ctx-bastion")
	pub.push(ref)
	digest := fx.digestOf(t, ociLocalKey, name, "1")
	pushed := time.Since(start)
	ready := startOCIBastion(t, fx, s)
	ready()
	installTook := time.Since(start)

	prefix := ociBastionHost + ":" + edgePort(t) + "/" + repoRef(ociLocalKey)
	bref := prefix + "/" + name + ":1"
	docker := fx.ociBastionSetup(t, ociLocalKey, ociDocker, name, "1", prefix)
	podman := fx.ociBastionSetup(t, ociLocalKey, ociPodman, name, "1", prefix)
	for id, content := range map[string]string{
		"docker.configure.shell": docker["docker.configure.shell"], "docker.install": docker["docker.install"],
		"docker.configure.kubernetes": docker["docker.configure.kubernetes"],
		"podman.configure.shell":      podman["podman.configure.shell"], "podman.install": podman["podman.install"],
	} {
		if !strings.Contains(content, prefix) {
			t.Fatalf("the client setup for the Bastion has no snippet %s naming the image prefix %s:\n%s", id, prefix, content)
		}
	}
	// run and prune are called inside the subtests with the subtest's own t, and the client's commands report to it
	run := func(t *testing.T, c *ociClient, id, snippet string) {
		t.Helper()
		c.t = t
		if out, err := c.shell(snippet); err != nil {
			t.Fatalf("snippet %s through the Bastion failed: %v\n%s\n%s", id, err, snippet, tailLines(out, 25))
		}
	}
	prune := func(t *testing.T) { ociClientExec(t, 1, ociDocker, "docker", "system", "prune", "--all", "--force") }
	steps := map[string]time.Duration{}
	c := fx.client(t, 1, ociDocker, "bastion")
	// The push was the set-up's last feed entry (the registration's came before it), and the replica received it at the latest when it
	// was ready: the cold pull fills the cache once that receipt is older than the skew margin, so that what the warm pulls ask is decided
	// by their answers alone.
	if quiet := time.Until(start.Add(max(pushed, installTook)).Add(ociFeedQuiet)); quiet > 0 {
		time.Sleep(quiet)
		steps["quiet"] = quiet
	}

	var fills map[string]int64 // the artifacts the main installation sent at the cold pull, with their bytes
	t.Run("R-BAS-060 R-BAS-052 docker login and a cold docker pull through the Bastion: the main installation sends each artifact once", func(t *testing.T) {
		began := time.Now()
		prune(t)
		w := openOCIBastionWindow(t, s)
		run(t, c, "docker.configure.shell", docker["docker.configure.shell"])
		run(t, c, "docker.install", docker["docker.install"])
		if !c.holds(bref, digest) {
			t.Fatalf("docker holds %q after the pull through the Bastion, want the digest %s of the push", c.digests(bref), digest)
		}
		reqs := w.done()
		fills = map[string]int64{}
		for _, r := range reqs {
			if !ociArtifact(r) || r.Status != 200 {
				continue
			}
			if _, twice := fills[r.Path]; twice {
				t.Errorf("the main installation sent the artifact %s twice for one cold pull", r.Path)
			}
			fills[r.Path] = r.RespBytes
		}
		if len(fills) < 3 {
			t.Errorf("the main installation sent %d artifacts for the cold pull, want at least the manifest, the config and two layers:\n%s", len(fills), describeReqs(reqs))
		}
		steps["cold"] = time.Since(began)
	})

	t.Run("R-BAS-060 R-BAS-053 a warm docker pull through the Bastion takes no artifact bytes from the main installation", func(t *testing.T) {
		if fills == nil {
			t.Skip("the cold pull failed")
		}
		began := time.Now()
		prune(t)
		cache := ociBastionCacheBytes(t, s)
		w := openOCIBastionWindow(t, s)
		run(t, c, "docker.install", docker["docker.install"])
		if !c.holds(bref, digest) {
			t.Fatalf("docker holds %q after the warm pull through the Bastion, want the digest %s", c.digests(bref), digest)
		}
		reqs := w.done()
		noArtifactBytes(t, "the warm docker pull", reqs)
		noArtifactRequest(t, "the warm docker pull", reqs)
		var blobs int64
		for p, n := range fills {
			if strings.Contains(p, "/blobs/") {
				blobs += n
			}
		}
		if rose := ociBastionCacheBytes(t, s) - cache; rose < float64(blobs) {
			t.Errorf("quader_bastion_bytes_served_total{source=\"cache\"} rose by %.0f on the warm pull, want at least the %d bytes of the blobs the cold pull filled", rose, blobs)
		}
		steps["warm"] = time.Since(began)
	})

	t.Run("R-BAS-060 a podman pull through the Bastion takes no artifact bytes from the main installation", func(t *testing.T) {
		began := time.Now()
		p := fx.client(t, 1, ociPodman, "bastion")
		ociClientExec(t, 1, ociPodman, append(append([]string{"podman"}, podmanFlags...), "system", "prune", "--all", "--force")...)
		w := openOCIBastionWindow(t, s)
		run(t, p, "podman.configure.shell", podman["podman.configure.shell"])
		run(t, p, "podman.install", podman["podman.install"])
		if !p.holds(bref, digest) {
			t.Fatalf("podman holds %q after the pull through the Bastion, want the digest %s", p.digests(bref), digest)
		}
		reqs := w.done()
		noArtifactBytes(t, "the podman pull", reqs)
		noArtifactRequest(t, "the podman pull", reqs)
		steps["podman"] = time.Since(began)
	})

	t.Run("R-BAS-060 a Kubernetes pod with the image pull secret of the snippet pulls its image through the Bastion", func(t *testing.T) {
		began := time.Now()
		node := os.Getenv("QUADER_E2E_OCI_NODE")
		if node == "" {
			t.Fatal("QUADER_E2E_OCI_NODE is not set; run `deploy/dev/e2e.sh oci`")
		}
		_, _ = ociKubectl(t, time.Minute, "", "delete", "secret", "quader-registry", "--ignore-not-found")
		cmd := exec.Command("sh", "-c", docker["docker.configure.kubernetes"])
		cmd.Env = append(os.Environ(), "KUBECONFIG="+ociKubeconfig(t), "QUADER_TOKEN="+fx.token)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("the Kubernetes snippet for the Bastion failed: %v\n%s", err, out)
		}
		defer ociKubectl(t, time.Minute, "", "delete", "secret", "quader-registry", "--ignore-not-found")

		// The image holds two files and no program: the kubelet pulls it and then cannot start a container, which is not this case's
		// question. The pull is the kubelet's event Pulled and the image the node's containerd holds.
		pod := "oci-bastion-" + fx.run
		w := openOCIBastionWindow(t, s)
		if out, err := ociKubectl(t, time.Minute, ociPodManifest(pod, bref, "quader-registry"), "apply", "-f", "-"); err != nil {
			t.Fatalf("create the pod: %v\n%s", err, out)
		}
		defer ociKubectl(t, time.Minute, "", "delete", "pod", pod, "--ignore-not-found", "--wait=false")
		var failed string
		waitFor(t, "the kubelet to pull the pod's image through the Bastion", 3*time.Minute, time.Second, func() bool {
			out, _ := ociKubectl(t, time.Minute, "", "get", "events", "--field-selector", "involvedObject.name="+pod,
				"-o", "jsonpath={range .items[*]}{.reason}{\"\\t\"}{.message}{\"\\n\"}{end}")
			for _, line := range strings.Split(out, "\n") {
				reason, msg, _ := strings.Cut(line, "\t")
				switch {
				case reason == "Pulled":
					return true
				case reason == "Failed" && strings.Contains(msg, "pull"):
					failed = msg
					return true
				}
			}
			return false
		})
		if failed != "" {
			t.Fatalf("the kubelet did not pull %s through the Bastion: %s", bref, failed)
		}
		out, err := exec.Command("docker", "exec", node, "crictl", "inspecti", "-o", "go-template", "--template", "{{range .status.repoDigests}}{{.}} {{end}}", bref).CombinedOutput()
		if err != nil || !strings.Contains(string(out), strings.TrimPrefix(digest, "sha256:")) {
			t.Fatalf("the node's containerd holds %q for %s (%v), want the digest %s", out, bref, err, digest)
		}
		reqs := w.done()
		noArtifactBytes(t, "the kubelet's pull", reqs)
		noArtifactRequest(t, "the kubelet's pull", reqs)
		steps["kubelet"] = time.Since(began)
	})

	t.Logf("the case took %s after the set's fixture: the Bastion's release ready after %s (the image built and pushed beside it after %s), the wait for the feed %s, cold pull %s, warm pull %s, podman %s, kubelet %s",
		time.Since(start).Round(100*time.Millisecond), installTook.Round(100*time.Millisecond), pushed.Round(100*time.Millisecond),
		steps["quiet"].Round(100*time.Millisecond), steps["cold"].Round(100*time.Millisecond), steps["warm"].Round(100*time.Millisecond),
		steps["podman"].Round(100*time.Millisecond), steps["kubelet"].Round(100*time.Millisecond))
}
