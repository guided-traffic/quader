//go:build e2e

package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestOCILimits_R_LIM_020_WhatDockerAndPodmanShowAtARefusal is the registry's one case of the download limits (R-LIM-039): how docker
// and podman react to a refusal. The figures themselves are the integration set's. Each step has a repository of its own with a rule
// for every technical token (the publisher's token is one): a request figure of one a second, whose 429 carries Retry-After 1, and a
// byte figure of one byte an hour, which refuses the image's content after the manifest of its tag. One image of a few hundred bytes.
//
// docker does not retry a 429 of the registry: it prints the message and exits 1 at once. podman retries a 429, waiting what
// Retry-After says up to 60 s, five times, and the whole pull three times more: at a request figure it succeeds; at a byte figure,
// whose wait is up to an hour, it fails only after about 20 minutes (measured once on 2026-11-20, in the manual's table), which is
// why podman meets the request figure only here.
func TestOCILimits_R_LIM_020_WhatDockerAndPodmanShowAtARefusal(t *testing.T) {
	fx := ociFx(t)
	host := ociRegistryHost(t)
	repo := func(t *testing.T, purpose string, figures map[string]any) string {
		key := "oci-limits-" + purpose + "-" + fx.run
		ociLimitsRepo(t, fx, key, "limits "+purpose)
		limitRule(t, fx.adm, key, "technicalToken", figures)
		return key
	}
	pull := func(t *testing.T, tool, key, figure string) (string, int) {
		c := fx.client(t, 0, tool, "limits-"+figure)
		c.login(host)
		start := time.Now()
		out, err := c.try("pull", fx.image(t, key, "tiny", "1"))
		code := exitOf(err)
		noteRefusal(t, out, clientRefusal{
			Client: tool + " pull", Figure: figure, Printed: firstLineWith(out, "limit of"),
			Exit: code, Seconds: time.Since(start).Seconds(), Retried: tool == ociPodman && code == 0,
		})
		return out, code
	}

	t.Run("R-LIM-020 requestsPerSecond: docker exits 1 with the message, podman waits and succeeds", func(t *testing.T) {
		key := repo(t, "rps", map[string]any{"requestsPerSecond": 1, "requestsBurst": 1})
		if out, code := pull(t, ociDocker, key, "requestsPerSecond"); code != 1 || !strings.Contains(out, "error from registry: The request limit of 1 requests per 1 seconds is used up") {
			t.Errorf("docker pull at one request a second exited %d, want 1 with the limit's message:\n%s", code, tailLines(out, 20))
		}
		if out, code := pull(t, ociPodman, key, "requestsPerSecond"); code != 0 {
			t.Errorf("podman pull at one request a second exited %d, want its retries to succeed:\n%s", code, tailLines(out, 20))
		}
	})

	t.Run("R-LIM-021 cachedBytesPerHour: docker's pull of the image's content is refused with the message", func(t *testing.T) {
		key := repo(t, "bytes", map[string]any{"cachedBytesPerHour": 1})
		// the manifest of a tag is a document (R-LIM-007): its bytes count, and after it the hour is used up
		fx.registry(t).do(http.MethodGet, "/v2/"+repoRef(key)+"/tiny/manifests/1", nil, "Accept", ociManifestAccept).want(t, "the manifest", 200)
		time.Sleep(2 * time.Second) // the bytes reach the window at the next flush
		if out, code := pull(t, ociDocker, key, "cachedBytesPerHour"); code != 1 || !strings.Contains(out, "error from registry: The download limit of 1 bytes per hour is used up") {
			t.Errorf("docker pull with the hour's bytes used up exited %d, want 1 with the limit's message:\n%s", code, tailLines(out, 20))
		}
	})
}

// TestOCILimits_F161_SignInsAndPullsFromOneAddressMeetNoAnonymousBudget is the registry's handshake in Enforce from one address with a
// small anonymous budget: a registry client asks without a credential first, and that challenge is no request of the address
// (Q-API-357, F-161), so ten sign-ins and pulls of a private image all pass; the anonymous pulls of a public repository meet the
// address's budget and docker prints toomanyrequests.
func TestOCILimits_F161_SignInsAndPullsFromOneAddressMeetNoAnonymousBudget(t *testing.T) {
	fx := ociFx(t)
	host := ociRegistryHost(t)
	key := "oci-limits-handshake-" + fx.run
	ociLimitsRepo(t, fx, key, "handshake")
	ip := strings.TrimSpace(ociKubectlMust(t, "get", "pod", ociClientPod(t, 0), "-o", "jsonpath={.status.podIP}"))
	open := fx.image(t, ociOpenKey, fx.name("limits-open"), "1")
	pub := fx.client(t, 0, ociDocker, "limits-open-push")
	pub.login(host)
	pub.build(open, "limits open "+fx.run)
	pushed := time.Now()
	pub.push(open)
	pub.exec("rmi", open)
	// Q-OCI-225: docker pushes into public artifacts without its credential first (a HEAD of each blob, then the upload's POST);
	// the HEADs are reads of the address, the writes are answered with the challenge and are none
	if heads, writes := chargedTo(t, pushed, "anonymous:"+ip); writes != 0 {
		t.Fatalf("the push into a public repository charged %d writes to the address %s (and %d reads), want none", writes, ip, heads)
	} else {
		t.Logf("the push into a public repository charged %d reads and no write to the address %s", heads, ip)
	}
	fx.withAnonymousRead(t)

	r := fx.adm.do(http.MethodPost, "/api/v1/limits/assignments", map[string]any{
		"who": map[string]any{"kind": "address", "prefix": ip + "/32"}, "profile": "anonymous", "overrides": map[string]any{"requestsPerHour": 8},
		"note": "end-to-end: the registry's handshake from one address",
	}).want(t, "the address rule", 201)
	var rule struct {
		ID string `json:"id"`
	}
	r.JSON(t, &rule)
	t.Cleanup(func() { fx.adm.do(http.MethodDelete, "/api/v1/limits/assignments/"+rule.ID, nil) })
	awaitLimitsIndex(t)

	ref := fx.image(t, key, "tiny", "1")
	c := fx.client(t, 0, ociDocker, "limits-handshake")
	start := time.Now()
	for i := range 10 {
		c.exec("logout", host)
		c.login(host)
		if out, err := c.try("pull", ref); err != nil {
			t.Fatalf("sign-in and pull %d from one address exited %d:\n%s", i+1, exitOf(err), tailLines(out, 20))
		}
		c.exec("rmi", ref)
	}
	t.Logf("ten sign-ins and pulls of a private image from %s with eight anonymous requests an hour: all passed in %s", ip, time.Since(start).Round(time.Second))

	c.exec("logout", host)
	var out string
	var err error
	pulls := 0
	for pulls < 8 {
		pulls++
		if out, err = c.try("pull", open); err != nil {
			break
		}
		c.exec("rmi", open)
	}
	noteRefusal(t, out, clientRefusal{
		Client: "docker pull (anonymous)", Figure: "requestsPerHour", Printed: firstLineWith(out, "is used up") + firstLineWith(out, "429 Too Many Requests"),
		Exit: exitOf(err), Retried: strings.Contains(out, "Retrying"),
	})
	// the limit's message when the refused request was a GET; a HEAD answer has no body, and docker names the status only
	if err == nil || (!strings.Contains(out, "is used up") && !strings.Contains(out, "429 Too Many Requests")) {
		t.Fatalf("eight anonymous pulls of a public image passed the address's eight requests an hour; the last printed:\n%s", tailLines(out, 20))
	}
	t.Logf("the anonymous pull %d met the address's budget", pulls)
}

// TestOCIProbe_R_REPO_065_ManifestInspectThroughAVirtualRepositoryDownloadsTheImage is W-25 with a real client: `docker manifest
// inspect` of a tag through a virtual repository asks for the manifest only, and the remote member downloads the image's blobs in the
// background (a row of origin probe of its download queue, remote-curation.md R-CUR-013), so a pull afterwards is served from the cache. The path through a virtual repository
// is tested here once for every format (the member search is format-independent); the charge, the switch and the budget are the
// integration sets'. npm, NuGet, Debian and Ansible Galaxy have no client command that sends a probe without fetching the content
// itself (docs/spec/repositories-and-proxy.md R-REPO-065).
func TestOCIProbe_R_REPO_065_ManifestInspectThroughAVirtualRepositoryDownloadsTheImage(t *testing.T) {
	fx := ociFx(t)
	s := ociStub(t)
	host := ociRegistryHost(t)
	arch := ociArch(t)
	repos := fx.upstreamRepos(t, "probe", nil)
	s.reset(t)
	defer s.reset(t)
	pruneClients(t, 1, ociDocker)
	c := fx.client(t, 0, ociDocker, "probe")
	c.login(host)
	ref := fx.image(t, repos.all, "library/nginx", "1.27")
	if out, err := c.try("manifest", "inspect", ref); err != nil {
		t.Fatalf("docker manifest inspect: %v\n%s", err, tailLines(out, 20))
	}
	blobs := ociPlatformBlobs(t, s.ociImage(t, "library/nginx", "1.27"), arch)
	took := waitFor(t, "the blobs of linux/"+arch+" downloaded without a pull", time.Minute, time.Second, func() bool {
		counts := s.ociCounts(t)
		for _, d := range blobs {
			if counts.BlobFetches(d) != 1 {
				return false
			}
		}
		return true
	})
	t.Logf("docker manifest inspect through %s: the %d blobs of linux/%s were in the remote member %s after the client's answer", repos.all, len(blobs), arch, took.Round(time.Second))
	c.pull(ref)
	counts := s.ociCounts(t)
	for _, d := range blobs {
		if n := counts.BlobFetches(d); n != 1 {
			t.Errorf("the upstream delivered the blob %s %d times, want once: the pull was not served from what the probe stored", d, n)
		}
	}
}

// TestOCILimits_Q_LIM_27_APushOfManyTinyLayersMeetsTheWriteClass measures what decides package_write on the registry's paths (F-161,
// "Open, for the owner"): the unsafe requests of a push. docker and podman push an image of 40 layers of a few bytes each in Enforce;
// the request log of the packages replicas gives the unsafe requests per second at the peak and the answers 429 of the class.
func TestOCILimits_Q_LIM_27_APushOfManyTinyLayersMeetsTheWriteClass(t *testing.T) {
	fx := ociFx(t)
	host := ociRegistryHost(t)
	key := "oci-limits-push-" + fx.run
	fx.adm.do(http.MethodPost, reposRoute, map[string]any{"key": key, "class": "local", "format": "oci"}).want(t, "create "+key, 201)
	t.Cleanup(func() { deleteRepository(t, fx.adm, key) })
	ensurePolicy(t, fx.adm, key, []string{key}, [2]any{"sa:" + ociPublisher, []string{"read", "deploy"}})
	ociReadableOnEveryReplica(t, fx.token, key)
	const layers = 40
	// docker builds the image; podman takes it through the pod's shared /work (podman's own build of 40 COPY steps fails on the
	// storage of the client pod: "failed to read security.capability attribute")
	builder := fx.client(t, 0, ociDocker, "limits-push")
	builder.login(host)
	var df strings.Builder
	df.WriteString("FROM scratch\n")
	for i := range layers {
		builder.write(fmt.Sprintf("layers/f%02d", i), []byte(fmt.Sprintf("%s layer %d\n", fx.run, i)))
		fmt.Fprintf(&df, "COPY f%02d /f%02d\n", i, i)
	}
	builder.write("layers/Dockerfile", []byte(df.String()))
	built := fx.image(t, key, "built", "1")
	builder.exec("build", "--tag", built, "layers")
	archive := "/work/limits-push-" + fx.run + ".tar"
	builder.exec("save", "--output", archive, built)
	for _, tool := range []string{ociDocker, ociPodman} {
		c := fx.client(t, 0, tool, "limits-push")
		c.login(host)
		name := tool + "-layers"
		ref := fx.image(t, key, name, "1")
		if tool == ociPodman {
			c.exec("load", "--input", archive)
		}
		c.exec("tag", built, ref)
		since := time.Now()
		out, err := c.try("push", ref)
		took := time.Since(since)
		peak, unsafe, refused := pushReading(t, since, "/v2/"+repoRef(key)+"/"+name+"/")
		t.Logf("%s push of %d layers in Enforce: exit %d after %s; %d unsafe requests, at the peak %d in one second, %d answered 429",
			tool, layers, exitOf(err), took.Round(100*time.Millisecond), unsafe, peak, refused)
		noteResult(t, "limits-push-"+tool, map[string]any{"layers": layers, "exit": exitOf(err), "seconds": took.Seconds(), "unsafe": unsafe, "peakPerSecond": peak, "refused": refused})
		if err != nil || refused != 0 {
			t.Errorf("%s push of %d tiny layers exited %d with %d answers 429:\n%s", tool, layers, exitOf(err), refused, tailLines(out, 20))
		}
	}
}

// pushReading reads the request log of the packages replicas since a time for a path prefix: the unsafe requests per second at the
// peak, their number, and the answers 429 among them.
func pushReading(t testing.TB, since time.Time, prefix string) (peak, unsafe, refused int) {
	t.Helper()
	time.Sleep(2 * time.Second) // the last lines reach the log
	per := map[int64]int{}
	for _, p := range readyPackagesPods(t) {
		logs := kn(t, "logs", p, "--since-time="+since.Add(-time.Second).UTC().Format(time.RFC3339))
		sc := bufio.NewScanner(strings.NewReader(logs))
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			var l struct {
				Time   time.Time `json:"time"`
				Method string    `json:"method"`
				Path   string    `json:"path"`
				Status int       `json:"status"`
			}
			if json.Unmarshal(sc.Bytes(), &l) != nil || !strings.HasPrefix(l.Path, prefix) || !unsafeMethod(l.Method) || l.Time.Before(since) {
				continue
			}
			unsafe++
			per[l.Time.Unix()]++
			if l.Status == http.StatusTooManyRequests {
				refused++
			}
		}
	}
	counts := make([]int, 0, len(per))
	for _, n := range per {
		counts = append(counts, n)
	}
	sort.Ints(counts)
	if len(counts) > 0 {
		peak = counts[len(counts)-1]
	}
	return peak, unsafe, refused
}

// chargedTo counts the requests the request log of the packages replicas charged to a subject of the download limits since a time
// (limits_subject, R-LIM-030): the safe ones and the writes.
func chargedTo(t testing.TB, since time.Time, subject string) (safe, writes int) {
	t.Helper()
	time.Sleep(2 * time.Second) // the last lines reach the log
	for _, p := range readyPackagesPods(t) {
		sc := bufio.NewScanner(strings.NewReader(kn(t, "logs", p, "--since-time="+since.Add(-time.Second).UTC().Format(time.RFC3339))))
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			var l struct {
				Time    time.Time `json:"time"`
				Method  string    `json:"method"`
				Subject string    `json:"limits_subject"`
			}
			if json.Unmarshal(sc.Bytes(), &l) != nil || l.Subject != subject || l.Time.Before(since) {
				continue
			}
			if unsafeMethod(l.Method) {
				writes++
			} else {
				safe++
			}
		}
	}
	return safe, writes
}

// ociLimitsRepo creates a private repository of the case's own with one image of a few hundred bytes, pushed by the publisher before a
// rule exists, and takes the image out of the pushing client's store.
func ociLimitsRepo(t *testing.T, fx *ociFixture, key, payload string) {
	t.Helper()
	fx.adm.do(http.MethodPost, reposRoute, map[string]any{"key": key, "class": "local", "format": "oci"}).want(t, "create "+key, 201)
	t.Cleanup(func() { deleteRepository(t, fx.adm, key) })
	ensurePolicy(t, fx.adm, key, []string{key}, [2]any{"sa:" + ociPublisher, []string{"read", "deploy"}})
	ociReadableOnEveryReplica(t, fx.token, key)
	c := fx.client(t, 0, ociDocker, "limits-seed")
	c.login(ociRegistryHost(t))
	ref := fx.image(t, key, "tiny", "1")
	c.build(ref, payload+" "+fx.run)
	c.push(ref)
	c.exec("rmi", ref)
}

// ociKubectlMust is ociKubectl for a command that must succeed.
func ociKubectlMust(t testing.TB, args ...string) string {
	t.Helper()
	out, err := ociKubectl(t, time.Minute, "", args...)
	if err != nil {
		t.Fatalf("kubectl %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}
