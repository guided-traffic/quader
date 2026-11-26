package nuget

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/formats/nuget/nupkg"
	"github.com/guided-traffic/quader/internal/formats/nuget/rows"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The reading of a stored package's file list for a version stored before the ingestion read it (formats/nuget.md R-NUGET-038,
// R-NUGET-039): the job nuget-contents opens the stored .nupkg with ranged reads of its end record and its central directory
// (nupkg.Open, the reader of the ingestion, with its bounds), and writes what the names say (nupkg.ContentsOf) as the member contents of
// the version's metadata. No entry is inflated and nothing is unpacked. The task remote-describe queues it for every live version whose
// metadata has no member contents; the ingestion and a remote's fill write the member themselves, so a store described once queues
// nothing. The statements are in internal/meta/postgres/queries/nuget_contents.sql.

// ContentsJobKind is the job that reads the file list of one stored version (queue default, coalesce, key "nuget-contents:<version id>").
const ContentsJobKind = "nuget-contents"

const (
	contentsAttempts = 5
	contentsTimeout  = 5 * time.Minute
)

// ContentsArgs are the arguments of the job nuget-contents.
type ContentsArgs struct {
	VersionID int64 `json:"versionId"`
}

// RegisterContentsJob registers the kind nuget-contents; a nil handler answers the permanent failure of a job this replica cannot run.
func RegisterContentsJob(r *jobs.Registry, read jobs.Handler) {
	if read == nil {
		read = func(context.Context, *jobs.Run) error {
			return jobs.Permanent(errors.New("nuget-contents: this replica does not read stored packages"))
		}
	}
	r.RegisterKind(ContentsJobKind, jobs.KindSpec{
		Queue: "default", DedupMode: jobs.Coalesce, MaxAttempts: contentsAttempts, Timeout: contentsTimeout, Handler: read,
	})
}

// ContentsHandler is the handler of nuget-contents over the data port.
func ContentsHandler(s *Store) jobs.Handler {
	return func(ctx context.Context, run *jobs.Run) error {
		var a ContentsArgs
		if err := json.Unmarshal(run.Args, &a); err != nil {
			return jobs.Permanent(fmt.Errorf("nuget-contents: the arguments do not read: %w", err))
		}
		return s.ReadContents(ctx, a.VersionID)
	}
}

// ReadContents reads the file list of a stored version whose metadata has none and writes it. A version that is read, gone or without
// a stored package is done; a package the reader refuses within its bounds today is written as unreadable, so that it is not selected
// again and the screens say nothing of it; an object store that fails is an error, and the job tries again.
func (s *Store) ReadContents(ctx context.Context, versionID int64) error {
	if s.o.Blobs == nil {
		return jobs.Permanent(errors.New("nuget-contents: this replica reads no blob"))
	}
	var target []q.NuGetContentsTargetRow
	if err := s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		target, err = qs.NuGetContentsTarget(ctx, versionID)
		return err
	}); err != nil || len(target) == 0 {
		return err
	}
	t := target[0]
	// The object store is read outside a transaction.
	c := &rows.Contents{Frameworks: []string{}, Build: []string{}, Analyzers: []string{}, Scripts: []string{}, Unreadable: true}
	a, err := nupkg.Open(ctx, s.o.Blobs.ReaderAt(ctx, t.BlobSha256, t.Size), t.Size, nupkg.Limits{})
	switch {
	case err == nil:
		c = nupkg.ContentsOf(a.Entries(), t.LowerID)
	case !nupkg.IsRefusal(err):
		return err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		_, err = qs.NuGetPutContents(ctx, q.NuGetPutContentsParams{VersionID: versionID, Contents: raw})
		return err
	})
}

// ContentsPort is the NuGet part of the task remote-describe (R-NUGET-039): the live versions of every NuGet repository, local or
// remote, whose file list was never read.
type ContentsPort struct{ S *Store }

// QueueUndescribed queues nuget-contents for at most limit versions after the cursor, at the priority given, in one transaction.
func (p ContentsPort) QueueUndescribed(ctx context.Context, after int64, limit, priority int) (next int64, queued int, done bool, err error) {
	next = after
	err = p.S.o.DB.InTx(ctx, func(tx meta.Tx) error {
		next, queued, done = after, 0, false
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		ids, err := qs.NuGetUnreadContents(ctx, q.NuGetUnreadContentsParams{After: after, RowLimit: int32(min(limit, 1<<20))}) //nolint:gosec // bounded above
		if err != nil {
			return err
		}
		done = len(ids) < limit
		for _, id := range ids {
			if p.S.o.Jobs != nil {
				r, err := p.S.o.Jobs.Enqueue(ctx, tx, jobs.EnqueueSpec{
					Kind: ContentsJobKind, Args: ContentsArgs{VersionID: id}, DedupKey: ContentsJobKind + ":" + strconv.FormatInt(id, 10), Priority: priority,
				})
				if err != nil {
					return err
				}
				if r.Created {
					queued++
				}
			}
			next = id
		}
		return nil
	})
	return next, queued, done, err
}

// HasUndescribed reports whether a live version is left whose file list was never read.
func (p ContentsPort) HasUndescribed(ctx context.Context) (bool, error) {
	var left bool
	err := p.S.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		ids, err := qs.NuGetUnreadContents(ctx, q.NuGetUnreadContentsParams{After: 0, RowLimit: 1})
		left = len(ids) > 0
		return err
	})
	return left, err
}
