package app

import (
	"context"
	"errors"
	"sync"
	"time"

	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formathost"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/mirror/discover"
	"github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/catalogue"
	"github.com/guided-traffic/quader/internal/remotecuration/requests"
	"github.com/guided-traffic/quader/internal/vuln"
)

// The upstream catalogue of the remote repositories (remote-curation.md R-CUR-007 to R-CUR-009, docs/developer/remote-curation.md R3): the
// engine of the process, built at its first use in a role that serves the management API (api, web, all); its upstream is lent by the
// mirror host of the process (wire_mirror_host.go), which in role web or all stands on the engine and the host of the client path and in
// role api builds a background engine of its own at the first call. Its budget is the cluster limiter's (R-ARCH-030): one limiter per
// repository's requests and one per person's, each a minute's window of proxy.catalogue.

// The limiters of the catalogue's budget (quader_ratelimit_* label limiter).
const (
	LimiterCatalogueRepository = "catalogue.repository"
	LimiterCataloguePerson     = "catalogue.person"
)

// catalogueState is the App's part of the catalogue: the engine, built once.
type catalogueState struct {
	catalogueOnce sync.Once
	catalogueEng  *catalogue.Engine
	catalogueErr  error
}

// upstreamCatalogue returns the catalogue's engine of the process.
func (a *App) upstreamCatalogue() (*catalogue.Engine, error) {
	a.catalogueOnce.Do(func() { a.catalogueEng, a.catalogueErr = a.buildCatalogue() })
	return a.catalogueEng, a.catalogueErr
}

func (a *App) buildCatalogue() (*catalogue.Engine, error) {
	c := a.cfg.Proxy.Catalogue
	o := catalogue.Options{
		Catalogues: func(format string) (discover.Catalogue, bool) {
			cat := formats.RemoteCapabilities(formats.ID(format)).Catalogue
			return cat, cat != nil
		},
		Lender: catalogueLender{a},
		Settings: func() catalogue.Settings {
			return catalogue.Settings{
				RequestsPerMinute: c.RequestsPerMinute, RequestsPerMinutePerPerson: c.RequestsPerMinutePerPerson,
				CacheBytes: int64(c.CacheBytes), CacheTTL: c.CacheTTL, MaxDocumentBytes: int64(c.MaxDocumentBytes), Timeout: c.Timeout,
			}
		},
		Log: a.log.With("component", "upstream-catalogue"),
	}
	if ls := part[limitsArea](a.parts).set; ls != nil {
		o.RepoBudget = limiterBudget{ls.NewLimiter(LimiterCatalogueRepository, cluster.ScopeCluster, float64(c.RequestsPerMinute)/60, c.RequestsPerMinute)}
		o.PersonBudget = limiterBudget{ls.NewLimiter(LimiterCataloguePerson, cluster.ScopeCluster, float64(c.RequestsPerMinutePerPerson)/60, c.RequestsPerMinutePerPerson)}
	}
	if a.tele != nil {
		m, err := catalogue.NewMetrics(a.tele.metrics.Registry(), a.tele.metrics.Repos().Value)
		if err != nil {
			return nil, err
		}
		a.tele.metrics.Repos().OnForget(m.Forget)
		o.Metrics = m
	}
	return catalogue.New(o)
}

// maxOpenCounted bounds the count of a repository's open requests the capabilities answer: the list reads at most this many.
const maxOpenCounted = 1000

// limiterBudget is a cluster limiter as the catalogue's budget.
type limiterBudget struct{ l *cluster.Limiter }

func (b limiterBudget) Take(ctx context.Context, bucket string) (bool, time.Duration) {
	v := b.l.Take(ctx, bucket)
	return v.Allowed, v.RetryAfter
}

// catalogueLender lends the catalogue an upstream of the mirror host of the process.
type catalogueLender struct{ a *App }

func (l catalogueLender) Lend(ctx context.Context, t catalogue.Target, o catalogue.Lend) (discover.Upstream, error) {
	b := part[mirrorEngineArea](l.a.parts).b
	if b == nil {
		return nil, errors.New("app: this process has no remote engine for the upstream catalogue")
	}
	mh, err := b.get()
	if err != nil {
		return nil, err
	}
	return mh.CatalogueUpstream(ctx, t.RepoID, formathost.CatalogueLend{MaxBytes: o.MaxBytes, Spend: o.Spend})
}

// upstreamCatalogueAPI are the services of the catalogue's operations (R-CUR-009); a process whose engine cannot be built answers them
// with 503.
func (a *App) upstreamCatalogueAPI() v1.UpstreamCatalogueServices {
	eng, err := a.upstreamCatalogue()
	if err != nil {
		a.log.Warn("the upstream catalogue is not available; its operations answer 503", "error", err)
		return v1.UpstreamCatalogueServices{}
	}
	out := v1.UpstreamCatalogueServices{
		Engine: eng, Decider: part[deliveryRulesArea](a.parts).decider,
		Stored: catalogue.Stored{Runner: a.database()},
	}
	if svc, err := a.downloads(); err == nil && svc != nil {
		out.Queue = svc
	}
	if req, _, err := a.curationRequests(); err == nil && req != nil {
		out.Requests = req
		out.OpenRequests = func(ctx context.Context, repoID int64) (int, error) {
			open, err := req.List(ctx, repoID, requests.Filter{State: string(remotecuration.RequestOpen), Limit: maxOpenCounted})
			return len(open), err
		}
	}
	vz := part[vulnArea](a.parts)
	if vz.evaluator != nil {
		out.Verdicts = vuln.MissVerdicts{Evaluator: vz.evaluator}
		if c, ok := vz.evaluator.(interface{ Covers(string) bool }); ok {
			out.Verdicts.Covers = c.Covers
		}
	}
	return out
}

// catalogueLookup is the requests' check of an ask against the upstream (requests.Lookup, R-CUR-015 "Asking"): the catalogue's lookup by
// exact name, spent from the asker's budget; a version is looked for among the package's versions that hold its text.
type catalogueLookup struct{ a *App }

var _ requests.Lookup = catalogueLookup{}

func (l catalogueLookup) Exists(ctx context.Context, repoID int64, name, version string) (bool, error) {
	eng, err := l.a.upstreamCatalogue()
	if err != nil {
		return false, err
	}
	r := l.a.Repositories()
	if r == nil {
		return false, errors.New("app: this process has no repository reader for the upstream catalogue")
	}
	repo, err := r.ByID(ctx, repoID)
	if err != nil {
		return false, err
	}
	rs, _ := repo.Remote()
	t := catalogue.Target{
		RepoID: repoID, Repo: repo.Name.String(), Format: repo.Format, Upstream: rs.URL,
		Person: principal.IDOrEmpty(httpserver.PrincipalFrom(ctx).ID),
	}
	rq := discover.VersionsRequest{Limit: 1}
	if version != "" {
		rq = discover.VersionsRequest{Limit: discover.MaxVersionsOnPage, Filter: version}
	}
	info, err := eng.Versions(ctx, t, name, rq)
	if e, ok := errs.As(err); ok && e.Code == errs.CodeCatalogueNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if version == "" {
		return true, nil
	}
	want, _ := catalog.Default().CanonicalVersion(repo.Format, version)
	for _, v := range info.Versions {
		if v.Version == version || v.Version == want {
			return true, nil
		}
	}
	return false, nil
}
