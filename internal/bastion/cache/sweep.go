package cache

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/metrics"
)

// The thresholds of R-BAS-031: a sweep acts above 95 % of a limit and deletes until 85 % remain.
const (
	sweepHighPercent   = 95
	sweepTargetPercent = 85
	// sweepProtectPercent is the most of an area's limit that is protected (R-BAS-098): below the target, so that a sweep
	// reaches its target by evicting what was served once and what lost its protection, never a protected object.
	sweepProtectPercent = 80
)

// sweepJitter is the spread of the randomized interval of a bucket without Valkey: sweep_interval +- 20 %.
const sweepJitter = 0.2

// sweepGap is the least time from the end of one sweep to the start of a sweep of the size (Q-BAS-303): it bounds the
// listings of a backend that fails, or of an area that fills faster than it is swept, to one a second.
const sweepGap = time.Second

// SweepLease is the lease of the one replica that sweeps a shared bucket (R-BAS-031): `sweep` in Valkey, for
// 2 × sweep_interval.
type SweepLease interface {
	TryAcquire(ctx context.Context, ttl time.Duration) (held bool, release func(), err error)
}

// LeaseSource is what the sweep asks before each cycle: the sweep lease while the replicas share state, nil
// otherwise. shared.State is the source in a replica; the interface keeps the cache below the shared state, which
// signs its answer records with this package's Signer.
type LeaseSource interface {
	SweepLease() SweepLease
}

// sweepLease returns the lease of src, nil for a nil source or one that shares no state.
func sweepLease(src LeaseSource) SweepLease {
	if src == nil {
		return nil
	}
	return src.SweepLease()
}

func percent(limit, p int64) int64 { return limit * p / 100 }

// SweepOnce is one cycle of R-BAS-031 for this replica, started by the timer: see [Cache.sweep].
func (c *Cache) SweepOnce(ctx context.Context, src LeaseSource) error {
	return c.sweep(ctx, src, metrics.SweepInterval)
}

// sweep is one cycle of R-BAS-031 for this replica: in the first hour after the start, and while it still finds some, it
// deletes the objects of other layouts (R-BAS-061); it measures what the backend holds, sets the running estimate of the areas
// from the listing (R-BAS-089), publishes quader_bastion_cache_bytes, and evicts in each area above 95 % of its limit down to
// 85 %, what was served once before what was served twice, with at most 80 % of the limit protected (R-BAS-098). A sweep of
// the timer lists the markers of an area below its mark too and publishes quader_bastion_cache_protected_bytes (Q-BAS-463).
// With the bucket backend and Valkey only the replica that holds the sweep lease runs it; another replica that committed since
// its last listing lists the areas to set its estimate and deletes nothing, and shows no protected bytes. Without Valkey every
// replica sweeps, which is safe because a sweep only deletes. With the volume backend each replica sweeps its own volume. src
// may be nil, which is a replica without shared state. trigger says what started it (quader_bastion_cache_sweeps_total).
func (c *Cache) sweep(ctx context.Context, src LeaseSource, trigger string) error {
	if lease := sweepLease(src); c.cfg.Backend == "bucket" && lease != nil {
		held, release, err := lease.TryAcquire(ctx, 2*c.cfg.SweepInterval)
		if err != nil {
			c.log.Warn("the sweep lease could not be taken; sweeping without it, which is safe", "error", err)
		} else {
			if !held {
				c.m.CacheProtected.WithLabelValues(metrics.AreaArtifacts).Set(0)
				c.m.CacheProtected.WithLabelValues(metrics.AreaDocuments).Set(0)
				return c.relist(ctx)
			}
			defer release()
		}
	}
	c.m.CacheSweeps.WithLabelValues(trigger).Inc()
	var errs []error
	if err := c.sweepOtherLayouts(ctx); err != nil {
		errs = append(errs, err)
	}
	u, err := c.measure(ctx)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	arts, docs := c.limits()
	for _, a := range []struct {
		area  string
		limit int64
		used  int64
	}{{metrics.AreaArtifacts, arts, u.Artifacts}, {metrics.AreaDocuments, docs, u.Documents}} {
		above := a.used > percent(a.limit, sweepHighPercent)
		if !above && trigger != metrics.SweepInterval {
			continue
		}
		res, err := c.back.Sweep(ctx, a.area, SweepBounds{
			High: percent(a.limit, sweepHighPercent), Target: percent(a.limit, sweepTargetPercent),
			Protect: percent(a.limit, sweepProtectPercent),
		})
		c.m.CacheEvictions.WithLabelValues(metrics.EvictSize).Add(float64(res.Evicted))
		if err != nil {
			errs = append(errs, err)
		} else {
			c.m.CacheProtected.WithLabelValues(a.area).Set(float64(res.Protected))
		}
		if res.Evicted > 0 {
			c.log.Info("cache sweep", "area", a.area, "evicted", res.Evicted, "protected_bytes", res.Protected, "limit", a.limit, "trigger", trigger)
		}
	}
	_, _ = c.measure(ctx) // a failure is counted by the backend; the estimate keeps the first listing and the commits since
	return errors.Join(errs...)
}

// measure lists what the backend holds, sets the running estimate from it and publishes it.
func (c *Cache) measure(ctx context.Context) (Usage, error) {
	mark := c.est.mark()
	u, err := c.back.Usage(ctx)
	if err != nil {
		return Usage{}, err
	}
	c.est.rebase(u, mark)
	c.publish(u)
	return u, nil
}

// relist is the cycle of a replica that does not hold the sweep lease of a shared bucket: it sets its estimate from a listing
// when it committed since its last one, so that it learns what the other replicas wrote and evicted, and deletes nothing.
func (c *Cache) relist(ctx context.Context) error {
	if !c.est.dirty() {
		return nil
	}
	_, err := c.measure(ctx)
	return err
}

func (c *Cache) publish(u Usage) {
	c.m.CacheBytes.WithLabelValues(metrics.AreaArtifacts).Set(float64(u.Artifacts))
	c.m.CacheBytes.WithLabelValues(metrics.AreaDocuments).Set(float64(u.Documents))
}

// interval is the time to the next sweep: sweep_interval, randomized around it for a bucket (R-BAS-031).
func (c *Cache) interval() time.Duration {
	d := c.cfg.SweepInterval
	if c.cfg.Backend != "bucket" {
		return d
	}
	return time.Duration(float64(d) * (1 - sweepJitter + 2*sweepJitter*rand.Float64())) //nolint:gosec // jitter, not security
}

// RunSweeper lists the areas at once, which sets the running estimate (R-BAS-089) and starts a sweep when an area is above
// its high mark, and then sweeps every sweep_interval and whenever a fill asks for one ([Cache.wantSweep]) until ctx ends. The
// sweeps run one at a time; a request while one runs is served by one more sweep after it, at least [sweepGap] after it
// ended. A failed cycle is logged and counted by the backend; the next one runs on schedule.
func (c *Cache) RunSweeper(ctx context.Context, src LeaseSource) {
	c.listMu.Lock()
	u, err := c.measure(ctx)
	c.listMu.Unlock()
	if err == nil {
		arts, docs := c.limits()
		if u.Artifacts > percent(arts, sweepHighPercent) || u.Documents > percent(docs, sweepHighPercent) {
			c.wantSweep()
		}
	}
	t := time.NewTimer(c.interval())
	defer t.Stop()
	var ended time.Time
	for {
		trigger := metrics.SweepInterval
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-c.sweepNow:
			trigger = metrics.SweepSize
			if !c.waitGap(ctx, ended) {
				return
			}
		}
		if err := c.sweep(ctx, src, trigger); err != nil && ctx.Err() == nil {
			c.log.Warn("cache sweep failed", "error", err, "trigger", trigger)
		}
		ended = time.Now()
		t.Reset(c.interval())
	}
}

// waitGap waits until [sweepGap] has passed since ended; false when ctx ends first.
func (c *Cache) waitGap(ctx context.Context, ended time.Time) bool {
	wait := c.gap - time.Since(ended)
	if ended.IsZero() || wait <= 0 {
		return true
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
