package deb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/deb/debfile"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The description of the package files a remote repository cached (R-DEB-037): the job deb-remote-describe reads the control paragraph
// of every live package file a remote holds of a package and has no row of deb_binaries for, out of the stored file with ranged reads
// (the ar headers and the control archive; the data archive is never read, R-DEB-007), and writes the row a local file has. The fill of
// a package file queues it in the fill's transaction; the removal of a cached file takes its row in the removal's transaction. The job
// holds no transaction while it reads a blob.

// DescribeJobKind is the job that describes the cached package files of one package of a remote repository (queue index, coalesce, key
// "deb-describe:<repository id>:<package>", see [DescribeKey]).
const DescribeJobKind = "deb-remote-describe"

const (
	describeAttempts = 5
	describeTimeout  = 10 * time.Minute
)

// BlobRanges reads a stored blob with ranged reads, which are not verified against the digest: the control paragraph is display text
// and the stored file was verified against its index when it was fetched (R-DEB-022). The composition root implements it over the blob
// store.
type BlobRanges interface {
	ReaderAt(ctx context.Context, sha256 []byte, size int64) io.ReaderAt
}

// DescribeArgs are the arguments of the job deb-remote-describe.
type DescribeArgs struct {
	RepoID  int64  `json:"repoId"`
	Package string `json:"package"`
}

// DescribeKey is the dedup key of the job of a package of a remote repository.
func DescribeKey(repoID int64, pkg string) string {
	return "deb-describe:" + strconv.FormatInt(repoID, 10) + ":" + pkg
}

// RegisterDescribeJob registers the kind deb-remote-describe in the registry the composition root freezes; a nil handler answers the
// permanent failure of a job this replica cannot run.
func RegisterDescribeJob(r *jobs.Registry, describe jobs.Handler) {
	if describe == nil {
		describe = func(context.Context, *jobs.Run) error {
			return jobs.Permanent(errNotBuilt("the description of a cached file"))
		}
	}
	r.RegisterKind(DescribeJobKind, jobs.KindSpec{
		Queue: "index", DedupMode: jobs.Coalesce, MaxAttempts: describeAttempts, Timeout: describeTimeout, Handler: describe,
	})
}

// DescribeHandler is the handler of deb-remote-describe over the data port.
func DescribeHandler(s *Store) jobs.Handler {
	return func(ctx context.Context, run *jobs.Run) error {
		var a DescribeArgs
		if err := json.Unmarshal(run.Args, &a); err != nil {
			return jobs.Permanent(fmt.Errorf("deb-remote-describe: the arguments do not read: %w", err))
		}
		return s.DescribeCached(ctx, a.RepoID, a.Package)
	}
}

// QueueDescribe queues the job deb-remote-describe of a package of a remote repository in the transaction tx (R-DEB-037). A data port
// without a queue queues nothing.
func (s remoteStore) QueueDescribe(ctx context.Context, tx meta.Tx, repoID int64, pkg string) error {
	if s.o.Jobs == nil {
		return nil
	}
	_, err := s.o.Jobs.Enqueue(ctx, tx, jobs.EnqueueSpec{
		Kind: DescribeJobKind, Args: DescribeArgs{RepoID: repoID, Package: pkg}, DedupKey: DescribeKey(repoID, pkg),
	})
	return err
}

// ForgetCached deletes the rows of package files a remote repository removed from its cache, in the removal's transaction (R-DEB-037).
func (s remoteStore) ForgetCached(ctx context.Context, tx meta.Tx, repoID int64, itemIDs []int64) error {
	if len(itemIDs) == 0 {
		return nil
	}
	return s.g().read(ctx, tx, func(qs *q.Queries) error {
		_, err := qs.DebDeleteCachedRows(ctx, q.DebDeleteCachedRowsParams{RepoID: repoID, ItemIds: itemIDs})
		return err
	})
}

// DescribeCached writes the row of deb_binaries of every live package file a remote repository cached of the package and has no row for
// (R-DEB-037). A file whose control data Quader refuses (a package an upload would be refused for, or a stored object that is lost) gets
// the paragraph its parsed index row gives (cachedParagraph), so that no file is tried again for ever; a blob store that fails is an
// error, and the job tries again.
func (s *Store) DescribeCached(ctx context.Context, repoID int64, pkg string) error {
	if s.o.Blobs == nil {
		return jobs.Permanent(errors.New("deb-remote-describe: this replica reads no blob"))
	}
	var files []q.DebDescribeFilesRow
	if err := s.remoteStore.read(ctx, nil, func(qs *q.Queries) (err error) {
		files, err = qs.DebDescribeFiles(ctx, q.DebDescribeFilesParams{RepoID: repoID, Package: pkg})
		return err
	}); err != nil {
		return err
	}
	for _, f := range files {
		var p *debfile.Package
		readErr := errNoBlob
		if len(f.Sha256) == 32 {
			p, readErr = debfile.Read(s.o.Blobs.ReaderAt(ctx, f.Sha256, f.Size), f.Size, untrusted.Limits{})
		}
		row, err := cachedParagraph(f, p, readErr)
		if err != nil {
			return err
		}
		if err := s.remoteStore.read(ctx, nil, func(qs *q.Queries) error {
			if _, err := qs.DebForgetCachedRow(ctx, q.DebForgetCachedRowParams{
				RepoID: repoID, Package: row.Package, Version: row.Version, Architecture: row.Architecture, ItemID: f.ItemID,
			}); err != nil {
				return err
			}
			row.ItemID, row.RepoID = f.ItemID, repoID
			_, err := qs.DebPutCachedRow(ctx, row)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// errNoBlob is the read of a file whose item names no blob: a refusal, as for a file the reader refuses.
var errNoBlob error = errs.E(errs.Unprocessable, errs.CodeDebInvalidArchive, "The cached file has no stored content.")

// cachedParagraph is the row of a cached file: the paragraph of its control archive, or, when the reader refused the file (any *errs.Error:
// a package an upload would be refused for, a lost object), the paragraph its parsed index row gives: Package and Version of its version,
// the architecture of the index (else of the file name) and the short description. An error the reader did not classify (a blob store
// that fails) is returned, and the job tries again.
func cachedParagraph(f q.DebDescribeFilesRow, p *debfile.Package, readErr error) (q.DebPutCachedRowParams, error) {
	if readErr == nil {
		if control, err := p.Control.Bytes(); err == nil {
			return q.DebPutCachedRowParams{
				Package: p.Name, Version: p.Version, Architecture: p.Architecture, Source: p.Source, Control: string(control),
			}, nil
		}
	} else if _, refused := errs.As(readErr); !refused {
		return q.DebPutCachedRowParams{}, readErr
	}
	arch := f.Architecture
	if arch == "" {
		base := strings.TrimSuffix(path.Base(f.Path), ".deb")
		arch = base[strings.LastIndexByte(base, '_')+1:]
	}
	line := func(v string) string { return strings.Join(strings.Fields(v), " ") }
	control := "Package: " + line(f.Package) + "\nVersion: " + line(f.Version) + "\nArchitecture: " + line(arch) + "\n"
	if d := line(f.Description); d != "" {
		control += "Description: " + d + "\n"
	}
	return q.DebPutCachedRowParams{
		Package: f.Package, Version: f.Version, Architecture: line(arch), Source: f.Package, Control: control,
	}, nil
}

// QueueUndescribed queues deb-remote-describe for at most limit packages of Debian remote repositories after the cursor that hold a
// live file without its row, at the priority given, in one transaction (the task remote-describe, R-DEB-037). It returns the cursor of
// the last package read, how many it queued, and done when it read fewer than limit.
func (s *Store) QueueUndescribed(ctx context.Context, after int64, limit, priority int) (next int64, queued int, done bool, err error) {
	next = after
	err = s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		next, queued, done = after, 0, false
		qs, err := txQueries(tx)
		if err != nil {
			return err
		}
		list, err := qs.DebUndescribedPackages(ctx, q.DebUndescribedPackagesParams{After: after, RowLimit: int32(min(limit, 1<<20))}) //nolint:gosec // bounded above
		if err != nil {
			return err
		}
		done = len(list) < limit
		for _, p := range list {
			if s.o.Jobs != nil {
				r, err := s.o.Jobs.Enqueue(ctx, tx, jobs.EnqueueSpec{
					Kind: DescribeJobKind, Args: DescribeArgs{RepoID: p.RepoID, Package: p.Name}, DedupKey: DescribeKey(p.RepoID, p.Name),
					Priority: priority,
				})
				if err != nil {
					return err
				}
				if r.Created {
					queued++ // a package whose job waits already is not queued again
				}
			}
			next = p.ID
		}
		return nil
	})
	return next, queued, done, err
}

// HasUndescribed reports whether a Debian remote repository holds a live file without its row (the start of the task remote-describe).
func (s *Store) HasUndescribed(ctx context.Context) (bool, error) {
	var left bool
	err := s.remoteStore.read(ctx, nil, func(qs *q.Queries) error {
		list, err := qs.DebUndescribedPackages(ctx, q.DebUndescribedPackagesParams{After: 0, RowLimit: 1})
		left = len(list) > 0
		return err
	})
	return left, err
}
