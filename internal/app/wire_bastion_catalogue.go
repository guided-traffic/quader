package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain/catalogue"
	"github.com/guided-traffic/quader/internal/cache"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/formats/ansible/docgen"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/packages/readme"
)

// The builder of the Bastions' public catalogues (bastion.md R-BAS-104 to R-BAS-108; W-35's package P6): the worker's job
// bastion-catalogue, its hourly task, the triggers that start it within seconds of a change, and the renderer it reads the
// management API through.

// AreaBastionCatalogue is the trigger of the public catalogue's builder on a replica that claims jobs.
const AreaBastionCatalogue Area = "bastion-catalogue"

// KindBastionCatalogue is the job and the task that build the public catalogues (R-BAS-105).
const KindBastionCatalogue = "bastion-catalogue"

// The figures of the builder (R-BAS-105, the measurement of 2026-11-26 in docs/developer/bastion.md).
const (
	// catalogueSchedule is the reconciliation: every hour by default (maintenance.tasks.bastion-catalogue), so a change a trigger
	// missed stays at most an hour.
	catalogueSchedule = "41 * * * *"
	// catalogueDebounce is the delay of a run a trigger starts: the changes of one publish arrive as several signals and change
	// areas within it, and one run takes them all (the Coalesce key).
	catalogueDebounce = 5 * time.Second
	// catalogueWorkers render the packages of a registration in parallel.
	catalogueWorkers = 4
	// catalogueActiveFor is how long the trigger trusts its answer to "does any registration have a catalogue to build".
	catalogueActiveFor = 10 * time.Second
)

// bastionCatalogueArea is the trigger component; it is nil on a replica that claims no jobs.
type bastionCatalogueArea struct{ trigger *catalogueTrigger }

// catalogueRuntime is what the job's handler runs with, set by the area's step and read late by the handler, because the job
// registry is built before the areas.
type catalogueRuntime struct {
	builder *catalogue.Builder
	regs    func(context.Context) ([]catalogue.Registration, error)
}

// wireBastionCatalogue builds the builder over the store of 00146 and the renderer of the management API, and the trigger that
// starts its job after a change (R-BAS-105). The renderer and the API services it reads are built at the first run: every area
// they read exists by then.
func (a *App) wireBastionCatalogue(_ context.Context, in infra, co coordArea, _ packagesAPIArea, _ vulnAPIArea) (bastionCatalogueArea, error) {
	if err := requireInputs(AreaBastionCatalogue, given("database", in.db), given("job queue", co.queue)); err != nil {
		return bastionCatalogueArea{}, err
	}
	store := catalogue.NewDB(in.db)
	render := sync.OnceValues(a.catalogueRenderer)
	b := catalogue.New(catalogue.Options{
		Store: store, Renderer: lateRenderer{render}, Main: a.mainOrigins(), PolicyHash: a.readmePolicy(), DocsPolicy: docsPolicy(),
		Workers: catalogueWorkers, Log: a.log.With("component", "bastion-catalogue"),
	})
	a.catalogue.set(catalogueRuntime{builder: b, regs: a.catalogueRegistrations})
	t := &catalogueTrigger{a: a, queue: co.queue, signals: co.signals, store: store, kick: make(chan struct{}, 1)}
	if co.caches != nil {
		cache.Register(co.caches, "bastion_catalogue_trigger", cache.Options[struct{}, struct{}]{
			MaxEntries: 1, SafetyTTL: time.Hour, Areas: []string{cluster.AreaRepoConfig, cluster.AreaAuthz, cluster.AreaVulnData},
			OnChange: func(*cache.Cache[struct{}, struct{}], string, int, json.RawMessage) { t.fire() },
			OnFlush:  func(string) { t.fire() },
		})
	}
	return bastionCatalogueArea{trigger: t}, a.startComponent(context.Background(), t)
}

// registerBastionCatalogueJobs registers the job and its task (R-BAS-105): one run at a time in the installation (Coalesce: a change
// during a run waits as the one job behind it), on the default queue, the reconciliation every hour.
func (a *App) registerBastionCatalogueJobs(r *jobs.Registry) {
	r.RegisterKind(KindBastionCatalogue, jobs.KindSpec{
		Queue: "default", DedupMode: jobs.Coalesce, MaxAttempts: 3,
		Handler: a.catalogue.handler("the public catalogue's builder", func(ctx context.Context, c catalogueRuntime, run *jobs.Run) error {
			regs, err := c.regs(ctx)
			if err != nil {
				return err
			}
			rep, err := c.builder.Run(ctx, regs)
			if rerr := run.SetResult(map[string]any{
				"built": rep.Built, "dropped": rep.Dropped, "stopped": rep.Stopped, "rendered": rep.Rendered,
				"documents": rep.Documents, "deleted": rep.Deleted, "failed": rep.Failed, "swept": rep.Swept,
			}); rerr != nil && err == nil {
				err = rerr
			}
			return err
		}),
	})
	task := jobs.TaskSpec{Schedule: catalogueSchedule}
	if c, ok := a.cfg.Maintenance.Tasks[KindBastionCatalogue]; ok && c.Schedule != "" {
		task.Schedule, task.Disabled = c.Schedule, !c.Enabled
	}
	r.RegisterTask(KindBastionCatalogue, task)
}

// catalogueRegistrations lists every registration with its switches and the base URL its documents are rendered on (R-BAS-104): the
// base URL the registration reported at its last handshake, when it is an entry of server.additional_base_urls. A disabled
// registration builds nothing.
func (a *App) catalogueRegistrations(ctx context.Context) ([]catalogue.Registration, error) {
	in := part[infra](a.parts)
	regs := a.bastionRegistrations()
	if in.db == nil || regs == nil {
		return nil, errors.New("the public catalogue needs the database and the registrations")
	}
	list, err := catalogue.NewDB(in.db).Registrations(ctx)
	if err != nil {
		return nil, err
	}
	reported := bastionBaseURLs{db: in.db}
	out := make([]catalogue.Registration, 0, len(list))
	for _, l := range list {
		reg, err := regs.Get(ctx, l.Name)
		if err != nil {
			return nil, err
		}
		sw := a.bastionSwitches(l.Name, reg.Switches)
		r := catalogue.Registration{ID: l.ID, Name: l.Name, GUI: l.Enabled && sw.GUI, Docs: sw.Docs}
		if r.GUI {
			base, _, err := reported.ReportedBaseURL(ctx, l.Name)
			if err != nil {
				return nil, err
			}
			r.BaseURL, _ = catalogue.BaseURL(base, a.cfg.Server.AdditionalBaseURLs)
		}
		out = append(out, r)
	}
	return out, nil
}

// bastionSwitches are a registration's switches gui and docs: the stored ones of its configuration (R-BAS-093, R-BAS-102), or the
// test's when it set them (Options.BastionSwitches).
func (a *App) bastionSwitches(name string, stored iface.Switches) iface.Switches {
	sw := stored
	if a.o.BastionSwitches != nil {
		o := a.o.BastionSwitches(name)
		sw.GUI, sw.Docs = o.GUI, o.Docs
	}
	return sw
}

// mainOrigins are the origins of the main installation no document may name (B.13): server.base_url, every additional base URL and
// every further endpoint's base URL. The builder replaces an occurrence with the registration's base URL, which is one of them.
func (a *App) mainOrigins() []string {
	var out []string
	for _, o := range a.ownOrigins() {
		if u, err := url.Parse(o); err == nil && u.Scheme != "" && u.Host != "" {
			out = append(out, u.Scheme+"://"+u.Host)
		}
	}
	return out
}

// readmePolicy is the README renderer's policy hash: a changed sanitizer renders every document again (D1).
func (a *App) readmePolicy() string {
	if rd := part[packagesAPIArea](a.parts).services.Readme; rd != nil {
		return rd.PolicyHash()
	}
	return ""
}

// docsPolicy is what besides the README policy decides the documentation's HTML the catalogue keeps (R-BAS-124): the README
// renderer's version and the Ansible markup converter's. Both convert on every read of the main installation, so no stored fact
// changes with them.
func docsPolicy() string {
	return "readme/" + strconv.Itoa(readme.Version) + " ansible-markup/" + strconv.Itoa(docgen.MarkupVersion)
}

// catalogueRenderer builds the renderer: the management API's handlers on a router of the default endpoint's snapshot, behind a
// chain that resolves the base URL and nothing else (no limit, no request log, no usage record, every caller anonymous). The
// request comes from the loopback, which this resolver alone trusts, with the Bastion's origin as the forwarded one, so the base
// URL of every link is the Bastion's, as for a request of the Bastion itself (R-BAS-104).
func (a *App) catalogueRenderer() (catalogue.Renderer, error) {
	var def *config.ResolvedEndpoint
	for _, e := range a.cfg.Server.ResolveEndpoints() {
		if e.Default {
			def = &e
		}
	}
	if def == nil {
		return nil, errors.New("the public catalogue: no default endpoint")
	}
	ep, err := httpserver.NewEndpoint(*def, a.cfg.Server.AdditionalBaseURLs, httpserver.ForRoleFamilies(config.RoleFamilies(config.RoleAPI)))
	if err != nil {
		return nil, err
	}
	resolver, err := httpserver.NewResolver([]string{catalogueLoopback + "/32"}, def.BaseURL, a.cfg.Server.AdditionalBaseURLs,
		httpserver.WithForwardedHeader(httpserver.HeaderXForwardedFor), httpserver.ForEndpoint(ep))
	if err != nil {
		return nil, err
	}
	doc, err := a.apiDocument()
	if err != nil {
		return nil, err
	}
	router := httpserver.NewEndpointRouter(ep)
	// No interface route answers here: the Bastion's services are none, and the routes the builder reads need none.
	v1.New(a.apiOptions(doc, v1.BastionServices{}, nil, nil, nil)).Register(router)
	statement := a.cfg.Database.RequestStatementTimeout
	chain := httpserver.Chain{
		Router: router, Log: a.log, Endpoint: ep, Resolver: resolver,
		RequestContext: func(ctx context.Context) context.Context { return meta.WithStatementTimeout(ctx, statement) },
	}
	return chainRenderer{h: chain.Handler()}, nil
}

// catalogueLoopback is the peer of the renderer's requests.
const catalogueLoopback = "127.0.0.1"

// chainRenderer reads through the renderer's chain.
type chainRenderer struct{ h http.Handler }

// Get implements catalogue.Renderer.
func (c chainRenderer) Get(ctx context.Context, base, target string) (catalogue.Answer, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return catalogue.Answer{}, fmt.Errorf("the public catalogue: the base URL %q", base)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return catalogue.Answer{}, err
	}
	req.Host, req.RemoteAddr = u.Host, catalogueLoopback+":1"
	req.Header.Set("X-Forwarded-Proto", u.Scheme)
	req.Header.Set("X-Forwarded-Host", u.Host)
	w := &answerWriter{h: http.Header{}}
	c.h.ServeHTTP(w, req)
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return catalogue.Answer{Status: w.status, ContentType: w.h.Get("Content-Type"), Body: w.b.Bytes()}, nil
}

// answerWriter keeps an answer in memory.
type answerWriter struct {
	h      http.Header
	status int
	b      bytes.Buffer
}

func (w *answerWriter) Header() http.Header { return w.h }
func (w *answerWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
}

func (w *answerWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.b.Write(p)
}

// lateRenderer builds the renderer at its first read.
type lateRenderer struct {
	f func() (catalogue.Renderer, error)
}

// Get implements catalogue.Renderer.
func (l lateRenderer) Get(ctx context.Context, base, target string) (catalogue.Answer, error) {
	r, err := l.f()
	if err != nil {
		return catalogue.Answer{}, err
	}
	return r.Get(ctx, base, target)
}

// catalogueTrigger starts the job a debounce after a change that may change a catalogue (R-BAS-105): a package's metadata, a
// verdict of the malware scanning or of a signature, a repository's settings, the authorization, the advisory data. It enqueues
// only while some registration's web interface is on or some registration still holds a catalogue, so an installation without
// one queues nothing.
type catalogueTrigger struct {
	a       *App
	queue   *jobs.Queue
	signals *cluster.Signals
	store   *catalogue.DB
	kick    chan struct{}
	cancel  context.CancelFunc
	unsub   []func()
	wg      sync.WaitGroup
	// activeAt and active cache the answer of active for catalogueActiveFor.
	mu       sync.Mutex
	activeAt time.Time
	active   bool
	fired    atomic.Int64
}

// Name implements Component.
func (t *catalogueTrigger) Name() string { return "bastion-catalogue-trigger" }

func (t *catalogueTrigger) fire() {
	select {
	case t.kick <- struct{}{}:
	default:
	}
}

// Start subscribes to the signals and runs the enqueue loop.
func (t *catalogueTrigger) Start(ctx context.Context) error {
	lctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	t.cancel = cancel
	if t.signals != nil {
		for _, area := range []string{cluster.AreaMetadataChanged, cluster.AreaMalwareVerdict, cluster.AreaSignatureVerdict} {
			unsub, err := t.signals.Subscribe(lctx, area, func([]string, bool) { t.fire() })
			if err != nil {
				t.a.log.Warn("the public catalogue's builder does not hear a signal; its hourly run takes the change", "area", area, "error", err)
				continue
			}
			t.unsub = append(t.unsub, unsub)
		}
	}
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		for {
			select {
			case <-lctx.Done():
				return
			case <-t.kick:
			}
			if !t.isActive(lctx) {
				continue
			}
			if _, err := t.queue.EnqueueNow(lctx, jobs.EnqueueSpec{Kind: KindBastionCatalogue, Delay: catalogueDebounce}); err != nil && lctx.Err() == nil {
				t.a.log.Warn("the public catalogue's builder was not started after a change; its hourly run takes it", "error", err)
			}
			t.fired.Add(1)
		}
	}()
	return nil
}

// isActive reports whether some registration has a catalogue to build or to drop.
func (t *catalogueTrigger) isActive(ctx context.Context) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if time.Since(t.activeAt) < catalogueActiveFor {
		return t.active
	}
	active := false
	if held, err := t.store.Held(ctx); err == nil && len(held) > 0 {
		active = true
	} else if regs, err := t.a.catalogueRegistrations(ctx); err == nil {
		for _, r := range regs {
			active = active || r.GUI
		}
	}
	t.active, t.activeAt = active, time.Now()
	return active
}

// Stop implements Component.
func (t *catalogueTrigger) Stop(context.Context) error {
	for _, u := range t.unsub {
		u()
	}
	if t.cancel != nil {
		t.cancel()
	}
	t.wg.Wait()
	return nil
}
