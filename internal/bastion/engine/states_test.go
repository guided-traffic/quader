package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/guided-traffic/quader/internal/bastion/answers"
	"github.com/guided-traffic/quader/internal/bastion/guard"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/observability"
)

// ---- R-BAS-027 --------------------------------------------------------------------------------------------

func TestR_BAS_027_AnswersHeldServeCachedContentForThePeriodAndTheGraceThenNothing(t *testing.T) {
	s := newSiteHarness(t, options{})
	var paths []string
	for i := 0; i < 5; i++ {
		p := fmt.Sprintf("/api/npm/web/npm-all/pkg%d.tgz", i)
		s.site.artifacts[p] = payload(300 + i)
		paths = append(paths, p)
		s.get(p, "Authorization", bearer(s.full))
	}
	s.site.docs[doc] = "all packages"
	s.get(doc, "Authorization", bearer(s.full))
	s.clock.advance(2 * time.Minute)
	s.setUnreachable(true)
	before := s.main.count()

	// seen two minutes before: the warm run completes, also past the revalidation periods
	s.clock.advance(6 * time.Minute) // 8 minutes: past the period (5 m), inside period + grace (20 m)
	for _, p := range paths {
		if w := s.get(p, "Authorization", bearer(s.full)); w.Code != 200 {
			t.Fatalf("%s: %d %s", p, w.Code, w.Body)
		}
	}
	if w := s.get(doc, "Authorization", bearer(s.full)); w.Code != 200 || w.Body.String() != "all packages" {
		t.Fatalf("%d", w.Code)
	}
	// a token never seen, and a miss, get 503 unavailable with Retry-After: 30
	for name, c := range map[string][]string{
		"an unseen token": {"/api/npm/web/npm-all/pkg0.tgz", bearer(s.part)},
		"a cold package":  {"/api/npm/web/npm-all/cold.tgz", bearer(s.full)},
		"anonymous":       {"/api/npm/web/npm-all/pkg0.tgz", ""},
	} {
		hdr := []string{}
		if c[1] != "" {
			hdr = []string{"Authorization", c[1]}
		}
		w := s.get(c[0], hdr...)
		if w.Code != 503 || problemCode(w) != "unavailable" || w.Header().Get("Retry-After") != "30" || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: %d %s %v", name, w.Code, w.Body, w.Header())
		}
	}
	if s.main.count() != before {
		t.Fatalf("%d requests went to the main installation while it was unreachable", s.main.count()-before)
	}
	if counterValue(t, s.harness, "refused", metrics.RefusedUnreachable) != 3 {
		t.Fatal("the refusals are not counted")
	}

	// twenty minutes after the answers were obtained: nothing
	s.clock.advance(13 * time.Minute)
	for _, p := range append(paths, doc) {
		if w := s.get(p, "Authorization", bearer(s.full)); w.Code != 503 {
			t.Fatalf("%s: %d after the grace period", p, w.Code)
		}
	}

	// back: the replica serves again without a restart
	s.setUnreachable(false)
	if w := s.get(paths[0], "Authorization", bearer(s.full)); w.Code != 200 {
		t.Fatalf("%d", w.Code)
	}
}

func TestR_BAS_027_TheGraceExtendsOnlyWhileTheMainInstallationIsUnreachable(t *testing.T) {
	s := newSiteHarness(t, options{cfg: []func(*configT){func(c *configT) { c.Bastion.Cache.ArtifactRevalidateAfter = time.Hour }}})
	warm(t, s)
	s.clock.advance(8 * time.Minute)
	before := s.main.count()
	s.get(tgz, "Authorization", bearer(s.full))
	if s.main.count() == before {
		t.Fatal("an answer past its period was used while the main installation is reachable")
	}
}

// ---- R-BAS-028 --------------------------------------------------------------------------------------------

func TestR_BAS_028_AMismatchOrARefusedTokenOnARequestMakesTheReplicaUnreachable(t *testing.T) {
	mismatch := fmt.Errorf("%w", &mainclient.MismatchError{Main: iface.InterfaceVersion + 1})
	for _, row := range []struct {
		name     string
		cause    error
		viaCheck bool
	}{
		{"a mismatch on a forward", mismatch, false},
		{"a mismatch on a check", mismatch, true},
		// A refused token counts on the Bastion's own calls only; on a forward it does not (Q-BAS-121).
		{"a refused token on a check", mainclient.ErrTokenRefused, true},
	} {
		t.Run(row.name, func(t *testing.T) {
			s := newSiteHarness(t, options{})
			who := s.full
			if row.viaCheck {
				s.site.artifacts[tgz] = []byte("tarball")
				// The entry exists and the reader has no shape answer yet: the check asks the main installation.
				if w := s.get(tgz, "Authorization", bearer(s.full)); w.Code != 200 {
					t.Fatalf("prime: %d %s", w.Code, w.Body)
				}
				who = s.part
				s.main.checkFn = func(iface.CheckRequest) (iface.CheckAnswer, error) { return iface.CheckAnswer{}, row.cause }
			}
			s.main.doFn = func(mainclient.Forward) (*mainclient.Response, error) { return nil, row.cause }
			w := s.get(tgz, "Authorization", bearer(who))
			if w.Code != 503 || problemCode(w) != "unavailable" {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if s.link.Reachable() {
				t.Fatal("the link was not told: Link.Guard makes the replica unreachable at once")
			}
			if st := s.link.State(); errors.Is(row.cause, mainclient.ErrInterfaceMismatch) && !st.InterfaceMismatch {
				t.Fatalf("the mismatch is not recorded: %+v", st)
			}
			before := s.main.count()
			s.get(tgz, "Authorization", bearer(who))
			if s.main.count() != before {
				t.Fatal("the replica kept sending to the main installation")
			}
		})
	}
}

// TestR_BAS_006_R_BAS_021_ARefusalOfTheTokenOnAForwardDoesNotMakeTheReplicaUnreachable: a forward is a path the
// caller chose, so an answer that refuses the Bastion's token to it (the bad answer the real client makes of it, 502, or
// the typed refusal, which the guard does not note either) leaves the link reachable (Q-BAS-121).
func TestR_BAS_006_R_BAS_021_ARefusalOfTheTokenOnAForwardDoesNotMakeTheReplicaUnreachable(t *testing.T) {
	for name, row := range map[string]struct {
		cause  error
		status int
		code   string
	}{
		"the typed refusal": {mainclient.ErrTokenRefused, 503, "unavailable"},
		"the bad answer":    {&mainclient.AnswerError{Status: 401, Code: iface.CodeTokenInvalid}, 502, "bad_gateway"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newSiteHarness(t, options{})
			s.main.doFn = func(mainclient.Forward) (*mainclient.Response, error) { return nil, row.cause }
			w := s.get(tgz, "Authorization", bearer(s.full))
			if w.Code != row.status || problemCode(w) != row.code {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if !s.link.Reachable() {
				t.Fatal("a forward made the replica unreachable")
			}
		})
	}
}

// ---- R-BAS-029 --------------------------------------------------------------------------------------------

// TestR_BAS_029_EveryOwnAnswerIsInTheFormatsShapeWithNoStoreAndTheRightFields: every row of R-BAS-029's table on npm's family,
// whose declared shape is npm's (R-BAS-095): the status, the code, a detail, no-store, the challenge of a 401 and the wait of a
// 429 or 503.
func TestR_BAS_029_EveryOwnAnswerIsInTheFormatsShapeWithNoStoreAndTheRightFields(t *testing.T) {
	type row struct {
		name   string
		status int
		code   string
		setup  func(s *siteHarness)
		call   func(s *siteHarness) *httptest.ResponseRecorder
		retry  string
		auth   bool
	}
	get := func(path string, hdr ...string) func(s *siteHarness) *httptest.ResponseRecorder {
		return func(s *siteHarness) *httptest.ResponseRecorder { return s.get(path, hdr...) }
	}
	rows := []row{
		{name: "invalid_token", status: 401, code: "invalid_token", call: func(s *siteHarness) *httptest.ResponseRecorder {
			return s.get(tgz, "Authorization", "Bearer nonsense")
		}, auth: true},
		{name: "credentials_required", status: 401, code: "credentials_required", setup: func(s *siteHarness) { s.setAnonymous(false) }, call: get(tgz), auth: true},
		{name: "invalid_token kept", status: 401, code: "invalid_token", call: func(s *siteHarness) *httptest.ResponseRecorder {
			s.get(tgz, "Authorization", bearer(s.bad))
			return s.get(tgz, "Authorization", bearer(s.bad))
		}, auth: true},
		{name: "none 404", status: 404, code: "not_found", call: func(s *siteHarness) *httptest.ResponseRecorder {
			s.get(tgz, "Authorization", bearer(s.none))
			return s.get(tgz, "Authorization", bearer(s.none))
		}},
		{name: "none 403", status: 403, code: "forbidden", call: func(s *siteHarness) *httptest.ResponseRecorder {
			s.e.answers.put(context.Background(), answers.Shape, shapeKey(credentialKey(s.full), "web/npm-all"),
				&answerVal{Answer: answerBase(answers.Shape, s.clock.now(), tokenSubject("full", nil), "web/npm-all"), Read: iface.ReadNone, Status: 403})
			return s.get(tgz, "Authorization", bearer(s.full))
		}},
		{name: "none 401", status: 401, code: "credentials_required", call: func(s *siteHarness) *httptest.ResponseRecorder {
			s.e.answers.put(context.Background(), answers.Shape, shapeKey(credentialKey(s.full), "web/npm-all"),
				&answerVal{Answer: answerBase(answers.Shape, s.clock.now(), tokenSubject("full", nil), "web/npm-all"), Read: iface.ReadNone, Status: 401})
			return s.get(tgz, "Authorization", bearer(s.full))
		}, auth: true},
		{name: "not_found", status: 404, code: "not_found", call: get("/ui/")},
		{name: "read_only", status: 403, code: "read_only", call: func(s *siteHarness) *httptest.ResponseRecorder {
			return s.do(http.MethodPut, tgz, strings.NewReader("x"))
		}},
		{name: "method_not_allowed", status: 405, code: "method_not_allowed", call: func(s *siteHarness) *httptest.ResponseRecorder {
			return s.do(http.MethodOptions, tgz, nil)
		}},
		{name: "payload_too_large", status: 413, code: "payload_too_large", call: func(s *siteHarness) *httptest.ResponseRecorder {
			r := httptest.NewRequest(http.MethodPost, "https://packages.example.com/api/npm/web/npm-all/-/npm/v1/security/advisories/bulk", strings.NewReader("x"))
			r.ContentLength = 100 << 20
			r.RemoteAddr = "203.0.113.7:1"
			w := httptest.NewRecorder()
			s.e.ServeHTTP(w, r)
			return w
		}},
		{name: "misdirected_request", status: 421, code: "misdirected_request", call: func(s *siteHarness) *httptest.ResponseRecorder {
			r := httptest.NewRequest(http.MethodGet, "https://quader.example.com"+tgz, nil)
			r.RemoteAddr = "203.0.113.7:1"
			w := httptest.NewRecorder()
			s.e.ServeHTTP(w, r)
			return w
		}},
		{name: "rate_limited", status: 429, code: "rate_limited", retry: "7", setup: func(s *siteHarness) {
			s.guard.request = guard.Verdict{Status: 429, Code: iface.CodeRateLimited, RetryAfter: 7 * time.Second, Reason: metrics.RefusedRateLimited}
		}, call: get(tgz)},
		{name: "rate_limited by the miss limit", status: 429, code: "rate_limited", retry: "10", setup: func(s *siteHarness) { s.guard.missLimit = 1; s.guard.misses = 1 }, call: get(tgz)},
		{name: "auth_timeout", status: 429, code: "auth_timeout", retry: "900", setup: func(s *siteHarness) {
			s.guard.timeout = guard.Verdict{Status: 429, Code: iface.CodeAuthTimeout, RetryAfter: 15 * time.Minute, Reason: metrics.RefusedAuthTimeout}
		}, call: func(s *siteHarness) *httptest.ResponseRecorder { return s.get(tgz, "Authorization", bearer(s.part)) }},
		{name: "address_blocked", status: 429, code: "address_blocked", retry: "60", setup: func(s *siteHarness) {
			s.guard.blocked = guard.Verdict{Status: 429, Code: iface.CodeAddressBlocked, RetryAfter: time.Minute, Reason: metrics.RefusedBlocked}
		}, call: get(tgz)},
		{name: "bad_gateway", status: 502, code: "bad_gateway", setup: func(s *siteHarness) {
			s.main.doFn = func(mainclient.Forward) (*mainclient.Response, error) { return nil, mainclient.ErrUnreachable }
		}, call: get(tgz, "Authorization", "")},
		{name: "unavailable", status: 503, code: "unavailable", retry: "30", setup: func(s *siteHarness) { s.setUnreachable(true) }, call: get(tgz)},
		{name: "busy", status: 503, code: "busy", retry: "1", setup: func(s *siteHarness) {
			s.main.doFn = func(mainclient.Forward) (*mainclient.Response, error) { return nil, mainclient.ErrBusy }
		}, call: get(tgz)},
		{name: "gateway_timeout", status: 504, code: "gateway_timeout", setup: func(s *siteHarness) {
			s.main.doFn = func(mainclient.Forward) (*mainclient.Response, error) { return nil, context.DeadlineExceeded }
		}, call: get(tgz)},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			s := newSiteHarness(t, options{})
			s.site.artifacts[tgz] = []byte("x")
			if r.setup != nil {
				r.setup(s)
			}
			w := r.call(s)
			if w.Code != r.status || problemCode(w) != r.code {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control %q", w.Header().Get("Cache-Control"))
			}
			shape := iface.ErrorShapeNPM
			if r.name == "not_found" {
				shape = iface.ErrorShapeProblem // a path of no format's family
			}
			if p, err := errs.ReadShape(shape, w.Header().Get("Content-Type"), w.Body.Bytes()); err != nil || p.Code != r.code || p.Detail == "" {
				t.Errorf("not the shape %s with the code %s: %v %s", shape, r.code, err, w.Body)
			}
			if r.auth && w.Header().Get("WWW-Authenticate") != `Basic realm="quader"` {
				t.Errorf("WWW-Authenticate %q", w.Header().Get("WWW-Authenticate"))
			}
			if r.retry != "" && w.Header().Get("Retry-After") != r.retry {
				t.Errorf("Retry-After %q, want %q", w.Header().Get("Retry-After"), r.retry)
			}
			if r.status == 429 && (w.Header().Get("RateLimit-Policy") == "" || w.Header().Get("RateLimit") == "") {
				t.Errorf("the RateLimit fields are missing: %v", w.Header())
			}
		})
	}
}

// ---- R-BAS-035 (the miss limit) ---------------------------------------------------------------------------

func TestR_BAS_035_AMissLimitedAddressKeepsGettingItsHits(t *testing.T) {
	g := newGuard()
	s := newSiteHarness(t, options{guard: g})
	warm(t, s)
	g.miss = guard.Verdict{Status: 429, Code: iface.CodeRateLimited, RetryAfter: 30 * time.Second, Reason: metrics.RefusedMissLimited}
	if w := s.get(tgz, "Authorization", bearer(s.full)); w.Code != 200 {
		t.Fatalf("a hit was refused: %d", w.Code)
	}
	w := s.get("/api/npm/web/npm-all/cold.tgz", "Authorization", bearer(s.full))
	if w.Code != 429 || problemCode(w) != "rate_limited" || !strings.Contains(w.Header().Get("RateLimit-Policy"), `"misses"`) {
		t.Fatalf("%d %s %v", w.Code, w.Body, w.Header())
	}
	if counterValue(t, s.harness, "refused", metrics.RefusedMissLimited) != 1 {
		t.Fatal("not counted")
	}
}

// ---- R-BAS-042 --------------------------------------------------------------------------------------------

func TestR_BAS_042_TheRequestLogCarriesTheFieldsOfTheBastionAndNoToken(t *testing.T) {
	s := newSiteHarness(t, options{})
	warm(t, s)
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	h := observability.RequestLog(log, observability.NewMetrics("bastion"))(s.e)
	call := func(auth string) {
		r := httptest.NewRequest(http.MethodGet, "https://packages.example.com"+tgz, nil)
		r.RemoteAddr = "203.0.113.7:4711"
		r.Header.Set("Authorization", auth)
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
	call(bearer(s.full))
	call("Bearer garbage")
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d records: %s", len(lines), buf.String())
	}
	var hit, refused map[string]any
	_ = json.Unmarshal([]byte(lines[0]), &hit)
	_ = json.Unmarshal([]byte(lines[1]), &refused)
	if hit["remote_addr"] != "203.0.113.7" || hit["repo"] != "web/npm-all" || hit["format"] != "npm" || hit["cache"] != "hit" ||
		hit["bastion_class"] != "artifact" || hit["bastion_result"] != "hit" || hit["token_id"] != "full" || !strings.HasPrefix(fmt.Sprint(hit["token_hint"]), "qdr_pat_") {
		t.Fatalf("%v", hit)
	}
	if refused["bastion_class"] != "refused" || refused["bastion_result"] != "refused" || refused["deny_reason"] != "invalid_token" || refused["cache"] != "refused" {
		t.Fatalf("%v", refused)
	}
	if strings.Contains(buf.String(), s.full) || strings.Contains(buf.String(), "garbage") {
		t.Fatal("a token is in the log")
	}
}

func TestR_BAS_042_TheRequestLogNamesTheMainInstallationsRequestIDOfAForward(t *testing.T) {
	s := newSiteHarness(t, options{})
	s.site.artifacts[tgz] = payload(100)
	inner := s.main.doFn
	s.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
		r, err := inner(f)
		if r != nil {
			r.Header.Set("X-Request-Id", "main-trace-1")
		}
		return r, err
	}
	var buf bytes.Buffer
	h := observability.RequestLog(slog.New(slog.NewJSONHandler(&buf, nil)), observability.NewMetrics("bastion"))(s.e)
	r := httptest.NewRequest(http.MethodGet, "https://packages.example.com"+tgz, nil)
	r.RemoteAddr = "203.0.113.7:1"
	r.Header.Set("Authorization", bearer(s.full))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !strings.Contains(buf.String(), `"main_request_id":"main-trace-1"`) {
		t.Fatalf("%s", buf.String())
	}
	if w.Header().Get("X-Request-Id") == "main-trace-1" {
		t.Fatal("the main installation's request ID replaced the Bastion's")
	}
}

func TestR_BAS_042_TheSpanOfARequestCarriesTheClassAndTheResult(t *testing.T) {
	s := newSiteHarness(t, options{})
	warm(t, s)
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	ctx, span := tp.Tracer("test").Start(context.Background(), "request")
	r := httptest.NewRequest(http.MethodGet, "https://packages.example.com"+tgz, nil).WithContext(ctx)
	r.RemoteAddr = "203.0.113.7:1"
	r.Header.Set("Authorization", bearer(s.full))
	s.e.ServeHTTP(httptest.NewRecorder(), r)
	span.End()
	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("%d spans", len(spans))
	}
	got := map[string]string{}
	for _, a := range spans[0].Attributes() {
		if a.Value.Type() == attribute.STRING {
			got[string(a.Key)] = a.Value.AsString()
		}
	}
	if got["quader.bastion.class"] != "artifact" || got["quader.bastion.result"] != "hit" {
		t.Fatalf("%v", got)
	}
}

func TestR_BAS_042_TheContextOfTheCallerIsPassedToTheMainInstallation(t *testing.T) {
	type key struct{}
	s := newSiteHarness(t, options{})
	var seen any
	inner := s.main.doFn
	s.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
		seen = f.Request.Context().Value(key{})
		return inner(f)
	}
	r := httptest.NewRequest(http.MethodGet, "https://packages.example.com"+tgz, nil)
	r = r.WithContext(context.WithValue(r.Context(), key{}, "ctx"))
	r.RemoteAddr = "203.0.113.7:1"
	r.Header.Set("Authorization", bearer(s.full))
	s.e.ServeHTTP(httptest.NewRecorder(), r)
	if seen != "ctx" {
		t.Fatal("the forwarded request does not carry the request's context, so no trace is propagated")
	}
}

// ---- the lifecycle ----------------------------------------------------------------------------------------

func TestR_BAS_043_ReadinessWaitsForTheCacheBackendsFirstCheck(t *testing.T) {
	s := newSiteHarness(t, options{})
	s.cache.fault.SetDown(true)
	if err := s.e.Ready(context.Background()); err == nil {
		t.Fatal("ready before the cache check passed")
	}
	s.cache.fault.SetDown(false)
	if err := s.e.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.cache.fault.SetDown(true)
	if err := s.e.Ready(context.Background()); err != nil {
		t.Fatal("the backend is gated at the first readiness only")
	}
}

func TestR_BAS_043_StartRunsTheJanitorAndDrainStopsItAndWaitsForTheBackgroundRefresh(t *testing.T) {
	s := newSiteHarness(t, options{})
	if err := s.e.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.e.Start(context.Background()); err == nil {
		t.Fatal("started twice")
	}
	release := make(chan struct{})
	if !s.e.background(func() { <-release }) {
		t.Fatal("a background task was refused before the drain")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := s.e.Drain(ctx); err == nil {
		t.Fatal("the drain did not wait for the running background task")
	}
	if s.e.background(func() {}) {
		t.Fatal("a background task started after the drain began")
	}
	close(release)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if err := s.e.Drain(ctx2); err != nil {
		t.Fatal(err)
	}
}

// The sweep is the cache's (cache.Cache.RunSweeper, run by the server): its tests are TestR_BAS_031_* in
// internal/bastion/cache, and the server's wiring is TestR_BAS_031_TheServerRunsTheCacheSweeper.

// TestR_BAS_019_AnAnonymousRequestBeforeTheFirstHandshakeIs503: until a handshake succeeded the replica does not
// know whether the main installation admits anonymous requests, so it neither admits one nor claims that
// anonymous access is off (Q-BAS-99); an unseen token gets the same 503, and a replica that has handshaken and
// is told anonymous access is off answers 401.
func TestR_BAS_019_AnAnonymousRequestBeforeTheFirstHandshakeIs503(t *testing.T) {
	s := newSiteHarness(t, options{down: true})
	s.site.artifacts[tgz] = []byte("tarball")
	for name, hdr := range map[string][]string{"anonymous": nil, "an unseen token": {"Authorization", bearer(s.full)}} {
		w := s.get(tgz, hdr...)
		if w.Code != http.StatusServiceUnavailable || problemCode(w) != "unavailable" || w.Header().Get("Retry-After") != "30" {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	if s.main.count() != 0 {
		t.Fatalf("%d requests went to the main installation", s.main.count())
	}
	s.main.mu.Lock()
	s.main.hsErr = nil
	s.main.mu.Unlock()
	s.setAnonymous(false)
	if w := s.get(tgz); w.Code != http.StatusUnauthorized || problemCode(w) != "credentials_required" {
		t.Fatalf("anonymous access off after the handshake: %d %s", w.Code, w.Body)
	}
}
