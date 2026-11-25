package queue

import (
	"time"

	"github.com/guided-traffic/quader/internal/remotecuration"
)

// The waits between the attempts of a row (R-CUR-011 "Retries"): 30 s after the first failed attempt, doubled after each further one,
// at most an hour, each ±20 %; the upstream's Retry-After instead when it named one.
const (
	backoffBase = 30 * time.Second
	backoffMax  = time.Hour
	// backoffSpread is the ±20 % of a wait.
	backoffSpread = 0.2
)

// backoff is the wait before the next attempt after attempt (1 for the first) failed. retryAfter is the upstream's Retry-After (0 for
// none); jitter is a number in [0, 1) that spreads the wait.
func backoff(attempt int, retryAfter time.Duration, jitter float64) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	d := backoffBase
	for i := 1; i < attempt && d < backoffMax; i++ {
		d *= 2
	}
	d = min(d, backoffMax)
	factor := 1 - backoffSpread + 2*backoffSpread*min(max(jitter, 0), 1)
	return time.Duration(float64(d) * factor)
}

// settlement is the state a row takes at the end of an attempt.
type settlement struct {
	State     remotecuration.State
	Outcome   remotecuration.Outcome
	Failure   *remotecuration.Failure
	NotBefore time.Time
	// Limited reports an attempt that ended at an upstream's rate limit: the row waits again, the attempt is not counted, and the
	// repository's limit sets NotBefore (Q-CUR-170).
	Limited bool
}

// settle decides the end of an attempt of row (whose Attempts counts it) from what the run reported (R-CUR-011): a cancelled run ends
// cancelled; an upstream's rate limit waits again without counting the attempt, for the time of the repository's limit (Q-CUR-170);
// a failure whose class a later attempt can overcome waits for the next attempt until the row's attempts are spent; any other failure
// ends the row failed; a run without a failure ends done with its outcome, keeping the last refusal of a file as the row's failure.
func settle(row Row, res Result, now time.Time, jitter float64) settlement {
	switch {
	case res.Cancelled:
		return settlement{State: remotecuration.StateCancelled, Failure: res.Failure}
	case res.Failed:
		f := res.Failure
		if f == nil {
			f = &remotecuration.Failure{Class: remotecuration.ClassInternal, Step: remotecuration.StepPlan, Code: "internal"}
		}
		if isLimit(f) {
			return settlement{State: remotecuration.StateWaiting, Failure: f, Limited: true}
		}
		if f.Class.Retryable() && row.Attempts < row.MaxAttempts {
			wait := backoff(row.Attempts, time.Duration(f.RetryAfter)*time.Second, jitter)
			return settlement{State: remotecuration.StateWaiting, Failure: f, NotBefore: now.Add(wait)}
		}
		return settlement{State: remotecuration.StateFailed, Failure: f}
	}
	out := res.Outcome
	if out == "" {
		out = remotecuration.OutcomeCompleted
	}
	return settlement{State: remotecuration.StateDone, Outcome: out, Failure: res.Failure}
}

// freePlaces is how many waiting rows a repository may start now: the bound of running rows less those that run.
func freePlaces(per, running int) int {
	return max(max(per, 1)-running, 0)
}
