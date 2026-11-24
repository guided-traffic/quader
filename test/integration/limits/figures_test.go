//go:build integration

package limits

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/npmstub"
	"github.com/guided-traffic/quader/test/npmstub/corpus"
)

// The figures, the subjects, the block, the ceiling and the switch of the mode, each once and without a format's client: what is the
// same for every format is tested here, in seconds (R-LIM-039; docs/developer/e2e.md#what-a-new-format-adds-to-the-tests). What only a
// real client shows, how it reacts to a refusal, is one case in each format's own end-to-end set.
//
// Every rule of these tests sits at the repository place with the profile Unlimited and the one figure under test as an override, so a
// test reads as the figure it holds (R-LIM-011: an override sets a single figure of the profile).

// trustedLoopback lets a test name the client address in X-Forwarded-For (R-ARCH-006): two anonymous addresses from one test process.
const trustedLoopback = `QUADER_SERVER_TRUSTED_PROXIES=["127.0.0.0/8"]`

// rule writes a rule at a repository of the default project ("" is the instance place) and waits until the replica's index has it.
func (e *env) rule(t *testing.T, repo string, who, overrides map[string]any) {
	t.Helper()
	path := "/api/v1/limits/assignments"
	if repo != "" {
		path = "/api/v1/projects/" + testenv.DefaultProject + "/repositories/" + repo + "/limits/assignments"
	}
	a := e.st.Do(e.r, "POST", path, e.admin, map[string]any{"who": who, "profile": "unlimited", "overrides": overrides, "note": ""},
		"Idempotency-Key", testenv.RandHex(8))
	if a.Status != 201 {
		t.Fatalf("the rule %v %v at %q: %d %s", who, overrides, repo, a.Status, a.Body)
	}
	e.awaitIndex(t)
}

func every(kind string) map[string]any { return map[string]any{"kind": "every", "callerKind": kind} }

// from is the header that makes a request come from an address (trustedLoopback).
func from(addr string) []string { return []string{"X-Forwarded-For", addr} }

// refusal is a 429 of the limits with its problem members.
func refusal(t *testing.T, a testenv.Answer, what, code, figure string) {
	t.Helper()
	if a.Status != 429 || a.Code(t) != code {
		t.Fatalf("%s: %d %s, want 429 %s", what, a.Status, a.Body, code)
	}
	if figure != "" && a.JSON(t)["figure"] != figure {
		t.Fatalf("%s: the problem names the figure %v, want %s", what, a.JSON(t)["figure"], figure)
	}
	if s, err := strconv.Atoi(a.Header.Get("Retry-After")); err != nil || s < 1 {
		t.Fatalf("%s: Retry-After %q", what, a.Header.Get("Retry-After"))
	}
}

// R-LIM-004, R-LIM-020, R-LIM-021: the request figure of the hour and the day's volume. The download that crosses the volume completes
// and is charged; the next answers 429 before its first byte, a HEAD (no download) is admitted.
func TestR_LIM_021_TheDownloadThatCrossesTheDayCompletesAndTheNextIsRefusedBeforeItsFirstByte(t *testing.T) {
	var tok string
	e := newEnvWith(t, envOptions{valkey: true, seed: func(st *testenv.Stack) {
		sam := st.User("sam")
		st.Policy("sam-reads", []string{"read"}, []string{rFiles}, sam)
		tok = st.Token(sam, scope.Default())
	}})
	e.rule(t, rFiles, every("user"), map[string]any{"bytesPerDay": 1})
	if a := e.st.Do(e.r, "GET", e.filePath, e.reader, nil); a.Status != 200 || len(a.Body) != fileSize {
		t.Fatalf("the download that crosses the day's volume: %d, %d bytes", a.Status, len(a.Body))
	}
	e.awaitFlush(t) // the bytes reach the window at the flush
	a := e.st.Do(e.r, "GET", e.filePath, e.reader, nil)
	refusal(t, a, "the next download", "download_limit_reached", "bytesPerDay")
	if !strings.HasPrefix(a.Header.Get("Content-Type"), "application/problem+json") || len(a.Body) >= fileSize {
		t.Fatalf("the refusal is not a problem before the first byte: %s, %d bytes", a.Header.Get("Content-Type"), len(a.Body))
	}
	if ws, _ := a.JSON(t)["windowSeconds"].(float64); ws != 86400 {
		t.Fatalf("windowSeconds %v, want 86400", a.JSON(t)["windowSeconds"])
	}
	if h := e.st.Do(e.r, "HEAD", e.filePath, e.reader, nil); h.Status != 200 {
		t.Fatalf("a HEAD with the day's volume used up: %d", h.Status)
	}

	t.Run("R-LIM-004 requestsPerHour refuses the request after the hour's last with rate_limited", func(t *testing.T) {
		e.rule(t, rFiles, map[string]any{"kind": "user", "user": "ada"}, map[string]any{"requestsPerHour": 2})
		// ada is an administrator: decided at the instance only (R-LIM-012), so the rule above never applies to her
		for i := range 4 {
			if h := e.st.Do(e.r, "HEAD", e.filePath, e.admin, nil); h.Status != 200 {
				t.Fatalf("the administrator's request %d: %d", i, h.Status)
			}
		}
		e.rule(t, rFiles, map[string]any{"kind": "user", "user": "sam"}, map[string]any{"requestsPerHour": 2})
		for i := range 2 {
			if h := e.st.Do(e.r, "HEAD", e.filePath, tok, nil); h.Status != 200 {
				t.Fatalf("request %d of the hour: %d %s", i, h.Status, h.Body)
			}
		}
		a := e.st.Do(e.r, "GET", e.filePath, tok, nil)
		refusal(t, a, "the third request of the hour", "rate_limited", "requestsPerHour")
		if s, _ := strconv.Atoi(a.Header.Get("Retry-After")); s < 60 {
			t.Fatalf("Retry-After %d of an hour's window, want the time until the window has room again", s)
		}
	})
}

// R-LIM-023, R-LIM-024: a download beyond cachedBytesPerSecond is slowed and completes, never refused; while it holds the subject's one
// parallel place, a further download is refused (the wait is 0 in this stack). In audit neither happens.
func TestR_LIM_024_AFurtherDownloadIsRefusedWhileTheOnlyPlaceIsTakenAndTheFirstIsSlowed(t *testing.T) {
	e := newEnvWith(t, envOptions{valkey: true, env: []string{"QUADER_LIMITS_PACING_MIN_BYTES_PER_SECOND=16384"}})
	e.rule(t, rFiles, every("user"), map[string]any{"cachedBytesPerSecond": 1, "parallelDownloads": 1})
	var first testenv.Answer
	var took time.Duration
	var wg sync.WaitGroup
	wg.Go(func() {
		start := time.Now()
		first = e.st.Do(e.r, "GET", e.filePath, e.reader, nil)
		took = time.Since(start)
	})
	time.Sleep(700 * time.Millisecond)
	second := e.st.Do(e.r, "GET", e.filePath, e.reader, nil)
	wg.Wait()
	refusal(t, second, "the second download while the first runs", "too_many_parallel_downloads", "parallelDownloads")
	if first.Status != 200 || len(first.Body) != fileSize {
		t.Fatalf("the slowed download: %d, %d bytes", first.Status, len(first.Body))
	}
	// 64 KiB at the floor of 16 KiB a second: about 4 s; the bucket's first second is its burst
	if took < 2500*time.Millisecond {
		t.Fatalf("the download took %s at 1 byte a second, want it slowed to the floor (about 4 s)", took)
	}
	t.Logf("64 KiB paced at the floor of 16 KiB/s took %s; the parallel one was refused at once", took.Round(10*time.Millisecond))

	t.Run("R-LIM-026 in audit neither is applied", func(t *testing.T) {
		e.setMode(t, "audit")
		e.awaitIndex(t)
		var wg sync.WaitGroup
		res := make([]testenv.Answer, 2)
		start := time.Now()
		for i := range 2 {
			wg.Go(func() { res[i] = e.st.Do(e.r, "GET", e.filePath, e.reader, nil) })
		}
		wg.Wait()
		for i, a := range res {
			if a.Status != 200 {
				t.Fatalf("download %d in audit: %d %s", i, a.Status, a.Body)
			}
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("two downloads in audit took %s: slowed", d)
		}
	})
}

// R-LIM-024: downloads that wait for a parallel slot hold at most a quarter of the endpoint's request slots. With eight slots two
// may wait; the next is refused at once, and another caller is served beside them. The first download is paced to the floor so it holds
// the reader's one parallel place for about two seconds.
func TestR_LIM_024_WaitingDownloadsHoldAtMostTheirShareOfTheRequestSlots(t *testing.T) {
	var other string
	e := newEnvWith(t, envOptions{valkey: true, seed: func(st *testenv.Stack) {
		o := st.User("other")
		st.Policy("other-reads", []string{"read"}, []string{rFiles}, o)
		other = st.Token(o, scope.Default())
	}, env: []string{"QUADER_LIMITS_PARALLEL_WAIT=10s", "QUADER_SERVER_MAX_IN_FLIGHT=8", "QUADER_LIMITS_PACING_MIN_BYTES_PER_SECOND=32768"}})
	e.rule(t, rFiles, every("user"), map[string]any{"cachedBytesPerSecond": 1, "parallelDownloads": 1})
	var wg sync.WaitGroup
	statuses := make([]int, 3)
	for i := range 3 { // the first holds the place, the next two wait for it
		wg.Go(func() { statuses[i] = e.st.Do(e.r, "GET", e.filePath, e.reader, nil).Status })
		time.Sleep(300 * time.Millisecond)
	}
	start := time.Now()
	refusal(t, e.st.Do(e.r, "GET", e.filePath, e.reader, nil), "the download beyond the waiting share", "too_many_parallel_downloads", "parallelDownloads")
	if took := time.Since(start); took > time.Second {
		t.Fatalf("the download beyond the waiting share was refused after %s, want at once", took)
	}
	if a := e.st.Do(e.r, "HEAD", e.filePath, other, nil); a.Status != 200 {
		t.Fatalf("another caller beside the waiting downloads: %d", a.Status)
	}
	wg.Wait()
	if statuses[0] != 200 {
		t.Fatalf("the download that held the place: %d", statuses[0])
	}
	t.Logf("the downloads that waited ended %v", statuses[1:])
}

// R-LIM-025, Q-LIM-7, Q-LIM-15: refused requests above refusedPerHour block the budget key, and only it: a caller who reads only what it
// may read (a miss included) is never blocked; a valid credential from the blocked address is not; the block ends when it is lifted. In
// audit no block is set.
func TestR_LIM_025_RefusedRequestsBlockTheBudgetKeyAndNothingElse(t *testing.T) {
	var prober, prober2 string
	e := newEnvWith(t, envOptions{valkey: true, env: []string{trustedLoopback}, seed: func(st *testenv.Stack) {
		prober = st.Token(st.ServiceAccount("prober"), scope.Scope{Inherit: true})
		prober2 = st.Token(st.ServiceAccount("prober2"), scope.Scope{Inherit: true})
		st.Repository("open", "local", "generic", `{}`) // public, without a rule of its own: decided at the instance place
		st.Exec(`UPDATE project SET visibility = 'public'`)
		st.Exec(`UPDATE repositories SET visibility_artifacts = 'public' WHERE key IN ($1, 'open')`, rFiles)
	}})
	e.rule(t, rFiles, every("technicalToken"), map[string]any{"refusedPerHour": 3})
	e.rule(t, rFiles, every("user"), map[string]any{"refusedPerHour": 3})
	e.rule(t, rFiles, every("anonymous"), map[string]any{"refusedPerHour": 3})
	missing := "/files/it/" + rFiles + "/missing.bin"

	t.Run("Q-LIM-7 a reader's misses are not refused requests", func(t *testing.T) {
		for range 5 {
			if a := e.st.Do(e.r, "GET", missing, e.reader, nil); a.Status != 404 {
				t.Fatalf("a miss: %d", a.Status)
			}
		}
		e.awaitFlush(t) // had the misses counted, the flush would have passed the limit
		if a := e.st.Do(e.r, "GET", e.filePath, e.reader, nil); a.Status != 200 {
			t.Fatalf("the reader after five misses: %d %s", a.Status, a.Body)
		}
	})

	t.Run("a token that may not write the repository is blocked after its fourth refusal", func(t *testing.T) {
		// prober reads the public file, as everyone may, and is refused every write: a 403 of the decision point
		for i := range 4 {
			if a := e.st.Do(e.r, "PUT", "/files/it/"+rFiles+"/p"+strconv.Itoa(i)+".bin", prober, []byte("x"), "Content-Type", "application/octet-stream"); a.Status != 403 {
				t.Fatalf("write %d of a reader: %d %s", i, a.Status, a.Body)
			}
		}
		e.awaitFlush(t) // the flush passes the limit and sets the marker
		a := e.st.Do(e.r, "GET", e.filePath, prober, nil)
		refusal(t, a, "the blocked token's read", "caller_blocked", "")
		if s, _ := strconv.Atoi(a.Header.Get("Retry-After")); s > 900 {
			t.Fatalf("Retry-After %d, want at most the block time of 900 s", s)
		}
		if a := e.st.Do(e.r, "GET", e.filePath, prober2, nil); a.Status != 200 {
			t.Fatalf("another token: %d %s", a.Status, a.Body)
		}
	})

	t.Run("H-1010 an address blocked by wrong credentials still serves a valid one", func(t *testing.T) {
		// A failed credential is a refused request of the address at the place of the repository it named (R-LIM-003); "open" has no
		// rule, so the address's rule at the instance applies.
		e.rule(t, "", map[string]any{"kind": "address", "prefix": "198.51.100.20/32"}, map[string]any{"refusedPerHour": 3})
		for range 4 {
			if a := e.st.Do(e.r, "GET", "/files/it/open/x", "qdr_pat_"+strings.Repeat("y", 40), nil, from("198.51.100.20")...); a.Status != 401 {
				t.Fatalf("a wrong token: %d", a.Status)
			}
		}
		e.awaitFlush(t)
		refusal(t, e.st.Do(e.r, "GET", "/files/it/open/x", "", nil, from("198.51.100.20")...), "an anonymous read from the blocked address", "caller_blocked", "")
		if a := e.st.Do(e.r, "GET", "/files/it/open/x", e.reader, nil, from("198.51.100.20")...); a.Status != 404 {
			t.Fatalf("a valid credential from the same address: %d %s, want the 404 of a missing file", a.Status, a.Body)
		}
		// Q-LIM-15: the block holds the address at the instance place only; the repository's own rule is another budget key
		if a := e.st.Do(e.r, "GET", e.filePath, "", nil, from("198.51.100.20")...); a.Status != 200 {
			t.Fatalf("the address at a repository with a rule of its own: %d %s", a.Status, a.Body)
		}
	})

	t.Run("R-LIM-033 lifting the block ends it", func(t *testing.T) {
		a := e.st.Do(e.r, "GET", "/api/v1/limits/blocks?place=repository:it/"+rFiles, e.admin, nil)
		items, _ := a.JSON(t)["items"].([]any)
		lifted := 0
		for _, it := range items {
			b := it.(map[string]any)
			if b["subject"].(map[string]any)["kind"] != "technicalToken" {
				continue
			}
			if d := e.st.Do(e.r, "DELETE", "/api/v1/limits/blocks/"+b["id"].(string), e.admin, nil); d.Status != 204 {
				t.Fatalf("lift: %d %s", d.Status, d.Body)
			}
			lifted++
		}
		if lifted != 1 {
			t.Fatalf("%d blocks of a technical token at the repository, want 1: %s", lifted, a.Body)
		}
		if a := e.st.Do(e.r, "GET", e.filePath, prober, nil); a.Status != 200 {
			t.Fatalf("the token after the lift: %d %s", a.Status, a.Body)
		}
	})

	t.Run("R-LIM-026 in audit no block is set and would_limit is recorded", func(t *testing.T) {
		e.setMode(t, "audit")
		e.awaitIndex(t)
		for i := range 5 {
			e.st.Do(e.r, "PUT", "/files/it/"+rFiles+"/q"+strconv.Itoa(i)+".bin", prober2, []byte("x"), "Content-Type", "application/octet-stream")
		}
		e.awaitFlush(t) // in enforce the flush would set the block
		if a := e.st.Do(e.r, "GET", e.filePath, prober2, nil); a.Status != 200 {
			t.Fatalf("audit blocked: %d %s", a.Status, a.Body)
		}
		e.awaitRows(t, "a would_limit of refusedPerHour", `SELECT count(*) FROM usage_limit_event WHERE figure = 'refusedPerHour' AND mode = 'audit'`)
	})
}

// R-LIM-017, R-LIM-020, R-LIM-008, R-LIM-025, Q-LIM-58: a refused request is not counted in the request windows, so a caller at one
// request a second that retries every second, as Retry-After tells it, is admitted again; a caller that keeps sending is held to its
// figure's rate, its refusals are no refused requests, and refusedPerHour does not block it. HEADs carry no body: the requests are the
// payload.
func TestR_LIM_017_ARefusedRequestFillsNoRequestWindowAndAHammeringCallerIsNotBlocked(t *testing.T) {
	var hammer, technical string
	e := newEnvWith(t, envOptions{valkey: true, seed: func(st *testenv.Stack) {
		sa := st.ServiceAccount("hammer")
		hammer = st.Token(sa, scope.Scope{Inherit: true})
		technical = st.Text(`SELECT technical_token_id::text FROM api_token WHERE principal_id = $1::uuid`, sa.ID)
		st.Policy("hammer-reads", []string{"read"}, []string{rFiles}, sa)
	}})
	e.rule(t, rFiles, every("user"), map[string]any{"requestsPerSecond": 1, "requestsBurst": 1})
	e.rule(t, rFiles, every("technicalToken"), map[string]any{"requestsPerSecond": 1, "requestsBurst": 1, "refusedPerHour": 5})

	t.Run("R-LIM-017 R-LIM-020 a caller that retries every second is admitted again, and after the Retry-After of a refusal", func(t *testing.T) {
		admitted, refused := 0, 0
		for range 6 {
			switch a := e.st.Do(e.r, "HEAD", e.filePath, e.reader, nil); a.Status {
			case 200:
				admitted++
			case 429:
				refused++
			default:
				t.Fatalf("%d %s", a.Status, a.Body)
			}
			time.Sleep(time.Second)
		}
		// an admitted request weighs on the next window: every second retry fits; when refusals were counted none did after the first
		if admitted < 3 || refused == 0 {
			t.Fatalf("%d of 6 requests a second apart admitted and %d refused, want every second one admitted", admitted, refused)
		}
		var a testenv.Answer
		for range 3 { // two at once: the second is refused
			if a = e.st.Do(e.r, "HEAD", e.filePath, e.reader, nil); a.Status == 429 {
				break
			}
		}
		s, _ := strconv.Atoi(a.Header.Get("Retry-After"))
		wait := time.Duration(s) * time.Second
		if a.Status != 429 || wait < time.Second {
			t.Fatalf("no refusal with a Retry-After: %d %q", a.Status, a.Header.Get("Retry-After"))
		}
		time.Sleep(wait)
		if a := e.st.Do(e.r, "HEAD", e.filePath, e.reader, nil); a.Status != 200 {
			t.Fatalf("refused after waiting the Retry-After of %s: %d %s", wait, a.Status, a.Body)
		}
	})

	t.Run("R-LIM-008 R-LIM-025 Q-LIM-58 a caller that keeps sending gets its figure's rate, counts no refused request and is not blocked", func(t *testing.T) {
		// 3 s of 20 requests a second at a figure of one a second and refusedPerHour 5: under the taken-back rule the ~55 refusals
		// would have blocked the token within the first second.
		admitted, limited := 0, 0
		for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
			switch a := e.st.Do(e.r, "HEAD", e.filePath, hammer, nil); {
			case a.Status == 200:
				admitted++
			case a.Status == 429 && a.Header.Get("Retry-After") != "":
				limited++ // a HEAD has no body; caller_blocked would show in the GET below
			default:
				t.Fatalf("%d %v", a.Status, a.Header)
			}
		}
		// one a second, and an admitted request weighs on the next window: about every second second, at most one per window
		if admitted < 1 || admitted > 4 || limited < 20 {
			t.Fatalf("3 s of hammering at one request a second: %d admitted, %d refused", admitted, limited)
		}
		e.awaitFlush(t) // had the refusals counted, the flush would have set the block
		a := e.st.Do(e.r, "GET", e.filePath, hammer, nil)
		if a.Status == 429 {
			if a.Code(t) == "caller_blocked" {
				t.Fatalf("the token was blocked by %d refusals of its request figure: %s", limited, a.Body)
			}
			s, _ := strconv.Atoi(a.Header.Get("Retry-After"))
			time.Sleep(time.Duration(s) * time.Second)
			a = e.st.Do(e.r, "GET", e.filePath, hammer, nil)
		}
		if a.Status != 200 {
			t.Fatalf("the token after waiting the Retry-After: %d %s", a.Status, a.Body)
		}
		d := e.st.Do(e.r, "GET", "/api/v1/usage/subjects/technicalToken/"+technical, e.admin, nil)
		if d.Status != 200 {
			t.Fatalf("the token's usage: %d %s", d.Status, d.Body)
		}
		budgets, _ := d.JSON(t)["budgets"].([]any)
		if len(budgets) == 0 {
			t.Fatalf("the token has no budget key: %s", d.Body)
		}
		for _, b := range budgets {
			fig := b.(map[string]any)["figures"].(map[string]any)["refusedPerHour"].(map[string]any)
			if fig["used"] != float64(0) || b.(map[string]any)["blockedUntil"] != nil {
				t.Fatalf("refusedPerHour after %d refusals of the request figure: %v, blocked until %v", limited, fig, b.(map[string]any)["blockedUntil"])
			}
		}
		if n := e.st.Number(`SELECT COALESCE(sum(refused), 0) FROM usage_bucket WHERE granularity = '5m' AND subject_kind = 'technical_token' AND subject_ref = $1`, technical); n != 0 {
			t.Fatalf("%d refused requests recorded for the token, want 0", n)
		}
	})
}

// R-LIM-001, R-LIM-002, R-LIM-012, R-LIM-038: every subject has a budget of its own at a place. A user's personal tokens share one; each
// technical token of an account has its own, at every level of account; each address has its own; an administrator is unlimited.
func TestR_LIM_001_EachSubjectSpendsItsOwnBudget(t *testing.T) {
	var pat1, pat2, inst1, inst2 string
	e := newEnvWith(t, envOptions{valkey: true, env: []string{trustedLoopback}, seed: func(st *testenv.Stack) {
		u := st.User("uma")
		st.Policy("uma-reads", []string{"read"}, []string{rFiles}, u)
		pat1, pat2 = st.Token(u, scope.Default()), st.Token(u, scope.Default())
		sa := st.ServiceAccount("deploy")
		st.Policy("deploy-reads", []string{"read"}, []string{rFiles}, sa)
		inst1, inst2 = st.Token(sa, scope.Scope{Inherit: true}), st.Token(sa, scope.Scope{Inherit: true})
		st.Exec(`UPDATE project SET visibility = 'public'`)
		st.Exec(`UPDATE repositories SET visibility_artifacts = 'public' WHERE key = $1`, rFiles)
	}})
	for _, kind := range []string{"user", "technicalToken", "anonymous"} {
		e.rule(t, rFiles, every(kind), map[string]any{"requestsPerHour": 2})
	}
	spend := func(t *testing.T, what, tok string, h ...string) {
		t.Helper()
		if a := e.st.Do(e.r, "HEAD", e.filePath, tok, nil, h...); a.Status != 200 {
			t.Fatalf("%s: %d %s", what, a.Status, a.Body)
		}
	}
	refused := func(t *testing.T, what, tok string, h ...string) {
		t.Helper()
		refusal(t, e.st.Do(e.r, "GET", e.filePath, tok, nil, h...), what, "rate_limited", "requestsPerHour")
	}
	technical := func(t *testing.T, base string) (string, string) {
		t.Helper()
		var toks []string
		for i := range 2 {
			a := e.st.Do(e.r, "POST", base+"/technical-tokens", e.admin, map[string]any{
				"name": "t" + strconv.Itoa(i), "description": "", "ttlSeconds": 3600, "scope": map[string]any{"inherit": true},
			}, "Idempotency-Key", testenv.RandHex(8))
			if a.Status != 201 {
				t.Fatalf("a technical token of %s: %d %s", base, a.Status, a.Body)
			}
			toks = append(toks, a.JSON(t)["token"].(string))
		}
		return toks[0], toks[1]
	}

	t.Run("a user's personal tokens share the user's budget", func(t *testing.T) {
		spend(t, "the first token", pat1)
		spend(t, "the second token", pat2)
		refused(t, "the first token again", pat1)
	})
	t.Run("R-LIM-038 two technical tokens of one account have two budgets, at every level", func(t *testing.T) {
		levels := map[string][2]string{"instance": {inst1, inst2}}
		for level, path := range map[string]string{
			"project":    "/api/v1/projects/" + testenv.DefaultProject + "/service-accounts",
			"repository": "/api/v1/projects/" + testenv.DefaultProject + "/repositories/" + rFiles + "/service-accounts",
		} {
			body := map[string]any{"name": level + "-acct", "description": ""}
			if level == "repository" {
				body["actions"] = []string{"read"}
			}
			if a := e.st.Do(e.r, "POST", path, e.admin, body, "Idempotency-Key", testenv.RandHex(8)); a.Status != 201 {
				t.Fatalf("the %s account: %d %s", level, a.Status, a.Body)
			}
			a, b := technical(t, path+"/"+level+"-acct")
			levels[level] = [2]string{a, b}
		}
		for level, toks := range levels {
			spend(t, level+" token A", toks[0])
			spend(t, level+" token A", toks[0])
			refused(t, level+" token A, the third", toks[0])
			spend(t, level+" token B of the same account", toks[1])
		}
	})
	t.Run("R-LIM-002 each address has its own budget", func(t *testing.T) {
		spend(t, "address A", "", from("198.51.100.30")...)
		spend(t, "address A", "", from("198.51.100.30")...)
		refused(t, "address A, the third", "", from("198.51.100.30")...)
		spend(t, "address B", "", from("198.51.100.31")...)
		// IPv6 per /64: two hosts of one /64 share one budget
		spend(t, "2001:db8:1:2::10", "", from("2001:db8:1:2::10")...)
		spend(t, "2001:db8:1:2:ffff::1", "", from("2001:db8:1:2:ffff::1")...)
		refused(t, "the /64's third", "", from("2001:db8:1:2::77")...)
		spend(t, "another /64", "", from("2001:db8:1:3::1")...)
	})
	t.Run("R-LIM-012 an administrator is unlimited", func(t *testing.T) {
		for i := range 4 {
			spend(t, "administrator "+strconv.Itoa(i), e.admin)
		}
	})
}

// R-LIM-015, R-LIM-022: with the caller's upstream budget used up, what is cached is still served and what needs the upstream answers
// upstream_limit_reached; the remote's ceiling for all anonymous callers holds a second address too, while a signed-in caller is served.
func TestR_LIM_022_WhatIsCachedIsStillServedAndTheCeilingHoldsTheWholeClass(t *testing.T) {
	stub, err := npmstub.New(npmstub.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ss, err := npmstub.NewSelfSigned("localhost", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	run, err := stub.Start("127.0.0.1:0", ss.ServerConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = run.Close() })
	ca := filepath.Join(t.TempDir(), "stub-ca.pem")
	if err := os.WriteFile(ca, ss.CAPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	const remote = "npm-remote"
	e := newEnvWith(t, envOptions{valkey: true, env: []string{
		trustedLoopback, "QUADER_PROXY_OUTBOUND_TLS_CA_FILES=[" + strconv.Quote(ca) + "]", `QUADER_PROXY_OUTBOUND_ADDRESS_POLICY_ALLOW_CIDRS=["127.0.0.1/32"]`,
	}, readable: []string{remote}, seed: func(st *testenv.Stack) {
		st.Repository(remote, "remote", "npm", fmt.Sprintf(`{"remote":{"url":%q,"outboundProxy":{"mode":"direct"},"cache":{"metadataTtlSeconds":3600}}}`, run.URL+"/"))
		st.Exec(`UPDATE project SET visibility = 'public'`)
		st.Exec(`UPDATE repositories SET visibility_artifacts = 'public' WHERE key = $1`, remote)
	}})
	var names []string
	for _, p := range corpus.Default().Packages {
		if !strings.HasPrefix(p.Name, "@") && len(names) < 3 {
			names = append(names, p.Name)
		}
	}
	doc := func(name string) string { return "/api/npm/it/" + remote + "/" + name }
	if a := e.st.Do(e.r, "PUT", "/api/v1/projects/it/repositories/"+remote+"/limits/ceilings", e.admin,
		map[string]any{"anonymous": map[string]any{"upstreamRequestsPerHour": 1, "uncachedBytesPerHour": nil}, "signedIn": map[string]any{"upstreamRequestsPerHour": nil, "uncachedBytesPerHour": nil}},
		"If-Match", "*"); a.Status != 200 {
		t.Fatalf("ceilings: %d %s", a.Status, a.Body)
	}
	e.rule(t, remote, every("user"), map[string]any{"uncachedBytesPerHour": 1})

	t.Run("the anonymous ceiling holds a second address", func(t *testing.T) {
		if a := e.st.Do(e.r, "GET", doc(names[0]), "", nil, from("198.51.100.40")...); a.Status != 200 {
			t.Fatalf("the first anonymous fill: %d %s", a.Status, a.Body)
		}
		a := e.st.Do(e.r, "GET", doc(names[1]), "", nil, from("198.51.100.41")...)
		refusal(t, a, "a second address's fill", "upstream_limit_reached", "")
		// npm's own error shape (R-LIM-020): error, message and code
		if d, _ := a.JSON(t)["error"].(string); !strings.Contains(d, "of all anonymous callers of this repository") {
			t.Fatalf("the detail does not name the class: %q", d)
		}
		if a := e.st.Do(e.r, "GET", doc(names[0]), "", nil, from("198.51.100.41")...); a.Status != 200 {
			t.Fatalf("the cached document for the second address: %d %s", a.Status, a.Body)
		}
	})
	t.Run("a signed-in caller is served beside the anonymous ceiling, until its own upstream bytes are used up", func(t *testing.T) {
		if a := e.st.Do(e.r, "GET", doc(names[1]), e.reader, nil); a.Status != 200 {
			t.Fatalf("the reader's fill: %d %s", a.Status, a.Body)
		}
		e.awaitFlush(t) // the uncached bytes reach the window at the flush
		a := e.st.Do(e.r, "GET", doc(names[2]), e.reader, nil)
		refusal(t, a, "the reader's next fill", "upstream_limit_reached", "")
		if d, _ := a.JSON(t)["error"].(string); !strings.Contains(d, "The upstream limit of 1 bytes") {
			t.Fatalf("the error does not name the figure: %q", d)
		}
		if a := e.st.Do(e.r, "GET", doc(names[1]), e.reader, nil); a.Status != 200 {
			t.Fatalf("the reader's cached document: %d %s", a.Status, a.Body)
		}
	})
	if n := stub.Counts().Documents(names[2]); n != 0 {
		t.Fatalf("the upstream was asked %d times for a document no budget admitted", n)
	}
}

// R-LIM-026: a change of the mode takes effect on every replica within seconds, without a restart.
func TestR_LIM_026_TheSwitchReachesEveryReplicaWithinSeconds(t *testing.T) {
	e := newEnv(t)
	other := e.st.Start(t, testenv.ReplicaOptions{Name: "web-2"})
	e.rule(t, rFiles, every("user"), map[string]any{"requestsPerHour": 1})
	if a := e.st.Do(e.r, "HEAD", e.filePath, e.reader, nil); a.Status != 200 {
		t.Fatalf("the hour's one request: %d", a.Status)
	}
	for _, r := range []*testenv.Replica{e.r, other} {
		refusal(t, e.st.Do(r, "GET", e.filePath, e.reader, nil), "the second request", "rate_limited", "requestsPerHour")
	}
	switchTo := func(mode string, want int) time.Duration {
		e.setMode(t, mode)
		return testenv.WaitFor(t, "the mode "+mode+" on the second replica", 10*time.Second, func() bool {
			return e.st.Do(other, "HEAD", e.filePath, e.reader, nil).Status == want
		})
	}
	toAudit := switchTo("audit", 200)
	toEnforce := switchTo("enforce", 429)
	t.Logf("the second replica followed the switch to audit in %s and back to enforce in %s", toAudit.Round(time.Millisecond), toEnforce.Round(time.Millisecond))
}
