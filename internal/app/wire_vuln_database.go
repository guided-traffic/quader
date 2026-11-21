package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/guided-traffic/quader/internal/api/scanlisten"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formathost"
	"github.com/guided-traffic/quader/internal/formats/oci"
	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/mirror"
	"github.com/guided-traffic/quader/internal/mirror/discover"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/vuln/imagescan"
	"github.com/guided-traffic/quader/internal/vuln/store"
	vsync "github.com/guided-traffic/quader/internal/vuln/sync"
)

// The image scanning's database lies in a repository of the installation (vulnerability-scanning.md R-VULN-044, ADR-214): the task
// vuln-sync-trivy-db reads it through the repository layer of the worker (imageDatabase, vsync.ImageDatabase); a worker's run looks up
// whether the repository holds the layer in use (databaseInUse, imagescan.Database); and the scan listener of packages serves the layer
// to the loader of every scanner pod (trivyDatabaseSource, scanlisten.TrivyDatabaseSource, R-VULN-049). A remote is read through the
// worker's format host and remote engine as system work (Q-VULN-172), a local repository through the item store; nothing is read from
// anywhere else.

// imageDatabase implements [vsync.ImageDatabase] over the repository model, the mirror host of the process and the item store, each
// read when a run asks: the item store and the mirror host are built after the area that owns the sync.
type imageDatabase struct{ a *App }

var _ vsync.ImageDatabase = imageDatabase{}

// Resolve implements [vsync.ImageDatabase]: the repository must exist, be of the format oci, be local or remote, and not be disabled
// (R-VULN-044). A virtual repository is refused: it adds a resolution the database does not need.
func (d imageDatabase) Resolve(ctx context.Context, key, jobID string) (vsync.DatabaseRepository, error) {
	repo, store, err := d.a.databaseRepository(ctx, key)
	if err != nil {
		return nil, err
	}
	r := &databaseRepo{repo: repo, items: store, db: part[infra](d.a.parts).db}
	if repo.Class == repository.Remote {
		area := part[mirrorEngineArea](d.a.parts)
		if area.b == nil {
			return nil, errors.New("the vulnerability database: this process has no remote engine for background fetches")
		}
		mh, err := area.b.get()
		if err != nil {
			return nil, err
		}
		up, err := mh.ImageDatabaseUpstream(repo, jobID)
		if err != nil {
			return nil, err
		}
		r.host, r.up = mh, up
	}
	return r, nil
}

// databaseRepository resolves the repository the setting names and checks it: the codes of Q-VULN-174 through the sentinels of the sync.
func (a *App) databaseRepository(ctx context.Context, key string) (repository.Repository, *items.Store, error) {
	repos := part[repositoryArea](a.parts).reader
	store, ok := a.itemStore.get()
	if repos == nil || !ok {
		return repository.Repository{}, nil, errors.New("the vulnerability database: the repository layer of this process is not wired yet")
	}
	invalid := func(detail string) error {
		return &vsync.DatabaseRefusal{Kind: vsync.ErrDatabaseRepositoryInvalid, Detail: detail}
	}
	name, err := qname.ParseRepo(key)
	if err != nil {
		return repository.Repository{}, nil, invalid(fmt.Sprintf("%q is not a repository's qualified name", key))
	}
	repo, err := repos.ByName(ctx, name)
	if errs.KindOf(err) == errs.NotFound {
		return repository.Repository{}, nil, invalid("the repository " + key + " does not exist")
	}
	if err != nil {
		return repository.Repository{}, nil, err
	}
	switch {
	case repo.Format != "oci":
		return repository.Repository{}, nil, invalid(fmt.Sprintf("the repository %s is of the format %s, not oci", key, repo.Format))
	case repo.Class != repository.Local && repo.Class != repository.Remote:
		return repository.Repository{}, nil, invalid(fmt.Sprintf("the repository %s is %s, not local or remote", key, repo.Class))
	case repo.Disabled:
		return repository.Repository{}, nil, &vsync.DatabaseRefusal{Kind: vsync.ErrDatabaseRepositoryOff, Detail: "the repository " + key + " is switched off"}
	}
	return repo, store, nil
}

// databaseRepo is the database's repository for one run of the task.
type databaseRepo struct {
	repo  repository.Repository
	items *items.Store
	// db runs the retention's reads and the transactions of its removals.
	db meta.Runner
	// host and up are a remote's: the mirror host of the process and the upstream of the run (MirrorHost.ImageDatabaseUpstream).
	host *formathost.MirrorHost
	up   discover.Upstream
}

var _ vsync.DatabaseRepository = (*databaseRepo)(nil)

// Manifest implements [vsync.DatabaseRepository].
func (r *databaseRepo) Manifest(ctx context.Context, image, tag string) ([]byte, bool, error) {
	if r.up == nil {
		return r.localManifest(ctx, image, tag)
	}
	// A name the repository's patterns refuse costs no request: the repository does not hold it (R-REPO-005).
	admitted, err := r.host.AdmitsName(ctx, r.repo, image)
	if err != nil {
		return nil, false, err
	}
	if !admitted {
		return nil, false, missing("the include and exclude patterns of " + r.repo.Name.String() + " do not admit " + image)
	}
	doc, found, err := r.up.Document(ctx, oci.TagPath(image, tag), "")
	if err != nil {
		return nil, false, upstreamFailed(ctx, err)
	}
	if !found {
		return nil, false, missing("the registry of " + r.repo.Name.String() + " has no such tag")
	}
	return doc, true, nil
}

// missing is the refusal database_missing with what the repository lacks (Q-VULN-174).
func missing(detail string) error {
	return &vsync.DatabaseRefusal{Kind: vsync.ErrDatabaseMissing, Detail: detail}
}

// localManifest reads the tag's item of a local repository: the bytes of the manifest it points at (formats/oci.md R-OCI-007).
func (r *databaseRepo) localManifest(ctx context.Context, image, tag string) ([]byte, bool, error) {
	rc, found, err := openStored(ctx, r.items, r.repo, oci.TagPath(image, tag))
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, missing("nothing was pushed to the local repository under this tag")
	}
	defer func() { _ = rc.Close() }()
	body, err := io.ReadAll(io.LimitReader(rc, oci.MaxManifestBytes+1))
	if err != nil {
		return nil, false, err
	}
	return body, true, nil
}

// Holds implements [vsync.DatabaseRepository].
func (r *databaseRepo) Holds(ctx context.Context, image, digest string) (bool, error) {
	it, found, err := r.items.Live(ctx, items.RepoOf(r.repo), oci.BlobPath(image, digest))
	if err != nil || !found {
		return false, err
	}
	return it.Kind == items.KindFile && items.ContentError(it) == nil, nil
}

// Store implements [vsync.DatabaseRepository]: a remote fills the layer through the gate and the engine (Host.ImageDatabaseUpstream);
// what it stores is read back by Open, so the read here asks for no byte of it.
func (r *databaseRepo) Store(ctx context.Context, image, digest string) (bool, error) {
	if r.up == nil {
		return r.Holds(ctx, image, digest)
	}
	_, found, err := r.up.Artifact(ctx, oci.BlobPath(image, digest), 0)
	var refusal *mirror.RefusedError
	switch {
	case errors.As(err, &refusal):
		// The patterns, the gate or the upstream refused the layer: the repository does not hold it.
		return false, missing("the registry of " + r.repo.Name.String() + " refused the layer " + digest + " (" + refusal.Reason + ")")
	case err != nil:
		return false, upstreamFailed(ctx, err)
	case !found:
		return false, missing("the registry of " + r.repo.Name.String() + " has no layer " + digest)
	}
	return true, nil
}

// RetentionHours implements [vsync.DatabaseRepository]: a remote's own cache period, 0 for a local repository.
func (r *databaseRepo) RetentionHours() int {
	if st, ok := r.repo.Remote(); ok {
		return st.Cache.UnusedRetentionHours
	}
	return 0
}

// pruneBatch is the number of files one transaction of the retention removes.
const pruneBatch = 200

// imageDatabaseActor is who the retention's removals name in the audit trail: the database's task (R-AUTHZ-002).
var imageDatabaseActor = authz.ImageDatabaseActor()

// Prune implements [vsync.DatabaseRepository] (Q-VULN-194): the candidates are the store's (VulnImageDatabaseStale, which never names
// content that is the database in use), the removal is the item store's, in batches of their own transactions, so that the format's hook
// drops a manifest's rows, the blob goes to the collection and every file's removal is audited with the actor system:image-database.
func (r *databaseRepo) Prune(ctx context.Context, image, manifest string, before time.Time) (int, error) {
	db := r.db
	if db == nil {
		return 0, errors.New("the vulnerability database: this process has no metadata store")
	}
	rows := store.NewImages(db)
	keep := []string{oci.ManifestPath(image, manifest)}
	removed, after := 0, ""
	for {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		paths, err := rows.DatabaseStale(ctx, r.repo.NumericID(), image, keep, before, after, pruneBatch)
		if err != nil || len(paths) == 0 {
			return removed, err
		}
		if err := db.InTx(ctx, func(tx meta.Tx) error {
			return r.items.RemoveIn(ctx, tx, items.RemoveSpec{Repo: items.RepoOf(r.repo), Paths: paths, Actor: imageDatabaseActor})
		}); err != nil {
			return removed, err
		}
		removed += len(paths)
		after = paths[len(paths)-1]
	}
}

// Open implements [vsync.DatabaseRepository].
func (r *databaseRepo) Open(ctx context.Context, image, digest string) (io.ReadCloser, error) {
	rc, found, err := openStored(ctx, r.items, r.repo, oci.BlobPath(image, digest))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, imagescan.ErrLayerMissing
	}
	return rc, nil
}

// openStored opens the live file at a path of the repository through the item store; found is false when the repository holds none, or
// only one whose stored object is missing.
func openStored(ctx context.Context, store *items.Store, repo repository.Repository, path string) (io.ReadCloser, bool, error) {
	it, found, err := store.Live(ctx, items.RepoOf(repo), path)
	if err != nil || !found || it.Kind != items.KindFile {
		return nil, false, err
	}
	if it, err = store.Usable(ctx, it); err != nil {
		if items.ContentLost(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	rc, err := store.Open(ctx, it, nil)
	if items.ContentLost(err) {
		return nil, false, nil
	}
	return rc, err == nil, err
}

// upstreamFailed names a failure of a remote's upstream for the code upstream_failed (Q-VULN-174): the engine's error, its open
// circuit, offline mode. The detail is what the engine says to a caller and, in brackets, the cause below it (a refused connection, a
// timeout), so the sources screen reads one sentence and its reason. A context that ended is the run's own end and stays as it is.
func upstreamFailed(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return err
	}
	detail := err.Error()
	if ee, ok := errs.As(err); ok && ee.Detail != "" {
		detail = ee.Detail
		if cause := errors.Unwrap(ee); cause != nil {
			detail += " (" + cause.Error() + ")"
		}
	}
	return &vsync.DatabaseRefusal{Kind: vsync.ErrDatabaseUpstream, Detail: detail, Err: err}
}

// databaseInUse implements [imagescan.Database]: the database in use is the source trivy's digest, and a worker's run checks that the
// repository the setting names holds the item `<image>/_blobs/<digest>`; it reads no byte of it (R-VULN-044, R-VULN-048).
type databaseInUse struct {
	a           *App
	rows        *store.Images
	repo, image string
}

var _ imagescan.Database = databaseInUse{}

// InUse implements [imagescan.Database]. A repository that no longer resolves, or does not hold the layer, is ErrLayerMissing: the scan
// wakes the task, which records why (R-VULN-044).
func (d databaseInUse) InUse(ctx context.Context) (string, error) {
	db, found, err := d.rows.DB(ctx)
	switch {
	case err != nil:
		return "", err
	case !found:
		return "", imagescan.ErrNoDatabase
	case !imagescan.ValidDigest(db.Digest):
		// The digest names an item and every scanner pod's directory: one that is not sha256 and 64 hexadecimal digits decides no path.
		return "", fmt.Errorf("the database in use names %q, which is no sha256 digest", db.Digest)
	}
	_, found, err = d.a.databaseLayer(ctx, d.repo, d.image, db.Digest)
	switch {
	case errors.Is(err, vsync.ErrDatabaseRepositoryInvalid), errors.Is(err, vsync.ErrDatabaseRepositoryOff):
		return "", fmt.Errorf("%w: %w", imagescan.ErrLayerMissing, err)
	case err != nil:
		return "", err
	case !found:
		return "", fmt.Errorf("%w: %s holds no %s", imagescan.ErrLayerMissing, d.repo, oci.BlobPath(d.image, db.Digest))
	}
	return db.Digest, nil
}

// databaseLayer looks up the layer digest in the database's repository: the live, usable item `<image>/_blobs/<digest>`.
func (a *App) databaseLayer(ctx context.Context, key, image, digest string) (items.Item, bool, error) {
	repo, st, err := a.databaseRepository(ctx, key)
	if err != nil {
		return items.Item{}, false, err
	}
	it, found, err := st.Live(ctx, items.RepoOf(repo), oci.BlobPath(image, digest))
	if err != nil || !found || it.Kind != items.KindFile || items.ContentError(it) != nil {
		return items.Item{}, false, err
	}
	return it, true, nil
}

// trivyDatabaseSource is what the scan listener's database routes serve (R-VULN-049): the database in use from the source state, and its
// layer from the item store of the repository the setting names, read through the store, which is no download and moves nothing.
type trivyDatabaseSource struct {
	a           *App
	rows        *store.Images
	repo, image string
}

var _ scanlisten.TrivyDatabaseSource = trivyDatabaseSource{}

// InUse implements [scanlisten.TrivyDatabaseSource].
func (s trivyDatabaseSource) InUse(ctx context.Context) (imagescan.DatabaseListing, error) {
	db, found, err := s.rows.DB(ctx)
	if err != nil || !found || !imagescan.ValidDigest(db.Digest) {
		return imagescan.DatabaseListing{}, err
	}
	out := imagescan.DatabaseListing{Digest: db.Digest}
	if !db.BuiltAt.IsZero() {
		built := db.BuiltAt
		out.BuiltAt = &built
	}
	it, held, err := s.a.databaseLayer(ctx, s.repo, s.image, db.Digest)
	switch {
	case errors.Is(err, vsync.ErrDatabaseRepositoryInvalid), errors.Is(err, vsync.ErrDatabaseRepositoryOff):
	case err != nil:
		return imagescan.DatabaseListing{}, err
	case held:
		out.Size = it.Size
	}
	return out, nil
}

// Open implements [scanlisten.TrivyDatabaseSource]: the layer of digest while it is the database in use and the repository holds it;
// scanlisten.ErrNoTrivyDatabase otherwise.
func (s trivyDatabaseSource) Open(ctx context.Context, digest string) (io.ReadCloser, int64, error) {
	db, found, err := s.rows.DB(ctx)
	if err != nil {
		return nil, 0, err
	}
	if !found || db.Digest != digest {
		return nil, 0, scanlisten.ErrNoTrivyDatabase
	}
	repo, st, err := s.a.databaseRepository(ctx, s.repo)
	switch {
	case errors.Is(err, vsync.ErrDatabaseRepositoryInvalid), errors.Is(err, vsync.ErrDatabaseRepositoryOff):
		return nil, 0, scanlisten.ErrNoTrivyDatabase
	case err != nil:
		return nil, 0, err
	}
	it, held, err := s.a.databaseLayer(ctx, s.repo, s.image, digest)
	if err != nil {
		return nil, 0, err
	}
	if !held {
		return nil, 0, scanlisten.ErrNoTrivyDatabase
	}
	rc, found, err := openStored(ctx, st, repo, oci.BlobPath(s.image, digest))
	if err != nil {
		return nil, 0, err
	}
	if !found {
		return nil, 0, scanlisten.ErrNoTrivyDatabase
	}
	return rc, it.Size, nil
}
