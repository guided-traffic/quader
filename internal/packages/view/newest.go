package view

import (
	"context"
	"sort"

	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/qname"
)

// The newest version of named packages and the verified badge of a list's latest versions: what the documentation search across
// collections (R-ANS-058) and the package list's badge (R-ANS-057) read of the view.

// Newest is the newest version a caller may read of one package in one physical repository.
type Newest struct {
	RepoID  int64
	Repo    qname.Repo
	Name    string
	Version string
}

// PackageKey names a package row: its repository and its name.
type PackageKey struct {
	RepoID int64
	Name   string
}

// NewestOf returns, for each package row asked that a physical repository of the scope holds, the version the package's page opens on
// (R-UI-218): the highest visible release by the format's order, else the highest pre-release, else the newest version the format does
// not rank (latestOf). Two statements whatever the number of rows: the withheld versions and the candidates
// (PackageViewLatestOfPackages, each row reached by its key, one index walk per row and kind). Ordered by repository and name.
func (s *Service) NewestOf(ctx context.Context, sc Scope, format string, pkgs []PackageKey) ([]Newest, error) {
	a, ok := s.adapters.For(format)
	repos := sc.Repos()
	if !ok || len(repos) == 0 || len(pkgs) == 0 {
		return nil, nil
	}
	withheld, err := s.withheld(ctx, sc, []string{format}, repos)
	if err != nil {
		return nil, err
	}
	repoIDs, names := make([]int64, len(pkgs)), make([]string, len(pkgs))
	for i, p := range pkgs {
		repoIDs[i], names[i] = p.RepoID, p.Name
	}
	var rows []q.PackageViewLatestOfPackagesRow
	if err := s.read(ctx, func(qs *q.Queries) (err error) {
		rows, err = qs.PackageViewLatestOfPackages(ctx, q.PackageViewLatestOfPackagesParams{
			RepoIds: repoIDs, Names: names, Withheld: ids(withheld), Whole: ids(sc.whole), Rows: keys(sc.rows), Scope: repos,
		})
		return err
	}); err != nil {
		return nil, err
	}
	type key struct {
		repo int64
		name string
	}
	cands := map[key][]q.PackageViewVersionsRow{}
	var order []key
	for _, r := range rows {
		k := key{r.RepoID, r.Name}
		if _, seen := cands[k]; !seen {
			order = append(order, k)
		}
		cands[k] = append(cands[k], q.PackageViewVersionsRow{
			VersionID: r.VersionID, Format: r.Format, Name: r.Name, Version: r.Version, CreatedAt: r.CreatedAt, RepoID: r.RepoID,
		})
	}
	out := make([]Newest, 0, len(order))
	for _, k := range order {
		if l, ok := latestOf(a, cands[k]); ok {
			out = append(out, Newest{RepoID: k.repo, Repo: sc.topo.Name(k.repo), Name: k.name, Version: l.Version})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RepoID != out[j].RepoID {
			return out[i].RepoID < out[j].RepoID
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Verified returns the badge state of the versions among ids that carry a verified signature (PackageViewVerified, R-ANS-057):
// "verified_internal" or "verified_public" by id; a version without one is absent. One statement.
func (s *Service) Verified(ctx context.Context, versionIDs []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(versionIDs) == 0 {
		return out, nil
	}
	err := s.read(ctx, func(qs *q.Queries) error {
		rows, err := qs.PackageViewVerified(ctx, versionIDs)
		for _, r := range rows {
			out[r.VersionID] = r.State
		}
		return err
	})
	return out, err
}
