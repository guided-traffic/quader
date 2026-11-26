// Package drainp99 is the tail-latency rule of the drain per role (R-HA-059, Q-HA-497): the p99 of k6's requests from the first
// eviction until 2 minutes after the last replacement was ready is held to twice the larger of two baselines, the warm-up's before
// the first eviction and the post-drain window after the drain window, so that the load a neighbour on the shared runner puts on
// the drain and on what follows it does not read as the drain's. It has no build tag so that the rule is unit-tested without a
// cluster; TestDrainRoles in test/e2e applies it.
package drainp99

import (
	"sort"
	"time"
)

// Sample is one request of k6: when it started, its kind (document or tarball) and how long it took.
type Sample struct {
	At   time.Time
	Kind string
	Took time.Duration
}

// Window is the p99 of the requests of one kind (every kind for "") that started in [From, To), and how many there were.
type Window struct {
	From, To time.Time
	P99      time.Duration
	N        int
}

// Of reads the window [from, to) of the samples.
func Of(samples []Sample, kind string, from, to time.Time) Window {
	var d []time.Duration
	for _, s := range samples {
		if (kind == "" || s.Kind == kind) && !s.At.Before(from) && s.At.Before(to) {
			d = append(d, s.Took)
		}
	}
	return Window{From: from, To: to, P99: percentile(d, 0.99), N: len(d)}
}

// Verdict is the comparison of one kind: the drain window against the warm-up and the post-drain window.
type Verdict struct {
	Warm, Drain, Post Window
}

// Judge reads the three windows: the warm-up [warmFrom, firstEviction), the drain [firstEviction, drainTo) and the post-drain
// window [drainTo, postTo).
func Judge(samples []Sample, kind string, warmFrom, firstEviction, drainTo, postTo time.Time) Verdict {
	return Verdict{
		Warm:  Of(samples, kind, warmFrom, firstEviction),
		Drain: Of(samples, kind, firstEviction, drainTo),
		Post:  Of(samples, kind, drainTo, postTo),
	}
}

// Baseline is the larger p99 of the warm-up and the post-drain window.
func (v Verdict) Baseline() time.Duration { return max(v.Warm.P99, v.Post.P99) }

// Bound is what the drain window's p99 may reach: twice the baseline.
func (v Verdict) Bound() time.Duration { return 2 * v.Baseline() }

// Within reports whether the drain window's p99 is at most the bound.
func (v Verdict) Within() bool { return v.Drain.P99 <= v.Bound() }

// Enough reports whether each of the three windows holds at least n requests, so that its p99 says something.
func (v Verdict) Enough(n int) bool { return v.Warm.N >= n && v.Drain.N >= n && v.Post.N >= n }

// percentile is the e2e suite's: the sample at index (n-1)·p of the sorted samples, 0 for none.
func percentile(d []time.Duration, p float64) time.Duration {
	if len(d) == 0 {
		return 0
	}
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return d[int(float64(len(d)-1)*p)]
}
