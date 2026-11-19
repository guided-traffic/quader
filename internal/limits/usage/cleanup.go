package usage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// TaskCleanup is the daily task of the download limits (R-LIM-028, R-OPS-019).
const TaskCleanup = "limits-cleanup"

// The retention of R-LIM-027 and R-LIM-028.
const (
	FiveMinuteRetention = 25 * time.Hour
	HourlyRetention     = 30 * 24 * time.Hour
	BlockRetention      = 30 * 24 * time.Hour
)

// CleanupResult counts what one run did.
type CleanupResult struct {
	Folded, FiveMinute, Hourly, Events, Summaries, Blocks, Erased, Rules int64
}

// Cleanup runs limits-cleanup once (R-LIM-028): the hourly rows older than 30 days are folded into the daily sums without a
// person and deleted; five-minute rows older than 25 hours, daily sums older than the summary retention and blocks older
// than 30 days are deleted; the usage rows, blocks and rules of users and groups that no longer exist are deleted and the
// rules audited (Q-LIM-25). Each step is a transaction of its own.
func Cleanup(ctx context.Context, run meta.Runner, rec audit.Recorder, now time.Time, summaryRetention time.Duration) (CleanupResult, error) {
	if rec == nil {
		rec = audit.Nop{}
	}
	var res CleanupResult
	step := func(fn func(qs *q.Queries, tx meta.Tx) error) error {
		return run.InTx(ctx, func(tx meta.Tx) error {
			ptx, ok := postgres.Unwrap(tx)
			if !ok {
				return nil
			}
			return fn(q.New(ptx), tx)
		})
	}
	hourCut := ts(now.Add(-HourlyRetention))
	if err := step(func(qs *q.Queries, _ meta.Tx) error {
		n, err := qs.FoldUsageDays(ctx, hourCut)
		if err != nil {
			return err
		}
		res.Folded = n
		if res.Hourly, err = qs.DeleteUsageBuckets(ctx, q.DeleteUsageBucketsParams{Granularity: "1h", Cutoff: hourCut}); err != nil {
			return err
		}
		res.Events, err = qs.DeleteUsageEvents(ctx, q.DeleteUsageEventsParams{Granularity: "1h", Cutoff: hourCut})
		return err
	}); err != nil {
		return res, err
	}
	fiveCut := ts(now.Add(-FiveMinuteRetention))
	if err := step(func(qs *q.Queries, _ meta.Tx) (err error) {
		if res.FiveMinute, err = qs.DeleteUsageBuckets(ctx, q.DeleteUsageBucketsParams{Granularity: "5m", Cutoff: fiveCut}); err != nil {
			return err
		}
		n, err := qs.DeleteUsageEvents(ctx, q.DeleteUsageEventsParams{Granularity: "5m", Cutoff: fiveCut})
		res.Events += n
		return err
	}); err != nil {
		return res, err
	}
	if err := step(func(qs *q.Queries, _ meta.Tx) (err error) {
		if summaryRetention <= 0 {
			summaryRetention = 400 * 24 * time.Hour
		}
		if res.Summaries, err = qs.DeleteUsageSummaries(ctx, now.Add(-summaryRetention).UTC().Format("2006-01-02")); err != nil {
			return err
		}
		res.Blocks, err = qs.DeleteLimitBlocksBefore(ctx, ts(now.Add(-BlockRetention)))
		return err
	}); err != nil {
		return res, err
	}
	err := step(func(qs *q.Queries, tx meta.Tx) error {
		n, err := qs.EraseUsageOfDeletedSubjects(ctx)
		if err != nil {
			return err
		}
		m, err := qs.EraseEventsOfDeletedSubjects(ctx)
		if err != nil {
			return err
		}
		k, err := qs.EraseBlocksOfDeletedSubjects(ctx)
		if err != nil {
			return err
		}
		res.Erased = n + m + k
		rules, err := qs.DeleteRulesOfDeletedSubjects(ctx)
		if err != nil {
			return err
		}
		res.Rules = int64(len(rules))
		for _, r := range rules {
			d, _ := json.Marshal(map[string]any{
				"id": r.ID, "place": r.PlaceKind, "who": map[string]string{"kind": r.WhoKind, "ref": r.WhoRef},
				"reason": "the caller the rule names was deleted",
			})
			if err := rec.RecordTx(ctx, tx, audit.Event{
				Type: audit.EventLimitsAssignmentDeleted, Actor: audit.Actor{Kind: "system"},
				Outcome: audit.Success, Details: d,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return res, err
}
