package cluster

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

// The counters of a Bastion in windows of time: the byte counters of its throughput (R-BAS-097, Q-BAS-332), the bytes the
// replicas took from a slice of a bucket and the bytes a class read in an hour, and the counters of the budgets of its place
// (R-BAS-096): a subject's bytes of an hour or a day, its paced bytes of a slice, its transfers in progress and its block. A key
// is throughput:v1:<name>:<index> or budget:v1:<name>:<index>, the index counting slices, hours or days from the Unix epoch, so
// two replicas name the same window without asking each other. Only the commands of the Bastion's ACL user are used (INCRBY,
// EXPIRE, GET, MULTI, EXEC); INCRBY with a negative amount gives bytes back, which the user may run as it may INCRBY.

// CounterFamily selects the key family of a counter.
type CounterFamily int

// The families of the Bastion's counters.
const (
	// CountersThroughput are the throughput's (R-BAS-097), kept at most two hours.
	CountersThroughput CounterFamily = iota
	// CountersBudget are the budgets' of the Bastion's place (R-BAS-096), kept at most two days and two hours: a counter of a
	// day lives until the next day's has ended.
	CountersBudget
)

func (f CounterFamily) family() *KeyFamily {
	if f == CountersBudget {
		return budgetFamily
	}
	return throughputFamily
}

func (s *BastionStore) counterKey(fam CounterFamily, name string, idx int64, ttl time.Duration) (Key, error) {
	if ttl <= 0 {
		return Key{}, errors.New("cluster: a counter needs a positive expiry")
	}
	if idx < 0 {
		return Key{}, fmt.Errorf("cluster: the window index %d is negative", idx)
	}
	f := fam.family()
	return s.v.formKeyTTL(f, min(ttl, f.maxTTL), name, strconv.FormatInt(idx, 10))
}

// incrBy adds n to the key in one round trip (MULTI, INCRBY, EXPIRE NX, GET of also when it is set, EXEC) and returns the new
// value and the value of also (0 for none).
func (s *BastionStore) incrBy(ctx context.Context, k Key, n int64, also *Key) (cur, other int64, err error) {
	err = s.v.exec(ctx, "incrby", func(ctx context.Context, c valkey.Client) error {
		cmds := valkey.Commands{
			c.B().Multi().Build(),
			c.B().Incrby().Key(k.name).Increment(n).Build(),
			c.B().Expire().Key(k.name).Seconds(ceilSeconds(k.ttl)).Nx().Build(),
		}
		if also != nil {
			cmds = append(cmds, c.B().Get().Key(also.name).Build())
		}
		cmds = append(cmds, c.B().Exec().Build())
		res := c.DoMulti(ctx, cmds...)
		for _, r := range res[:len(res)-1] {
			if err := r.Error(); err != nil {
				return err
			}
		}
		out, err := res[len(res)-1].ToArray()
		if err != nil {
			return err
		}
		if want := len(cmds) - 2; len(out) != want {
			return fmt.Errorf("EXEC returned %d replies, want %d", len(out), want)
		}
		if cur, err = out[0].AsInt64(); err != nil {
			return err
		}
		if also != nil {
			other, err = optionalCounter(out[2])
		}
		return err
	})
	return cur, other, err
}

// ClaimBytes is [BastionStore.Claim] of the throughput's counters.
func (s *BastionStore) ClaimBytes(ctx context.Context, name string, idx int64, ttl time.Duration, want, limit int64) (int64, error) {
	return s.Claim(ctx, CountersThroughput, name, idx, ttl, want, limit)
}

// ReleaseBytes is [BastionStore.Release] of the throughput's counters.
func (s *BastionStore) ReleaseBytes(ctx context.Context, name string, idx int64, ttl time.Duration, n int64) error {
	return s.Release(ctx, CountersThroughput, name, idx, ttl, n)
}

// AddBytes is [BastionStore.Add] of the throughput's counters.
func (s *BastionStore) AddBytes(ctx context.Context, name string, idx int64, ttl time.Duration, n int64) (cur, prev int64, err error) {
	return s.Add(ctx, CountersThroughput, name, idx, ttl, n)
}

// Claim adds up to want to the counter (name, idx) of the family while it stays within limit and returns what it added: want,
// a part of it, or 0 when the counter was at the limit. The counter is raised by want first and the part above the limit is
// given back at once, so concurrent claims never grant more than the limit together; a claim that meets another's excess
// before it was given back grants less than it could have, never more. ttl is the counter's expiry, set when the counter is
// made and not extended.
func (s *BastionStore) Claim(ctx context.Context, fam CounterFamily, name string, idx int64, ttl time.Duration, want, limit int64) (int64, error) {
	if s.v == nil {
		return 0, errNoBastionClient
	}
	if want <= 0 {
		return 0, nil
	}
	k, err := s.counterKey(fam, name, idx, ttl)
	if err != nil {
		return 0, err
	}
	v, _, err := s.incrBy(ctx, k, want, nil)
	if err != nil {
		return 0, s.fail(ctx, err)
	}
	granted := min(want, max(0, limit-(v-want)))
	if excess := want - granted; excess > 0 {
		if _, _, err := s.incrBy(ctx, k, -excess, nil); err != nil {
			// The excess stays counted until the window ends: the counter admits less, never more.
			_ = s.fail(ctx, err)
		}
	}
	return granted, nil
}

// Release gives n of the counter (name, idx) of the family back: bytes a reader claimed and did not read, a transfer that ended.
func (s *BastionStore) Release(ctx context.Context, fam CounterFamily, name string, idx int64, ttl time.Duration, n int64) error {
	if s.v == nil {
		return errNoBastionClient
	}
	if n <= 0 {
		return nil
	}
	k, err := s.counterKey(fam, name, idx, ttl)
	if err != nil {
		return err
	}
	_, _, err = s.incrBy(ctx, k, -n, nil)
	return s.fail(ctx, err)
}

// Add adds n to the counter (name, idx) of the family, where n = 0 only reads it, and returns the counter and the one of idx-1.
func (s *BastionStore) Add(ctx context.Context, fam CounterFamily, name string, idx int64, ttl time.Duration, n int64) (cur, prev int64, err error) {
	if s.v == nil {
		return 0, 0, errNoBastionClient
	}
	k, err := s.counterKey(fam, name, idx, ttl)
	if err != nil {
		return 0, 0, err
	}
	pk, err := s.counterKey(fam, name, max(idx-1, 0), ttl)
	if err != nil {
		return 0, 0, err
	}
	cur, prev, err = s.incrBy(ctx, k, n, &pk)
	if idx == 0 {
		prev = 0
	}
	return cur, prev, s.fail(ctx, err)
}
