//go:build integration

package limits

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestMeasurePackageWriteUnderAMultiPlatformPush is a measurement, not a test (CLAUDE.md, "Tests stay runnable"): it runs only with
// QUADER_TEST_MEASURE_PUSH set and writes its numbers to the log, which docs/developer/usage-and-limits.md keeps. It answers Q-LIM-56's
// open question: what the class package_write of R-API-016, at its defaults in enforce, does to the push of an image index of several
// platforms with many distinct tiny layers, sent the way a registry client sends it, as one token.
//
//	QUADER_TEST_MEASURE_PUSH=2x70,2x100 test/integration/run.sh -run TestMeasurePackageWriteUnderAMultiPlatformPush -v ./test/integration/limits/
//
// The value is one or more <platforms>x<layers per platform>. For each it pushes four times, each as a user of its own (the class's
// bucket is the subject): with docker push's requests per blob (HEAD, POST, PATCH, PUT) and with buildx's (HEAD, POST, PUT with the
// body), each once as a client that ends at the first 429 (docker exits 1 there, measured on the kind cluster on 2026-11-20,
// Q-LIM-56) and once as a client that waits the Retry-After of every 429 and sends the request again.
func TestMeasurePackageWriteUnderAMultiPlatformPush(t *testing.T) {
	spec := os.Getenv("QUADER_TEST_MEASURE_PUSH")
	if spec == "" {
		t.Skip("a measurement: set QUADER_TEST_MEASURE_PUSH to <platforms>x<layers>, such as 2x70,2x100")
	}
	type size struct{ platforms, layers int }
	var sizes []size
	for _, f := range strings.Split(spec, ",") {
		var z size
		if _, err := fmt.Sscanf(strings.TrimSpace(f), "%dx%d", &z.platforms, &z.layers); err != nil || z.platforms < 1 || z.layers < 1 {
			t.Fatalf("QUADER_TEST_MEASURE_PUSH=%q: a comma-separated list of <platforms>x<layers>", spec)
		}
		sizes = append(sizes, z)
	}
	type run struct {
		size
		shape  pushShape
		honour bool
		who    string
	}
	var runs []run
	for _, z := range sizes {
		for _, shape := range []pushShape{shapeDockerPush, shapeBuildx} {
			for _, honour := range []bool{false, true} {
				runs = append(runs, run{size: z, shape: shape, honour: honour, who: fmt.Sprintf("pusher-%dx%d-%s-%t", z.platforms, z.layers, shape.name, honour)})
			}
		}
	}
	// The classes of R-API-016 at their defaults: the stack turns them off (QUADER_API_RATE_LIMITS_ENABLED=false), nothing else is
	// set. A new instance is in enforce, and the built-in profiles are the migrations'.
	st := testenv.NewStack(t, testenv.StackOptions{Name: "limitspush", Valkey: true, Env: []string{
		"QUADER_API_RATE_LIMITS_ENABLED=true", "QUADER_VULNERABILITIES_SOURCES_OSV_MODE=off",
	}})
	const repo = "oci-push"
	st.Repository(repo, "local", "oci", "{}")
	tokens := map[string]string{}
	for _, r := range runs {
		u := st.User(r.who)
		st.Policy("p-"+r.who, []string{"read", "deploy"}, []string{repo}, u)
		tokens[r.who] = st.Token(u, scope.Default())
	}
	if mode := st.Text(`SELECT mode FROM limit_settings`); mode != "enforce" {
		t.Fatalf("the mode of a new instance is %q, want enforce", mode)
	}
	rep := st.Start(t, testenv.ReplicaOptions{Name: "web-1"})
	for _, r := range runs {
		c := &registryClient{t: t, base: st.Origin(rep.Addr("default")), shape: r.shape, honour: r.honour, workers: 5}
		// An image of its own per run: a tag pushed again would need the action overwrite.
		res := c.push(repo, r.who, tokens[r.who], r.who, r.platforms, r.layers)
		t.Logf("MEASURE %d platforms x %d layers, client=%s honours-Retry-After=%t: %s", r.platforms, r.layers, r.shape.name, r.honour, res)
	}
}

// pushShape is the requests a client sends per blob.
type pushShape struct {
	name string
	// patch: the body goes in a PATCH and the PUT closes the session without one (docker push); otherwise the PUT carries it (buildx).
	patch bool
}

var (
	shapeDockerPush = pushShape{name: "docker-push", patch: true}
	shapeBuildx     = pushShape{name: "buildx", patch: false}
)

// registryClient sends a push as a registry client does: the challenge, the token by the password grant, the blobs of each platform
// (configuration first, then the layers) with workers at a time, each platform's manifest, then the index.
type registryClient struct {
	t       *testing.T
	base    string
	shape   pushShape
	honour  bool
	workers int

	start            time.Time
	requests, unsafe atomic.Int64
	refused          atomic.Int64
	waited           atomic.Int64 // milliseconds slept on Retry-After
	mu               sync.Mutex
	first429         string // what the first 429 was: its request number among the unsafe ones, its time, its fields
	perSecond        map[int64]int
	stopped          atomic.Bool
}

type pushResult struct {
	requests, unsafe, refused int64
	first429                  string
	peak                      int
	took                      time.Duration
	waited                    time.Duration
	finished                  bool
}

func (r pushResult) String() string {
	return fmt.Sprintf("finished=%t in %s; %d requests (%d unsafe, %d of them admitted, %d in the busiest second); %d answered 429; "+
		"slept %s on Retry-After; first 429: %s",
		r.finished, r.took.Round(10*time.Millisecond), r.requests, r.unsafe, r.unsafe-r.refused, r.peak, r.refused, r.waited.Round(time.Second), r.first429)
}

func digest(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

func (c *registryClient) push(repo, name, pat, who string, platforms, layers int) pushResult {
	c.perSecond = map[int64]int{}
	c.start = time.Now()
	path := "/v2/" + testenv.DefaultProject + "/" + repo + "/" + name
	// The challenge: a request without a credential, answered from memory and not charged (Q-API-290).
	if a := c.send("GET", "/v2/", "", nil, nil); a.status != http.StatusUnauthorized {
		c.t.Fatalf("the version check without a credential: %d", a.status)
	}
	form := url.Values{"grant_type": {"password"}, "username": {who}, "password": {pat}, "service": {testenv.DefaultHost},
		"scope": {"repository:" + testenv.DefaultProject + "/" + repo + "/" + name + ":pull,push"}}
	tok := c.send("POST", "/v2/_token", "", []byte(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if tok.status != http.StatusOK {
		c.t.Fatalf("the token: %d %s", tok.status, tok.body)
	}
	var tb struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(tok.body, &tb); err != nil || tb.Token == "" {
		c.t.Fatalf("the token answer %q: %v", tok.body, err)
	}
	bearer := tb.Token
	var manifests [][]byte
	for p := range platforms {
		arch := fmt.Sprintf("arch%d", p)
		cfg := []byte(fmt.Sprintf(`{"architecture":%q,"os":"linux","who":%q}`, arch, who))
		blobs := [][]byte{cfg}
		var ls []any
		for i := range layers {
			l := []byte(fmt.Sprintf("%s platform %d layer %d\n", who, p, i))
			blobs = append(blobs, l)
			ls = append(ls, map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar", "digest": digest(l), "size": len(l)})
		}
		if !c.blobs(path, bearer, blobs) {
			return c.result(false)
		}
		m, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
			"config": map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": digest(cfg), "size": len(cfg)},
			"layers": ls})
		if a := c.send("PUT", path+"/manifests/"+digest(m), bearer, m, map[string]string{"Content-Type": "application/vnd.oci.image.manifest.v1+json"}); a.status != http.StatusCreated {
			return c.result(false)
		}
		manifests = append(manifests, m)
	}
	var ms []any
	for i, m := range manifests {
		ms = append(ms, map[string]any{"mediaType": "application/vnd.oci.image.manifest.v1+json", "digest": digest(m),
			"size": len(m), "platform": map[string]any{"os": "linux", "architecture": fmt.Sprintf("arch%d", i)}})
	}
	idx, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.index.v1+json", "manifests": ms})
	a := c.send("PUT", path+"/manifests/v1", bearer, idx, map[string]string{"Content-Type": "application/vnd.oci.image.index.v1+json"})
	return c.result(a.status == http.StatusCreated)
}

func (c *registryClient) result(finished bool) pushResult {
	r := pushResult{requests: c.requests.Load(), unsafe: c.unsafe.Load(), refused: c.refused.Load(), took: time.Since(c.start),
		waited: time.Duration(c.waited.Load()) * time.Millisecond, finished: finished, first429: c.first429}
	for _, n := range c.perSecond {
		r.peak = max(r.peak, n)
	}
	if r.first429 == "" {
		r.first429 = "none"
	}
	return r
}

// blobs uploads the blobs with c.workers at a time; false when the client stopped at a 429 or a request failed.
func (c *registryClient) blobs(path, bearer string, blobs [][]byte) bool {
	work := make(chan []byte)
	var wg sync.WaitGroup
	var failed atomic.Bool
	for range c.workers {
		wg.Go(func() {
			for b := range work {
				if !failed.Load() && !c.blob(path, bearer, b) {
					failed.Store(true)
				}
			}
		})
	}
	for _, b := range blobs {
		work <- b
	}
	close(work)
	wg.Wait()
	return !failed.Load()
}

func (c *registryClient) blob(path, bearer string, b []byte) bool {
	d := digest(b)
	if a := c.send("HEAD", path+"/blobs/"+d, bearer, nil, nil); a.status == http.StatusOK {
		return true
	} else if a.status != http.StatusNotFound {
		return false
	}
	start := c.send("POST", path+"/blobs/uploads/", bearer, nil, nil)
	if start.status != http.StatusAccepted {
		return false
	}
	loc := start.location
	if c.shape.patch {
		p := c.send("PATCH", loc, bearer, b, map[string]string{"Content-Type": "application/octet-stream"})
		if p.status != http.StatusAccepted {
			return false
		}
		loc = p.location
		return c.send("PUT", withDigest(loc, d), bearer, nil, nil).status == http.StatusCreated
	}
	return c.send("PUT", withDigest(loc, d), bearer, b, map[string]string{"Content-Type": "application/octet-stream"}).status == http.StatusCreated
}

func withDigest(loc, d string) string {
	sep := "?"
	if strings.Contains(loc, "?") {
		sep = "&"
	}
	return loc + sep + "digest=" + url.QueryEscape(d)
}

type answer struct {
	status   int
	location string
	body     []byte
}

// send sends one request; a 429 is waited out and sent again by a client that honours Retry-After, and stops every other request
// of a client that does not.
func (c *registryClient) send(method, path, bearer string, body []byte, hdr map[string]string) answer {
	for {
		if c.stopped.Load() {
			return answer{status: http.StatusTooManyRequests}
		}
		a, retry := c.once(method, path, bearer, body, hdr)
		if a.status != http.StatusTooManyRequests {
			return a
		}
		if !c.honour {
			c.stopped.Store(true)
			return a
		}
		c.waited.Add(retry.Milliseconds())
		time.Sleep(retry)
	}
}

func (c *registryClient) once(method, path, bearer string, body []byte, hdr map[string]string) (answer, time.Duration) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	if u, err := url.Parse(path); err == nil && u.IsAbs() {
		path = u.RequestURI()
	}
	req, err := http.NewRequestWithContext(context.Background(), method, c.base+path, rd)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Host = testenv.DefaultHost
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := testenv.Client.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	b, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	n := c.requests.Add(1)
	unsafe := method != http.MethodGet && method != http.MethodHead
	var u int64
	if unsafe {
		u = c.unsafe.Add(1)
		c.mu.Lock()
		c.perSecond[int64(time.Since(c.start)/time.Second)]++
		c.mu.Unlock()
	}
	a := answer{status: res.StatusCode, location: res.Header.Get("Location"), body: b}
	if res.StatusCode != http.StatusTooManyRequests {
		return a, 0
	}
	c.refused.Add(1)
	secs, _ := strconv.Atoi(res.Header.Get("Retry-After"))
	c.mu.Lock()
	if c.first429 == "" {
		c.first429 = fmt.Sprintf("request %d (unsafe %d, %s %s) after %s, Retry-After %q, RateLimit %q, RateLimit-Policy %q",
			n, u, method, strings.SplitN(path, "?", 2)[0][strings.LastIndex(strings.SplitN(path, "?", 2)[0], "/")+1:],
			time.Since(c.start).Round(time.Millisecond), res.Header.Get("Retry-After"),
			strings.Join(res.Header.Values("RateLimit"), ", "), strings.Join(res.Header.Values("RateLimit-Policy"), ", "))
	}
	c.mu.Unlock()
	return a, time.Duration(max(secs, 1)) * time.Second
}
