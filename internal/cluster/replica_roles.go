package cluster

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// FillsClientMisses reports whether a replica of the role fills client misses from an upstream and therefore holds a
// share of a remote repository's fetch budget (R-REPO-040, Q-ARCH-297): packages, web and all. api sends the upstream
// test of R-REPO-024, which takes no slot, and worker runs the passes of a mirror inside the budget of the replicas that
// fill (R-REPO-055).
func FillsClientMisses(role string) bool {
	switch role {
	case config.RolePackages, config.RoleWeb, config.RoleAll:
		return true
	}
	return false
}

// FillCount is the n of the fetch share (R-REPO-040, Q-ARCH-297): the number of live replicas that fill client misses,
// whatever the role and pool of the replica that asks. It reads the registry once per heartbeat interval, so every
// remote repository of the process divides its budget by the same n, and a failed read keeps the last value: the share
// must not depend on PostgreSQL for the request in flight. n is 1 until the first read.
type FillCount struct {
	db         q.DBTX
	staleAfter time.Duration
	every      time.Duration
	n          atomic.Int32

	mu sync.Mutex
	// life is the refresh loop's: Stop waits for a count in flight instead of cutting it (Q-ARCH-503). Nil before Start.
	life *loopLife
}

// NewFillCount creates the count over the replica's control connection; every is system.heartbeat_interval and
// staleAfter system.heartbeat_stale_after.
func NewFillCount(db q.DBTX, staleAfter, every time.Duration) (*FillCount, error) {
	if db == nil || staleAfter <= 0 || every <= 0 {
		return nil, errors.New("cluster: the fill count needs a connection, a stale bound and a refresh interval")
	}
	return &FillCount{db: db, staleAfter: staleAfter, every: every}, nil
}

// Name implements the composition root's Component.
func (c *FillCount) Name() string { return "fill-count" }

// Get returns the count, at least 1. It is the func() int of the proxy's Options.Replicas.
func (c *FillCount) Get() int { return max(1, int(c.n.Load())) }

// Refresh reads the count once and keeps the last value when the read fails.
func (c *FillCount) Refresh(ctx context.Context) error {
	n, err := q.New(c.db).CountFillingReplicas(ctx, c.staleAfter.Seconds())
	if err != nil {
		return fmt.Errorf("counting the replicas that fill: %w", err)
	}
	c.n.Store(n)
	return nil
}

// Start reads the count once and starts the refresh loop, which runs until Stop.
func (c *FillCount) Start(ctx context.Context) error {
	rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	_ = c.Refresh(rctx) // the first value is a convenience: n stays 1 until a read succeeds
	cancel()
	life := newLoopLife(ctx)
	c.mu.Lock()
	c.life = life
	c.mu.Unlock()
	life.run(func() {
		t := time.NewTicker(c.every)
		defer t.Stop()
		for {
			select {
			case <-life.ctx.Done():
				return
			case <-life.stopping:
				return
			case <-t.C:
			}
			if life.stopped() {
				return
			}
			rctx, cancel := context.WithTimeout(life.ctx, 2*time.Second)
			_ = c.Refresh(rctx)
			cancel()
		}
	})
	return nil
}

// Stop ends the refresh loop; a count in flight is waited for until ctx ends, not cut (Q-ARCH-503).
func (c *FillCount) Stop(ctx context.Context) error {
	c.mu.Lock()
	life := c.life
	c.mu.Unlock()
	if life != nil {
		life.stop(ctx)
	}
	return nil
}
