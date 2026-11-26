package dependents

import (
	"context"
	"slices"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// Source reads the dependencies of versions of one format from the format's stored rows (npm_versions, nuget_version, deb_binaries,
// ansible_collection_versions), in the transaction of qs; a version the rows do not describe yet is answered without dependencies.
type Source func(ctx context.Context, qs *q.Queries, versionIDs []int64) (map[int64][]Dependency, error)

// Fill is the task remote-describe's entry for the table (R-API-086): it reads the dependencies of every live version of a format with
// a [Source] whose dependencies no extractor of the current [Reader] has read, a batch per transaction, from the stored rows: what was
// stored before the table existed, what a restore left without rows (the table is derived and not exported), and what an older
// extractor read. It queues no job; it writes in the run, a batch per transaction, and the run saves its cursor after each.
type Fill struct {
	DB      meta.Runner
	Sources map[string]Source
}

func (f Fill) formats() []string {
	out := make([]string, 0, len(f.Sources))
	for k := range f.Sources {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// QueueUndescribed reads at most limit versions after the cursor whose dependencies are not read and writes their rows, in one
// transaction. It returns the cursor of the last version read and done when it read fewer than limit; it queues nothing, so it reports
// none queued.
func (f Fill) QueueUndescribed(ctx context.Context, after int64, limit, _ int) (next int64, queued int, done bool, err error) {
	if f.DB == nil {
		return after, 0, true, errs.E(errs.Internal, errs.CodeInternal, "dependents: the fill needs a transaction runner")
	}
	err = f.DB.InTx(ctx, func(tx meta.Tx) error {
		next, done = after, false
		qs, err := Queries(tx)
		if err != nil {
			return err
		}
		rows, err := qs.PackageDependenciesUnread(ctx, q.PackageDependenciesUnreadParams{
			After: after, Formats: f.formats(), Reader: Reader, RowLimit: int32(min(max(limit, 1), 1<<20)), //nolint:gosec // bounded above
		})
		if err != nil {
			return err
		}
		done = len(rows) < limit
		byFormat := map[string][]int64{}
		for _, r := range rows {
			byFormat[r.Format] = append(byFormat[r.Format], r.VersionID)
			next = r.VersionID
		}
		for format, ids := range byFormat {
			deps, err := f.Sources[format](ctx, qs, ids)
			if err != nil {
				return err
			}
			for _, id := range ids {
				if err := Put(ctx, qs, id, deps[id]); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return next, 0, done, err
}

// HasUndescribed reports whether a live version of a format with a source has dependencies no extractor of the current reader read.
func (f Fill) HasUndescribed(ctx context.Context) (bool, error) {
	if f.DB == nil {
		return false, nil
	}
	var left bool
	err := f.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := Queries(tx)
		if err != nil {
			return err
		}
		rows, err := qs.PackageDependenciesUnread(ctx, q.PackageDependenciesUnreadParams{Formats: f.formats(), Reader: Reader, RowLimit: 1})
		left = len(rows) > 0
		return err
	})
	return left, err
}
