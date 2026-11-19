package v1

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/limits/httplimit"
	"github.com/guided-traffic/quader/internal/qname"
)

// Stage 7 of the chain (R-ARCH-005): the general rate-limit classes of the management API (R-API-016) and, on the protocol
// paths, the download limits of usage-and-limits.md. Each class is a cluster-scoped limiter of R-ARCH-034, so a bucket admits
// the same budget whichever replica a request reaches.
//
//	read       safe methods on /api/v1/            bucket: the principal (token ID, or session user)
//	write      unsafe methods on /api/v1/          bucket: the principal
//	anonymous  requests without credentials        bucket: the client address
//
// The protocol paths (/api/<format>/…, /files/… and everything below /v2/, R-LIM-005) are counted by the figures of the
// download limits, per subject and place (limits.Engine.Check, one round trip). Beside them the package classes of R-API-016
// keep a bucket of their own per subject (R-LIM-001): package_read and package_anonymous while the mode is audit, the safety
// net of Audit mode (Q-LIM-4), package_write in both modes until the upload figures exist (Q-LIM-27). The class counter
// travels in the same round trip as the request windows. package_anonymous and the figures leave out a request on a known
// repository whose artifacts are not public, which the decision point answers with the challenge
// (RateLimitOptions.Challenged, Q-API-290, R-LIM-005).
//
// The stage runs after authentication, so the principal is known. The class an endpoint applies on top of these is R-EP-015.

// Names of the limiters and values of the class label.
const (
	LimiterRead      = "api_read"
	LimiterWrite     = "api_write"
	LimiterAnonymous = "api_anonymous"

	LimiterPackageRead      = "api_package_read"
	LimiterPackageWrite     = "api_package_write"
	LimiterPackageAnonymous = "api_package_anonymous"
)

func classOf(name string) string { return strings.TrimPrefix(name, "api_") }

// RateLimitOptions configure [NewRateLimit].
type RateLimitOptions struct {
	Config   config.APIRateLimits
	Limiters *cluster.LimiterSet
	// IsAdmin reports whether the caller holds the admin capability; with
	// api.rate_limits.exempt_admins it bypasses the limits. Nil means nobody is
	// exempt.
	IsAdmin func(r *http.Request) bool
	// Registry receives quader_api_rate_limited_total{class}; nil registers nothing.
	Registry prometheus.Registerer
	// Challenged reports a package request without a credential that the decision point answers with the challenge whatever
	// its route, at no more cost than a decision in memory: [ChallengedByLevels]. The class package_anonymous does not charge
	// such a request (Q-API-290). Nil charges every package request without a credential.
	Challenged func(r *http.Request) bool
	// Limits is the engine of the download limits; nil is a replica without them: the package classes then hold in every
	// mode, keyed by the subject.
	Limits *limits.Engine
	// IsAdministrator reports whether the caller's principal holds the role admin now, whatever the token's scope: the
	// administrator of R-LIM-012 (Q-LIM-18). Nil means nobody is.
	IsAdministrator func(r *http.Request) bool
	// Repo is the repository a protocol request names for the download limits (R-LIM-005), which knows a host bound to a
	// repository (R-EP-007); nil reads the address alone (httplimit.Repo).
	Repo func(r *http.Request) qname.Repo
}

// LevelReader is the part of the grant index [ChallengedByLevels] reads; *authz.Index implements it.
type LevelReader interface {
	Levels(key qname.Repo) (artifacts, wiki authz.Level, ok bool)
}

// ChallengedByLevels is [RateLimitOptions.Challenged] on the grant index that index returns. The anonymous subject holds no
// grant, so only a public level admits it (R-AUTHZ-033), and the format dispatcher decides the route's action on the
// repository before a handler, a body or a repository lookup runs (R-ARCH-011): a request whose address names a repository
// the index holds with artifacts that are not public is answered with the challenge, and costs no more than a decision in
// memory. A NuGet client sends each request without its configured credentials first and repeats it with them after that 401
// (R-NUGET-012); the class of a request without a credential is meant for callers who read without one, not for that first
// attempt. A repository the index does not hold is charged: the request asks PostgreSQL whether it was just created before it
// is decided (R-ARCH-011 step 2). So are a path that names no repository, a raw-file path and a replica without an index.
func ChallengedByLevels(index func() LevelReader) func(r *http.Request) bool {
	return func(r *http.Request) bool {
		a, ok := qname.ParseAddress(r.URL.EscapedPath())
		if !ok || a.Family != qname.FamilyAPI {
			return false
		}
		idx := index()
		if idx == nil {
			return false
		}
		artifacts, _, known := idx.Levels(a.Repo)
		return known && artifacts != authz.LevelPublic
	}
}

type classLimiter struct {
	l       *cluster.Limiter
	policy  string // the RateLimit-Policy member after the class name, "q=200;w=4"
	limited prometheus.Counter
}

// NewRateLimit builds stage 7. With api.rate_limits.enabled false, or without limiters, the classes of R-API-016 are off; the
// download limits of the protocol paths are not R-API-016's and stay while there is an engine (o.Limits).
func NewRateLimit(o RateLimitOptions) (func(http.Handler) http.Handler, error) {
	if !o.Config.Enabled || o.Limiters == nil {
		if o.Limits == nil {
			return func(next http.Handler) http.Handler { return next }, nil
		}
		st := protocolStage{o: o, classesOff: true}
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if httplimit.ProtocolPath(r.URL.Path) && familyServed(r) {
					st.serve(w, r, next)
					return
				}
				next.ServeHTTP(w, r)
			})
		}, nil
	}
	rejected := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "quader_api_rate_limited_total",
		Help: "Requests refused with 429 by the general rate limits, by class.",
	}, []string{"class"})
	if o.Registry != nil {
		if err := o.Registry.Register(rejected); err != nil {
			return nil, err
		}
	}
	mk := func(name string, rate float64, burst int) classLimiter {
		l := o.Limiters.NewLimiter(name, cluster.ScopeCluster, rate, burst)
		w := int64(l.Window() / 1e9)
		return classLimiter{
			l: l, limited: rejected.WithLabelValues(classOf(name)),
			policy: `"` + classOf(name) + `";q=` + strconv.Itoa(burst) + ";w=" + strconv.FormatInt(w, 10),
		}
	}
	classes := map[string]classLimiter{
		LimiterRead:      mk(LimiterRead, o.Config.Read.RequestsPerSecond, o.Config.Read.Burst),
		LimiterWrite:     mk(LimiterWrite, o.Config.Write.RequestsPerSecond, o.Config.Write.Burst),
		LimiterAnonymous: mk(LimiterAnonymous, o.Config.Anonymous.RequestsPerSecond, o.Config.Anonymous.Burst),

		LimiterPackageRead:      mk(LimiterPackageRead, o.Config.PackageRead.RequestsPerSecond, o.Config.PackageRead.Burst),
		LimiterPackageWrite:     mk(LimiterPackageWrite, o.Config.PackageWrite.RequestsPerSecond, o.Config.PackageWrite.Burst),
		LimiterPackageAnonymous: mk(LimiterPackageAnonymous, o.Config.PackageAnonymous.RequestsPerSecond, o.Config.PackageAnonymous.Burst),
	}
	st := protocolStage{o: o, classes: classes}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limitedPath(r.URL.Path) || !familyServed(r) {
				next.ServeHTTP(w, r)
				return
			}
			if httplimit.ProtocolPath(r.URL.Path) {
				st.serve(w, r, next)
				return
			}
			if o.Config.ExemptAdmins && o.IsAdmin != nil && o.IsAdmin(r) {
				next.ServeHTTP(w, r)
				return
			}
			name, bucket := classify(r)
			c := classes[name]
			v := c.l.TakeAt(r.Context(), bucket, httpserver.InfoFrom(r.Context()).Received)
			setRateLimitFields(w, c, name, v)
			if v.Allowed {
				next.ServeHTTP(w, r)
				return
			}
			c.limited.Inc()
			httpserver.Problem(w, r, classRefusal(v))
		})
	}, nil
}

func classRefusal(v cluster.Verdict) error {
	return errs.E(errs.RateLimited, errs.CodeRateLimited,
		"Too many requests; retry after the period in the Retry-After header.", errs.WithRetryAfter(v.RetryAfter))
}

// protocolStage is stage 7 on a protocol path (R-LIM-005): the subject, the place, the request windows and the block of the
// download limits, and the package class of R-API-016 that is in force, in one round trip.
type protocolStage struct {
	o       RateLimitOptions
	classes map[string]classLimiter
	// classesOff: api.rate_limits.enabled is false, so no class of R-API-016 applies.
	classesOff bool
}

func (s protocolStage) serve(w http.ResponseWriter, r *http.Request, next http.Handler) {
	ctx := r.Context()
	p := httpserver.PrincipalFrom(ctx)
	// A preflight is not a request of the protocol, and a request without a credential that the decision point is certain to
	// answer with the challenge is no request of a subject (Q-API-290, R-LIM-005).
	if r.Method == http.MethodOptions || (p.Anonymous && s.o.Challenged != nil && s.o.Challenged(r)) {
		next.ServeHTTP(w, r)
		return
	}
	admin := s.o.IsAdministrator != nil && s.o.IsAdministrator(r)
	caller := httplimit.CallerOf(r, p, admin)
	subject, _ := limits.SubjectOf(caller)
	mode := limits.ModeAudit
	if s.o.Limits != nil {
		mode = s.o.Limits.Mode()
	}
	name := packageClass(r, p, mode)
	if s.classesOff {
		name = ""
	}
	if name != "" && s.o.Config.ExemptAdmins && s.o.IsAdmin != nil && s.o.IsAdmin(r) {
		name = ""
	}
	received := httpserver.InfoFrom(ctx).Received
	if s.o.Limits == nil {
		if name == "" {
			next.ServeHTTP(w, r)
			return
		}
		c := s.classes[name]
		v := c.l.TakeAt(ctx, subject.String(), received)
		setRateLimitFields(w, c, name, v)
		if !v.Allowed {
			c.limited.Inc()
			httpserver.Problem(w, r, classRefusal(v))
			return
		}
		next.ServeHTTP(w, r)
		return
	}
	repo := httplimit.Repo
	if s.o.Repo != nil {
		repo = s.o.Repo
	}
	charge := s.o.Limits.Begin(caller, repo(r))
	if r.URL.Path == httplimit.TokenPath {
		charge.DeferRequestFigures() // the token service charges its subject after it authenticated (R-LIM-003)
	}
	var check limits.ClassCheck
	if name != "" {
		c := s.classes[name]
		check = func(b *cluster.LimitBatch) func([]cluster.LimitResult, error) error {
			decide := c.l.BatchTake(b, subject.String(), received)
			return func(res []cluster.LimitResult, err error) error {
				v := decide(res, err)
				setRateLimitFields(w, c, name, v)
				if v.Allowed {
					return nil
				}
				c.limited.Inc()
				return classRefusal(v)
			}
		}
	}
	st := s.o.Limits.Check(ctx, charge, received, check)
	httplimit.SetPolicies(w.Header(), st.Policies)
	if st.Err != nil {
		httplimit.SetRefusalPolicy(w.Header(), charge)
		httplimit.Log(ctx, charge)
		s.o.Limits.Finish(charge)
		httpserver.Problem(w, r, st.Err)
		return
	}
	cw := httplimit.Wrap(ctx, w, s.o.Limits, charge)
	defer func() {
		cw.Close()
		httplimit.Log(ctx, charge)
		s.o.Limits.Finish(charge)
	}()
	next.ServeHTTP(cw, r.WithContext(limits.WithCharge(ctx, charge)))
}

// packageClass is the class of R-API-016 in force for a protocol request (Q-LIM-4, Q-LIM-27): package_write for an unsafe
// method in both modes, package_read and package_anonymous in audit only; "" for none.
func packageClass(r *http.Request, p httpserver.Principal, mode limits.Mode) string {
	safe := r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions
	switch {
	case p.Anonymous && mode != limits.ModeEnforce:
		return LimiterPackageAnonymous
	case !safe:
		return LimiterPackageWrite
	case mode == limits.ModeEnforce:
		return ""
	}
	return LimiterPackageRead
}

// limitedPath reports whether a path lies in a family stage 7 limits: the management API and the protocol paths. The stage
// asks [familyServed] as well: a path of a family the endpoint does not serve is limited no more than an unknown path is.
func limitedPath(p string) bool {
	if p == ExchangePath {
		return false
	}
	return isAPIPath(p) || httplimit.ProtocolPath(p)
}

// ExchangePath is the CI token exchange (R-CI-007). The general API rate limit skips it: its own limiters,
// ci_exchange_address before the body is read and ci_exchange_subject after the subject token passed validation,
// bound it cluster-wide (R-CI-021), and a request of a runner fleet behind one NAT address must not spend the
// anonymous class of every other caller of that address.
const ExchangePath = "/api/v1/auth/oidc/token"

// classify picks the limiter and the bucket of a request of the management API.
func classify(r *http.Request) (limiter, bucket string) {
	p := httpserver.PrincipalFrom(r.Context())
	if p.Anonymous {
		return LimiterAnonymous, httpserver.InfoFrom(r.Context()).Client.String()
	}
	limiter = LimiterWrite
	// The interface of a Bastion has read semantics whatever the method: its check and its report of downloads
	// are POSTs that change no package content, and the write class is sized for a person's changes (R-BAS-008).
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions || p.Kind == httpserver.KindBastion {
		limiter = LimiterRead
	}
	switch {
	case p.Credential.TokenID != "":
		bucket = "token:" + p.Credential.TokenID
	case p.ID != "":
		bucket = "principal:" + p.ID
	default:
		bucket = "name:" + p.Name
	}
	return limiter, bucket
}

// setRateLimitFields sets RateLimit-Policy and RateLimit of
// draft-ietf-httpapi-ratelimit-headers (R-API-016). During the fallback q and
// r describe the replica's share.
func setRateLimitFields(w http.ResponseWriter, c classLimiter, name string, v cluster.Verdict) {
	class := classOf(name)
	policy := c.policy
	if v.Mode != cluster.ModeCluster {
		policy = `"` + class + `";q=` + strconv.Itoa(v.Limit) + ";w=" + strconv.FormatInt(int64(v.Window/1e9), 10)
	}
	t := int64((v.Reset + 999_999_999) / 1e9)
	// Added, not set: the endpoint's class (R-EP-015) runs before this stage and
	// its policy is a member of the same structured list.
	w.Header().Add("RateLimit-Policy", policy)
	w.Header().Add("RateLimit", `"`+class+`";r=`+strconv.Itoa(v.Remaining)+";t="+strconv.FormatInt(t, 10))
}

// EdgeStage composes the stages of the management API that sit at the chain's
// stage 7: the response headers of R-API-017, CORS (R-API-018) and the rate
// limits (R-API-016), in this order, so a preflight is answered before it
// counts against a limit.
func EdgeStage(cors func(http.Handler) http.Handler, limit func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		h := next
		if limit != nil {
			h = limit(h)
		}
		if cors != nil {
			h = cors(h)
		}
		return apiHeaders(h)
	}
}

// apiHeaders sets the response headers every /api/v1/ answer carries
// (R-API-017) where the endpoint serves the management API. X-Content-Type-Options
// and X-Request-Id come from the chain. A handler may replace Cache-Control (the
// problem catalog is cacheable).
func apiHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiRequest(r) {
			setAPIHeaders(w.Header())
		}
		next.ServeHTTP(w, r)
	})
}

// familyServed reports whether the endpoint whose listener accepted the request serves the route family of its path, as
// the process's role narrowed it: [httpserver.Endpoint.ServesRequest], the question the authentication stage asks through
// auth.EndpointRules.Served. Outside a chain bound to an endpoint every family is served. Every path-based rule of this
// stage and of [Preflight] asks it before it applies (R-EP-005, Q-ARCH-306): a request of a family the endpoint does not
// serve gets no header of the management API, no CORS answer and no rate-limit class, so before and after authentication
// its answer is the one of an unknown path.
func familyServed(r *http.Request) bool {
	e := httpserver.EndpointFrom(r.Context())
	return e == nil || e.ServesRequest(r.URL.Path)
}

// apiRequest reports whether the request is one of the management API on an endpoint that serves it.
func apiRequest(r *http.Request) bool { return isAPIPath(r.URL.Path) && familyServed(r) }

func setAPIHeaders(h http.Header) {
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	h.Set("Referrer-Policy", "no-referrer")
}
