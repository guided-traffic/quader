package view

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/qname"
)

// The package suggestions of the search field (R-API-061, R-API-062, R-API-064). They are read without the aggregation of the
// list and without enumerating candidate versions: statement A takes the names that start with the text, statement B, while A
// leaves room, the names whose words begin with the words of the text; each reads at most SuggestCandidates rows. A candidate in a
// repository the caller reads whole is readable; one in a repository it reads in part is decided on the package's document and
// the repository's include and exclude patterns (Q-API-275).

// SuggestCandidates bounds the rows each package statement of a suggestion request reads.
const SuggestCandidates = 200

// SuggestQuery is the text of a request as the package statements take it; the API builds it (R-API-062).
type SuggestQuery struct {
	// Text is q trimmed and lower-cased.
	Text string
	// Low and High bound the names that start with Text in byte order; HasHigh is false when no upper bound exists, and then
	// statement A does not run.
	Low, High string
	HasHigh   bool
	// TSQuery is the to_tsquery text of the words of Text; "" when the text has no word, and then statement B does not run.
	TSQuery string
	// Limit is the number of packages the answer shows.
	Limit int
}

// Suggestion is one package of the suggestions, placed in the first repository by qualified name that holds it and that the
// caller reads (R-API-062).
type Suggestion struct {
	ID     string
	Format string
	// Name is the display name; Key the stored, normalized name the rank is computed on.
	Name string
	Key  string
	Repo qname.Repo
	// Rank is 0 for a name equal to the text, 1 for a name that starts with it, 2 for a match by words.
	Rank int
	// Deprecated says that the latest version of the package in Repo is deprecated (R-API-085), as a row of the list marks it. It is
	// read for a repository the caller reads whole; one it reads in part decides on the package alone and marks nothing.
	Deprecated bool
}

// candidate is one row of the package statements.
type candidate struct {
	id, repo                  int64
	name, displayName, format string
}

// Suggest answers the package suggestions of a caller: at most q.Limit packages, best first, and whether a readable match was
// left out. rs is the caller's read scope on the request's index over the physical repositories of topo.
func (s *Service) Suggest(ctx context.Context, rs authz.ReadScope, topo Topology, sq SuggestQuery) ([]Suggestion, bool, error) {
	have := map[string]bool{}
	for _, f := range s.adapters.Formats() {
		have[f] = true
	}
	var repos []int64
	whole := map[int64]bool{}
	for _, id := range topo.Physical() {
		r, _ := topo.Repository(id)
		if !have[r.Format] {
			continue
		}
		switch shape := rs.Shape(r.Name); {
		case shape == authz.ReadAll && !topo.Restricts(id):
			whole[id] = true
			repos = append(repos, id)
		case shape != authz.ReadNone:
			repos = append(repos, id)
		}
	}
	if len(repos) == 0 || (!sq.HasHigh && sq.TSQuery == "") {
		return nil, false, nil
	}
	withheld, err := s.suggestWithheld(ctx, topo, repos)
	if err != nil {
		return nil, false, err
	}
	readable := func(c candidate) bool {
		if whole[c.repo] {
			return true
		}
		return topo.Admits(c.repo, c.name) && rs.MayRead(topo.Name(c.repo), c.name, "")
	}
	found := map[string]Suggestion{}
	keep := func(rows []candidate) {
		for _, c := range rows {
			if !readable(c) {
				continue
			}
			k := c.format + "\x1f" + c.name
			cur, seen := found[k]
			if seen && !topo.Less(c.repo, topo.ID(cur.Repo)) {
				continue
			}
			found[k] = Suggestion{
				ID: ID(c.format, c.name), Format: c.format, Name: c.displayName, Key: c.name, Repo: topo.Name(c.repo),
				Rank: suggestionRank(sq.Text, c.name),
			}
		}
	}
	err = s.read(ctx, func(qs *q.Queries) error {
		if err := qs.SearchCustomPlans(ctx); err != nil {
			return err
		}
		var skip []int64
		if sq.HasHigh {
			rows, err := qs.SearchPackagesByPrefix(ctx, q.SearchPackagesByPrefixParams{
				Repos: ids(repos), Low: sq.Low, High: sq.High, Withheld: ids(withheld), MaxRows: SuggestCandidates,
			})
			if err != nil {
				return err
			}
			cands := make([]candidate, 0, len(rows))
			for _, r := range rows {
				cands = append(cands, candidate{id: r.ID, repo: r.RepoID, name: r.Name, displayName: r.DisplayName, format: r.Format})
				skip = append(skip, r.ID)
			}
			keep(cands)
		}
		if sq.TSQuery == "" || len(found) > sq.Limit {
			return nil
		}
		rows, err := qs.SearchPackagesByWords(ctx, q.SearchPackagesByWordsParams{
			Repos: ids(repos), Query: sq.TSQuery, Skip: ids(skip), Withheld: ids(withheld), MaxRows: SuggestCandidates,
		})
		if err != nil {
			return err
		}
		cands := make([]candidate, 0, len(rows))
		for _, r := range rows {
			cands = append(cands, candidate{id: r.ID, repo: r.RepoID, name: r.Name, displayName: r.DisplayName, format: r.Format})
		}
		keep(cands)
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	out := make([]Suggestion, 0, len(found))
	for _, sg := range found {
		out = append(out, sg)
	}
	slices.SortFunc(out, func(a, b Suggestion) int {
		return cmp.Or(cmp.Compare(a.Rank, b.Rank), strings.Compare(a.Key, b.Key), strings.Compare(a.Format, b.Format))
	})
	more := len(out) > sq.Limit
	out = out[:min(sq.Limit, len(out))]
	if err := s.markDeprecated(ctx, topo, out, whole, withheld); err != nil {
		return nil, false, err
	}
	return out, more, nil
}

// markDeprecated marks the suggestions whose package's latest version in the repository they are placed in is deprecated (R-API-085):
// the candidates of the latest version by the stored sort key (PackageViewLatest) for the suggestions placed in a repository the caller
// reads whole, the latest of them by latestOf, and the formats' summaries of those versions. It reads nothing for an answer without such
// a suggestion.
func (s *Service) markDeprecated(ctx context.Context, topo Topology, out []Suggestion, whole map[int64]bool, withheld []int64) error {
	var pkgs []string
	var repos []int64
	for _, sg := range out {
		if id := topo.ID(sg.Repo); whole[id] {
			pkgs = append(pkgs, sg.Format+"\x1f"+sg.Key)
			repos = append(repos, id)
		}
	}
	if len(pkgs) == 0 {
		return nil
	}
	var rows []q.PackageViewLatestRow
	if err := s.read(ctx, func(qs *q.Queries) (err error) {
		rows, err = qs.PackageViewLatest(ctx, q.PackageViewLatestParams{
			Withheld: ids(withheld), Whole: ids(repos), Rows: []string{}, Packages: pkgs, Scope: ids(repos), HasRepos: true, Repos: ids(repos),
		})
		return err
	}); err != nil {
		return err
	}
	byPkg := map[string][]q.PackageViewVersionsRow{}
	for _, r := range rows {
		k := r.Format + "\x1f" + r.Name + "\x1f" + topo.Name(r.RepoID).String()
		byPkg[k] = append(byPkg[k], q.PackageViewVersionsRow{
			VersionID: r.VersionID, Format: r.Format, Name: r.Name, Version: r.Version, CreatedAt: r.CreatedAt, RepoID: r.RepoID,
		})
	}
	latest := make([]int64, len(out))
	byFormat := map[string][]Version{}
	for i, sg := range out {
		a, ok := s.adapters.For(sg.Format)
		if !ok {
			continue
		}
		l, ok := latestOf(a, byPkg[sg.Format+"\x1f"+sg.Key+"\x1f"+sg.Repo.String()])
		if !ok {
			continue
		}
		latest[i] = l.VersionID
		byFormat[sg.Format] = append(byFormat[sg.Format], Version{ID: l.VersionID, Format: l.Format, Repo: sg.Repo.String(), Package: l.Name, Version: l.Version})
	}
	deprecated := map[int64]bool{}
	for format, vs := range byFormat {
		a, _ := s.adapters.For(format)
		got, err := a.Describe(ctx, vs, FieldSummary, func(string) bool { return true })
		if err != nil {
			return err
		}
		for id, d := range got {
			deprecated[id] = d.Deprecation != nil
		}
	}
	for i := range out {
		out[i].Deprecated = latest[i] != 0 && deprecated[latest[i]]
	}
	return nil
}

// suggestionRank is the rank of a package name (R-API-062): 0 equal to the text, 1 starts with it, 2 a match by words. It is
// computed on the name and not on the statement that found it, so a name that statement A left out for its bound and statement
// B found ranks as it should.
func suggestionRank(text, name string) int {
	lower := strings.ToLower(name)
	switch {
	case lower == text:
		return 0
	case strings.HasPrefix(lower, text):
		return 1
	}
	return 2
}

// suggestWithheld are the stored versions the formats of the repositories do not serve, as the list reads them.
func (s *Service) suggestWithheld(ctx context.Context, topo Topology, repos []int64) ([]int64, error) {
	formats := map[string]bool{}
	for _, id := range repos {
		r, _ := topo.Repository(id)
		formats[r.Format] = true
	}
	texts := topo.Texts(repos)
	var out []int64
	for _, f := range s.adapters.Formats() {
		a, ok := s.adapters.For(f)
		if !ok || !formats[f] {
			continue
		}
		got, err := a.Withheld(ctx, texts)
		if err != nil {
			return nil, err
		}
		out = append(out, got...)
	}
	return out, nil
}
