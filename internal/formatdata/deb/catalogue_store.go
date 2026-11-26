package deb

import (
	"context"

	"github.com/guided-traffic/quader/internal/formats/deb/rows"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The upstream catalogue's part of the data port (remote-curation.md R-CUR-007, formats/debian.md R-DEB-036): the release directories a
// remote holds with their parsed binary indexes, a search of their rows by words, and the rows of a name. Every method reads; none
// writes.

// CatalogueSuites implements the format's CatalogueStore.
func (s remoteStore) CatalogueSuites(ctx context.Context, repoID int64) ([]rows.CatalogueSuite, error) {
	var dirs []string
	var idx []q.CatalogueDebIndexesRow
	err := s.read(ctx, nil, func(qs *q.Queries) (err error) {
		if dirs, err = qs.CatalogueDebDirs(ctx, repoID); err != nil {
			return err
		}
		idx, err = qs.CatalogueDebIndexes(ctx, repoID)
		return err
	})
	if err != nil {
		return nil, err
	}
	out := make([]rows.CatalogueSuite, 0, len(dirs))
	at := map[string]int{}
	for _, d := range dirs {
		at[d] = len(out)
		out = append(out, rows.CatalogueSuite{Dir: d})
	}
	for _, r := range idx {
		if i, ok := at[r.Dir]; ok {
			out[i].Indexes = append(out[i].Indexes, r.Path)
		}
	}
	return out, nil
}

// CatalogueSearch implements the format's CatalogueStore.
func (s remoteStore) CatalogueSearch(ctx context.Context, cq rows.CatalogueQuery) ([]rows.CatalogueHit, error) {
	var list []q.CatalogueDebSearchRow
	err := s.read(ctx, nil, func(qs *q.Queries) (err error) {
		list, err = qs.CatalogueDebSearch(ctx, q.CatalogueDebSearchParams{
			RepoID: cq.RepoID, Dir: cq.Dir, IndexPath: cq.IndexPath, After: cq.After, Patterns: append([]string{}, cq.Patterns...),
			NamesOnly: cq.NamesOnly, Lim: int32(max(min(cq.Limit, 1000), 1)), //nolint:gosec // bounded above
		})
		return err
	})
	out := make([]rows.CatalogueHit, 0, len(list))
	for _, r := range list {
		out = append(out, rows.CatalogueHit{Name: r.Package, Versions: r.Versions, Description: r.Description})
	}
	return out, err
}

// CatalogueRows implements the format's CatalogueStore.
func (s remoteStore) CatalogueRows(ctx context.Context, repoID int64, name string) ([]rows.Entry, error) {
	var list []q.CatalogueDebRowsRow
	err := s.read(ctx, nil, func(qs *q.Queries) (err error) {
		list, err = qs.CatalogueDebRows(ctx, q.CatalogueDebRowsParams{RepoID: repoID, Name: name})
		return err
	})
	out := make([]rows.Entry, 0, len(list))
	for _, r := range list {
		out = append(out, rows.Entry{
			Dir: r.Dir, IndexPath: r.IndexPath, Package: r.Package, Version: r.Version, Architecture: r.Architecture, Path: r.Path,
			SHA256: sum32(r.Sha256), Size: r.Size, Description: r.Description,
		})
	}
	return out, err
}
