package deb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
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
// (R-DEB-037). A file whose control data Quader refuses (a package an upload would be refused for) gets no row and stays listed by its
// generic rows; a blob store that fails is an error, and the job tries again.
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
		if len(f.Sha256) != 32 {
			continue
		}
		p, err := debfile.Read(s.o.Blobs.ReaderAt(ctx, f.Sha256, f.Size), f.Size, untrusted.Limits{})
		if err != nil {
			if _, refused := errs.As(err); refused {
				continue
			}
			return err
		}
		control, err := p.Control.Bytes()
		if err != nil {
			continue
		}
		if err := s.remoteStore.read(ctx, nil, func(qs *q.Queries) error {
			if _, err := qs.DebForgetCachedRow(ctx, q.DebForgetCachedRowParams{
				RepoID: repoID, Package: p.Name, Version: p.Version, Architecture: p.Architecture, ItemID: f.ItemID,
			}); err != nil {
				return err
			}
			_, err := qs.DebPutCachedRow(ctx, q.DebPutCachedRowParams{
				ItemID: f.ItemID, RepoID: repoID, Package: p.Name, Version: p.Version, Architecture: p.Architecture, Source: p.Source,
				Control: string(control),
			})
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}
