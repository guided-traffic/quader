package app

import (
	"context"

	v1 "github.com/guided-traffic/quader/internal/api/v1"
	debdata "github.com/guided-traffic/quader/internal/formatdata/deb"
	"github.com/guided-traffic/quader/internal/meta"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules/store"
	"github.com/guided-traffic/quader/internal/repository"
)

// The wiring of the delivery rules of a remote repository (remote-curation.md R-CUR-001 to R-CUR-006, docs/developer/remote-curation.md
// package R2): the rules' store behind the management API, the record of a client's refusal (the event, the metric and the count in the
// request list) for the format host and /files/, and the decider over the replica's snapshot, which replaces the stub of the
// contracts' package.

// AreaDeliveryRules is the delivery rules' part of every process.
const AreaDeliveryRules Area = "delivery rules"

// deliveryRulesArea is what the area builds.
type deliveryRulesArea struct {
	store    *store.Store
	recorder *store.Recorder
	decider  rc.Decider
}

// wireDeliveryRules builds the store, the recorder (whose summed counts a component writes until the process stops) and the decider.
func (a *App) wireDeliveryRules(ctx context.Context, in infra, repo repositoryArea) (deliveryRulesArea, error) {
	if err := requireInputs(AreaDeliveryRules, given("database", in.db), given("audit recorder", in.rec),
		given("change log", in.changes), given("repository reader", repo.reader)); err != nil {
		return deliveryRulesArea{}, err
	}
	rs, err := store.NewStore(store.Options{
		Runner: in.db, Recorder: in.rec, Notifier: in.changes, Compile: repository.CompileSection, MaxRules: a.cfg.Proxy.Delivery.MaxRules,
		OnChange: debVirtualsOnRules,
	})
	if err != nil {
		return deliveryRulesArea{}, err
	}
	repos := a.tele.metrics.Repos()
	refusals := a.tele.metrics.NewCounterVec("quader_remote_delivery_refusals_total",
		"Refusals of clients by the delivery rules of a remote repository (R-CUR-004).", "repo", "reason", "kind")
	repos.DeleteOnForget(refusals)
	rec := store.NewRecorder(store.RecorderOptions{
		Runner: in.db, Audit: in.rec, Refusals: refusals, MaxClientEntries: a.cfg.Proxy.Delivery.MaxClientEntries,
		Label: func(name string) string { return repos.Value("quader_remote_delivery_refusals_total", name) },
		Log:   a.log.With("component", "delivery-rules"),
	})
	if err := a.startComponent(ctx, &refusalComponent{rec: rec}); err != nil {
		return deliveryRulesArea{}, err
	}
	return deliveryRulesArea{store: rs, recorder: rec, decider: repository.Decider{Reader: repo.reader}}, nil
}

// debVirtualsOnRules enqueues the index build of the virtual Debian repositories that list a remote whose rules changed (R-DEB-036).
func debVirtualsOnRules(ctx context.Context, tx meta.Tx, t store.Target) error {
	if t.Format != "deb" {
		return nil
	}
	return debdata.EnqueueVirtualBuilds(ctx, tx, t.ID)
}

// refusalComponent writes the recorder's summed counts until the process stops, and once more then.
type refusalComponent struct {
	rec    *store.Recorder
	cancel context.CancelFunc
	done   chan struct{}
}

func (c *refusalComponent) Name() string { return "delivery refusals" }

func (c *refusalComponent) Start(context.Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel, c.done = cancel, make(chan struct{})
	go func() {
		defer close(c.done)
		c.rec.Run(ctx)
	}()
	return nil
}

func (c *refusalComponent) Stop(ctx context.Context) error {
	c.cancel()
	select {
	case <-c.done:
	case <-ctx.Done():
	}
	return nil
}

// deliveryRulesAPI is the services of the rules' operations; a process without the area answers them with 503.
func (a *App) deliveryRulesAPI() v1.DeliveryRulesServices {
	d := part[deliveryRulesArea](a.parts)
	r := part[repositoryArea](a.parts)
	return v1.DeliveryRulesServices{Store: d.store, Reader: r.reader}
}

// deliveryRecorder is the record of a client's refusal; nil in a process without the area.
func (a *App) deliveryRecorder() *store.Recorder { return part[deliveryRulesArea](a.parts).recorder }
