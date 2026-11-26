package history

import (
	"context"
	"time"

	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// Task is the name of the task and kind that keep the history bounded (R-STOR-110, operations-and-observability.md R-OPS-019).
const Task = "storage-history"

// DefaultSchedule runs it every hour, away from the other storage tasks' minutes.
const DefaultSchedule = "37 * * * *"

// PurgePage is how many rows one statement of the task deletes or resolves at most (Q-STOR-521).
const PurgePage = 10000

// DeadReplicaAfter is how long a replica's heartbeat may be old before the task ends the intervals it left open.
const DeadReplicaAfter = 10 * time.Minute

// JobRun is the part of the job framework's run handle a run uses. *jobs.Run implements it.
type JobRun interface {
	SetResult(v any) error
}

// TaskOptions configure a run.
type TaskOptions struct {
	// Retention is storage.history.retention.
	Retention time.Duration
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Result is what a run stores with its job: what it deleted, ended and resolved.
type Result struct {
	FindingsDeleted   int64 `json:"findingsDeleted"`
	EventsDeleted     int64 `json:"eventsDeleted"`
	CountsDeleted     int64 `json:"countsDeleted"`
	EventsEnded       int64 `json:"eventsEnded"`
	FindingsResolved  int64 `json:"findingsResolved"`
	RetentionSeconds  int64 `json:"retentionSeconds"`
	CutoffUnixSeconds int64 `json:"cutoff"`
}

// RunTask is the body of the handler of the kind storage-history (Q-STOR-518, Q-STOR-521): it deletes, in pages of PurgePage, the
// resolved findings, the ended intervals and the counts older than the retention (an open finding stays whatever its age); it ends the
// open intervals of a replica the registry has not seen for DeadReplicaAfter, at the time it last saw them; and it resolves as
// reconciled an open finding whose copy is no longer in the state it records (a restored database, a path the triggers did not see).
// A run that a drain or a cancellation ends has done part of it; the next does the rest.
func RunTask(ctx context.Context, db *postgres.DB, o TaskOptions, run JobRun) error {
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	cutoff := now().Add(-o.Retention)
	res := Result{RetentionSeconds: int64(o.Retention / time.Second), CutoffUnixSeconds: cutoff.Unix()}
	c := unix(cutoff)
	steps := []struct {
		into *int64
		page func(qs *q.Queries) (int64, error)
	}{
		{&res.FindingsDeleted, func(qs *q.Queries) (int64, error) {
			return qs.HistoryPurgeFindings(ctx, q.HistoryPurgeFindingsParams{CutoffUnix: c, PageSize: PurgePage})
		}},
		{&res.EventsDeleted, func(qs *q.Queries) (int64, error) {
			return qs.HistoryPurgeEvents(ctx, q.HistoryPurgeEventsParams{CutoffUnix: c, PageSize: PurgePage})
		}},
		{&res.CountsDeleted, func(qs *q.Queries) (int64, error) {
			return qs.HistoryPurgeCounts(ctx, q.HistoryPurgeCountsParams{CutoffUnix: c, PageSize: PurgePage})
		}},
		{&res.FindingsResolved, func(qs *q.Queries) (int64, error) { return qs.HistoryReconcileFindings(ctx, PurgePage) }},
	}
	p := PG{DB: db}
	for _, s := range steps {
		for {
			var n int64
			if err := p.exec(ctx, func(qs *q.Queries) (err error) { n, err = s.page(qs); return err }); err != nil {
				return err
			}
			*s.into += n
			if n < PurgePage {
				break
			}
		}
	}
	if err := p.exec(ctx, func(qs *q.Queries) (err error) {
		res.EventsEnded, err = qs.HistoryCloseDeadReplicas(ctx, DeadReplicaAfter.Seconds())
		return err
	}); err != nil {
		return err
	}
	return run.SetResult(res)
}
