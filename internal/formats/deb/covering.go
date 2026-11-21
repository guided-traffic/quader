package deb

import (
	"context"

	"github.com/guided-traffic/quader/internal/formats"
)

var _ formats.SignatureCoverage = (*Format)(nil)

// Covering implements [formats.SignatureCoverage] (signatures.md R-SIG-030): apt checks the signature of a repository's index, not of
// a package, so the details of a .deb of a remote list each stored release whose parsed index names the file with its SHA-256 (the rows
// of R-DEB-023), with the document whose signatures cover it. A local or a virtual repository signs its indexes with its own key and
// covers nothing here: its details name that key (R-DEB-016, R-DEB-025).
func (f *Format) Covering(ctx context.Context, data any, repo formats.RepoRef, path string, sha256 [32]byte) ([]formats.CoveringDocument, error) {
	if repo.Class != formats.Remote {
		return nil, nil
	}
	st, err := storeOf(data)
	if err != nil {
		return nil, err
	}
	rels, err := st.Covering(ctx, repo.ID, path, sha256)
	if err != nil {
		return nil, err
	}
	out := make([]formats.CoveringDocument, 0, len(rels))
	for _, r := range rels {
		d := formats.CoveringDocument{
			Path: joinDir(r.Dir, r.Document), SHA256: r.SHA256, Name: suiteOf(r.Dir), Verifier: r.VerifiedBy,
		}
		if r.ReleaseDate != nil {
			d.Date = *r.ReleaseDate
		}
		out = append(out, d)
	}
	return out, nil
}
