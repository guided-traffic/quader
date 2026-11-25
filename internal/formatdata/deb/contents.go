package deb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/formats/deb/debfile"
	"github.com/guided-traffic/quader/internal/formats/deb/rows"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The reading of a package file's data archive (R-DEB-039, Q-DEB-140): the job deb-contents reads, for every live package file of a
// package in a repository that the current reader has not read, the stored file's data archive as a stream (debfile.ReadContents, with
// its bounds) and writes the row of deb_contents: the newest part of the changelog, the copyright file and its licences, the file list,
// or why the reader refused the archive. The upload of a local file queues it in the upload's transaction, and the description of a file
// a remote cached queues it when it wrote the file's row; the task remote-describe queues it for every file stored before. It holds no
// transaction while it reads a blob, and a refusal is a row, never a failed upload or fill.

// ContentsJobKind is the job that reads the data archives of one package of a repository (queue default, coalesce, key
// "deb-contents:<repository id>:<package>", see [ContentsKey]). The default queue, not the index queue: a read of a large package file
// must not hold back an index build.
const ContentsJobKind = "deb-contents"

// ContentsReader is the version of the reader that writes deb_contents.reader. A change of what the reader keeps raises it, and the task
// remote-describe reads every file again whose row an older reader wrote.
const ContentsReader = 1

const (
	contentsAttempts = 5
	contentsTimeout  = 30 * time.Minute
)

// ContentsArgs are the arguments of the job deb-contents.
type ContentsArgs struct {
	RepoID  int64  `json:"repoId"`
	Package string `json:"package"`
}

// ContentsKey is the dedup key of the job of a package of a repository.
func ContentsKey(repoID int64, pkg string) string {
	return "deb-contents:" + strconv.FormatInt(repoID, 10) + ":" + pkg
}

// RegisterContentsJob registers the kind deb-contents in the registry the composition root freezes; a nil handler answers the permanent
// failure of a job this replica cannot run.
func RegisterContentsJob(r *jobs.Registry, read jobs.Handler) {
	if read == nil {
		read = func(context.Context, *jobs.Run) error {
			return jobs.Permanent(errNotBuilt("the reading of a data archive"))
		}
	}
	r.RegisterKind(ContentsJobKind, jobs.KindSpec{
		Queue: "default", DedupMode: jobs.Coalesce, MaxAttempts: contentsAttempts, Timeout: contentsTimeout, Handler: read,
	})
}

// ContentsHandler is the handler of deb-contents over the data port.
func ContentsHandler(s *Store) jobs.Handler {
	return func(ctx context.Context, run *jobs.Run) error {
		var a ContentsArgs
		if err := json.Unmarshal(run.Args, &a); err != nil {
			return jobs.Permanent(fmt.Errorf("deb-contents: the arguments do not read: %w", err))
		}
		return s.ReadContents(ctx, a.RepoID, a.Package)
	}
}

// queueContents queues deb-contents of a package of a repository in tx at the priority given (0: the default); a port without a queue
// queues nothing. It reports whether a job was created (one that waits already takes the package).
func (g *group) queueContents(ctx context.Context, tx meta.Tx, repoID int64, pkg string, priority int) (bool, error) {
	if g.o.Jobs == nil {
		return false, nil
	}
	r, err := g.o.Jobs.Enqueue(ctx, tx, jobs.EnqueueSpec{
		Kind: ContentsJobKind, Args: ContentsArgs{RepoID: repoID, Package: pkg}, DedupKey: ContentsKey(repoID, pkg), Priority: priority,
	})
	return r.Created, err
}

// ReadContents reads the data archive of every live package file of the package in the repository that the current reader has not read,
// and writes its row (R-DEB-039). A file the reader refuses gets a row with the reason; a blob store that fails is an error, and the job
// tries again.
func (s *Store) ReadContents(ctx context.Context, repoID int64, pkg string) error {
	if s.o.Blobs == nil {
		return jobs.Permanent(errors.New("deb-contents: this replica reads no blob"))
	}
	var files []q.DebContentsFilesRow
	if err := s.viewStore.read(ctx, nil, func(qs *q.Queries) (err error) {
		files, err = qs.DebContentsFiles(ctx, q.DebContentsFilesParams{RepoID: repoID, Package: pkg, Reader: ContentsReader})
		return err
	}); err != nil {
		return err
	}
	for _, f := range files {
		row := q.DebPutContentsParams{ItemID: f.ItemID, Reader: ContentsReader, Licenses: []string{}, Files: json.RawMessage("[]")}
		if len(f.Sha256) != 32 {
			row.State, row.Reason, row.Detail = rows.ContentsRefused, debfile.ReasonNoContent, "The stored file has no content."
		} else {
			c, err := debfile.ReadContents(ctx, s.o.Blobs.ReaderAt(ctx, f.Sha256, f.Size), f.Size, f.Package)
			var refusal *debfile.Refusal
			switch {
			case errors.As(err, &refusal):
				row.State, row.Reason, row.Detail = rows.ContentsRefused, refusal.Reason, refusal.Detail
			case err != nil:
				return err
			default:
				if err := fill(&row, c); err != nil {
					return err
				}
			}
		}
		if err := s.viewStore.read(ctx, nil, func(qs *q.Queries) error {
			_, err := qs.DebPutContents(ctx, row)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// fill copies what the reader kept into the row.
func fill(row *q.DebPutContentsParams, c *debfile.Contents) error {
	files, err := json.Marshal(c.Files)
	if err != nil {
		return err
	}
	row.State = rows.ContentsRead
	row.Changelog, row.ChangelogFile, row.ChangelogTruncated = c.Changelog, c.ChangelogFile, c.ChangelogTruncated
	row.Copyright, row.CopyrightTruncated = c.Copyright, c.CopyrightTruncated
	if c.Licenses != nil {
		row.Licenses = c.Licenses
	}
	row.DocLink, row.Files, row.FileCount = c.DocLink, files, int32(min(c.FileCount, 1<<30)) //nolint:gosec // bounded by debfile.MaxEntries
	return nil
}

// VersionContents reads what the reader kept of every package file of a version in a repository, one per architecture, without the
// file lists (R-DEB-039). A file the reader has not read yet is in the state rows.ContentsPending.
func (s viewStore) VersionContents(ctx context.Context, repoID int64, pkg, version string) ([]rows.ContentsHead, error) {
	var out []rows.ContentsHead
	err := s.read(ctx, nil, func(qs *q.Queries) error {
		list, err := qs.DebVersionContents(ctx, q.DebVersionContentsParams{RepoID: repoID, Package: pkg, Version: version})
		for _, r := range list {
			h := rows.ContentsHead{
				Architecture: r.Architecture, ItemID: r.ItemID, State: r.State, Reason: r.Reason, Detail: r.Detail,
				Changelog: r.Changelog, ChangelogFile: r.ChangelogFile, ChangelogTruncated: r.ChangelogTruncated,
				Copyright: r.Copyright, CopyrightTruncated: r.CopyrightTruncated, Licenses: r.Licenses, DocLink: r.DocLink,
				FilesKept: int(r.FilesKept), FileCount: int(r.FileCount),
			}
			if r.ReadAt.Valid {
				h.ReadAt = r.ReadAt.Time
			}
			out = append(out, h)
		}
		return err
	})
	return out, err
}

// ContentsFiles reads a page of the kept file list of a package file: the entries whose path contains needle (compared without case;
// empty for all), after offset, at most limit (R-DEB-039).
func (s viewStore) ContentsFiles(ctx context.Context, itemID int64, needle string, offset, limit int) (rows.ContentsPage, error) {
	var out rows.ContentsPage
	err := s.read(ctx, nil, func(qs *q.Queries) error {
		list, err := qs.DebContentsFileList(ctx, q.DebContentsFileListParams{
			ItemID: itemID, Needle: needle, RowOffset: int64(offset), RowLimit: int64(limit),
		})
		if err != nil {
			return err
		}
		out.Files = make([]rows.ContentsFile, 0, len(list))
		for _, r := range list {
			var f rows.ContentsFile
			if err := json.Unmarshal([]byte(r.Entry), &f); err != nil {
				return err
			}
			out.Files = append(out.Files, f)
			out.Matched = int(r.Matched)
		}
		return nil
	})
	return out, err
}

// ContentsPort is the Debian part of the task remote-describe for the data archives (R-DEB-039): the packages of local and remote
// repositories that hold a live package file the current reader has not read, which it queues deb-contents for.
type ContentsPort struct{ S *Store }

// QueueUndescribed queues deb-contents for at most limit packages after the cursor, at the priority given, in one transaction.
func (p ContentsPort) QueueUndescribed(ctx context.Context, after int64, limit, priority int) (next int64, queued int, done bool, err error) {
	next = after
	err = p.S.o.DB.InTx(ctx, func(tx meta.Tx) error {
		next, queued, done = after, 0, false
		qs, err := txQueries(tx)
		if err != nil {
			return err
		}
		list, err := qs.DebUnreadPackages(ctx, q.DebUnreadPackagesParams{
			After: after, Reader: ContentsReader, RowLimit: int32(min(limit, 1<<20)), //nolint:gosec // bounded above
		})
		if err != nil {
			return err
		}
		done = len(list) < limit
		for _, pk := range list {
			created, err := p.S.viewStore.queueContents(ctx, tx, pk.RepoID, pk.Name, priority)
			if err != nil {
				return err
			}
			if created {
				queued++
			}
			next = pk.ID
		}
		return nil
	})
	return next, queued, done, err
}

// HasUndescribed reports whether a package file is left that the current reader has not read.
func (p ContentsPort) HasUndescribed(ctx context.Context) (bool, error) {
	var left bool
	err := p.S.viewStore.read(ctx, nil, func(qs *q.Queries) error {
		list, err := qs.DebUnreadPackages(ctx, q.DebUnreadPackagesParams{After: 0, Reader: ContentsReader, RowLimit: 1})
		left = len(list) > 0
		return err
	})
	return left, err
}
