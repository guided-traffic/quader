// Package clamav is the data port of the clamav format (Q-ARCH-193, formats/clamav.md R-CLAMAV-002, R-CLAMAV-003): the statements on
// clamav_version (internal/meta/postgres/queries/clamav.sql, migration 00094), behind the Store interface that internal/formats/clamav
// declares. The format runs no SQL; this package holds it, and the composition root binds one value per replica.
//
// Beside the port, the malware scanning's database task (internal/malware/database, malware-scanning.md R-MAL-010, R-MAL-034) uses
// [Store.MarkSets], [Store.List] and [Store.DeleteHistory] in its own transactions. It is a domain package: the values that cross it
// are those of internal/formats/clamav/rows, a leaf.
package clamav

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/backup/tables"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/clamav/rows"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The table of migration 00094 is data: a restore brings back the metadata and the marks of the set in use, which no pass rebuilds
// from the items (R-BAK-004).
func init() {
	tables.Register("clamav_version", tables.Data, tables.Options{})
}

// Options configure [New].
type Options struct {
	// DB reads outside a transaction.
	DB meta.Runner
}

// Store implements the data port of the format.
type Store struct{ o Options }

// New creates the store.
func New(o Options) (*Store, error) {
	if o.DB == nil {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "clamav: the data port needs a database")
	}
	return &Store{o: o}, nil
}

var errForeignTx = errors.New("clamav: the transaction is not one of internal/meta/postgres")

func txQueries(tx meta.Tx) (*q.Queries, error) {
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return nil, errForeignTx
	}
	return q.New(ptx), nil
}

// read runs fn in tx when there is one and in a short transaction of its own otherwise.
func (s *Store) read(ctx context.Context, tx meta.Tx, fn func(qs *q.Queries) error) error {
	run := func(tx meta.Tx) error {
		qs, err := txQueries(tx)
		if err != nil {
			return err
		}
		return fn(qs)
	}
	if tx != nil {
		return run(tx)
	}
	return s.o.DB.InTx(ctx, run)
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// PutContainer implements the port: the side row from the payload of a container's commit.
func (s *Store) PutContainer(ctx context.Context, tx meta.Tx, versionID int64, file string, c rows.Container) error {
	qs, err := txQueries(tx)
	if err != nil {
		return err
	}
	if c.Members == nil {
		c.Members = []rows.Member{}
	}
	md, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return qs.ClamavPutContainer(ctx, q.ClamavPutContainerParams{
		VersionID: versionID, Db: c.DB, Version: int32(c.Version), File: file, BuiltAt: stamp(c.BuiltAt), Metadata: md, //nolint:gosec // a container's version is a header field below 2^31
	})
}

// PutSignature implements the port: the detached signature of a version.
func (s *Store) PutSignature(ctx context.Context, tx meta.Tx, versionID int64, name string, size int64, sig rows.Signature) error {
	qs, err := txQueries(tx)
	if err != nil {
		return err
	}
	return qs.ClamavPutSignature(ctx, q.ClamavPutSignatureParams{
		VersionID: versionID, Db: sig.DB, Version: int32(sig.Version), SignName: name, SignSize: size, SignSignatures: int32(sig.Signatures), //nolint:gosec // bounded by the header and the 64 KiB of a signature file
	})
}

// FilesRemoved implements the port: the versions whose container the cleanup removed become history.
func (s *Store) FilesRemoved(ctx context.Context, tx meta.Tx, versionIDs []int64) error {
	qs, err := txQueries(tx)
	if err != nil {
		return err
	}
	return qs.ClamavFilesRemoved(ctx, versionIDs)
}

// Served implements the port: the version of a database the set in use names.
func (s *Store) Served(ctx context.Context, repoID int64, db string) (rows.Served, bool, error) {
	var out rows.Served
	found := false
	err := s.read(ctx, nil, func(qs *q.Queries) error {
		rs, err := qs.ClamavServed(ctx, q.ClamavServedParams{RepoID: repoID, Db: db})
		if err != nil || len(rs) == 0 {
			return err
		}
		out, found = rows.Served{Version: int(rs[0].Version), File: rs[0].File}, true
		return nil
	})
	return out, found, err
}

// Versions implements the port: the side rows of the given versions.
func (s *Store) Versions(ctx context.Context, versionIDs []int64) ([]rows.Version, error) {
	var out []rows.Version
	err := s.read(ctx, nil, func(qs *q.Queries) error {
		rs, err := qs.ClamavVersions(ctx, versionIDs)
		if err != nil {
			return err
		}
		for _, r := range rs {
			out = append(out, version(r.VersionID, r.Db, r.Version, r.File, r.Metadata, r.SignName, r.SignSize, r.SignSignatures, r.InUse,
				r.Previous, r.StoredAt, r.FilesRemovedAt.Valid, r.FilesRemovedAt.Time))
		}
		return nil
	})
	return out, err
}

// Stored implements the port: the versions of a database, the stored ones first.
func (s *Store) Stored(ctx context.Context, repoID int64, db string, limit int) ([]rows.Version, error) {
	if limit <= 0 {
		limit = 1000
	}
	var out []rows.Version
	err := s.read(ctx, nil, func(qs *q.Queries) error {
		rs, err := qs.ClamavStored(ctx, q.ClamavStoredParams{RepoID: repoID, Db: db, MaxRows: int32(min(limit, 100000))}) //nolint:gosec // bounded above
		if err != nil {
			return err
		}
		for _, r := range rs {
			out = append(out, version(r.VersionID, r.Db, r.Version, r.File, r.Metadata, r.SignName, r.SignSize, r.SignSignatures, r.InUse,
				r.Previous, r.StoredAt, r.FilesRemovedAt.Valid, r.FilesRemovedAt.Time))
		}
		return nil
	})
	return out, err
}

// List reads every side row of a repository, by database and version, in tx (nil: a transaction of its own): what the database
// task's cleanup decides on (R-MAL-034).
func (s *Store) List(ctx context.Context, tx meta.Tx, repoID int64) ([]rows.Version, error) {
	var out []rows.Version
	err := s.read(ctx, tx, func(qs *q.Queries) error {
		rs, err := qs.ClamavList(ctx, repoID)
		if err != nil {
			return err
		}
		for _, r := range rs {
			out = append(out, version(r.VersionID, r.Db, r.Version, r.File, r.Metadata, r.SignName, r.SignSize, r.SignSignatures, r.InUse,
				r.Previous, r.StoredAt, r.FilesRemovedAt.Valid, r.FilesRemovedAt.Time))
		}
		return nil
	})
	return out, err
}

// MarkSets marks, in tx, the versions of the repository that the set in use names (in_use) and those the set before it names
// (previous), and clears the marks of every other version: the database task calls it in the transaction that records the set in use
// and in a put-back (R-MAL-010 step 3). The endpoint serves what in_use names (Q-MAL-99). A version whose files are gone takes no mark.
func (s *Store) MarkSets(ctx context.Context, tx meta.Tx, repoID int64, inUse, previous []rows.DBVersion) error {
	qs, err := txQueries(tx)
	if err != nil {
		return err
	}
	return qs.ClamavMarkSets(ctx, q.ClamavMarkSetsParams{RepoID: repoID, InUse: marks(inUse), Previous: marks(previous)})
}

func marks(vs []rows.DBVersion) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.DB+":"+strconv.Itoa(v.Version))
	}
	return out
}

// DeleteHistory deletes, in tx, the version rows (and their side rows) whose files the cleanup removed before the time, and raises the
// revision of their packages: the end of the history, malware.rescan.cycle after the files went (R-MAL-034). It returns the number of
// versions it deleted.
func (s *Store) DeleteHistory(ctx context.Context, tx meta.Tx, repoID int64, before time.Time) (int, error) {
	qs, err := txQueries(tx)
	if err != nil {
		return 0, err
	}
	pkgs, err := qs.ClamavDeleteHistory(ctx, q.ClamavDeleteHistoryParams{RepoID: repoID, Before: stamp(before)})
	if err != nil {
		return 0, err
	}
	for _, id := range slices.Compact(slices.Sorted(slices.Values(pkgs))) {
		if _, err := qs.BumpPackageRevision(ctx, id); err != nil {
			return 0, err
		}
	}
	return len(pkgs), nil
}

// version builds a row of the port from the columns of a statement. The metadata column is the format's own writing; an unreadable
// one shows no metadata.
func version(id int64, db string, v int32, file string, md json.RawMessage, signName string, signSize int64, signs int32,
	inUse, previous bool, storedAt time.Time, removed bool, removedAt time.Time,
) rows.Version {
	out := rows.Version{
		VersionID: id, DB: db, Version: int(v), File: file, SignName: signName, SignSize: signSize, SignSignatures: int(signs),
		InUse: inUse, Previous: previous, StoredAt: storedAt.UTC(),
	}
	if len(md) > 0 {
		_ = json.Unmarshal(md, &out.Container) //nolint:errcheck // see above
	}
	if removed {
		at := removedAt.UTC()
		out.FilesRemovedAt = &at
	}
	return out
}
