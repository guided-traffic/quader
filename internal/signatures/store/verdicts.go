package store

import (
	"context"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signatures"
)

// PublicProvenance answers, of stored files of a repository (npm's provenance bundles by their canonical paths), the predicate type of each
// whose signature npm itself would verify (signatures.md R-SIG-028, Q-SIG-35): valid, not blocked, and known in the installation's scope
// by the shipped anchor sigstore-public while it is switched on. A path absent from the answer is not served to npm. It reads the stored
// verdicts only.
func (s *Store) PublicProvenance(ctx context.Context, repoID int64, paths []string) (map[string]string, error) {
	out := map[string]string{}
	if len(paths) == 0 || repoID == 0 {
		return out, nil
	}
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rows, err := qs.SignaturePublicProvenance(ctx, q.SignaturePublicProvenanceParams{RepoID: repoID, Paths: paths})
		if err != nil {
			return err
		}
		for _, r := range rows {
			out[r.Path] = r.PredicateType
		}
		return nil
	})
	return out, err
}

// VersionBlobs answers the blobs of the files of versions of a package in repositories, in one statement (R-SIG-034): what a page of the
// version list counts the signatures of, and, for a version a remote holds as a document (an OCI tag), the document's blob (Q-SIG-219).
func (s *Store) VersionBlobs(ctx context.Context, repoIDs []int64, name string, versions []string) ([]signatures.VersionBlob, error) {
	if len(repoIDs) == 0 || len(versions) == 0 {
		return nil, nil
	}
	asked := make(map[string]bool, len(versions))
	for _, v := range versions {
		asked[v] = true
	}
	var out []signatures.VersionBlob
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rows, err := qs.SignatureVersionBlobs(ctx, q.SignatureVersionBlobsParams{RepoIds: repoIDs, Name: name, Versions: versions})
		if err != nil {
			return err
		}
		for _, r := range rows {
			v := r.Version
			if !asked[v] {
				v = r.DisplayVersion
			}
			out = append(out, signatures.VersionBlob{Version: v, RepoID: r.RepoID, Blob: digest(r.BlobSha256), Path: r.Path, Document: r.Document})
		}
		return nil
	})
	return out, err
}
