package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/vuln"
	"github.com/guided-traffic/quader/internal/vuln/advisory"
	"github.com/guided-traffic/quader/internal/vuln/imagescan"
	"github.com/guided-traffic/quader/internal/vuln/store"
)

// The vulnerability database of the image scanning (R-VULN-035, R-VULN-044): one archive, published as an OCI artifact, that lies in a
// repository of the installation, upstream/scanner-trivy by default (ADR-214). The task vuln-sync-trivy-db resolves the tag of the
// database's image through that repository in process, makes the repository hold the layer, checks the archive and records the layer's
// digest on the row of the source trivy as the database in use (Q-VULN-173). Nothing is copied anywhere beside the repository, and
// nothing falls back to the publisher (Q-VULN-174): a run that cannot use the repository fails with a code that names why, and the
// database in use stays. A worker reads the layer from the repository (imagescan.LocalDatabase).

// maxDatabaseManifest bounds the manifest of the database's tag (formats/oci.md R-OCI-020).
const maxDatabaseManifest = 4 << 20

// ImageDatabase is the repository layer as the task reads the database's repository; internal/app implements it over the repository
// model, the worker's format host and the item store (R-VULN-044, Q-VULN-172).
type ImageDatabase interface {
	// Resolve resolves the repository by its qualified name for one run, and checks what it must be: it exists, is of the format oci,
	// is local or remote, and is not disabled. A refusal wraps [ErrDatabaseRepositoryInvalid] or [ErrDatabaseRepositoryOff]. jobID
	// names the run's job in the upstream log.
	Resolve(ctx context.Context, repo, jobID string) (DatabaseRepository, error)
}

// DatabaseRepository is the database's repository for one run.
type DatabaseRepository interface {
	// Manifest returns the manifest the tag of the image names: a remote's tag document is fetched or revalidated through the
	// worker's format host as a background request, a local repository's is read from the item store. found is false when the
	// repository does not hold it: a local repository before the push, a remote whose upstream answers 404 or whose include and exclude
	// patterns refuse the name. An error of a remote's upstream wraps [ErrDatabaseUpstream].
	Manifest(ctx context.Context, image, tag string) (body []byte, found bool, err error)
	// Holds reports whether the repository holds the layer, the item <image>/_blobs/<digest>.
	Holds(ctx context.Context, image, digest string) (bool, error)
	// Store makes a remote repository hold the layer: through the format's gate, the engine's integrity check of the digest and the
	// repository's patterns, with the populate record of system:image-database; found is false when the upstream does not have it.
	// A local repository stores nothing and answers whether it holds the layer.
	Store(ctx context.Context, image, digest string) (found bool, err error)
	// Open reads the stored layer through the item store, which is no download (R-STOR-015).
	Open(ctx context.Context, image, digest string) (io.ReadCloser, error)
	// RetentionHours is the repository's own cache period, remote.cache.unusedRetentionHours of a remote: 48 on upstream/scanner-trivy,
	// fixed by the product (Q-VULN-175). 0 keeps every layer: a local repository, which is its operator's to keep, and a remote without
	// a period (Q-VULN-194).
	RetentionHours() int
	// Prune removes the layers and manifests of image stored before before, other than the manifest with the digest manifest and other
	// than content that is the database in use (vuln_image_database_in_use), through the item store's removal with the actor
	// system:image-database, so that the blobs, the format's rows and the audit trail follow the way of every removal; it answers how
	// many files went.
	Prune(ctx context.Context, image, manifest string, before time.Time) (int, error)
}

// The refusals of the database's repository (Q-VULN-174); an implementation of [ImageDatabase] wraps them with what it found.
var (
	// ErrDatabaseRepositoryInvalid: the repository does not exist, is not of the format oci, or is virtual.
	ErrDatabaseRepositoryInvalid = errors.New("the database's repository is no local or remote OCI repository")
	// ErrDatabaseRepositoryOff: the repository is disabled.
	ErrDatabaseRepositoryOff = errors.New("the database's repository is switched off")
	// ErrDatabaseUpstream: a remote repository could not reach its upstream (the engine's failure, its open circuit, offline mode).
	ErrDatabaseUpstream = errors.New("the database's repository cannot reach its registry")
	// ErrDatabaseMissing: the repository does not hold the image at the tag, or the layer its manifest names; the refusal says why (a
	// local repository before the push, a remote whose upstream answers 404 or whose patterns refuse the name).
	ErrDatabaseMissing = errors.New("the database's repository does not hold the database")
)

// DatabaseRefusal is a refusal of the database's repository with what was found: Kind is one of the sentinels above, Detail the
// sentence the source's row shows after the repository and the reference, Err the error below it, if any.
type DatabaseRefusal struct {
	Kind   error
	Detail string
	Err    error
}

func (r *DatabaseRefusal) Error() string { return r.Kind.Error() + ": " + r.Detail }

// Unwrap answers the sentinel and the error below it.
func (r *DatabaseRefusal) Unwrap() []error {
	if r.Err == nil {
		return []error{r.Kind}
	}
	return []error{r.Kind, r.Err}
}

// RunTrivyDB is the handler of vuln-sync-trivy-db. Like the other sources it records what ended a failing run on its row, and it leaves the
// database in use as it is: the images are evaluated against the last good one until a new one has been recorded. jobID is the run's
// job, which the upstream log names.
func (s *Service) RunTrivyDB(ctx context.Context, run Run, jobID string) error {
	if !s.o.Config.Enabled || !s.o.Config.Images.Enabled {
		return run.SetResult(map[string]string{"skipped": "the image scanning is off"})
	}
	var at attempt
	res, err := guarded(ctx, s, store.SourceTrivy, advisory.EcosystemOCI, kindFull, &at, func() (Result, error) {
		return s.syncTrivy(ctx, jobID, &at)
	})
	if err != nil {
		if released(ctx) {
			return context.Cause(ctx)
		}
		if ctx.Err() != nil {
			s.recordEnd(ctx, store.SourceTrivy, advisory.EcosystemOCI, kindFull, at.at, context.Cause(ctx))
			return context.Cause(ctx)
		}
		s.recordFailure(ctx, store.SourceTrivy, advisory.EcosystemOCI, kindFull, at.at, err)
		return err
	}
	return run.SetResult(res)
}

func (s *Service) syncTrivy(ctx context.Context, jobID string, at *attempt) (Result, error) {
	res := Result{Ecosystem: advisory.EcosystemOCI, Kind: kindFull}
	if err := s.o.DB.InTx(ctx, func(tx metaTx) error {
		db, err := store.DBOf(tx)
		if err != nil {
			return err
		}
		if err := s.st.Ensure(ctx, db, store.SourceTrivy, advisory.EcosystemOCI); err != nil {
			return err
		}
		at.at, err = s.st.Attempt(ctx, db, store.SourceTrivy, advisory.EcosystemOCI)
		return err
	}); err != nil {
		return res, err
	}
	var st store.SyncState
	if err := s.o.DB.Read(ctx, func(db q.DBTX) (err error) {
		st, _, err = s.st.Get(ctx, db, store.SourceTrivy, advisory.EcosystemOCI)
		return err
	}); err != nil {
		return res, err
	}
	if s.o.ImageDatabase == nil {
		return res, errors.New("the vulnerability database: this replica reads no repositories")
	}
	cfg := s.o.Config.Images.Database
	dctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	ref := cfg.Repository + " " + cfg.Image + ":" + imagescan.DatabaseTag
	repo, err := s.o.ImageDatabase.Resolve(dctx, cfg.Repository, jobID)
	if err != nil {
		return res, databaseFailure(ref, err)
	}
	manifest, found, err := repo.Manifest(dctx, cfg.Image, imagescan.DatabaseTag)
	if err != nil {
		return res, databaseFailure(ref, err)
	}
	if !found {
		return res, problemf(errs.CodeDatabaseMissing, ref+": the repository holds no such tag", nil)
	}
	layer, err := databaseLayer(manifest)
	if err != nil {
		return res, problemf(errs.CodeDatabaseMissing, ref+": "+err.Error(), nil)
	}
	held, err := repo.Holds(dctx, cfg.Image, layer.Digest)
	if err != nil {
		return res, err
	}
	// The digest's SHA-256 on the row is what the trim's exemption reads: a row without it (written before Q-VULN-191) is recorded again.
	if held && layer.Digest == st.ETag && st.Complete() && "sha256:"+hex.EncodeToString(st.ArchiveSHA256) == layer.Digest {
		res, err = s.trivyUnchanged(ctx, res)
		return s.prune(ctx, res, err, repo, cfg.Image, manifest)
	}
	if limit := int64(cfg.MaxArchiveSize); layer.Size > limit {
		return res, problemf(errs.CodeArchiveTooLarge, fmt.Sprintf("%s: the layer is %d bytes, vulnerabilities.images.database.max_archive_size is %d", ref, layer.Size, limit), nil)
	}
	if !held {
		stored, err := repo.Store(dctx, cfg.Image, layer.Digest)
		if err != nil {
			return res, databaseFailure(ref, err)
		}
		if !stored {
			return res, problemf(errs.CodeDatabaseMissing, fmt.Sprintf("%s: the repository holds no layer %s", ref, layer.Digest), nil)
		}
	}
	md, err := readLayerMetadata(dctx, repo, cfg.Image, layer.Digest)
	if err != nil {
		return res, problemf(errs.CodeArchiveLimit, fmt.Sprintf("%s: the layer %s: %v", ref, layer.Digest, err), nil)
	}
	res, err = s.recordTrivy(ctx, res, layer.Digest, md)
	return s.prune(ctx, res, err, repo, cfg.Image, manifest)
}

// retentionCutoff is the time before which the database's repository lets the layers that are not in use go: its own cache period
// before now, 48 hours on upstream/scanner-trivy (Q-VULN-175). ok is false for a repository without a period, which keeps every layer
// (Q-VULN-194).
func retentionCutoff(hours int, now time.Time) (before time.Time, ok bool) {
	if hours <= 0 {
		return time.Time{}, false
	}
	return now.Add(-time.Duration(hours) * time.Hour), true
}

// prune applies the repository's retention after a run that succeeded, and only then (Q-VULN-194): a run that failed keeps every layer,
// so a publisher or a repository in trouble never costs the installation its last good database or the ones before it. The manifest the
// run read stays, and so does the layer in use. A removal that fails is logged and noted in the run's result; the run itself succeeded.
func (s *Service) prune(ctx context.Context, res Result, err error, repo DatabaseRepository, image string, manifest []byte) (Result, error) {
	if err != nil {
		return res, err
	}
	before, ok := retentionCutoff(repo.RetentionHours(), s.now())
	if !ok {
		return res, nil
	}
	sum := sha256.Sum256(manifest)
	n, perr := repo.Prune(ctx, image, "sha256:"+hex.EncodeToString(sum[:]), before)
	res.Deleted = n
	if perr != nil {
		s.log.WarnContext(ctx, "the older layers of the vulnerability database were not removed", "removed", n, "error", perr)
		res.Note = "the older layers were not removed: " + perr.Error()
	}
	return res, nil
}

// registered is the failure of a code with the kind the code registry gives it: database_repository_invalid and
// database_repository_off are conflicts of the configuration, 409, not failures of an upstream (problemf's 502).
func registered(code, detail string) *failure {
	f := problemf(code, detail, nil)
	if info, ok := errs.Lookup(code); ok {
		f.kind = info.Kind
	}
	return f
}

// databaseFailure maps a refusal of the repository layer to the code of R-VULN-044 and names the repository and the reference in the
// detail; another error (the metadata store's) is the run's as it is.
func databaseFailure(ref string, err error) error {
	detail := err.Error()
	var refusal *DatabaseRefusal
	if errors.As(err, &refusal) {
		detail = refusal.Detail
	}
	switch {
	case errors.Is(err, ErrDatabaseRepositoryInvalid):
		return registered(errs.CodeDatabaseRepositoryInvalid, ref+": "+detail)
	case errors.Is(err, ErrDatabaseRepositoryOff):
		return registered(errs.CodeDatabaseRepositoryOff, ref+": "+detail)
	case errors.Is(err, ErrDatabaseUpstream):
		return problemf(errs.CodeUpstreamFailed, ref+": "+detail, nil)
	case errors.Is(err, ErrDatabaseMissing):
		return problemf(errs.CodeDatabaseMissing, ref+": "+detail, nil)
	}
	return err
}

// databaseDescriptor is the layer of the database as the manifest names it.
type databaseDescriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

// databaseLayer returns the layer of the database's media type that the manifest names. The manifest is untrusted (an upstream's, or
// what an administrator pushed): it is decoded within its bound, and a digest that is not sha256 and 64 hexadecimal digits is refused,
// because it names an item and a directory of the worker's cache.
func databaseLayer(manifest []byte) (databaseDescriptor, error) {
	if len(manifest) > maxDatabaseManifest {
		return databaseDescriptor{}, fmt.Errorf("the manifest is larger than %d bytes", maxDatabaseManifest)
	}
	var m struct {
		Layers []databaseDescriptor `json:"layers"`
	}
	if err := json.Unmarshal(manifest, &m); err != nil {
		return databaseDescriptor{}, fmt.Errorf("the manifest does not decode: %w", err)
	}
	for _, l := range m.Layers {
		if l.MediaType != imagescan.LayerMediaType {
			continue
		}
		if !imagescan.ValidDigest(l.Digest) || l.Size < 0 {
			return databaseDescriptor{}, fmt.Errorf("the database's layer names the digest %q and the size %d", l.Digest, l.Size)
		}
		return l, nil
	}
	return databaseDescriptor{}, errors.New("the manifest holds no layer of the media type " + imagescan.LayerMediaType)
}

// readLayerMetadata reads metadata.json from the stored layer as R-VULN-035 reads an archive: the schema version and a build time.
func readLayerMetadata(ctx context.Context, repo DatabaseRepository, image, digest string) (imagescan.DBMetadata, error) {
	rc, err := repo.Open(ctx, image, digest)
	if err != nil {
		return imagescan.DBMetadata{}, err
	}
	defer func() { _ = rc.Close() }()
	return imagescan.ReadDBMetadata(rc)
}

// recordTrivy makes the layer the database in use, in one transaction: the row of the source with the digest and its SHA-256, a raised
// data epoch, the notification of the change area vuln_data and the wake-up of the scan (Q-VULN-173).
func (s *Service) recordTrivy(ctx context.Context, res Result, digest string, md imagescan.DBMetadata) (Result, error) {
	sum, err := hex.DecodeString(strings.TrimPrefix(digest, "sha256:"))
	if err != nil {
		return res, err
	}
	err = s.o.DB.InTx(ctx, func(tx metaTx) error {
		db, err := store.DBOf(tx)
		if err != nil {
			return err
		}
		cur, err := s.st.Lock(ctx, db, store.SourceTrivy, advisory.EcosystemOCI)
		if err != nil {
			return err
		}
		now := s.now()
		next := cur
		next.State, next.SyncedAt, next.FullSyncedAt = store.StateComplete, now, now
		next.ETag, next.ArchiveSHA256, next.Records = digest, sum, 1
		// The same digest stored again (a layer the repository had lost, or the first run after the upgrade of Q-VULN-191) is the same
		// database: the detections made against it stay current, so the data epoch stays and nothing is detected again.
		same := cur.ETag == digest && cur.Complete()
		if !same {
			next.DataEpoch = cur.DataEpoch + 1
		}
		if err := s.st.Save(ctx, db, next); err != nil {
			return err
		}
		if !same {
			if err := s.o.Changes.Notify(tx, cluster.AreaVulnData, vuln.DataChangePayloadVersion, vuln.DataChange{Ecosystem: advisory.EcosystemOCI, Epoch: next.DataEpoch}); err != nil {
				return err
			}
		}
		if s.o.Jobs == nil {
			return nil
		}
		_, err = s.o.Jobs.Enqueue(ctx, tx, jobs.EnqueueSpec{Kind: vuln.KindImageScan, DedupKey: vuln.DedupImageScan})
		return err
	})
	if err != nil {
		return res, err
	}
	res.Outcome = outcomeChanged
	res.Records = 1
	s.countRun(store.SourceTrivy, advisory.EcosystemOCI, kindFull, outcomeChanged)
	s.log.InfoContext(ctx, "vulnerability database recorded", "digest", digest, "built", md.UpdatedAt)
	return res, nil
}

// trivyUnchanged records a run that found the database in use still current and held by its repository: only the tag's document was
// revalidated. A publisher that stopped publishing shows as stale, because the state keeps the time the source last succeeded and the
// sources API and alert compare that with the age of the data separately.
func (s *Service) trivyUnchanged(ctx context.Context, res Result) (Result, error) {
	err := s.o.DB.InTx(ctx, func(tx metaTx) error {
		db, err := store.DBOf(tx)
		if err != nil {
			return err
		}
		cur, err := s.st.Lock(ctx, db, store.SourceTrivy, advisory.EcosystemOCI)
		if err != nil {
			return err
		}
		cur.SyncedAt = s.now()
		return s.st.Save(ctx, db, cur)
	})
	if err != nil {
		return res, err
	}
	res.Outcome = outcomeUnchanged
	s.countRun(store.SourceTrivy, advisory.EcosystemOCI, kindFull, outcomeUnchanged)
	return res, nil
}
