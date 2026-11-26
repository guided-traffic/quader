package cache

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/metrics"
)

// The sweep of other layouts (R-BAS-061, Q-BAS-215). A cache with a key reads nothing but its own tree, so the plain layout
// of a cache that ran without a key and the trees of other identifiers are objects it can never serve; a cache without a key
// cannot read any tree. They appear when a rollout changes the key, sets one or removes it, while replicas of both
// configurations run, and the sweeps of a replica's first hour delete them, in batches, as long as a sweep still finds some.
const (
	// foreignWindow is how long after its start a replica looks for other layouts in every sweep.
	foreignWindow = time.Hour
	// foreignBatch bounds the objects one sweep deletes from other layouts, so that the eviction of its own layout is not held
	// back; a sweep that reaches it makes the next one look again.
	foreignBatch = 10_000
)

// plainFamilies are the top-level names of R-BAS-030's layout.
func plainFamilies() []string {
	return []string{dirArtifacts + "/", dirDocuments + "/", dirEntries + "/", dirAccess + "/", dirSpool + "/", dirProbe + "/", "catalogue/"}
}

var errBatchFull = errors.New("cache: the batch of the sweep of other layouts is full")

// sweepForeign deletes up to limit objects that lie in another layout than the backend's: with a key the plain layout and the
// trees of other identifiers, without one the trees of every identifier. It returns how many it deleted. The listing of
// the trees passes over the backend's own tree, which it leaves alone.
func (b *backend) sweepForeign(ctx context.Context, limit int) (int, error) {
	own := ""
	prefixes := []string{treePrefix}
	if b.seal != nil {
		own = b.seal.id + "/"
		prefixes = append(plainFamilies(), treePrefix)
	}
	deleted := 0
	for _, p := range prefixes {
		var removeErr error
		err := b.s.list(ctx, p, func(key string, _ int64, _ time.Time) error {
			if p == treePrefix && (!treeName.MatchString(first(key)) || (own != "" && strings.HasPrefix(key, own))) {
				return nil
			}
			if deleted >= limit {
				return errBatchFull
			}
			if err := b.s.remove(ctx, key); err != nil {
				removeErr = err
				return err
			}
			deleted++
			return nil
		})
		switch {
		case errors.Is(err, errBatchFull):
			return deleted, nil
		case removeErr != nil:
			return deleted, b.fail(metrics.OpDelete, removeErr)
		case err != nil:
			return deleted, b.fail(metrics.OpList, err)
		}
	}
	return deleted, nil
}

// sweepOtherLayouts is the part of [Cache.SweepOnce] that deletes other layouts while it is due.
func (c *Cache) sweepOtherLayouts(ctx context.Context) error {
	if c.now().Sub(c.started) >= foreignWindow && !c.foreignFound.Load() {
		return nil
	}
	n, err := c.back.sweepForeign(ctx, foreignBatch)
	c.foreignFound.Store(n > 0)
	if n > 0 {
		c.log.Info("cache sweep deleted objects of another layout: another cache key, or a cache that ran with or without one", "objects", n)
	}
	return err
}
