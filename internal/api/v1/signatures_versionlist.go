package v1

import (
	"context"
	"slices"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/packages/view"
	"github.com/guided-traffic/quader/internal/signatures"
)

// versionListSignatures is the member signatures of the rows of a page of the version list (R-SIG-034, R-SIG-009): one statement reads
// the blobs of the page's versions in the repositories the rows name, and one count per project of those repositories (a repository of
// upstream counts in the installation's scope) reads what the badge rule needs; no statement runs per row. A version's badge is the
// best of its files' counted signatures in every repository that holds it for the caller; nil while the signatures are off, so the rows
// carry no member.
func (s *Server) versionListSignatures(ctx context.Context, name string, rows []view.VersionSummary) (map[string]*gen.SignatureStatus, error) {
	sv := s.o.Signatures
	if sv.Store == nil || !sv.Enabled || sv.Repos == nil || len(rows) == 0 {
		return nil, nil
	}
	ids, byProject, err := s.versionListRepositories(ctx, sv, rows)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	versions := make([]string, 0, len(rows))
	for _, v := range rows {
		versions = append(versions, v.Version)
	}
	blobs, err := sv.Store.VersionBlobs(ctx, ids, name, versions)
	if err != nil {
		return nil, err
	}
	counted := map[int64]map[[32]byte][]signatures.Counted{} // per repository: the counted signatures of each subject
	for project, repos := range byProject {
		var subjects [][32]byte
		for _, b := range blobs {
			if slices.Contains(repos, b.RepoID) {
				subjects = append(subjects, b.Blob)
			}
		}
		if len(subjects) == 0 {
			continue
		}
		got, err := sv.Store.Counts(ctx, signatures.ViewQuery{RepoIDs: repos, Subjects: subjects, ProjectID: project})
		if err != nil {
			return nil, err
		}
		for _, r := range repos {
			counted[r] = got
		}
	}
	perVersion := map[string][]signatures.Counted{}
	for _, b := range blobs {
		perVersion[b.Version] = append(perVersion[b.Version], counted[b.RepoID][b.Blob]...)
	}
	out := make(map[string]*gen.SignatureStatus, len(rows))
	for _, v := range rows {
		state, stale := signatures.Badge(perVersion[v.Version])
		out[v.Version] = &gen.SignatureStatus{State: gen.SignatureBadgeState(state), Stale: stale}
	}
	return out, nil
}

// versionListRepositories resolves the repositories the rows name: their IDs, and the IDs grouped by the project whose scope judges them.
func (s *Server) versionListRepositories(ctx context.Context, sv SignatureServices, rows []view.VersionSummary) ([]int64, map[string][]int64, error) {
	var ids []int64
	byProject := map[string][]int64{}
	for _, v := range rows {
		for _, key := range v.Repositories {
			repo, err := sv.Repos.ByName(ctx, key)
			if err != nil {
				continue // a repository gone since the view read it counts nothing
			}
			id := repo.NumericID()
			if slices.Contains(ids, id) {
				continue
			}
			ids = append(ids, id)
			p := scopeProject(repo)
			byProject[p] = append(byProject[p], id)
		}
	}
	return ids, byProject, nil
}
