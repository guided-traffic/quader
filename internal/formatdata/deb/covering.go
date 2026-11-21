package deb

import (
	"context"

	"github.com/guided-traffic/quader/internal/formats/deb/rows"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// Covering reads the stored release documents whose parsed indexes list the file at a path with its digest (signatures.md R-SIG-030):
// the releases that cover a package of a remote, with the signer the fill's verification recorded.
func (s remoteStore) Covering(ctx context.Context, repoID int64, path string, sha256 [32]byte) ([]rows.Release, error) {
	var list []q.DebRemoteCoveringRow
	err := s.g().read(ctx, nil, func(qs *q.Queries) (err error) {
		list, err = qs.DebRemoteCovering(ctx, q.DebRemoteCoveringParams{RepoID: repoID, Path: path, Sha256: sha256[:]})
		return err
	})
	if err != nil {
		return nil, err
	}
	out := make([]rows.Release, 0, len(list))
	for _, r := range list {
		rel := rows.Release{RepoID: repoID, Dir: r.Dir, Document: r.Document, SHA256: sum32(r.Sha256), VerifiedBy: r.VerifiedBy}
		if r.ReleaseDate.Valid {
			t := r.ReleaseDate.Time.UTC()
			rel.ReleaseDate = &t
		}
		out = append(out, rel)
	}
	return out, nil
}
