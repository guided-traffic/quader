package npm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/npm/manifest"
	"github.com/guided-traffic/quader/internal/formats/npm/tarball"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The description of the versions a remote repository cached (R-NPM-050): the job npm-remote-describe writes the row of npm_versions of
// every live version a remote holds of a package from the version's entry in the stored package document, and reads the cached tarball
// once for what only the tarball says (the file count, the unpacked size, the install script, the shrinkwrap and the README's file name).
// The fill of a tarball queues it in the fill's transaction, and so does a fill that replaced the stored document while the remote holds
// a version of the package, so that a deprecation the upstream adds later reaches the screens and a version cached before the job
// existed is described at the next change of its document. The job holds no transaction while it reads a blob.

// DescribeJobKind is the job that describes the cached versions of one package of a remote repository (queue index, coalesce, key
// "npm-describe:<repository id>:<name>", see [DescribeKey]).
const DescribeJobKind = "npm-remote-describe"

// The bounds of one run: the attempts and the time of one attempt; a document is read up to the size the format stores (the format's
// MaxDocumentBytes, R-NPM-024).
const (
	describeAttempts = 5
	describeTimeout  = 10 * time.Minute
	maxDocumentBytes = 128 << 20
	maxDeprecated    = 1024
)

// BlobReader reads a stored blob whole, verified against its digest by the blob store; the composition root implements it over the
// blob store.
type BlobReader interface {
	Open(ctx context.Context, sha256 [32]byte, size int64) (io.ReadCloser, error)
}

// Jobs is the job queue the data port enqueues the job npm-remote-describe into; *jobs.Queue implements it.
type Jobs interface {
	Enqueue(ctx context.Context, tx meta.Tx, e jobs.EnqueueSpec) (jobs.Result, error)
}

// DescribeArgs are the arguments of the job npm-remote-describe.
type DescribeArgs struct {
	RepoID int64  `json:"repoId"`
	Name   string `json:"name"`
}

// DescribeKey is the dedup key of the job of a package of a remote repository.
func DescribeKey(repoID int64, name string) string {
	return "npm-describe:" + strconv.FormatInt(repoID, 10) + ":" + name
}

// RegisterJobs registers the kind npm-remote-describe in the registry the composition root freezes; a nil handler answers the permanent
// failure of a job this replica cannot run.
func RegisterJobs(r *jobs.Registry, describe jobs.Handler) {
	if describe == nil {
		describe = func(context.Context, *jobs.Run) error {
			return jobs.Permanent(errors.New("npm-remote-describe: this replica has no npm data port"))
		}
	}
	r.RegisterKind(DescribeJobKind, jobs.KindSpec{
		Queue: "index", DedupMode: jobs.Coalesce, MaxAttempts: describeAttempts, Timeout: describeTimeout, Handler: describe,
	})
}

// DescribeHandler is the handler of npm-remote-describe over the data port.
func DescribeHandler(s *Store) jobs.Handler {
	return func(ctx context.Context, run *jobs.Run) error {
		var a DescribeArgs
		if err := json.Unmarshal(run.Args, &a); err != nil {
			return jobs.Permanent(fmt.Errorf("npm-remote-describe: the arguments do not read: %w", err))
		}
		return s.DescribeCached(ctx, a.RepoID, a.Name)
	}
}

// QueueDescribe queues the job npm-remote-describe of a package of a remote repository in the transaction tx (R-NPM-050). With ifHeld it
// queues only while the repository holds a live version of the package: the refresh of a document nobody cached a tarball of describes
// nothing. A data port without a queue queues nothing.
func (s *Store) QueueDescribe(ctx context.Context, tx meta.Tx, repoID int64, name string, ifHeld bool) error {
	if s.o.Jobs == nil {
		return nil
	}
	if ifHeld {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		held, err := qs.NPMHoldsPackage(ctx, q.NPMHoldsPackageParams{RepoID: repoID, Name: name})
		if err != nil || !held {
			return err
		}
	}
	_, err := s.o.Jobs.Enqueue(ctx, tx, jobs.EnqueueSpec{
		Kind: DescribeJobKind, Args: DescribeArgs{RepoID: repoID, Name: name}, DedupKey: DescribeKey(repoID, name),
	})
	return err
}

// DescribeCached writes the row of npm_versions of every live version a remote repository holds of the package (R-NPM-050): from the
// version's entry in the stored document, and, for a version without a row yet, from one read of its cached tarball. A package without a
// stored document or without a live version, and a version its document does not list (any more), are left as they are.
func (s *Store) DescribeCached(ctx context.Context, repoID int64, name string) error {
	if s.o.Blobs == nil {
		return jobs.Permanent(errors.New("npm-remote-describe: this replica reads no blob"))
	}
	var doc []q.NPMDescribeDocumentRow
	var vs []q.NPMDescribeVersionsRow
	err := s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		if doc, err = qs.NPMDescribeDocument(ctx, q.NPMDescribeDocumentParams{RepoID: repoID, Name: name}); err != nil {
			return err
		}
		vs, err = qs.NPMDescribeVersions(ctx, q.NPMDescribeVersionsParams{RepoID: repoID, Name: name})
		return err
	})
	if err != nil || len(doc) == 0 || len(vs) == 0 {
		return err
	}
	want := make(map[string]bool, len(vs))
	for _, v := range vs {
		want[v.Version] = true
	}
	entries, times, err := s.readEntries(ctx, doc[0].Sha256, doc[0].Size, want)
	if err != nil {
		return err
	}
	for _, v := range vs {
		raw, ok := entries[v.Version]
		if !ok {
			continue
		}
		row, err := cachedRow(raw, times[v.Version])
		if err != nil {
			continue // an entry that is no manifest within the bounds of R-ARCH-028 describes nothing
		}
		if !v.Described && len(v.Sha256) == 32 {
			if err := s.scan(ctx, v.Sha256, v.Size, &row); err != nil {
				return err
			}
		}
		row.VersionID = v.VersionID
		if err := s.o.DB.InTx(ctx, func(tx meta.Tx) error {
			qs, err := queries(tx)
			if err != nil {
				return err
			}
			_, err = qs.NPMPutCachedVersion(ctx, row)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// readEntries reads the stored document by streaming it and keeps the entries of the wanted versions and the time map. The document
// passed the checks of R-NPM-024 when it was stored; one that does not read now (a blob store that fails) is an error, and the job tries
// again.
func (s *Store) readEntries(ctx context.Context, sha256 []byte, size int64, want map[string]bool) (map[string]json.RawMessage, map[string]string, error) {
	var sum [32]byte
	copy(sum[:], sha256)
	body, err := s.o.Blobs.Open(ctx, sum, size)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = body.Close() }()
	dec := json.NewDecoder(bufio.NewReaderSize(io.LimitReader(body, maxDocumentBytes), 64<<10))
	entries, times := map[string]json.RawMessage{}, map[string]string{}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, nil, fmt.Errorf("npm-remote-describe: the stored document is no JSON object: %w", err)
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		switch keyTok {
		case "versions":
			err = readObject(dec, func(k string) error {
				var raw json.RawMessage
				if err := dec.Decode(&raw); err != nil {
					return err
				}
				if want[k] {
					entries[k] = raw
				}
				return nil
			})
		case "time":
			err = readObject(dec, func(k string) error {
				var v any
				if err := dec.Decode(&v); err != nil {
					return err
				}
				if s, ok := v.(string); ok && want[k] {
					times[k] = s
				}
				return nil
			})
		default:
			var skip json.RawMessage
			err = dec.Decode(&skip)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("npm-remote-describe: the stored document does not read: %w", err)
		}
	}
	return entries, times, nil
}

// readObject reads the object at the decoder's position member by member; a null is an empty object.
func readObject(dec *json.Decoder, member func(key string) error) error {
	tok, err := dec.Token()
	if err != nil || tok == nil {
		return err
	}
	if tok != json.Delim('{') {
		return errors.New("not an object")
	}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := k.(string)
		if err := member(key); err != nil {
			return err
		}
	}
	_, err = dec.Token()
	return err
}

// cachedRow is the row of a version's entry: the entry without what the screens do not read from it (the README a version entry of an
// old document may carry, read from the tarball instead, and the upstream's tarball address), its deprecation, its time, and the facts
// of the tarball the entry announces, which the read of the tarball replaces.
func cachedRow(raw json.RawMessage, published string) (q.NPMPutCachedVersionParams, error) {
	m, err := manifest.Parse(raw, manifest.Limits{})
	if err != nil {
		return q.NPMPutCachedVersionParams{}, err
	}
	delete(m, "readme")
	delete(m, "readmeFilename")
	dist, _ := m["dist"].(map[string]any)
	if dist != nil {
		delete(dist, "tarball")
	}
	stored, err := json.Marshal(m)
	if err != nil {
		return q.NPMPutCachedVersionParams{}, err
	}
	row := q.NPMPutCachedVersionParams{
		Manifest: string(stored), HasInstallScript: m["hasInstallScript"] == true || manifest.HasInstallScript(m, manifest.Facts{}),
		HasShrinkwrap: m["_hasShrinkwrap"] == true, FileCount: clampInt32(count(dist["fileCount"])), UnpackedSize: count(dist["unpackedSize"]),
	}
	if d, ok := m["deprecated"].(string); ok {
		row.Deprecated = clean(d, maxDeprecated)
	}
	if t, err := time.Parse(time.RFC3339Nano, published); err == nil {
		row.PublishedAt = stamp(t)
	}
	return row, nil
}

// count reads a count of a dist member: a JSON number that is a whole number not below zero, else 0.
func count(v any) int64 {
	n, ok := v.(json.Number)
	if !ok {
		return 0
	}
	i, err := n.Int64()
	if err != nil || i < 0 {
		return 0
	}
	return i
}

// scan reads the cached tarball once with the reader of a publish (R-NPM-007 step 3) for the facts only the tarball holds. A tarball the
// reader refuses (an upstream's archive may break a rule Quader applies to a publish) keeps the facts its entry announces; a blob store
// that fails is an error, and the job tries again.
func (s *Store) scan(ctx context.Context, sha256 []byte, size int64, row *q.NPMPutCachedVersionParams) error {
	var sum [32]byte
	copy(sum[:], sha256)
	body, err := s.o.Blobs.Open(ctx, sum, size)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	res, err := tarball.Validate(ctx, body, tarball.Limits{})
	if err != nil {
		if _, refused := errs.As(err); refused {
			return nil
		}
		return err
	}
	row.Scanned = true
	row.FileCount, row.UnpackedSize = clampInt32(res.FileCount), res.UnpackedSize
	row.HasInstallScript = manifest.HasInstallScript(res.Manifest, res.Facts)
	row.HasShrinkwrap = res.HasShrinkwrap
	if res.Readme != "" {
		row.ReadmeFilename = clean(res.ReadmeFilename, 255) // an empty README is none, as for a publish (R-NPM-051)
	}
	return nil
}
