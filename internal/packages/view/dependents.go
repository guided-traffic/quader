package view

import (
	"context"
	"slices"

	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// MaxDependents bounds the dependents one read answers (R-API-086): the screen lists them and says how many more there are, which the
// package list filtered by dependsOn shows.
const MaxDependents = 50

// Dependent is a package that depends on another (R-API-086): named by its newest visible version that does, with that version's
// relations to it.
type Dependent struct {
	ID      string
	Format  string
	Name    string
	Key     string
	Version string
	// Relations are the version's rows: the kind as its format names it ("" for a format with one list), the range as written, and
	// Via, the name it asks for: the package's own, or a name the package provides (Debian's Provides).
	Relations []Relation
}

// Relation is one relation of a dependent's version.
type Relation struct {
	Kind  string
	Range string
	Via   string
}

// Dependents is the answer of [Service.Dependents].
type Dependents struct {
	Items []Dependent
	// Total counts the dependent packages the caller may read, each once, also those past the bound.
	Total int64
	// Whole is true when the caller reads every repository of the format (of the project, when one is asked) whole: the list is what the
	// installation holds, not only what the caller may read. It says nothing of a dependent and is the same for every package of the
	// format.
	Whole bool
}

// dependentRepos returns the physical repositories of a format the scope reads (whole or row by row), narrowed to a project when one is
// given, and whether the scope reads every physical repository of the format there whole.
func (sc Scope) dependentRepos(format, project string) (repos []int64, whole bool) {
	whole = true
	for _, id := range sc.topo.Physical() {
		r, ok := sc.topo.Repository(id)
		if !ok || r.Format != format || (project != "" && r.Name.Project != project) {
			continue
		}
		in := slices.Contains(sc.whole, id)
		if !in {
			whole = false
		}
		if in || slices.Contains(sc.rowRepos, id) {
			repos = append(repos, id)
		}
	}
	return repos, whole
}

// ofFormats keeps the repositories of the formats.
func (sc Scope) ofFormats(repos []int64, formats []string) []int64 {
	var out []int64
	for _, id := range repos {
		if r, ok := sc.topo.Repository(id); ok && slices.Contains(formats, r.Format) {
			out = append(out, id)
		}
	}
	return out
}

// dependencyNames are the names a package is used under in the repositories: its own, and the names its visible versions provide.
func (s *Service) dependencyNames(ctx context.Context, sc Scope, qs *q.Queries, name string, repos, withheld []int64) ([]string, error) {
	provided, err := qs.PackageViewProvided(ctx, q.PackageViewProvidedParams{
		Repos: ids(repos), Name: name, Withheld: ids(withheld), Whole: ids(sc.whole), Rows: keys(sc.rows),
	})
	if err != nil {
		return nil, err
	}
	return append([]string{name}, provided...), nil
}

// Dependents returns the packages of the format that depend on the package name in the repositories the caller may read (R-API-086,
// Q-API-456, Q-API-458), each by its newest visible version that does, at most limit of them by name, with how many there are. The
// repositories are decided first, from the scope: a dependent in a repository the caller may not read is neither read nor counted. With
// a project, only that project's repositories count, as in the package list. The answer depends on nothing the caller may not read, so a
// name no visible repository holds has its dependents answered all the same.
func (s *Service) Dependents(ctx context.Context, sc Scope, format, name, project string, limit int) (Dependents, error) {
	repos, whole := sc.dependentRepos(format, project)
	out := Dependents{Items: []Dependent{}, Whole: whole}
	if len(repos) == 0 || len(s.formatsOf([]string{format})) == 0 {
		return out, nil
	}
	withheld, err := s.withheld(ctx, sc, []string{format}, repos)
	if err != nil {
		return out, err
	}
	var rows []q.PackageViewDependentsRow
	err = s.read(ctx, func(qs *q.Queries) error {
		names, err := s.dependencyNames(ctx, sc, qs, name, repos, withheld)
		if err != nil {
			return err
		}
		rows, err = qs.PackageViewDependents(ctx, q.PackageViewDependentsParams{
			Repos: repos, Names: names, Self: name, Withheld: ids(withheld), Whole: ids(sc.whole), Rows: keys(sc.rows),
			MaxRows: rows32(limit),
		})
		return err
	})
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		out.Total = r.Total
		d := Dependent{ID: ID(format, r.Name), Format: format, Name: r.DisplayName, Key: r.Name, Version: r.Version}
		for i := range r.Kinds {
			if i < len(r.Ranges) && i < len(r.Vias) {
				d.Relations = append(d.Relations, Relation{Kind: r.Kinds[i], Range: r.Ranges[i], Via: r.Vias[i]})
			}
		}
		out.Items = append(out.Items, d)
	}
	return out, nil
}

// dependentVersions returns the versions of the format in the repositories that depend on the package name (the filter dependsOn of the
// list, R-API-086), bounded as a search is.
func (s *Service) dependentVersions(ctx context.Context, sc Scope, name string, repos []int64) ([]int64, error) {
	var out []int64
	err := s.read(ctx, func(qs *q.Queries) error {
		names, err := s.dependencyNames(ctx, sc, qs, name, repos, nil)
		if err != nil {
			return err
		}
		out, err = qs.PackageViewDependentVersions(ctx, q.PackageViewDependentVersionsParams{
			Repos: ids(repos), Names: names, Self: name, MaxRows: rows32(s.maxMatches + 1),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if len(out) > s.maxMatches {
		return nil, tooBroad("The filter dependsOn")
	}
	return out, nil
}
