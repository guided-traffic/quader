package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/api/edge"
	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/cache"
	"github.com/guided-traffic/quader/internal/ci"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/limits/httplimit"
	"github.com/guided-traffic/quader/internal/limits/store"
	"github.com/guided-traffic/quader/internal/limits/usage"
	"github.com/guided-traffic/quader/internal/qname"
)

// The download limits (usage-and-limits.md, W-26): every role builds the engine, its compiled index and its buffers, because
// every role charges something: the client listeners the requests of their protocol paths, the worker the system's upstream
// work of mirror passes (R-LIM-001, Q-LIM-19). The stage of the client listener (wire_api.go) and the remote engine
// (wire_proxy_engine.go) take the engine from here.

// downloadLimitsArea is the result of the download limits' step.
type downloadLimitsArea struct {
	engine *limits.Engine
	store  *store.Store
	usage  *usage.Buffer
	reader usage.Reader
	index  *limitsIndex
	// share is n of the fallback (R-LIM-019): the listener's replica count once wireLimits ran, 1 on a role without one.
	share *atomic.Pointer[func() int]
}

// wireDownloadLimits builds the store, the index, the usage buffer and the engine of the download limits.
func (a *App) wireDownloadLimits(ctx context.Context, in infra) (downloadLimitsArea, error) {
	log := a.log.With("component", "limits")
	st := store.New(store.Options{Runner: in.db, Changes: in.changes, Audit: in.rec, Log: log})
	idx := &limitsIndex{store: st, log: log, epoch: newLimitsIndexEpoch(a.tele.metrics.Registry())}
	idx.register(in.caches)
	if err := a.startComponent(ctx, idx); err != nil {
		return downloadLimitsArea{}, err
	}
	cfg := a.cfg.Limits
	buf := usage.NewBuffer(usage.Options{
		Runner: in.db, Interval: cfg.Usage.FlushInterval, MaxKeys: cfg.Usage.MaxBufferedKeys, Log: log,
		Registry: a.tele.metrics.Registry(),
	})
	if err := a.startComponent(ctx, usageComponent{buf}); err != nil {
		return downloadLimitsArea{}, err
	}
	share := &atomic.Pointer[func() int]{}
	metrics := limits.NewMetrics(a.tele.metrics.Registry())
	o := limits.Options{
		Index: idx.get, Replica: a.id.Instance, FlushInterval: cfg.FlushInterval, LeaseTTL: cfg.Parallel.Lease,
		ParallelWait: cfg.Parallel.Wait, MinPacing: cfg.Pacing.MinBytesPerSecond, Usage: buf, Blocks: st, Metrics: metrics,
		Log: log,
		// The "Your limits" link of a refusal (R-LIM-020) names the base URL the caller reached the installation by: its
		// endpoint's, else the instance's.
		BaseURL: func(ctx context.Context) string {
			if b := httpserver.InfoFrom(ctx).BaseURL; b != "" {
				return b
			}
			return a.cfg.Server.BaseURL
		},
		Share: func() int {
			if f := share.Load(); f != nil {
				return (*f)()
			}
			return 1
		},
	}
	if counters := cluster.NewLimitCounters(in.vk); counters != nil {
		o.Store, o.Available, o.Degraded = counters, counters.Available, counters.CountDegraded
	}
	eng := limits.NewEngine(o)
	idx.onInstall = func(x *limits.Index) { metrics.SetMode(x.Settings().Mode) }
	if err := a.startComponent(ctx, engineComponent{eng}); err != nil {
		return downloadLimitsArea{}, err
	}
	return downloadLimitsArea{engine: eng, store: st, usage: buf, reader: usage.Reader{Runner: in.db}, index: idx, share: share}, nil
}

// isAdministrator is the administrator of R-LIM-012 (Q-LIM-18): the principal holds the role admin on the installed index,
// whatever its token's scope. It is a question in memory, not a decision: nothing is audited.
func (a *App) isAdministrator(az authzArea) func(r *http.Request) bool {
	return func(r *http.Request) bool {
		p := httpserver.PrincipalFrom(r.Context())
		if p.Anonymous {
			return false
		}
		idx := az.engine.Index()
		return idx != nil && idx.HoldsAdmin(edge.SubjectFrom(r))
	}
}

// limitsAPI are the services of the limits and usage operations (R-API-082).
func (a *App) limitsAPI() v1.LimitsServices {
	dl := part[downloadLimitsArea](a.parts)
	if dl.engine == nil {
		return v1.LimitsServices{}
	}
	out := v1.LimitsServices{Store: dl.store, Engine: dl.engine, Usage: dl.reader, Repos: part[repositoryArea](a.parts).reader}
	if p := part[identityArea](a.parts).principals; p != nil {
		out.Principals = p
	}
	az := part[authzArea](a.parts)
	if az.projects != nil {
		out.Projects = func(ctx context.Context, key string) (string, error) {
			p, err := az.projects.Get(ctx, key)
			return p.ID, err
		}
	}
	if az.engine != nil {
		out.IsAdministrator = func(_ context.Context, id string, groups []string) bool {
			return az.engine.Index().UserHoldsAdmin(id, groups)
		}
	}
	if is := part[ciArea](a.parts).issuers; is != nil {
		out.CI = ciIssuerNames{is}
	}
	if regs := a.bastionRegistrations(); regs != nil {
		// The place of a Bastion (R-LIM-043) by its registration's name, and the name of a place the usage store recorded.
		out.Bastion = func(ctx context.Context, name string) (string, error) {
			reg, err := regs.Get(ctx, name)
			return reg.ID, err
		}
		out.BastionName = func(ctx context.Context, id string) (string, bool) {
			all, err := regs.List(ctx)
			if err != nil {
				return "", false
			}
			for _, reg := range all {
				if reg.ID == id {
					return reg.Name, true
				}
			}
			return "", false
		}
	}
	return out
}

// ciIssuerNames translates between a CI issuer's name and its ID for the rules of CI projects (Q-LIM-1).
type ciIssuerNames struct{ is ci.Issuers }

func (c ciIssuerNames) IssuerID(ctx context.Context, name string) (string, bool, error) {
	is, err := c.is.Get(ctx, name)
	if errs.KindOf(err) == errs.NotFound {
		return "", false, nil
	}
	return is.ID, err == nil, err
}

func (c ciIssuerNames) IssuerName(ctx context.Context, id string) (string, bool, error) {
	all, err := c.is.List(ctx)
	if err != nil {
		return "", false, err
	}
	for _, is := range all {
		if is.ID == id {
			return is.Name, true, nil
		}
	}
	return "", false, nil
}

// limitsRepo is the repository a protocol request names for the download limits (R-LIM-005): the one its address names, and
// on a host whose root is bound to a repository of the registry format (R-EP-006, R-EP-007) that repository for every path below
// /v2/ but the version check, the catalog and the token service, as the dispatcher's match of /v2/ reads it
// (dispatch.Dispatcher.BoundRepository). The format host's area is read when a request asks, after every area was wired.
func (a *App) limitsRepo(r *http.Request) qname.Repo {
	p := r.URL.Path
	if strings.HasPrefix(p, registrytoken.BlobPathPrefix) {
		// The malware scanner's read is recorded under the repository its blob token names (Q-LIM-80); the path names none.
		if b := httpserver.PrincipalFrom(r.Context()).Credential.ScanBlob; b != nil {
			return b.Repo
		}
		return qname.Repo{}
	}
	if strings.HasPrefix(p, "/v2/") && p != "/v2/" && p != "/v2/_catalog" && p != httplimit.TokenPath {
		if disp := part[formatHostArea](a.parts).disp; disp != nil {
			if bound, ok, err := disp.BoundRepository(r); err == nil && ok {
				return bound
			}
		}
	}
	return httplimit.Repo(r)
}

// limitsFailedCredential records a request on a protocol path whose credential the authentication stage refused (R-LIM-003):
// one request and one refused request of the client's address, never of the subject the credential names.
func (a *App) limitsFailedCredential() func(*http.Request, error) {
	eng := part[downloadLimitsArea](a.parts).engine
	if eng == nil {
		return nil
	}
	return func(r *http.Request, err error) {
		if errs.KindOf(err) != errs.Unauthenticated || !httplimit.ProtocolPath(r.URL.Path) {
			return
		}
		eng.FailedCredential(httplimit.CallerOf(r, httpserver.AnonymousPrincipal(), false), a.limitsRepo(r))
	}
}

// limitsIndex holds the compiled index of the limits (R-LIM-013): loaded at start, rebuilt on a change of the area limits and
// of the repository configuration (a renamed or moved repository is a place of another name), and at least once a minute.
// A replica that has not loaded it yet decides with the built-in defaults of an empty index: Enforce, every subject unlimited.
type limitsIndex struct {
	store     *store.Store
	log       *slog.Logger
	cur       atomic.Pointer[limits.Index]
	onInstall func(*limits.Index)
	// epoch is quader_limits_index_epoch: the epoch of the area limits the index in use holds every change up to (Q-LIM-111)
	epoch prometheus.Gauge

	cancel context.CancelFunc
	wg     sync.WaitGroup
	kick   chan struct{}
}

const limitsIndexReload = time.Minute

func (x *limitsIndex) get() *limits.Index { return x.cur.Load() }

// register subscribes to the change areas through a cache of the registry, whose change handling knows the epochs.
func (x *limitsIndex) register(r *cache.Registry) {
	x.kick = make(chan struct{}, 1)
	if r == nil {
		return
	}
	cache.Register(r, "limits_index", cache.Options[struct{}, struct{}]{
		MaxEntries: 1, SafetyTTL: limitsIndexReload, Areas: []string{cluster.AreaLimits, cluster.AreaRepoConfig},
		OnChange: func(*cache.Cache[struct{}, struct{}], string, int, json.RawMessage) { x.reload() },
		OnFlush:  func(string) { x.reload() },
	})
}

func (x *limitsIndex) reload() {
	select {
	case x.kick <- struct{}{}:
	default:
	}
}

// Name implements Component.
func (x *limitsIndex) Name() string { return "limits-index" }

// Start loads the index once and runs the reload loop; a failed load is retried by the loop, the replica decides with the
// defaults meanwhile.
func (x *limitsIndex) Start(ctx context.Context) error {
	x.load(ctx)
	lctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	x.cancel = cancel
	x.wg.Add(1)
	go func() {
		defer x.wg.Done()
		t := time.NewTicker(limitsIndexReload)
		defer t.Stop()
		for {
			select {
			case <-lctx.Done():
				return
			case <-t.C:
			case <-x.kick:
			}
			x.load(lctx)
		}
	}()
	return nil
}

func (x *limitsIndex) load(ctx context.Context) {
	lctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	snap, err := x.store.Snapshot(lctx)
	if err != nil {
		if ctx.Err() == nil {
			x.log.Warn("the index of the download limits was not loaded; the last one stays", "error", err)
		}
		return
	}
	idx := limits.NewIndex(snap)
	x.cur.Store(idx)
	if x.epoch != nil {
		x.epoch.Set(float64(snap.Epoch))
	}
	if x.onInstall != nil {
		x.onInstall(idx)
	}
}

// newLimitsIndexEpoch registers quader_limits_index_epoch (Q-LIM-111): what a change of the limits waits for before it is
// known to apply on a replica, instead of a fixed time. A registry that refuses it leaves a gauge nothing reads.
func newLimitsIndexEpoch(reg prometheus.Registerer) prometheus.Gauge {
	g := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "quader_limits_index_epoch",
		Help: "The epoch of the change area limits up to which the replica's index of the download limits holds every change (R-LIM-013); 0 until the first load.",
	})
	if reg != nil {
		_ = reg.Register(g)
	}
	return g
}

// Stop ends the loop.
func (x *limitsIndex) Stop(context.Context) error {
	if x.cancel != nil {
		x.cancel()
	}
	x.wg.Wait()
	return nil
}

// engineComponent runs the flush of the charge buffer (R-LIM-017) and flushes it at shutdown.
type engineComponent struct{ e *limits.Engine }

func (c engineComponent) Name() string                    { return "limits" }
func (c engineComponent) Start(ctx context.Context) error { c.e.Start(ctx); return nil }
func (c engineComponent) Stop(ctx context.Context) error  { c.e.Stop(ctx); return nil }

// usageComponent runs the flush of the usage buffer (R-LIM-028); it stops after the engine, whose last flush feeds it.
type usageComponent struct{ b *usage.Buffer }

func (c usageComponent) Name() string                    { return "limits-usage" }
func (c usageComponent) Start(ctx context.Context) error { c.b.Start(ctx); return nil }
func (c usageComponent) Stop(ctx context.Context) error  { c.b.Stop(ctx); return nil }

// registerLimitsJobs registers the daily task limits-cleanup (R-LIM-028, R-OPS-019): it folds, deletes and erases on a worker.
func (a *App) registerLimitsJobs(r *jobs.Registry) {
	r.RegisterKind(usage.TaskCleanup, jobs.KindSpec{
		Queue: "maintenance", DedupMode: jobs.Exclusive, MaxAttempts: 3,
		Handler: func(ctx context.Context, run *jobs.Run) error {
			db := a.database()
			if db == nil {
				return jobs.Permanent(errors.New("limits-cleanup: this replica has no database"))
			}
			res, err := usage.Cleanup(ctx, db, a.auditRecorder(), time.Now(), a.cfg.Limits.Usage.SummaryRetention)
			if err != nil {
				return err
			}
			return run.SetResult(res)
		},
	})
	spec := jobs.TaskSpec{Schedule: "25 3 * * *", Mutating: true}
	if t, ok := a.cfg.Maintenance.Tasks[usage.TaskCleanup]; ok && t.Schedule != "" {
		spec.Schedule, spec.Disabled = t.Schedule, !t.Enabled
	}
	r.RegisterTask(usage.TaskCleanup, spec)
}
