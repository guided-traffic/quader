package ansible

import (
	"context"
	"strconv"

	"github.com/guided-traffic/quader/internal/formats/ansible/docgen"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// ReindexPort is the Ansible Galaxy entry of the task remote-describe (Q-ANS-99, remotedescribe.Port): it queues the job ansible-reindex
// for every local and remote repository that holds a version whose changelog was not read by docgen.Version, or whose generated
// documentation an older generator made (R-ANS-044, R-ANS-053). A worker that starts queues the task while such a repository exists, so an
// upgrade that raises the generator rebuilds what was published or cached before it without an operator's act, under the task's bounds:
// at most the task's batch of repositories per step, none while the task's bound of waiting jobs is reached, and the task's low priority.
// The unit is the repository, because the reindex is the job that reads its archives: one job per repository, coalescing on the key of
// R-REPO-016 with an operator's reindex, checkpointed by version and one transaction per version.
type ReindexPort struct{ S *Store }

// QueueUndescribed implements remotedescribe.Port: the repositories after the cursor (by id), at most limit.
func (p ReindexPort) QueueUndescribed(ctx context.Context, after int64, limit, priority int) (next int64, queued int, done bool, err error) {
	if p.S.o.Jobs == nil {
		return after, 0, true, errNotBuilt("the reindex queue")
	}
	err = p.S.o.DB.InTx(ctx, func(tx meta.Tx) error {
		next, queued, done = after, 0, false
		qs, err := txQueries(tx)
		if err != nil {
			return err
		}
		ids, err := qs.AnsibleDocsStaleRepositories(ctx, q.AnsibleDocsStaleRepositoriesParams{
			After: after, Generator: docgen.Version, RowLimit: int32(min(limit, 1<<20)), //nolint:gosec // bounded above
		})
		if err != nil {
			return err
		}
		done = len(ids) < limit
		for _, id := range ids {
			r, err := p.S.o.Jobs.Enqueue(ctx, tx, jobs.EnqueueSpec{
				Kind: ReindexJobKind, DedupKey: ReindexDedupKey(id), TargetKind: "repository", TargetKey: strconv.FormatInt(id, 10),
				Priority: priority,
			})
			if err != nil {
				return err
			}
			if r.Created {
				queued++ // a repository whose reindex waits already is not queued again
			}
			next = id
		}
		return nil
	})
	return next, queued, done, err
}

// HasUndescribed implements remotedescribe.Port: whether any repository is left.
func (p ReindexPort) HasUndescribed(ctx context.Context) (bool, error) {
	var left bool
	err := p.S.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := txQueries(tx)
		if err != nil {
			return err
		}
		ids, err := qs.AnsibleDocsStaleRepositories(ctx, q.AnsibleDocsStaleRepositoriesParams{After: 0, Generator: docgen.Version, RowLimit: 1})
		left = len(ids) > 0
		return err
	})
	return left, err
}
