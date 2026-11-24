//go:build integration

// Package limits tests the download limits of usage-and-limits.md against the integration stack: replicas started through the
// composition root with Valkey, so the real chain's stage 7, the format host's admission, the charge buffer and the usage store run.
package limits

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

func TestMain(m *testing.M) { os.Exit(testenv.Main(m)) }

const (
	rFiles   = "files"
	fileSize = 64 << 10
)

type env struct {
	st             *testenv.Stack
	r              *testenv.Replica
	admin, reader  string
	file           []byte
	filePath       string
	readerPerson   testenv.Person
	readerPrincipl string
}

// newEnv starts one replica with Valkey, a generic local repository with one file, an administrator and a reader. The flush
// intervals are short so the counters and the usage store follow within a second.
func newEnv(t *testing.T) *env { return newEnvWith(t, envOptions{valkey: true}) }

// envOptions vary the installation of newEnv: Valkey stopped (R-LIM-019), the replica's role, rows written before it starts, more
// of the replica's environment.
type envOptions struct {
	valkey bool
	role   string
	seed   func(st *testenv.Stack)
	env    []string
	// readable are further repositories (made by seed) the reader reads
	readable []string
	// endpoints is server.endpoints of the replica (QUADER_SERVER_ENDPOINTS); empty keeps the default endpoint as it is.
	endpoints string
}

func newEnvWith(t *testing.T, o envOptions) *env {
	t.Helper()
	st := testenv.NewStack(t, testenv.StackOptions{Name: "limits", Valkey: o.valkey, Env: []string{
		"QUADER_LIMITS_FLUSH_INTERVAL=200ms", "QUADER_LIMITS_USAGE_FLUSH_INTERVAL=1s", "QUADER_LIMITS_PARALLEL_WAIT=0s",
	}})
	ada, reader := st.User("ada"), st.User("reader")
	st.Bind("admin", ada)
	e := &env{st: st, readerPerson: reader, readerPrincipl: reader.ID}
	e.admin = st.Token(ada, scope.Scope{Inherit: true, Admin: true})
	e.reader = st.Token(reader, scope.Default())
	st.Repository(rFiles, "local", "generic", `{}`)
	if o.seed != nil {
		o.seed(st)
	}
	st.Policy("readers", []string{"read"}, append([]string{rFiles}, o.readable...), reader)
	st.Policy("writers", []string{"read", "deploy"}, []string{rFiles}, ada)
	e.r = st.Start(t, testenv.ReplicaOptions{Name: "web-1", Role: o.role, Env: o.env, Endpoints: o.endpoints})
	e.file = bytes.Repeat([]byte("q"), fileSize)
	e.filePath = "/files/it/" + rFiles + "/a.bin"
	if a := st.Do(e.r, "PUT", e.filePath, e.admin, e.file, "Content-Type", "application/octet-stream"); a.Status/100 != 2 {
		t.Fatalf("upload: %d %s", a.Status, a.Body)
	}
	return e
}

// setStandard replaces the figures of the profile Standard through the API and waits until the replica decides with them.
func (e *env) setStandard(t *testing.T, figures map[string]any) {
	t.Helper()
	a := e.st.Do(e.r, "PUT", "/api/v1/limits/profiles/standard", e.admin,
		map[string]any{"name": "Standard", "description": "test", "figures": figures}, "If-Match", "*")
	if a.Status != 200 {
		t.Fatalf("profile: %d %s", a.Status, a.Body)
	}
}

func (e *env) setMode(t *testing.T, mode string) {
	t.Helper()
	a := e.st.Do(e.r, "PATCH", "/api/v1/limits/settings", e.admin, []byte(`{"mode":"`+mode+`"}`),
		"Content-Type", "application/merge-patch+json", "If-Match", "*")
	if a.Status != 200 {
		t.Fatalf("mode: %d %s", a.Status, a.Body)
	}
}

// awaitIndex waits until every replica of the stack decides with every change of the limits made so far: its
// quader_limits_index_epoch reaches the epoch of the area limits in the database (Q-LIM-111). The bound is the change area's, a
// few seconds (R-ARCH-018); a replica that misses it fails the test with what it reports.
func (e *env) awaitIndex(t *testing.T) {
	t.Helper()
	want := e.st.Number(`SELECT epoch FROM cluster_epoch WHERE area = 'limits'`)
	for _, r := range e.st.Replicas() {
		deadline := time.Now().Add(10 * time.Second)
		for {
			got := testenv.MetricValue(r.Metrics(t), "quader_limits_index_epoch")
			if got >= float64(want) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("replica %s reports quader_limits_index_epoch %v after 10 s, want %d, the epoch of the area limits", r.Name, got, want)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// awaitFlush waits until every replica of the stack ended two rounds of its charge buffer after the call (Q-LIM-112): a round that
// began before a request may leave it out, the round after it has it, so what the requests before the call counted is in Valkey (or on
// the replica's share while Valkey cannot be used) and the markers it passes are set. The bound is far above the 200 ms interval of
// the env; the e2e cases wait the same way (awaitFlush of test/e2e).
func (e *env) awaitFlush(t *testing.T) {
	t.Helper()
	for _, r := range e.st.Replicas() {
		since := testenv.MetricValue(r.Metrics(t), "quader_limits_flushes_total")
		testenv.WaitFor(t, r.Name+" to end two rounds of its charge buffer", 10*time.Second, func() bool {
			return testenv.MetricValue(r.Metrics(t), "quader_limits_flushes_total") >= since+2
		})
	}
}

// awaitRows waits until the statement counts at least one row: the usage buffer writes what it holds at its interval (1 s in the
// env), so a row of the usage store is waited for by the row itself.
func (e *env) awaitRows(t *testing.T, what, statement string, args ...any) {
	t.Helper()
	testenv.WaitFor(t, what, 10*time.Second, func() bool { return e.st.Number(statement, args...) > 0 })
}

// limitsTrips is the round trips of the download limits' batches the replica sent so far.
func (e *env) limitsTrips(t *testing.T) float64 {
	return testenv.MetricValue(e.r.Metrics(t), `quader_valkey_command_duration_seconds_count{command="multi"}`)
}

func TestR_LIM_017_ACachedDownloadCostsTwoRoundTrips(t *testing.T) {
	e := newEnv(t)
	if a := e.st.Do(e.r, "GET", e.filePath, e.reader, nil); a.Status != 200 {
		t.Fatalf("warm-up: %d", a.Status)
	}
	before := e.limitsTrips(t)
	a := e.st.Do(e.r, "GET", e.filePath, e.reader, nil)
	if a.Status != 200 || !bytes.Equal(a.Body, e.file) {
		t.Fatalf("download: %d, %d bytes", a.Status, len(a.Body))
	}
	// The flush of every 200 ms may fall into the window; it is off the request's path and counted apart.
	got := e.limitsTrips(t) - before
	if got < 2 || got > 4 {
		t.Fatalf("%v round trips of the limits around one download, want 2 on its path (and at most two flushes)", got)
	}
	t.Logf("round trips of the limits around one cached download: %v", got)
}

// R-LIM-026, Q-LIM-4 (the owner's (b), 2026-11-19): a new instance starts in enforce; in audit nothing on a protocol path is refused
// or slowed, neither by a figure nor by package_write, and every figure that would have refused is recorded; enforce refuses.
func TestR_LIM_026_AuditRefusesNothingAndEnforceRefuses(t *testing.T) {
	// The stack turns the classes of R-API-016 off; this test needs package_write.
	e := newEnvWith(t, envOptions{env: []string{
		"QUADER_API_RATE_LIMITS_ENABLED=true",
		"QUADER_API_RATE_LIMITS_PACKAGE_WRITE_REQUESTS_PER_SECOND=0.01", "QUADER_API_RATE_LIMITS_PACKAGE_WRITE_BURST=3",
	}})
	if a := e.st.Do(e.r, "GET", "/api/v1/limits/settings", e.admin, nil); a.Status != 200 || a.JSON(t)["mode"] != "enforce" {
		t.Fatalf("the mode of a new instance: %d %s", a.Status, a.Body)
	}
	e.setMode(t, "audit")
	e.setStandard(t, map[string]any{"requestsPerSecond": 2, "requestsBurst": 2, "cachedBytesPerHour": fileSize + 1})
	e.awaitIndex(t)
	for i := range 6 {
		if a := e.st.Do(e.r, "GET", e.filePath, e.reader, nil); a.Status != 200 {
			t.Fatalf("audit, request %d: %d %s", i, a.Status, a.Body)
		} else if strings.Contains(a.Header.Get("RateLimit-Policy"), `"requests"`) {
			t.Fatalf("audit sends the RateLimit member of a figure: %q", a.Header.Get("RateLimit-Policy"))
		}
	}
	// The upload of newEnv took one of package_write's three; in audit the class refuses no write.
	for i := range 5 {
		a := e.st.Do(e.r, "PUT", "/files/it/"+rFiles+"/w"+strconv.Itoa(i)+".bin", e.admin, e.file, "Content-Type", "application/octet-stream")
		if a.Status/100 != 2 || strings.Contains(a.Header.Get("RateLimit-Policy"), "package_write") {
			t.Fatalf("audit, write %d: %d %v", i, a.Status, a.Header)
		}
	}
	e.awaitRows(t, "a would_limit event of audit", `SELECT count(*) FROM usage_limit_event WHERE mode = 'audit' AND subject_ref = $1`, e.readerPrincipl)
	e.setMode(t, "enforce")
	e.awaitIndex(t)
	t.Run("Q-LIM-27 in enforce package_write refuses the writes beyond its burst", func(t *testing.T) {
		var last testenv.Answer
		for i := range 4 {
			last = e.st.Do(e.r, "PUT", "/files/it/"+rFiles+"/e"+strconv.Itoa(i)+".bin", e.admin, e.file, "Content-Type", "application/octet-stream")
		}
		if last.Status != 429 || !strings.Contains(last.Header.Get("RateLimit-Policy"), `"package_write"`) {
			t.Fatalf("the 4th write in enforce: %d %v", last.Status, last.Header)
		}
	})
	codes := map[string]int{}
	for range 6 {
		a := e.st.Do(e.r, "GET", e.filePath, e.reader, nil)
		if a.Status == 429 {
			codes[a.Code(t)]++
			if a.Header.Get("Retry-After") == "" {
				t.Fatal("a 429 without Retry-After")
			}
		}
	}
	if codes["rate_limited"]+codes["download_limit_reached"] == 0 {
		t.Fatalf("enforce refused nothing: %v", codes)
	}
	t.Run("R-LIM-021 the problem names the figure", func(t *testing.T) {
		time.Sleep(2500 * time.Millisecond) // two burst windows: the admitted requests of the last one weigh on the next (R-ARCH-034)
		a := e.st.Do(e.r, "GET", e.filePath, e.reader, nil)
		if a.Status != 429 || a.Code(t) != "download_limit_reached" || a.JSON(t)["figure"] != "cachedBytesPerHour" {
			t.Fatalf("%d %s", a.Status, a.Body)
		}
		// R-LIM-020: the refusing figure's RateLimit policy beside the request figures', and the signed-in caller's link.
		if p := strings.Join(a.Header.Values("RateLimit-Policy"), ", "); !strings.Contains(p, `"cached-hour";q=`) || !strings.Contains(p, `qu="content-bytes"`) {
			t.Fatalf("RateLimit-Policy %q", p)
		}
		if d, _ := a.JSON(t)["detail"].(string); !strings.HasSuffix(d, "Your limits: http://"+testenv.DefaultHost+"/ui/profile/usage") {
			t.Fatalf("detail %q", d)
		}
	})
	t.Run("R-LIM-012 an administrator is unlimited", func(t *testing.T) {
		for i := range 6 {
			if a := e.st.Do(e.r, "GET", e.filePath, e.admin, nil); a.Status != 200 {
				t.Fatalf("administrator, request %d: %d", i, a.Status)
			}
		}
	})
}

// R-LIM-010: the built-in profiles the migrations write are the figures of limits.BuiltinProfiles, the request figures of the
// measurement included.
func TestR_LIM_010_TheBuiltInProfilesHoldTheFiguresOfTheMigrations(t *testing.T) {
	e := newEnv(t)
	a := e.st.Do(e.r, "GET", "/api/v1/limits/profiles", e.admin, nil)
	if a.Status != 200 {
		t.Fatalf("profiles: %d %s", a.Status, a.Body)
	}
	var list struct {
		Items []struct {
			Key     string         `json:"key"`
			Figures limits.Figures `json:"figures"`
			Builtin bool           `json:"builtin"`
		} `json:"items"`
	}
	if err := json.Unmarshal(a.Body, &list); err != nil {
		t.Fatal(err)
	}
	want := limits.BuiltinProfiles()
	seen := 0
	for _, p := range list.Items {
		if !p.Builtin {
			continue
		}
		seen++
		if p.Figures != want[p.Key] {
			t.Errorf("the built-in profile %s holds %v, want %v", p.Key, p.Figures, want[p.Key])
		}
	}
	if seen != len(want) {
		t.Fatalf("%d built-in profiles, want %d: %s", seen, len(want), a.Body)
	}
}

func TestR_LIM_003_AFailedCredentialIsARefusedRequestOfTheAddress(t *testing.T) {
	e := newEnv(t)
	for range 5 {
		if a := e.st.Do(e.r, "GET", e.filePath, "qdr_pat_"+strings.Repeat("x", 40), nil); a.Status != 401 {
			t.Fatalf("a wrong token: %d", a.Status)
		}
	}
	refused := `SELECT COALESCE(sum(refused), 0) FROM usage_bucket WHERE granularity = '5m' AND subject_kind = 'anonymous'`
	testenv.WaitFor(t, "five refused requests of the address in the usage store", 10*time.Second, func() bool { return e.st.Number(refused) >= 5 })
	if n := e.st.Number(`SELECT count(*) FROM usage_bucket WHERE subject_kind = 'user' AND refused > 0`); n != 0 {
		t.Fatalf("a user was charged %d refused rows", n)
	}
}

func TestR_LIM_034_TheUsageOverviewListsTheReader(t *testing.T) {
	e := newEnv(t)
	for range 3 {
		e.st.Do(e.r, "GET", e.filePath, e.reader, nil)
	}
	cached := `SELECT COALESCE(sum(cached_bytes), 0) FROM usage_bucket WHERE granularity = '5m' AND subject_kind = 'user' AND subject_ref = $1`
	testenv.WaitFor(t, "the reader's three downloads in the usage store", 10*time.Second, func() bool { return e.st.Number(cached, e.readerPrincipl) >= 3*fileSize })
	a := e.st.Do(e.r, "GET", "/api/v1/usage?kind=user", e.admin, nil)
	if a.Status != 200 {
		t.Fatalf("%d %s", a.Status, a.Body)
	}
	items, _ := a.JSON(t)["items"].([]any)
	found := false
	for _, it := range items {
		row := it.(map[string]any)
		if row["subject"].(map[string]any)["name"] == "reader" {
			found = true
			if cb, _ := row["cachedBytes"].(float64); cb < 3*fileSize {
				t.Fatalf("cachedBytes %v, want at least %d", cb, 3*fileSize)
			}
		}
	}
	if !found {
		t.Fatalf("the reader is not listed: %s", a.Body)
	}
	if a := e.st.Do(e.r, "GET", "/api/v1/users/me/usage", e.reader, nil); a.Status != 200 {
		t.Fatalf("own usage: %d %s", a.Status, a.Body)
	}
	if a := e.st.Do(e.r, "GET", "/api/v1/usage", e.reader, nil); a.Status != 403 {
		t.Fatalf("a reader reads the overview: %d", a.Status)
	}
	_ = strconv.Itoa
}

// R-LIM-027, Q-CLAMAV-12: a certificate subject's usage lists its certificates by serial, a renewed one beside the one it replaced.
// The rows are the ones the usage store writes for a client certificate's requests: the subject, and the serial as the credential.
func TestR_LIM_027_ACertificateSubjectsUsageListsItsCertificatesBySerial(t *testing.T) {
	e := newEnv(t)
	const subject = "CN=clamav-it.example.com,O=Example GmbH"
	for i, serial := range []string{"5c2f9e0b71a4d3e8", "0a7d33c19f6e2b45"} {
		e.st.Exec(`INSERT INTO usage_bucket (granularity, bucket_start, subject_kind, subject_ref, credential_ref, place, requests, cached_bytes)
			VALUES ('5m', date_trunc('minute', now()) - make_interval(mins => 10 + 5 * $3::int), 'certificate', $1, $2, 'instance', 2 + $3::int, 1000)`,
			subject, serial, i)
	}
	a := e.st.Do(e.r, "GET", "/api/v1/usage/subjects/certificate/"+url.PathEscape(subject), e.admin, nil)
	if a.Status != 200 {
		t.Fatalf("%d %s", a.Status, a.Body)
	}
	got := map[string]float64{}
	for _, c := range a.JSON(t)["credentials"].([]any) {
		row := c.(map[string]any)
		cred := row["credential"].(map[string]any)
		if cred["kind"] != "certificate" {
			t.Fatalf("a credential of the kind %v: %s", cred["kind"], a.Body)
		}
		got[cred["serial"].(string)] = row["requests"].(float64)
	}
	if len(got) != 2 || got["5c2f9e0b71a4d3e8"] != 2 || got["0a7d33c19f6e2b45"] != 3 {
		t.Fatalf("credentials %v, want both serials with 2 and 3 requests", got)
	}
}
