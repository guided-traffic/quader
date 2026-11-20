package cluster

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

// The Valkey side of the rate-limit counters of R-ARCH-034 (the limiter itself
// is in limiter.go). Keys are rl:v1:<limiter>:<bucket>:<window> with the bucket
// as a hash (R-ARCH-030) and the window as the integer index of the current
// window, so a window's counter expires on its own two windows after it opened.

// maxLimiterWindow is the longest window of a limiter. The figures of the download limits have windows up to a day
// (MaxLimitWindow) in a family of their own (lim, valkey_limits.go), whose keys live 48 hours.
const maxLimiterWindow = 12 * time.Hour

// windowKeys forms the keys of the current and the previous window.
func (v *Valkey) windowKeys(limiter, bucket string, idx int64, ttl time.Duration) (cur, prev Key, err error) {
	cur, err = v.formKeyTTL(rlFamily, ttl, limiter, bucket, strconv.FormatInt(idx, 10))
	if err != nil {
		return Key{}, Key{}, err
	}
	prev, err = v.formKeyTTL(rlFamily, ttl, limiter, bucket, strconv.FormatInt(idx-1, 10))
	return cur, prev, err
}

// windowHit counts one event whatever the limit (the counting form of Limiter.Estimate, which the Bastion's counters of a
// block use): one round trip, MULTI, INCR of the current window's key, EXPIRE <key> <ttl> NX, GET of the previous window's
// key, EXEC. It returns the counter of the current window including the event and the counter of the previous one. It runs
// no script: the Bastion's Valkey user may not (Q-BAS-127).
func (v *Valkey) windowHit(ctx context.Context, limiter, bucket string, idx int64, ttl time.Duration) (cur, prev int64, err error) {
	ck, pk, err := v.windowKeys(limiter, bucket, idx, ttl)
	if err != nil {
		return 0, 0, err
	}
	err = v.exec(ctx, "incr", func(ctx context.Context, c valkey.Client) error {
		res := c.DoMulti(ctx,
			c.B().Multi().Build(),
			c.B().Incr().Key(ck.name).Build(),
			c.B().Expire().Key(ck.name).Seconds(ceilSeconds(ck.ttl)).Nx().Build(),
			c.B().Get().Key(pk.name).Build(),
			c.B().Exec().Build(),
		)
		last := len(res) - 1
		for _, r := range res[:last] {
			if err := r.Error(); err != nil {
				return err
			}
		}
		out, err := res[last].ToArray()
		if err != nil {
			return err
		}
		if len(out) != 3 {
			return fmt.Errorf("EXEC returned %d replies, want 3", len(out))
		}
		if cur, err = out[0].AsInt64(); err != nil {
			return err
		}
		prev, err = optionalCounter(out[2])
		return err
	})
	return cur, prev, err
}

// windowTake is the check of a limiter that counts requests: one round trip that runs the take of valkey_take.go on the
// limiter's window (R-ARCH-034, Q-LIM-61). The n units are counted only when they fit; the counters returned are those
// without them.
func (v *Valkey) windowTake(ctx context.Context, w WindowTake, n int64) (WindowState, error) {
	var b LimitBatch
	b.Add(LimitOp{Kind: OpTake, Windows: []WindowTake{w}, N: max(1, n)})
	res, err := v.runLimits(ctx, &b)
	if err != nil {
		return WindowState{}, err
	}
	if len(res) != 1 || len(res[0].Windows) != 1 {
		return WindowState{}, fmt.Errorf("the take of a limiter returned %d results", len(res))
	}
	return res[0].Windows[0], nil
}

// windowRead is the check of the count-only form: a GET of the current and of
// the previous window's key, without INCR.
func (v *Valkey) windowRead(ctx context.Context, limiter, bucket string, idx int64, ttl time.Duration) (cur, prev int64, err error) {
	ck, pk, err := v.windowKeys(limiter, bucket, idx, ttl)
	if err != nil {
		return 0, 0, err
	}
	err = v.exec(ctx, "get", func(ctx context.Context, c valkey.Client) error {
		res := c.DoMulti(ctx, c.B().Get().Key(ck.name).Build(), c.B().Get().Key(pk.name).Build())
		for i, dst := range []*int64{&cur, &prev} {
			m, err := res[i].ToMessage()
			if valkey.IsValkeyNil(err) {
				continue // no counter yet: zero
			}
			if err != nil {
				return err
			}
			if *dst, err = optionalCounter(m); err != nil {
				return err
			}
		}
		return nil
	})
	return cur, prev, err
}

// windowCount is Count: MULTI, INCR of the current window's key, EXPIRE NX,
// EXEC.
func (v *Valkey) windowCount(ctx context.Context, limiter, bucket string, idx int64, ttl time.Duration) error {
	ck, _, err := v.windowKeys(limiter, bucket, idx, ttl)
	if err != nil {
		return err
	}
	_, err = v.incr(ctx, ck)
	return err
}

// optionalCounter reads a GET reply that is a counter or nil.
func optionalCounter(m valkey.ValkeyMessage) (int64, error) {
	if m.IsNil() {
		return 0, nil
	}
	return m.AsInt64()
}
