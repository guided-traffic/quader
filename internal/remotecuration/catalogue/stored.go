package catalogue

import (
	"context"
	"errors"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// Stored reads which versions of a page's packages a remote repository holds (R-CUR-009: a row's state stored), from PostgreSQL in one
// statement. It is Quader's own state, read at answer time and never cached.
type Stored struct {
	Runner meta.Runner
}

// maxStoredNames bounds the names of one read: a page holds at most 200.
const maxStoredNames = 500

// Versions answers, per name, the versions the repository holds; a name it holds no version of is absent.
func (s Stored) Versions(ctx context.Context, repoID int64, names []string) (map[string][]string, error) {
	out := map[string][]string{}
	if s.Runner == nil || len(names) == 0 {
		return out, nil
	}
	if len(names) > maxStoredNames {
		return nil, errors.New("catalogue: too many names for one read of the stored versions")
	}
	err := s.Runner.InTx(ctx, func(tx meta.Tx) error {
		ptx, ok := postgres.Unwrap(tx)
		if !ok {
			return errors.New("catalogue: the transaction is not PostgreSQL's")
		}
		rows, err := q.New(ptx).CatalogueStoredVersions(ctx, q.CatalogueStoredVersionsParams{RepoID: repoID, Names: names})
		if err != nil {
			return err
		}
		for _, r := range rows {
			out[r.Name] = append(out[r.Name], r.Version)
		}
		return nil
	})
	return out, err
}
