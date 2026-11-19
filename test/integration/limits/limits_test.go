//go:build integration

// Package limits tests the download limits of usage-and-limits.md against the integration stack: replicas started through the
// composition root with Valkey, so the real chain's stage 7, the format host's admission, the charge buffer and the usage store run.
package limits

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/scope"
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
func newEnv(t *testing.T) *env {
	t.Helper()
	st := testenv.NewStack(t, testenv.StackOptions{Name: "limits", Valkey: true, Env: []string{
		"QUADER_LIMITS_FLUSH_INTERVAL=200ms", "QUADER_LIMITS_USAGE_FLUSH_INTERVAL=1s", "QUADER_LIMITS_PARALLEL_WAIT=0s",
	}})
	ada, reader := st.User("ada"), st.User("reader")
	st.Bind("admin", ada)
	e := &env{st: st, readerPerson: reader, readerPrincipl: reader.ID}
	e.admin = st.Token(ada, scope.Scope{Inherit: true, Admin: true})
	e.reader = st.Token(reader, scope.Default())
	st.Repository(rFiles, "local", "generic", `{}`)
	st.Policy("readers", []string{"read"}, []string{rFiles}, reader)
	st.Policy("writers", []string{"read", "deploy"}, []string{rFiles}, ada)
	e.r = st.Start(t, testenv.ReplicaOptions{Name: "web-1"})
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

func TestR_LIM_026_AuditRefusesNothingNewAndEnforceRefuses(t *testing.T) {
	e := newEnv(t)
	e.setStandard(t, map[string]any{"requestsPerSecond": 2, "requestsBurst": 2, "cachedBytesPerHour": fileSize + 1})
	time.Sleep(1500 * time.Millisecond) // the change area reaches the replica's index
	for i := range 6 {
		if a := e.st.Do(e.r, "GET", e.filePath, e.reader, nil); a.Status != 200 {
			t.Fatalf("audit, request %d: %d %s", i, a.Status, a.Body)
		} else if strings.Contains(a.Header.Get("RateLimit-Policy"), `"requests"`) {
			t.Fatalf("audit sends the RateLimit member of a figure: %q", a.Header.Get("RateLimit-Policy"))
		}
	}
	time.Sleep(2 * time.Second) // the usage buffer flushes
	if n := e.st.Number(`SELECT count(*) FROM usage_limit_event WHERE mode = 'audit' AND subject_ref = $1`, e.readerPrincipl); n == 0 {
		t.Fatal("audit recorded no would_limit event")
	}
	e.setMode(t, "enforce")
	time.Sleep(1500 * time.Millisecond)
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
		time.Sleep(2500 * time.Millisecond) // two burst windows: the two-window estimate counts the refused requests too (R-ARCH-034)
		a := e.st.Do(e.r, "GET", e.filePath, e.reader, nil)
		if a.Status != 429 || a.Code(t) != "download_limit_reached" || a.JSON(t)["figure"] != "cachedBytesPerHour" {
			t.Fatalf("%d %s", a.Status, a.Body)
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

func TestR_LIM_003_AFailedCredentialIsARefusedRequestOfTheAddress(t *testing.T) {
	e := newEnv(t)
	for range 5 {
		if a := e.st.Do(e.r, "GET", e.filePath, "qdr_pat_"+strings.Repeat("x", 40), nil); a.Status != 401 {
			t.Fatalf("a wrong token: %d", a.Status)
		}
	}
	time.Sleep(2500 * time.Millisecond)
	if n := e.st.Number(`SELECT COALESCE(sum(refused), 0) FROM usage_bucket WHERE granularity = '5m' AND subject_kind = 'anonymous'`); n < 5 {
		t.Fatalf("%d refused requests of the address, want 5", n)
	}
	if n := e.st.Number(`SELECT count(*) FROM usage_bucket WHERE subject_kind = 'user' AND refused > 0`); n != 0 {
		t.Fatalf("a user was charged %d refused rows", n)
	}
}

func TestR_LIM_034_TheUsageOverviewListsTheReader(t *testing.T) {
	e := newEnv(t)
	for range 3 {
		e.st.Do(e.r, "GET", e.filePath, e.reader, nil)
	}
	time.Sleep(2500 * time.Millisecond)
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
