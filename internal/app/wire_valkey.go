package app

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/observability/health"
)

// valkeyComponent starts the Valkey client in the background (R-ARCH-024 step 7).
// Start never waits for an unreachable Valkey; step 10 waits for its first answer
// for at most valkey.startup_answer_timeout, and a configuration error found by
// that answer holds back the first readiness (Q-HA-13).
type valkeyComponent struct {
	a     *App
	facts postgres.SystemFacts
	vk    *cluster.Valkey
}

// wireValkey is the valkey area. The client is keyed by the keyspace of the
// system facts, which the postgres area read.
func (a *App) wireValkey(ctx context.Context, pg postgresArea) (valkeyArea, error) {
	if err := requireInputs(AreaValkey, given("database", pg.db)); err != nil {
		return valkeyArea{}, err
	}
	c := &valkeyComponent{a: a, facts: pg.facts}
	if err := a.startComponent(ctx, c); err != nil {
		return valkeyArea{}, err
	}
	return valkeyArea{vk: c.vk}, nil
}

func (c *valkeyComponent) Name() string { return "valkey" }

func (c *valkeyComponent) Start(ctx context.Context) error {
	a := c.a
	since := time.Now()
	opts := []cluster.ValkeyOption{
		cluster.WithTLSMetrics(a.tele.tls), cluster.WithKeyspaceID(keyspaceHex(c.facts.KeyspaceID)), cluster.WithScriptCheck(),
	}
	vk, err := cluster.NewValkey(ctx, a.cfg.Valkey, a.id.Instance, a.id.Role, a.log, a.tele.metrics.Registry(), opts...)
	if err != nil {
		return err
	}
	c.vk = vk
	a.setValkey(vk, since)
	a.health.AddReported("valkey", func(context.Context) error {
		return valkeyCheck(vk.StartupError(), vk.Status())
	}, health.WithInterval(a.cfg.Valkey.HealthCheckInterval))
	return nil
}

func (c *valkeyComponent) Stop(context.Context) error {
	if c.vk != nil {
		c.vk.Close()
	}
	return nil
}

// keyspaceHex turns system_state.keyspace_id, a UUID, into the hex digits the
// key registry takes (R-ARCH-030): the dashes are dropped.
func keyspaceHex(id string) string { return strings.ReplaceAll(id, "-", "") }

// valkeyCheck is the readiness check of the Valkey client. The reason it reports
// is the client's own code (cluster.ValkeyStatus.ReasonCode), the fixed
// vocabulary of cluster.ReasonCodes: the ops listener never shows the free text
// of a degraded reason, which can carry addresses and error strings of the
// Valkey topology; the text goes to the log and to the authenticated replica
// list only. A start-up error keeps the code of the failed connection, so a
// rejected credential reads auth_failed, and holds back the first readiness
// (Q-HA-13).
func valkeyCheck(startupErr error, st cluster.ValkeyStatus) error {
	code := string(st.ReasonCode)
	if startupErr != nil {
		if code == "" {
			code = string(cluster.ReasonConfigError)
		}
		return health.ConfigError(startupErr, code)
	}
	if !st.Available {
		if code == "" {
			code = string(cluster.ReasonProbeFailed)
		}
		return health.WithReason(errUnavailable, code)
	}
	return nil
}

// awaitValkey is the Valkey part of step 10 (R-HA-008): it returns once the
// client has had its first answer or valkey.startup_answer_timeout, counted from
// the client's start, has passed. A process without a Valkey client (the shell)
// does not wait.
func (a *App) awaitValkey(ctx context.Context) error {
	a.mu.Lock()
	vk, since := a.vk, a.vkSince
	a.mu.Unlock()
	if vk == nil {
		return nil
	}
	return awaitValkeyAnswer(ctx, vk, since, a.cfg.Valkey.StartupAnswerTimeout, &a.awaitingValkey, a.log)
}

// firstAnswer is what awaitValkeyAnswer reads of the Valkey client.
type firstAnswer interface {
	Answered() <-chan struct{}
	Status() cluster.ValkeyStatus
}

// awaitValkeyAnswer holds the first readiness until Valkey has answered once,
// accepted or refused, or until bound has passed since since (Q-HA-13 as the
// owner decided it on 2026-11-20). An answer ends the wait at once: an accepted
// credential lets the replica become ready, a refusal leaves the start-up error
// that the valkey check reports and that holds the first readiness. At the
// bound the replica becomes ready degraded and the log says why. awaiting is
// set while it waits, for the startup check's reason. Only step 10 calls it: a
// replica that has been ready never waits for Valkey again.
func awaitValkeyAnswer(ctx context.Context, vk firstAnswer, since time.Time, bound time.Duration, awaiting *atomic.Bool, log *slog.Logger) error {
	answered := vk.Answered()
	select {
	case <-answered:
		return nil
	default:
	}
	remaining := bound - time.Since(since)
	if remaining > 0 {
		awaiting.Store(true)
		defer awaiting.Store(false)
		log.Info("waiting for Valkey's first answer before the first readiness",
			"reason_code", string(vk.Status().ReasonCode), "bound", bound.String(), "left", remaining.Round(time.Millisecond).String())
		t := time.NewTimer(remaining)
		defer t.Stop()
		select {
		case <-answered:
			log.Info("valkey answered; the first readiness no longer waits for it", "waited", time.Since(since).Round(time.Millisecond).String())
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	st := vk.Status()
	log.Warn("valkey gave no answer within valkey.startup_answer_timeout: the replica becomes ready without it and runs degraded until Valkey answers; a wrong credential or certificate shows only then",
		"bound", bound.String(), "reason_code", string(st.ReasonCode), "reason", st.DegradedReason)
	return nil
}
