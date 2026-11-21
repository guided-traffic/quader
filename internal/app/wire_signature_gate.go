package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/cache"
	"github.com/guided-traffic/quader/internal/cluster"
	sgate "github.com/guided-traffic/quader/internal/signatures/gate"
	sigstore "github.com/guided-traffic/quader/internal/signatures/store"
)

// The gate per repository of the signatures, stage 3 of signatures.md (R-SIG-064 to R-SIG-069): every role that serves downloads builds
// it with the store; the format host and /files/ ask it at their serve points (wire_formats.go, wire_files.go), and it classifies an item
// through the format host, bound when the host exists. The replica's snapshot of anchors, blocks and the switch follows the change area
// signature_anchors (R-SIG-066), and a waiting download hears the signal area signature_verdict (R-SIG-067).

// newSignatureGate builds the gate and starts its component. It answers nil while the signatures are off: every gate is off then
// (R-SIG-064, H-1198).
func (a *App) newSignatureGate(ctx context.Context, in infra, st *sigstore.Store) (*sgate.Gate, error) {
	cfg := a.cfg.Signatures
	if st == nil || !cfg.Enabled {
		return nil, nil
	}
	log := a.log.With("component", "signature-gate")
	gm, err := sgate.NewMetrics(a.tele.metrics.Registry())
	if err != nil {
		return nil, err
	}
	o := sgate.Options{
		Store: st, Enabled: cfg.Enabled, Wait: cfg.Gate.DownloadWait, MaxWaiting: cfg.Gate.MaxWaiting, MaxObjectSize: int64(cfg.MaxObjectSize),
		BaseURL: a.cfg.Server.BaseURL, Metrics: gm, Audit: in.rec, Log: log,
	}
	if in.signals != nil {
		signals := in.signals
		o.WakeQueues = func(queues ...string) {
			for _, q := range queues {
				signals.Publish(cluster.AreaJobs, q)
			}
		}
	}
	g := sgate.New(o)
	c := &signatureGateComponent{gate: g, signals: in.signals, log: log}
	c.register(in.caches)
	if err := a.startComponent(ctx, c); err != nil {
		return nil, err
	}
	return g, nil
}

// signatureVerdictSignal is the publisher of the signal area signature_verdict: the job signature-verify after the commit of a verdict,
// the background fetch after a run's row, a subject's hex SHA-256; nil on a replica without signals.
func signatureVerdictSignal(signals *cluster.Signals) func(subject [32]byte) {
	if signals == nil {
		return nil
	}
	return func(subject [32]byte) { signals.Publish(cluster.AreaSignatureVerdict, hex.EncodeToString(subject[:])) }
}

// signatureVerdictSignalAll wakes every waiting download: a batch of the trust step committed (R-SIG-067).
func signatureVerdictSignalAll(signals *cluster.Signals) func() {
	if signals == nil {
		return nil
	}
	return func() { signals.Publish(cluster.AreaSignatureVerdict, "*") }
}

// signatureGateComponent holds the gate's snapshot of anchors and blocks (reloaded by the change area signature_anchors and once a
// minute as its safety period), its subscription to signature_verdict, and answers the waiting downloads when the replica drains.
type signatureGateComponent struct {
	gate    *sgate.Gate
	signals *cluster.Signals
	log     *slog.Logger

	kick   chan struct{}
	cancel context.CancelFunc
	unsub  func()
	wg     sync.WaitGroup
}

// signatureGateReload is the safety period of the snapshot: a lost change is read again after it.
const signatureGateReload = time.Minute

func (c *signatureGateComponent) register(r *cache.Registry) {
	c.kick = make(chan struct{}, 1)
	if r == nil {
		return
	}
	cache.Register(r, "signature_gate", cache.Options[struct{}, struct{}]{
		MaxEntries: 1, SafetyTTL: signatureGateReload, Areas: []string{cluster.AreaSignatureAnchors},
		OnChange: func(*cache.Cache[struct{}, struct{}], string, int, json.RawMessage) { c.reload() },
		OnFlush:  func(string) { c.reload() },
	})
}

func (c *signatureGateComponent) reload() {
	select {
	case c.kick <- struct{}{}:
	default:
	}
}

// Name implements Component.
func (c *signatureGateComponent) Name() string { return "signature-gate" }

// Start loads the snapshot once, subscribes the gate to the verdicts and runs the reload loop. A failed load leaves no snapshot: the gate
// reads it again at its first decision and fails closed while it cannot.
func (c *signatureGateComponent) Start(ctx context.Context) error {
	c.load(ctx)
	lctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	c.cancel = cancel
	if c.signals != nil {
		unsub, err := c.signals.Subscribe(lctx, cluster.AreaSignatureVerdict, c.gate.Signal)
		if err != nil {
			c.log.Warn("the gate does not hear the verdicts of signatures; a waiting download reads again once a second", "error", err)
		} else {
			c.unsub = unsub
		}
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		t := time.NewTicker(signatureGateReload)
		defer t.Stop()
		for {
			select {
			case <-lctx.Done():
				return
			case <-t.C:
			case <-c.kick:
			}
			c.load(lctx)
		}
	}()
	return nil
}

func (c *signatureGateComponent) load(ctx context.Context) {
	lctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := c.gate.Reload(lctx); err != nil && ctx.Err() == nil {
		c.log.Warn("the gate's snapshot of trust anchors was not read; the last one stays", "error", err)
	}
}

// Drain answers the downloads that wait at a gate at once (R-SIG-067): a draining replica holds none of them to its deadline.
func (c *signatureGateComponent) Drain(context.Context) { c.gate.Stop() }

// Stop ends the loop and the subscription.
func (c *signatureGateComponent) Stop(context.Context) error {
	if c.unsub != nil {
		c.unsub()
	}
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
	return nil
}
