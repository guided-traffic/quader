package queue

import (
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/remotecuration"
)

// An upstream's rate limit holds a repository's queue (remote-curation.md R-CUR-011 "An upstream's rate limit", Q-CUR-170 to
// Q-CUR-173): a run that meets a limit longer than it waits out hands its row back (waiting, the attempt not counted), and the
// repository's row of remote_download_limits says until when no row of the repository starts, since when the upstream limits and
// what it answered. After that time one row asks first; the first attempt that the upstream answers without a limit ends the hold.

// RunLimitWait is the longest rate-limit wait a run of the queue waits out inside itself; a longer one ends its attempt and holds the
// repository's queue instead (Q-CUR-171).
const RunLimitWait = 10 * time.Second

// maxLimitWait caps the time an upstream names: a wrong header does not park a queue for days (R-REPO-054's six hours).
const maxLimitWait = 6 * time.Hour

// Limit is an upstream's rate limit that holds a repository's queue (remote_download_limits).
type Limit struct {
	RepoID int64 `json:"repo_id"`
	// Since is the time of the answer that began the first of the waits in a row.
	Since time.Time `json:"since"`
	// Until is the time before which no row of the repository starts: the upstream's own where it named one, else the queue's back-off.
	Until time.Time `json:"until"`
	// Waits are the waits in a row: the back-off's count, which an attempt the upstream answers without a limit ends.
	Waits int `json:"waits"`
	// Hinted reports that the upstream named Until (Retry-After or the reset of its quota).
	Hinted bool `json:"hinted"`
	// Failure is the last rate-limit answer in a row's words: the status, the address, the excerpt and the headers of the limit.
	Failure remotecuration.Failure `json:"failure"`
}

// Holds reports whether the limit keeps every row of its repository from starting at now.
func (l Limit) Holds(now time.Time) bool { return l.Until.After(now) }

// isLimit reports whether a failure is an upstream's rate-limit answer: the class upstream_rate_limited with the engine's code, not
// a client's pre-emption, which shares the class.
func isLimit(f *remotecuration.Failure) bool {
	return f != nil && f.Class == remotecuration.ClassUpstreamRateLimited && f.Code == errs.CodeUpstreamRateLimited
}

// nextLimit is the limit after an answer f at now (Q-CUR-172). An answer that comes while the hold is in force is one of the rows that
// were running when it began: it keeps the count and the later of the two times. An answer after the hold ended is the next wait in a
// row. The time is the upstream's where it named one (at most six hours), else the queue's back-off over the waits in a row.
func nextLimit(prev Limit, found bool, repoID int64, f remotecuration.Failure, now time.Time, jitter float64) Limit {
	l := Limit{RepoID: repoID, Since: now, Waits: 1, Failure: f}
	if found {
		l.Since, l.Waits = prev.Since, max(prev.Waits, 1)
		if !prev.Holds(now) {
			l.Waits++
		}
	}
	wait := backoff(l.Waits, 0, jitter)
	if f.RetryAfter > 0 {
		wait, l.Hinted = min(time.Duration(f.RetryAfter)*time.Second, maxLimitWait), true
	}
	l.Until = now.Add(max(wait, time.Second))
	if found && prev.Holds(now) && prev.Until.After(l.Until) {
		l.Until, l.Hinted = prev.Until, prev.Hinted
	}
	return l
}

// endsLimit reports whether the end of an attempt of row shows that the upstream answers without a limit again: the attempt began
// after the limit's last answer, and it ended done (not skipped, which asked nothing) or failed with an answer of the upstream that is
// no limit. An attempt that was running when the limit began proves nothing: its requests may have been sent before.
func endsLimit(row Row, set settlement, l Limit) bool {
	if row.StartedAt == nil || !row.StartedAt.After(l.Failure.At) {
		return false
	}
	switch set.State {
	case remotecuration.StateDone:
		return set.Outcome != remotecuration.OutcomeSkipped
	case remotecuration.StateCancelled:
		return false
	}
	if set.Failure == nil {
		return false
	}
	switch set.Failure.Class {
	case remotecuration.ClassUpstreamNotFound, remotecuration.ClassUpstreamAuth, remotecuration.ClassUpstreamError,
		remotecuration.ClassUpstreamUnexpected, remotecuration.ClassIntegrity, remotecuration.ClassContentRefused:
		return true
	}
	return false
}

// limitPlaces is how many rows a repository may start now with a limit l: none while it holds, one at a time after it, until an
// attempt ends it (the first row asks for the others).
func limitPlaces(per, running int, l Limit, found bool, now time.Time) int {
	switch {
	case !found:
		return freePlaces(per, running)
	case l.Holds(now):
		return 0
	}
	return freePlaces(1, running)
}
