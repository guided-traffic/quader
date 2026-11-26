package app

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"github.com/guided-traffic/quader/internal/api/bastionmark"
	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/auth"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formathost"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/observability"
	"github.com/guided-traffic/quader/internal/qname"
)

// The wiring of the main installation's side of the quader Bastion (docs/developer/bastion.md, R-BAS-004 to
// R-BAS-017). The contract work wires nothing but the seams' places: the chain stage of the default endpoint,
// which is empty until the work packages fill it.
//
// Each work package edits its own marked block, and only that one, and adds the fields it needs to App in
// its own block of app.go:
//
//   - package 1 (registrations, the bst token type): builds the bastionmain.TokenAuthenticator;
//   - package 2 (interface handlers, recognition): the Recognize stage and the interface services;
//   - package 3 (marking, write probe): builds the bastionmain.ShapeResolver (package 2's check uses it) and
//     the Mark stage.

// bastionStage returns the Bastion stage of an endpoint's chain (httpserver.Chain.Bastion): only the default
// endpoint serves the Bastion interface and recognizes Bastion requests (R-BAS-005), so every other endpoint
// gets none.
func (a *App) bastionStage(rt *endpointRuntime) (func(http.Handler) http.Handler, error) {
	if !rt.cfg.Default {
		return nil, nil
	}
	st, err := a.bastionStages(rt)
	if err != nil {
		return nil, err
	}
	return st.Chain(), nil
}

// bastionStages assembles the two stages. A stage whose package is not built is nil, and a nil stage is
// skipped, so a main installation without the Bastion work in this build behaves as before.
func (a *App) bastionStages(rt *endpointRuntime) (bastionmain.Stages, error) {
	var st bastionmain.Stages
	// >>> package 2
	snap := a.bastionSnapshot()
	snap.WarnOverlap(prefixesOf(rt.cfg.TrustedProxies))
	forwarded, err := a.bastionForwarded(rt, snap.Egress)
	if err != nil {
		return st, err
	}
	st.Recognize = a.bastionRecognize(forwarded, snap.Egress)
	// <<< package 2

	// >>> package 3
	if shapes := a.bastionShapes(); shapes != nil {
		st.Mark = bastionmark.New(bastionmark.Options{
			Shapes: shapes, Metrics: a.bastionMetrics(), Log: a.log.With("component", "bastion"),
		}).Stage()
	}
	// <<< package 3
	return st, nil
}

// >>> package 1

// bastionRegistrations builds the registration service behind /api/v1/bastions (R-BAS-004). It is nil on a
// replica that has no identity area (a worker), whose API operations then answer 503.
func (a *App) bastionRegistrations() *bastionmain.Registrations {
	in, id := part[infra](a.parts), part[identityArea](a.parts)
	if in.db == nil || id.tokens == nil {
		return nil
	}
	return bastionmain.NewRegistrations(bastionmain.RegistrationOptions{
		DB: in.db, Notifier: in.changes, Recorder: in.rec, Tokens: id.tokens, Max: a.cfg.Bastions.MaxRegistrations,
	})
}

// bastionAuthenticator builds the bastionmain.TokenAuthenticator over the token service (R-BAS-005): the
// recognition stage of package 2 validates a Bastion token with it. It is nil where the token service is not
// built.
func (a *App) bastionAuthenticator() bastionmain.TokenAuthenticator {
	id := part[identityArea](a.parts)
	if id.tokens == nil {
		return nil
	}
	return bastionmain.NewAuthenticator(id.tokens)
}

// <<< package 1
// >>> package 3

// bastionShapes returns the ShapeResolver over the decision point (R-BAS-012): the Mark stage asks it for the
// caller's shape on the addressed repository and the check operation for a credential it has validated. A
// replica without the authorization area has none.
func (a *App) bastionShapes() bastionmain.ShapeResolver {
	eng := part[authzArea](a.parts).engine
	if eng == nil {
		return nil
	}
	return bastionmain.NewShapeResolver(eng)
}

// bastionMetrics returns the metrics of the main installation's side, registered once: the Mark stage counts
// the forwarded requests on the value the interface handlers count the interface requests on.
func (a *App) bastionMetrics() *bastionmain.Metrics {
	a.bastionMxOnce.Do(func() {
		m, err := bastionmain.NewMetrics(a.tele.metrics.Registry())
		if err != nil {
			a.log.Error("the Bastion metrics could not be registered", "error", err)
			return
		}
		a.bastionMx = m
	})
	return a.bastionMx
}

// <<< package 3

// >>> package 2

// bastionRecognize is the Recognize stage (R-BAS-008, R-BAS-009): its refusals are problem details of the
// management API on every path, written with the request's ID and logger. The source of a Bastion token is the
// request as stage 5 resolved it, before the Bastion's forwarded headers count (R-BAS-017); egress names the networks
// of each registration (Q-BAS-430).
func (a *App) bastionRecognize(forwarded func(*http.Request) context.Context, egress func(string) []netip.Prefix) func(http.Handler) http.Handler {
	return bastionmain.Recognize(bastionmain.RecognizeOptions{
		Tokens: a.bastionAuthenticator(), Metrics: a.bastionMetrics(), Throttle: a.bastionThrottle(), Egress: egress,
		Source: func(r *http.Request) (string, string) {
			src := httpserver.SourceFrom(r.Context())
			return src.ClientAddr, src.UserAgent
		},
		Forwarded: forwarded,
		Problem: func(w http.ResponseWriter, r *http.Request, err error) {
			errs.Write(w, r, err, httpserver.RequestID(r.Context()), httpserver.Logger(r.Context()))
		},
	})
}

// bastionForwarded is the re-derivation of the forwarded headers of a recognized Bastion request (R-BAS-017,
// Q-BAS-334, Q-BAS-430): when the address that reached this installation - stage 5's client address, after the main
// installation's own ingress in server.trusted_proxies - lies in the networks of the request's own registration
// (Recognition.FromEgress), the request is resolved again with those networks trusted as well, so the caller's address the
// Bastion forwards and the Bastion's host count for this request and no other. A recognized request from any other address,
// and every request of a registration that lists no network, keeps what stage 5 resolved, and its request line carries
// bastion_egress "unlisted". egress returns a registration's networks.
func (a *App) bastionForwarded(rt *endpointRuntime, egress func(string) []netip.Prefix) (func(*http.Request) context.Context, error) {
	res, err := httpserver.NewResolver(rt.cfg.TrustedProxies, rt.cfg.BaseURL, a.cfg.Server.AdditionalBaseURLs,
		httpserver.WithForwardedHeader(httpserver.ForwardedHeader(a.cfg.Server.ForwardedHeader)), httpserver.ForEndpoint(rt.ep))
	if err != nil {
		return nil, fmt.Errorf("the re-derivation of a Bastion's forwarded headers: %w", err)
	}
	return func(r *http.Request) context.Context {
		ctx := r.Context()
		rec, ok := bastionmain.RecognitionFrom(ctx)
		if !ok {
			return ctx
		}
		if !rec.FromEgress {
			// Not one of the registration's networks: what the request forwards does not count, and the line says so.
			observability.RequestInfoFrom(ctx).SetField("bastion_egress", "unlisted")
			return ctx
		}
		info := res.AlsoTrusting(egress(rec.BastionID)).Resolve(r)
		if !rt.ep.ChecksHost() && info.BaseURL != rt.cfg.BaseURL {
			// An endpoint without a host set binds by the request's own host (R-EP-006), which for a Bastion's forward is the main
			// installation's; the Bastion's host is the one of the response base URL the request resolved to, and its binding is
			// the one the request reaches and the handshake names (R-BAS-057, Q-BAS-178).
			if u, err := url.Parse(info.BaseURL); err == nil && u.Hostname() != "" {
				info.RootRepository = rt.ep.HostRoot(u.Hostname())
			}
		}
		annotate(r, info)
		return httpserver.WithResolved(ctx, info)
	}, nil
}

// prefixesOf reads a list of addresses and networks as validated configuration holds them; an entry that is neither is left out.
func prefixesOf(entries []string) []netip.Prefix {
	var out []netip.Prefix
	for _, e := range entries {
		if p, err := netip.ParsePrefix(e); err == nil {
			out = append(out, p.Masked())
		} else if a, err := netip.ParseAddr(e); err == nil {
			out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
		}
	}
	return out
}

// LimiterBastionDownloadEvents is the limiter of R-ARCH-034 the download events of a Bastion registration are
// charged to, in units of config.DownloadEventsPerUnit events, the registration as the bucket (R-BAS-016,
// Q-BAS-117).
const LimiterBastionDownloadEvents = "bastion.download_events"

// bastionEventBudget charges a batch of download events to its registration, its events rounded up to whole
// units; nil on a replica without the limiter.
func (a *App) bastionEventBudget() bastionmain.EventBudget {
	lim := part[limitsArea](a.parts).bastionEvents
	if lim == nil {
		return nil
	}
	dl := a.cfg.Bastions.Downloads
	return func(ctx context.Context, bastionID string, events int) (bool, time.Duration) {
		v := lim.TakeNAt(ctx, "bastion:"+bastionID, dl.Units(events), httpserver.InfoFrom(ctx).Received)
		return v.Allowed, v.RetryAfter
	}
}

// downloadUnitOf names the unit of an item a Bastion reports it served (operations-and-observability.md R-OPS-054), as the
// main installation's own download event of the item names it: the repository's format classifies the path and declares the unit.
// A repository this replica does not know, or one of a format it has not, names none.
func (a *App) downloadUnitOf(ctx context.Context, repo, path string) (pkg, version, key string) {
	repos := part[repositoryArea](a.parts).reader
	name, err := qname.ParseRepo(repo)
	if repos == nil || err != nil {
		return "", "", ""
	}
	r, err := repos.ByName(ctx, name)
	if err != nil {
		return "", "", ""
	}
	f, ok := formats.Lookup(r.Format)
	if !ok {
		return "", "", ""
	}
	u := formathost.UnitOf(f, path, "")
	return u.Package, u.Version, u.Key
}

// bastionThrottle builds the failed-credential throttle of the request's client address for the recognition of
// a Bastion token: the limiter and the exempt addresses of the authentication stage (R-AUTH-038). A replica
// without the limiter (the shell profile) has none.
func (a *App) bastionThrottle() func(*http.Request) bastionmain.Throttle {
	lim := part[limitsArea](a.parts).failures
	if lim == nil {
		return nil
	}
	exempt := a.cfg.Auth.RateLimit.ExemptPrefixes()
	return func(r *http.Request) bastionmain.Throttle {
		// A nil token.Throttle converts to a nil bastionmain.Throttle: no typed nil.
		return auth.NewAddressThrottle(lim, clientAddr(r), exempt, bastionmain.ThrottleScope)
	}
}

// bastionServices are the services of the four interface operations of the default endpoint (R-BAS-010,
// R-BAS-011, R-BAS-015, R-BAS-016); a replica without a database, and every other endpoint, has none.
func (a *App) bastionServices(rt *endpointRuntime) v1.BastionServices {
	in, id := part[infra](a.parts), part[identityArea](a.parts)
	if !rt.cfg.Default || in.db == nil || id.settings == nil {
		return v1.BastionServices{}
	}
	cfg := a.cfg
	rec := a.auditRecorder()
	return v1.BastionServices{
		Config: cfg.Bastions, Release: buildVersion(), BaseURL: cfg.Server.BaseURL,
		AnonymousReads: func(ctx context.Context) bool {
			return rt.cfg.Anonymous && a.anonymousReads(ctx, true)
		},
		Authenticate: a.bastionCheckStage(rt),
		Shapes:       a.bastionShapes(),
		Feed: bastionmain.NewFeed(bastionmain.FeedOptions{
			DB: in.db, Lookback: cfg.Bastions.Feed.Lookback, MaxPage: cfg.Bastions.Feed.MaxPage, Endpoint: rt.cfg.Name, Log: a.log,
		}),
		Contacts: bastionmain.NewContacts(bastionmain.ContactOptions{DB: in.db, Audit: rec}),
		Downloads: bastionmain.NewIntake(bastionmain.IntakeOptions{
			DB: in.db, Audit: rec, MaxBatch: cfg.Bastions.Downloads.MaxBatch, MaxAge: cfg.Bastions.Downloads.MaxAge, Record: cfg.Audit.RecordDownloads,
			Events: a.bastionEventBudget(), Unit: a.downloadUnitOf,
		}),
		// The handshake's format table (R-BAS-057) and the binding of the Bastion's host as the match of /v2/ reads it.
		Formats:         v1.BastionFormatTable(formats.List()),
		BoundRepository: a.bastionBoundRepository(),
		Handover:        a.bastionSnapshot(),
	}
}

// bastionSnapshot is the replica's snapshot of the Bastion registrations: the switches of the handover per blob (R-BAS-071),
// which the handshake reports, and the networks each Bastion connects from (R-BAS-017), which the recognition reads. Every role with
// a client listener builds it with the area of the signed links; elsewhere it is a nil snapshot, whose On says false and whose Egress
// names no network for every registration.
func (a *App) bastionSnapshot() *bastionmain.RegistrationSnapshot {
	return part[downloadLinksArea](a.parts).bastions
}

// bastionCheckStage is the authentication stage the check runs a caller's credential through (R-BAS-011, Q-BAS-66): the
// default endpoint's stage with its rules, recording nothing in the request log (Q-BAS-119), except that the check's own
// paths count as served whatever this process's role serves. The check hands a credential to the stage on a package path
// (/api/bastion-check) or, for a registry token, below the registry root (/v2/bastion-check, R-BAS-052), so that it is
// decided as the Bastion's forward of the caller's request is decided by a replica that serves the package families; on a
// replica of the role api, which serves the interface but not those families, the rule of Q-ARCH-302 would otherwise read
// the path as unknown, and the stage verifies a registry token only on a path below /v2/.
func (a *App) bastionCheckStage(rt *endpointRuntime) func(*http.Request) (httpserver.Principal, error) {
	rules := a.credentialRules(rt)
	rules.Served = nil // every family is served: the check's paths are no routes of this process
	stage := a.authStage().stage
	return withMinEpoch(func(r *http.Request) (httpserver.Principal, error) {
		res, err := stage.Authenticate(r, rules)
		if err != nil {
			return httpserver.Principal{}, err
		}
		return principalFromResult(res), nil
	})
}

// bastionBoundRepository is the dispatcher's reading of the binding of a request's host root (R-EP-006, R-OCI-029), which
// the handshake names as the Bastion's bound repository (R-BAS-057); nil on a replica without a format host.
func (a *App) bastionBoundRepository() func(*http.Request) (qname.Repo, bool, error) {
	disp := part[formatHostArea](a.parts).disp
	if disp == nil {
		return nil
	}
	return disp.BoundRepository
}

// <<< package 2
