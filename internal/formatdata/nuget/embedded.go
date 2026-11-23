package nuget

import (
	"context"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/nuget/nupkg"
	"github.com/guided-traffic/quader/internal/formats/nuget/rows"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The reads of the embedded files of a version (R-NUGET-024, Q-NUGET-135): what the .nuspec names, from PostgreSQL, and the bytes of
// one file, out of the stored .nupkg with ranged reads. The statement is in internal/meta/postgres/queries/nuget_embedded.sql.

// EmbeddedNames reads, in one statement, the package paths the .nuspec of a live version of the repository names for its icon, its
// license file and its README. found is false when the repository holds no live version of the number.
func (s *Store) EmbeddedNames(ctx context.Context, repoID int64, lowerID, lowerVersion string) (rows.Embedded, bool, error) {
	var out rows.Embedded
	var found bool
	err := s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rs, err := qs.NuGetEmbeddedNames(ctx, q.NuGetEmbeddedNamesParams{RepositoryID: repoID, LowerID: lowerID, LowerVersion: lowerVersion})
		if err != nil || len(rs) == 0 {
			return err
		}
		out, found = rows.Embedded{Icon: rs[0].Icon, License: rs[0].License, Readme: rs[0].Readme}, true
		return nil
	})
	return out, found, err
}

// EmbeddedFile reads one file of the stored package with the SHA-256 digest and the size given, at most max bytes, with ranged reads of
// the object store through the one reader of a stored package (nupkg.Open): the end record, the central directory and the entry. A
// package that cannot be opened, a path it does not hold, a folder and a file larger than max are no file (nil); an object store that
// fails is an error, so that a failure is never answered as a missing file.
func (s *Store) EmbeddedFile(ctx context.Context, sha256 []byte, size int64, path string, max int64) ([]byte, error) {
	if s.o.Blobs == nil {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "nuget: the data port reads no stored package")
	}
	a, err := nupkg.Open(ctx, s.o.Blobs.ReaderAt(ctx, sha256, size), size, nupkg.Limits{})
	if err != nil {
		if nupkg.IsRefusal(err) {
			return nil, nil
		}
		return nil, err
	}
	e, ok := a.Lookup(path)
	if !ok || e.Dir || e.Size > max {
		return nil, nil
	}
	b, err := a.Read(ctx, e, max)
	if err != nil {
		if nupkg.IsRefusal(err) {
			return nil, nil
		}
		return nil, err
	}
	return b, nil
}
