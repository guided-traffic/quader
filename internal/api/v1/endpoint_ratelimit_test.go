package v1

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/qname"
)

func TestR_EP_015_EndpointRateLimitClass(t *testing.T) {
	ls, err := cluster.NewLimiterSet(cluster.LimiterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	n := 1
	class := config.EndpointRateLimit{RequestsPerSecond: 1, Burst: 3, Key: "principal"}
	lim := ls.NewLimiterShared(EndpointLimiterName("ext-a"), cluster.ScopeCluster, class.RequestsPerSecond, class.Burst, func() int { return n })
	res, err := httpserver.NewResolver(nil, "https://packages.example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	// The chain resolves the client address from the peer and takes the caller
	// from X-Test-Token, so the stage sees what it sees in production.
	stage := func(endpoint string, c config.EndpointRateLimit) http.Handler {
		router := httpserver.NewClientRouter()
		router.HandleFunc("GET /api/npm/{rest...}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
		return httpserver.Chain{
			Router: router, Resolver: res,
			Authenticate: func(r *http.Request) (httpserver.Principal, error) {
				id := r.Header.Get("X-Test-Token")
				return httpserver.Principal{ID: "sa-" + id, Kind: httpserver.KindServiceAccount, Credential: httpserver.Credential{Method: httpserver.MethodBearer, TokenID: id}}, nil
			},
			RateLimit: NewEndpointRateLimit(EndpointRateLimitOptions{Endpoint: endpoint, Class: "ext-a", Limit: c, Limiter: lim}),
		}.Handler()
	}
	customers, partners := stage("customers", class), stage("partners", class)
	byAddr := stage("addr", config.EndpointRateLimit{RequestsPerSecond: 1, Burst: 3, Key: "address"})
	tokenOf := func(id string) string { return id }
	do := func(h http.Handler, tok, addr string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/npm/npm-customers/x", nil)
		r.Header.Set("X-Test-Token", tok)
		if addr == "" {
			addr = "198.51.100.1"
		}
		r.RemoteAddr = addr + ":40000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}

	t.Run("R-EP-015 the limiter is endpoint_<class> with underscores", func(t *testing.T) {
		if lim.Name() != "endpoint_ext_a" || EndpointLimiterName("external") != "endpoint_external" {
			t.Fatal(lim.Name())
		}
	})
	t.Run("R-EP-015 a package request is limited after the burst with 429, Retry-After and the endpoint class fields", func(t *testing.T) {
		for i := range 3 {
			if rec := do(customers, tokenOf("a"), ""); rec.Code != 200 {
				t.Fatalf("request %d: %d", i, rec.Code)
			}
		}
		rec := do(customers, tokenOf("a"), "")
		if rec.Code != 429 || rec.Header().Get("Retry-After") == "" || !strings.Contains(rec.Body.String(), `"code":"rate_limited"`) {
			t.Fatalf("%d %v %s", rec.Code, rec.Header(), rec.Body)
		}
		if p := rec.Header().Get("RateLimit-Policy"); !strings.HasPrefix(p, `"endpoint:ext-a";q=`) {
			t.Fatalf("RateLimit-Policy %q", p)
		}
		if p := rec.Header().Get("RateLimit"); !strings.HasPrefix(p, `"endpoint:ext-a";r=0;t=`) {
			t.Fatalf("RateLimit %q", p)
		}
	})
	t.Run("R-EP-015 a second token is not slowed down, nor the first token on another endpoint of the class", func(t *testing.T) {
		if rec := do(customers, tokenOf("b"), ""); rec.Code != 200 {
			t.Fatalf("second token: %d", rec.Code)
		}
		if rec := do(partners, tokenOf("a"), ""); rec.Code != 200 {
			t.Fatalf("first token on partners: %d", rec.Code)
		}
	})
	t.Run("R-EP-015 key address buckets by the client address whatever the token", func(t *testing.T) {
		for i := range 3 {
			if rec := do(byAddr, tokenOf(string(rune('c'+i))), "203.0.113.9"); rec.Code != 200 {
				t.Fatalf("request %d: %d", i, rec.Code)
			}
		}
		if rec := do(byAddr, tokenOf("z"), "203.0.113.9"); rec.Code != 429 {
			t.Fatalf("a fourth token from the same address: %d", rec.Code)
		}
		if rec := do(byAddr, tokenOf("z"), "203.0.113.10"); rec.Code != 200 {
			t.Fatalf("another address: %d", rec.Code)
		}
	})
	t.Run("R-EP-015 without Valkey the share divides by the replicas that serve the endpoint", func(t *testing.T) {
		defer func() { n = 1 }()
		n = 3
		admitted := 0
		for range 10 {
			if do(customers, tokenOf("shared"), "").Code == 200 {
				admitted++
			}
		}
		if admitted != 1 {
			t.Fatalf("%d admitted with a burst of 3 shared by 3 replicas, want 1", admitted)
		}
	})
}

// R-EP-015, Q-LIM-110: a request the endpoint's class admits and stage 7 refuses after it (the request figure of the download
// limits on a protocol path, a class of R-API-016 on the management API) is given back to the class: the class counts what stage 7
// lets on, and a client that retries after the 429 of stage 7 does not fill the endpoint's budget. The class's clock does not
// move, so its bucket refills nothing: what it admits at the end is what was given back.
func TestR_EP_015_ARequestStage7RefusesIsGivenBackToTheEndpointsClass(t *testing.T) {
	fixed := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	user := func(*http.Request) (httpserver.Principal, error) {
		return httpserver.Principal{ID: "u-a", Name: "a", Kind: httpserver.KindUser, Credential: httpserver.Credential{Method: httpserver.MethodBearer, TokenID: "a"}}, nil
	}
	chain := func(t *testing.T, o RateLimitOptions) (http.Handler, func(time.Duration)) {
		t.Helper()
		epSet, err := cluster.NewLimiterSet(cluster.LimiterOptions{Now: func() time.Time { return fixed }})
		if err != nil {
			t.Fatal(err)
		}
		class := config.EndpointRateLimit{RequestsPerSecond: 1, Burst: 2, Key: "principal"}
		ep := NewEndpointRateLimit(EndpointRateLimitOptions{
			Endpoint: "customers", Class: "ext", Limit: class,
			Limiter: epSet.NewLimiter(EndpointLimiterName("ext"), cluster.ScopeCluster, class.RequestsPerSecond, class.Burst),
		})
		apiClock := fixed
		apiSet, err := cluster.NewLimiterSet(cluster.LimiterOptions{Now: func() time.Time { return apiClock }})
		if err != nil {
			t.Fatal(err)
		}
		o.Limiters = apiSet
		stage, err := NewRateLimit(o)
		if err != nil {
			t.Fatal(err)
		}
		router := httpserver.NewClientRouter()
		router.HandleFunc("GET /api/npm/{rest...}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
		router.HandleFunc("GET /api/v1/{rest...}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
		res, err := httpserver.NewResolver(nil, "https://packages.example.com", nil)
		if err != nil {
			t.Fatal(err)
		}
		h := httpserver.Chain{
			Router: router, Resolver: res, Authenticate: user,
			RateLimit: func(next http.Handler) http.Handler { return ep(EdgeStage(nil, stage)(next)) },
		}.Handler()
		return h, func(d time.Duration) { apiClock = apiClock.Add(d) }
	}
	get := func(h http.Handler, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = "198.51.100.1:40000"
		h.ServeHTTP(rec, r)
		return rec
	}
	refusedBy := func(t *testing.T, rec *httptest.ResponseRecorder, what, policy string) {
		t.Helper()
		policies := strings.Join(rec.Header().Values("RateLimit-Policy"), ", ")
		if rec.Code != 429 || !strings.Contains(policies, policy) {
			t.Fatalf("%s: %d %q %s, want 429 with the policy %s", what, rec.Code, policies, rec.Body, policy)
		}
	}

	t.Run("on a protocol path the download limits' request figure refuses after the class", func(t *testing.T) {
		// One request an hour at web/npm-a, nothing at web/npm-b; the class counts the token's requests at both.
		var tight limits.Figures
		tight[limits.RequestsPerHour] = 1
		webA := qname.Repo{Project: "web", Key: "npm-a"}
		x := limits.NewIndex(limits.Snapshot{
			Profiles: []limits.Profile{{ID: "p-tight", Key: "tight", Name: "tight", Figures: tight}},
			Rules: []limits.Rule{{
				ID: "r-users", WhoKind: limits.WhoEvery, WhoRef: limits.EveryUser, Profile: "tight",
				Place: limits.Place{Kind: limits.PlaceRepository, Repo: webA, RepoID: 1, ProjectKey: "web", ProjectID: "proj-web"},
			}},
			Settings: limits.Settings{Mode: limits.ModeEnforce, BlockSeconds: 900, Version: 1},
		})
		eng := limits.NewEngine(limits.Options{
			Store: cluster.NewMemoryLimits(), Index: func() *limits.Index { return x }, Replica: "r1", LeaseTTL: time.Minute,
			Share: func() int { return 1 },
		})
		h, _ := chain(t, RateLimitOptions{Limits: eng})
		if rec := get(h, "/api/npm/web/npm-a/x"); rec.Code != 200 {
			t.Fatalf("the first request: %d %s", rec.Code, rec.Body)
		}
		for i := range 4 {
			refusedBy(t, get(h, "/api/npm/web/npm-a/x"), "retry "+strconv.Itoa(i+1)+" at web/npm-a", `"requests-hour"`)
		}
		if rec := get(h, "/api/npm/web/npm-b/x"); rec.Code != 200 {
			t.Fatalf("at web/npm-b, with one of the class's two places used: %d %q %s", rec.Code, rec.Header().Values("RateLimit-Policy"), rec.Body)
		}
		refusedBy(t, get(h, "/api/npm/web/npm-b/x"), "the third request the class admitted", `"endpoint:ext"`)
	})
	t.Run("on the management API a class of R-API-016 refuses after the endpoint's class", func(t *testing.T) {
		h, advance := chain(t, RateLimitOptions{Config: config.APIRateLimits{
			Enabled:   true,
			Read:      config.APIRateLimitRead{RequestsPerSecond: 1, Burst: 1},
			Write:     config.APIRateLimitWrite{RequestsPerSecond: 1, Burst: 1},
			Anonymous: config.APIRateLimitAnonymous{RequestsPerSecond: 1, Burst: 1},

			PackageWrite: config.APIRateLimitPackageWrite{RequestsPerSecond: 1, Burst: 1},
		}})
		if rec := get(h, "/api/v1/repositories"); rec.Code != 200 {
			t.Fatalf("the first request: %d %s", rec.Code, rec.Body)
		}
		for i := range 4 {
			refusedBy(t, get(h, "/api/v1/repositories"), "retry "+strconv.Itoa(i+1)+" within the read class's second", `"read"`)
		}
		advance(time.Second)
		if rec := get(h, "/api/v1/repositories"); rec.Code != 200 {
			t.Fatalf("a second later, with one of the class's two places used: %d %q %s", rec.Code, rec.Header().Get("RateLimit-Policy"), rec.Body)
		}
	})
}
