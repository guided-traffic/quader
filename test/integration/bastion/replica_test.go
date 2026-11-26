//go:build integration

package bastion

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/guided-traffic/quader/internal/bastion/engine"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/server"
	"github.com/guided-traffic/quader/internal/blob/s3"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/outbound"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// clock is the settable clock of a Bastion replica (engine.Deps.Now). It starts at the real time; the periods
// of the answers are crossed by advancing it. A test that needs both a revocation and an advanced clock
// revokes first: the feed stamps an entry with the real time of its receipt (R-BAS-020).
type clock struct {
	mu sync.Mutex
	d  time.Duration
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Add(c.d)
}

func (c *clock) Advance(d time.Duration) { c.mu.Lock(); c.d += d; c.mu.Unlock() }

// shared is what the replicas of one Bastion have in common: the cache bucket's prefix or the volume, the
// signing key and the Valkey key prefix.
type shared struct {
	backend   string // volume or bucket
	valkey    bool
	prefix    string // bucket prefix
	keyFile   string
	vprefix   string
	volume    string
	bucketYML string
	raw       *s3.Client
	replicas  int
}

// bastionOptions describe a Bastion (one or two replicas that share a cache and, with Valkey, their state).
type bastionOptions struct {
	Backend  string // "volume" (default) or "bucket"
	Valkey   bool
	Replicas int
	// Settings are lines below `bastion:` (indented by two spaces) that override the defaults of the harness.
	Settings string
	// Configure changes the configuration every handshake brings (R-BAS-093), on the Bastion's side of the wire: the flood
	// figures, the slots and the anonymous ceiling a test needs. The suite's main installation sends its interim configuration
	// until its side of the registration's configuration is built (E8 of docs/developer/bastion-limits.md, Q-BAS-352).
	Configure func(*iface.Configuration)
	// Cache are lines below `bastion.cache:` (indented by four spaces).
	Cache string
	// Server are lines below `server:`.
	Server string
	// Token overrides the Bastion token.
	Token string
	// Valkey section of a Bastion whose Valkey is unreachable.
	ValkeyDown bool
	// BaseURL overrides server.base_url.
	BaseURL string
	// Main overrides bastion.main.url (default: the front).
	Main string
	// MainSettings are lines below `bastion.main:` (indented by four spaces).
	MainSettings string
	// CAFile overrides bastion.main.ca_file (default: the front's CA), for a bundle that also trusts the object store a
	// handover's link names (R-BAS-074, Q-BAS-237).
	CAFile string
	// Deps sees each replica's engine dependencies after the server built them (the link client of a handover among them).
	Deps func(*engine.Deps)
	// CutsLink gives the link to the main installation the short bounds of a test that cuts it and waits for the Bastion to
	// notice (unreachable_after 3 s, connect_timeout 1 s, response_header_timeout 10 s). Every other Bastion keeps the product's
	// defaults (10 s, 5 s, 30 s): with the short ones a host at a load of 30 to 130 made a Bastion that nobody cut answer
	// 503 unavailable and 504 gateway_timeout in tests that compare answers (2026-11-23).
	CutsLink bool
}

// linkBounds are the bounds of the link to the main installation of a Bastion of o (bastionOptions.CutsLink).
func linkBounds(o bastionOptions) string {
	if o.CutsLink {
		return "    unreachable_after: 3s\n    connect_timeout: 1s\n    response_header_timeout: 10s\n"
	}
	return "    unreachable_after: 10s\n    connect_timeout: 5s\n    response_header_timeout: 30s\n"
}

// bastion is a Bastion: its replicas.
type bastion struct {
	t        *testing.T
	w        *world
	sh       *shared
	reps     []*replica
	clk      *clock
	tokenFil string
	o        bastionOptions
	dir      string
}

// addReplica starts one more replica of the Bastion, sharing its cache and, with Valkey, its state.
func (b *bastion) addReplica() *replica {
	b.t.Helper()
	r := b.start(b.t, b.dir, b.o, len(b.reps))
	b.reps = append(b.reps, r)
	b.wait()
	return r
}

// replica is one running Bastion replica.
type replica struct {
	srv    *server.Server
	name   string
	logs   *syncBuf
	client string
	ops    string
	cfg    string
	// mainNames are the strings that would name the main installation in an answer of this replica ([mainNames]).
	mainNames []string
}

// mainNames are the strings that name the main installation in an answer of a Bastion whose base URL is base and which reaches
// the main installation at mainURL (R-BAS-099 row 10): the main installation's host (its server.base_url), the address the
// Bastion reaches it at, the address of its replica behind the front, and a path of its management API or of its web interface,
// as a JSON string, as a header value or on the Bastion's host. The suite's main installation is quader.test, a name no
// content of the fixtures carries.
func mainNames(base, mainURL, replicaAddr string) []string {
	names := []string{mainHost, replicaAddr, `"/api/v1/`, `"/ui/`, base + "/api/v1/", base + "/ui/"}
	if u, err := url.Parse(mainURL); err == nil && u.Host != "" {
		names = append(names, u.Host)
	}
	return names
}

// nameOfMain returns what of the main installation an answer of the replica names, "" for nothing: the status line, every
// header and the whole body are scanned, so every format's replay through do holds the rule of R-BAS-099 for nothing.
func (r *replica) nameOfMain(res *http.Response, body []byte) string {
	var head strings.Builder
	head.WriteString(res.Status + "\n")
	for k, vs := range res.Header {
		for _, v := range vs {
			head.WriteString(k + ": " + v + "\n")
			if strings.HasPrefix(v, "/api/v1/") || strings.HasPrefix(v, "/ui/") {
				return k + ": " + v
			}
		}
	}
	for _, n := range r.mainNames {
		if n != "" && (strings.Contains(head.String(), n) || bytes.Contains(body, []byte(n))) {
			return n
		}
	}
	return ""
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func writeFile(t testing.TB, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// newBastion starts the replicas of a Bastion in this process, from a configuration file, as the role starts.
func (w *world) newBastion(t *testing.T, o bastionOptions) *bastion {
	t.Helper()
	if o.Backend == "" {
		o.Backend = "volume"
	}
	if o.Replicas == 0 {
		o.Replicas = 1
	}
	dir := t.TempDir()
	b := &bastion{t: t, w: w, clk: &clock{}, o: o, dir: dir}
	b.sh = &shared{backend: o.Backend, valkey: o.Valkey, replicas: o.Replicas, vprefix: "it-" + testenv.RandHex(4), volume: filepath.Join(dir, "cache")}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	b.sh.keyFile = writeFile(t, dir, "signing-key", string(key))
	if o.Backend == "bucket" {
		b.sh.prefix, b.sh.bucketYML, b.sh.raw = bucketSetup(t, dir)
	}
	tok := o.Token
	if tok == "" {
		tok = w.bst
	}
	b.tokenFil = writeFile(t, dir, "token", tok+"\n")
	for i := 0; i < o.Replicas; i++ {
		b.reps = append(b.reps, b.start(t, dir, o, i))
	}
	return b
}

func (b *bastion) start(t *testing.T, dir string, o bastionOptions, i int) *replica {
	t.Helper()
	name := fmt.Sprintf("bastion-%d", i+1)
	base, mainURL := o.BaseURL, o.Main
	if base == "" {
		base = baseURL
	}
	if mainURL == "" {
		mainURL = b.w.front.URL()
	}
	caFile := b.w.front.caFile
	if o.CAFile != "" {
		caFile = o.CAFile
	}
	var y strings.Builder
	fmt.Fprintf(&y, "server:\n  listen: 127.0.0.1:0\n  ops_listen: localhost:0\n  base_url: %s\n  trusted_proxies: [\"127.0.0.1/32\"]\n%s", base, o.Server)
	fmt.Fprintf(&y, "bastion:\n  replicas: %d\n  signing_key_file: %s\n", o.Replicas, b.sh.keyFile)
	fmt.Fprintf(&y, "  main:\n    url: %s\n    token_file: %s\n    ca_file: %s\n    handshake_interval: 5s\n%s    token_reload_interval: 1s\n%s",
		mainURL, b.tokenFil, caFile, linkBounds(o), o.MainSettings)
	y.WriteString("  feed:\n    poll_interval: 1s\n  audit:\n    flush_interval: 1s\n")
	if o.Backend == "bucket" {
		y.WriteString(b.sh.bucketYML)
	} else {
		fmt.Fprintf(&y, "  cache:\n    backend: volume\n    volume:\n      dir: %s\n", b.sh.volume)
	}
	y.WriteString(o.Cache)
	y.WriteString(o.Settings)
	if o.Valkey {
		y.WriteString(valkeyYAML(t, dir, b.sh.vprefix, o.ValkeyDown))
	}
	cfgFile := writeFile(t, dir, fmt.Sprintf("quader-%d.yaml", i+1), y.String())
	loaded, err := config.LoadBastion(config.Options{File: cfgFile, Command: config.CommandServe})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	logs := &syncBuf{}
	srv, err := server.New(server.Options{
		Loaded: loaded, Environ: []string{"QUADER_POD_NAME=" + name}, LogWriter: logs, DrainDelay: 10 * time.Millisecond,
		Deps: func(d *engine.Deps) {
			d.Now = b.clk.Now
			if o.Configure != nil {
				d.Main = configured{Client: d.Main, f: o.Configure}
			}
			if o.Deps != nil {
				o.Deps(d)
			}
		},
	})
	if err != nil {
		t.Fatalf("server: %v\n%s", err, logs)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v\n%s", err, logs)
	}
	r := &replica{
		srv: srv, name: name, logs: logs, client: "http://" + srv.ClientAddr().String(), ops: "http://" + srv.OpsAddr().String(), cfg: y.String(),
		mainNames: mainNames(base, mainURL, b.w.main.Addr("default")),
	}
	t.Cleanup(func() {
		if err := srv.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown %s: %v", name, err)
		}
		if t.Failed() {
			t.Logf("log of %s:\n%s", name, testenv.Tail(logs.String(), 40))
		}
	})
	return r
}

// wait waits until every replica is ready and has reached the main installation.
func (b *bastion) wait() *bastion {
	b.t.Helper()
	for _, r := range b.reps {
		testenv.WaitFor(b.t, r.name+" ready and connected", 30*time.Second, func() bool {
			return r.srv.Health().Ready() && metricValue(b.t, r.metrics(b.t), "quader_bastion_main_reachable") == 1
		})
	}
	return b
}

// configured is the client of the main installation with the configuration of every handshake changed by f
// (bastionOptions.Configure).
type configured struct {
	mainclient.Client
	f func(*iface.Configuration)
}

func (c configured) Handshake(ctx context.Context) (iface.Handshake, error) {
	hs, err := c.Client.Handshake(ctx)
	if err == nil {
		c.f(&hs.Configuration)
	}
	return hs, err
}

// bucketSetup prepares the bucket backend on the stack's S3 below a prefix of its own and returns the prefix
// and the lines below `bastion:` that configure it.
func bucketSetup(t *testing.T, dir string) (prefix, yml string, raw *s3.Client) {
	t.Helper()
	s := testenv.S3Config(t)
	ca := testenv.CAFile(t)
	var id [5]byte
	if _, err := rand.Read(id[:]); err != nil {
		t.Fatal(err)
	}
	prefix = "it-bastion/" + hex.EncodeToString(id[:])
	access := writeFile(t, dir, "access-key", s.AccessKey+"\n")
	secret := writeFile(t, dir, "secret-key", s.SecretKey+"\n")
	cfg := config.BastionCacheBucket{
		Endpoint: s.Endpoint, Region: s.Region, Bucket: s.BackupsBucket, Prefix: prefix, ForcePathStyle: s.ForcePathStyle, CAFile: ca,
		Credentials: config.BastionBucketCredentials{Source: "static", AccessKeyIDFile: access, SecretAccessKeyFile: secret},
	}
	raw, err := s3.NewFromFactory(context.Background(), cfg.StorageS3(), outbound.New(config.Outbound{TLS: config.OutboundTLS{UseSystemRoots: true}}))
	if err != nil {
		t.Fatalf("s3 client: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		token := ""
		for {
			objs, next, err := raw.ListPlain(ctx, prefix+"/", token, 1000)
			if err != nil {
				t.Logf("list %s: %v", prefix, err)
				return
			}
			for _, o := range objs {
				if err := raw.Delete(ctx, o.Key); err != nil {
					t.Logf("clean up %s: %v", o.Key, err)
				}
			}
			if next == "" {
				return
			}
			token = next
		}
	})
	var y strings.Builder
	y.WriteString("  cache:\n    backend: bucket\n    bucket:\n")
	fmt.Fprintf(&y, "      endpoint: %s\n      region: %s\n      bucket: %s\n      prefix: %s\n      ca_file: %s\n", s.Endpoint, s.Region, s.BackupsBucket, prefix, ca)
	if s.ForcePathStyle {
		y.WriteString("      force_path_style: true\n")
	}
	fmt.Fprintf(&y, "      credentials:\n        source: static\n        access_key_id_file: %s\n        secret_access_key_file: %s\n", access, secret)
	return prefix, y.String(), raw
}

// The Bastion's Valkey user of the stack (test/integration/conf/users.acl): the commands of the least-privilege
// rule, on the keys and channels below quader-bastion: only.
const (
	bastionUser = "bastion"
	bastionPass = "bastion-valkey-it"
	aclRules    = "on >" + bastionPass + " resetkeys ~quader-bastion:* resetchannels &quader-bastion:* -@all +get +set +delifeq +incr +incrby +expire +zadd +zrem +zremrangebyscore +zcard +multi +exec +publish +subscribe +unsubscribe +sunsubscribe +punsubscribe +discard +ping +hello +role +client|setname +client|setinfo"
)

var aclOnce sync.Once

// ensureBastionUser makes sure both data nodes know the user of users.acl, as the bastionshared suite does: a
// stack started before that line existed does not; the same rules are idempotent.
func ensureBastionUser(t *testing.T) {
	t.Helper()
	v := testenv.ValkeyConfig(t)
	aclOnce.Do(func() {
		pem, err := os.ReadFile(v.CAFile)
		if err != nil {
			t.Fatal(err)
		}
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(pem)
		for _, node := range v.Nodes {
			c, err := valkey.NewClient(valkey.ClientOption{
				InitAddress: []string{node}, Username: v.AdminUsername, Password: v.AdminPassword,
				TLSConfig:    &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
				DisableCache: true, ForceSingleClient: true, DisableRetry: true,
			})
			if err != nil {
				t.Fatalf("connect to %s: %v", node, err)
			}
			args := append([]string{"SETUSER", bastionUser}, strings.Fields(aclRules)...)
			if err := c.Do(t.Context(), c.B().Arbitrary("ACL").Args(args...).Build()).Error(); err != nil {
				c.Close()
				t.Fatalf("ACL SETUSER on %s: %v", node, err)
			}
			c.Close()
		}
	})
}

// valkeyYAML is the valkey section of a Bastion that talks to the stack the way the chart configures it. With
// down it points at a Sentinel nothing listens on: the state "Valkey unavailable" (R-BAS-034).
func valkeyYAML(t *testing.T, dir, prefix string, down bool) string {
	t.Helper()
	ensureBastionUser(t)
	v := testenv.ValkeyConfig(t)
	sentinels := v.Sentinels
	if down {
		sentinels = []string{"127.0.0.1:1"}
	}
	user := writeFile(t, dir, "vk-user", bastionUser+"\n")
	pass := writeFile(t, dir, "vk-pass", bastionPass+"\n")
	sp := writeFile(t, dir, "vk-sentinel-pass", v.SentinelPassword+"\n")
	quoted := make([]string, len(sentinels))
	for i, s := range sentinels {
		quoted[i] = `"` + s + `"`
	}
	return fmt.Sprintf(`valkey:
  mode: sentinel
  key_prefix: %s
  username_file: %s
  password_file: %s
  sentinel:
    addresses: [%s]
    primary_name: %s
    password_file: %s
    refresh_interval: 2s
  tls:
    mode: verify-full
    ca_file: %s
  connect_timeout: 2s
  command_timeout: 500ms
  health_check_interval: 500ms
`, prefix, user, pass, strings.Join(quoted, ","), v.PrimaryName, sp, v.CAFile)
}

// ---------------------------------------------------------------------------------------------------------
// Requests.

// reply is one answer of a Bastion.
type reply struct {
	Status int
	Header http.Header
	Body   []byte
}

func (r reply) Code() string {
	i := bytes.Index(r.Body, []byte(`"code":"`))
	if i < 0 {
		return ""
	}
	rest := r.Body[i+8:]
	return string(rest[:bytes.IndexByte(rest, '"')])
}

var clientHTTP = &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 30 * time.Second}

// callOpts shape one request to a Bastion.
type callOpts struct {
	token  string
	addr   string // the caller's address (the Bastion's ingress is trusted to say so)
	header []string
	body   []byte
	host   string
}

// do sends a request to replica r of the Bastion.
func (r *replica) do(t testing.TB, method, path string, o callOpts) reply {
	t.Helper()
	var rd io.Reader
	if o.body != nil {
		rd = bytes.NewReader(o.body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, r.client+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = baseHost
	if o.host != "" {
		req.Host = o.host
	}
	if o.token != "" {
		req.Header.Set("Authorization", "Bearer "+o.token)
	}
	if o.addr != "" {
		req.Header.Set("X-Forwarded-For", o.addr)
	}
	for i := 0; i+1 < len(o.header); i += 2 {
		req.Header.Set(o.header[i], o.header[i+1])
	}
	res, err := clientHTTP.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("%s %s: reading the body: %v", method, path, err)
	}
	if n := r.nameOfMain(res, b); n != "" {
		t.Errorf("%s %s: the answer %d of the Bastion names the main installation (%q, R-BAS-099)", method, path, res.StatusCode, n)
	}
	return reply{Status: res.StatusCode, Header: res.Header, Body: b}
}

// get is a GET with a token.
func (r *replica) get(t testing.TB, path, token string, hdr ...string) reply {
	t.Helper()
	return r.do(t, http.MethodGet, path, callOpts{token: token, header: hdr})
}

// metrics reads the ops listener's /metrics.
func (r *replica) metrics(t testing.TB) string {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, r.ops+"/metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := clientHTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return string(b)
}

// metricValue is the value of the series (name with its labels as exposed), -1 if absent.
func metricValue(t testing.TB, text, series string) float64 {
	t.Helper()
	v := testenv.MetricValue(text, series)
	return v
}

// sumMetric adds up every series of a metric whose line contains all the fragments.
func sumMetric(text, name string, fragments ...string) float64 {
	total := 0.0
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, name) || strings.HasPrefix(line, "#") {
			continue
		}
		rest := line[len(name):]
		if rest == "" || (rest[0] != ' ' && rest[0] != '{') {
			continue
		}
		ok := true
		for _, f := range fragments {
			if !strings.Contains(line, f) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		var v float64
		if i := strings.LastIndexByte(line, ' '); i >= 0 {
			_, _ = fmt.Sscanf(line[i+1:], "%g", &v)
		}
		total += v
	}
	return total
}
