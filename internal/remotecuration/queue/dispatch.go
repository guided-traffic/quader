package queue

import (
	"context"
	"errors"

	"github.com/guided-traffic/quader/internal/remotecuration"
)

// DispatchResult is what one run of the task remote-downloads-dispatch did, the result its job records.
type DispatchResult struct {
	// Orphans are the running rows whose job had ended and that wait again.
	Orphans int `json:"orphans"`
	// Started are the rows started in the repositories with a free place.
	Started int `json:"started"`
	// Expanded are the bulks whose expansion went one step on.
	Expanded int `json:"expanded"`
	// Settled are the bulks that ended because their rows all ended.
	Settled int `json:"settled"`
	// Syncs are the due syncs the dispatch looked at, and SyncRuns the runs it started (R-CUR-027).
	Syncs    int `json:"syncs"`
	SyncRuns int `json:"syncRuns"`
	// Pruned are the finished rows removed after their retention.
	Pruned int64 `json:"pruned"`
}

// maxPruneBatches bounds the retention of one dispatch: at most 50 batches of 1,000 rows (R-CUR-011).
const maxPruneBatches = 50

// Dispatch is one run of the task remote-downloads-dispatch (R-CUR-011, R-CUR-021, R-CUR-027): it finds the running rows whose job
// ended and lets them wait again without counting the attempt, starts the runs of the syncs whose time came, starts the waiting rows whose next attempt came in every repository with a free
// place and no upstream's rate limit in force, goes one step on with every bulk that expands where no limit holds, ends the bulks
// whose rows all ended, removes the limits of the repositories whose queue is empty and removes finished rows after their
// retention. describe turns a planner's error into a failure in words (internal/proxy). A failure of one repository's start or one
// bulk's step is logged and the run goes on.
func (s *Service) Dispatch(ctx context.Context, describe func(error) remotecuration.Failure) (DispatchResult, error) {
	var res DispatchResult
	m := s.o.Maintenance
	if m == nil {
		return res, errors.New("queue: this process has no maintenance store")
	}
	now := s.o.Now()
	orphaned, err := m.Orphans(ctx, now)
	if err != nil {
		return res, err
	}
	res.Orphans = len(orphaned)
	due, err := m.DueRepositories(ctx, now)
	if err != nil {
		return res, err
	}
	for _, id := range due {
		n, err := s.Start(ctx, id)
		if err != nil {
			s.o.Log.Warn("the dispatch could not start the waiting downloads of a repository", "repo_id", id, "error", err)
			continue
		}
		res.Started += n
	}
	// A sync's run starts before the expansions, so that its first step goes in the same dispatch (R-CUR-027).
	syncs, err := s.startSyncs(ctx, now)
	if err != nil {
		return res, err
	}
	res.Syncs, res.SyncRuns = syncs.Looked, syncs.Started
	bulks, err := m.ExpandingBulks(ctx)
	if err != nil {
		return res, err
	}
	for _, ref := range bulks {
		if l, found, err := s.o.Store.Limit(ctx, ref.RepoID); err != nil || (found && l.Holds(now)) {
			continue // the upstream limits requests: the expansion asks it nothing until the limit's time (Q-CUR-170)
		}
		b, _, found, err := s.o.Store.GetBulk(ctx, ref.ID, ref.RepoID)
		if err != nil || !found || b.State != BulkExpanding {
			continue
		}
		if err := s.expand(ctx, b, describe); err != nil {
			s.o.Log.Warn("a step of a bulk download failed", "bulk", b.ID, "error", err)
			continue
		}
		res.Expanded++
	}
	if res.Settled, err = m.SettleBulks(ctx); err != nil {
		return res, err
	}
	if _, err := m.PruneLimits(ctx, now); err != nil {
		return res, err
	}
	st := s.o.Settings()
	for range maxPruneBatches {
		n, err := m.Prune(ctx, now.Add(-st.DoneRetention), now.Add(-st.FailedRetention))
		if err != nil {
			return res, err
		}
		res.Pruned += n
		if n < 1000 {
			break
		}
	}
	if s.o.Metrics != nil {
		if rows, err := m.Gauge(ctx); err == nil {
			s.o.Metrics.gauge(rows)
		}
		if rows, err := m.SyncGauge(ctx); err == nil {
			s.o.Metrics.syncGauge(rows)
		}
	}
	return res, nil
}
