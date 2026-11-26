package guard

import (
	"context"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/settings"
	"github.com/guided-traffic/quader/internal/bastion/shared"
)

// The limiters of the anonymous ceiling (R-BAS-094): one key for all anonymous callers of the Bastion together. With Valkey
// they are the limiters of R-ARCH-034 under these names, shared by the replicas; without, each replica holds its share
// (the rate, the burst and the hour divided by bastion.replicas, Q-BAS-34).
const (
	limiterCeilingSecond = "bastion.ceiling_requests"
	limiterCeilingHour   = "bastion.ceiling_hour"
	ceilingKey           = "anonymous"
)

// ceilingHourRetry is the Retry-After of a request refused by the hour: the hour is a sliding window, so a minute lets its
// estimate fall by about a sixtieth of the figure; the format caps what its clients are told (R-BAS-091).
const ceilingHourRetry = time.Minute

// Ceiling is the anonymous ceiling of a Bastion (R-BAS-094, Q-BAS-445): every request a replica sends to the main
// installation on behalf of an anonymous caller (a forward, a conditional request, a check, the background refresh of an
// anonymous answer) takes one from it, in the slot budget (mainclient.Ceiling). The hour is read first and counted only once
// the second's bucket admitted the request, so a refused request takes nothing; requests in flight together may pass the hour
// by their number (H-2356). A failure of the shared state admits: the ceiling never refuses on its own fault.
type Ceiling struct {
	state   *shared.State
	figures func(ctx context.Context) *settings.Applied
}

// NewCeiling returns the ceiling over the shared state; figures returns the configuration of the request (settings.Holder.For).
func NewCeiling(state *shared.State, figures func(ctx context.Context) *settings.Applied) *Ceiling {
	return &Ceiling{state: state, figures: figures}
}

// Take implements mainclient.Ceiling.
func (c *Ceiling) Take(ctx context.Context) (bool, time.Duration) {
	f := c.figures(ctx).Ceiling
	if f.Unlimited() {
		return true, 0
	}
	l := c.state.Limiter
	if f.PerHour > 0 {
		if n, err := l.Count(ctx, limiterCeilingHour, ceilingKey, time.Hour); err == nil && int64(n) >= f.PerHour {
			return false, ceilingHourRetry
		}
	}
	if f.PerSecond > 0 {
		ok, wait, err := l.Allow(ctx, limiterCeilingSecond, ceilingKey, shared.Rate{PerSecond: float64(f.PerSecond), Burst: int(f.Burst)})
		if err == nil && !ok {
			return false, retryAfter(wait)
		}
	}
	if f.PerHour > 0 {
		_, _ = l.Add(ctx, limiterCeilingHour, ceilingKey, time.Hour)
	}
	return true, 0
}
