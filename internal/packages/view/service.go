package view

import (
	"context"
	"errors"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

// The bounds of what one request decides or matches in process. A request above a
// bound is refused, never answered with a partial set, because a partial set would
// make a count wrong without saying so.
const (
	// DefaultMaxRows bounds the versions of the repositories whose rows the decision
	// point decides one by one (authz.ReadRows).
	DefaultMaxRows = 200_000
	// DefaultMaxMatches bounds the versions a search term may match.
	DefaultMaxMatches = 100_000
)

// Options configure [New].
type Options struct {
	DB       meta.Runner
	Adapters Adapters
	// MaxRows and MaxMatches override the defaults; zero keeps them.
	MaxRows, MaxMatches int
}

// Service is the read model.
type Service struct {
	db         meta.Runner
	adapters   Adapters
	maxRows    int
	maxMatches int
}

// New creates the service.
func New(o Options) (*Service, error) {
	if o.DB == nil || o.Adapters == nil {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "view: a transaction runner and the adapters are required")
	}
	s := &Service{db: o.DB, adapters: o.Adapters, maxRows: o.MaxRows, maxMatches: o.MaxMatches}
	if s.maxRows <= 0 {
		s.maxRows = DefaultMaxRows
	}
	if s.maxMatches <= 0 {
		s.maxMatches = DefaultMaxMatches
	}
	return s, nil
}

// read runs the statements of fn in one transaction with the server's JIT compilation off (Q-API-428): the view's statements read
// package_version_listing, whose estimates the planner cannot know (a remote OCI repository's tags are cache entries behind a view), so
// a statement that runs in milliseconds was costed above jit_above_cost and compiled for 0.3 to 0.5 s (measured on 2026-11-23, H-829).
func (s *Service) read(ctx context.Context, fn func(*q.Queries) error) error {
	return s.db.InTx(ctx, func(tx meta.Tx) error {
		ptx, ok := postgres.Unwrap(tx)
		if !ok {
			return errors.New("view: the transaction is not one of internal/meta/postgres")
		}
		if _, err := ptx.Exec(ctx, "SELECT set_config('jit', 'off', true)"); err != nil {
			return err
		}
		return fn(q.New(ptx))
	})
}

func tooBroad(what string) error {
	return errs.E(errs.Invalid, errs.CodeInvalid, what+" covers more versions than one request decides; narrow it by format, repository or name.")
}

// Scope is what a caller may read, as the statements of the view take it.
type Scope struct {
	topo  Topology
	whole []int64
	rows  []string
	// rowRepos are the repositories that have any of rows, sorted.
	rowRepos        []int64
	virtualReadable map[int64]bool
	// wiki is the wiki question of the caller's read scope (authz.ReadScope.MayReadWiki); nil answers no.
	wiki func(repo qname.Repo, pkg, version string) bool
}

// MayReadWiki reports whether the caller may read the wiki of a package version in a repository (R-AUTHZ-034, R-API-058): the
// question of a management answer that carries wiki content. It asks nothing of the audit: the caller reads the version and asked
// about a part of it.
func (sc Scope) MayReadWiki(repo qname.Repo, pkg, version string) bool {
	return sc.wiki != nil && sc.wiki(repo, pkg, version)
}

// Repos returns the physical repositories the scope has any row of, sorted.
func (sc Scope) Repos() []int64 {
	out := append(slices.Clone(sc.whole), sc.rowRepos...)
	slices.Sort(out)
	return slices.Compact(out)
}

// Topology returns the topology the scope was built on.
func (sc Scope) Topology() Topology { return sc.topo }

// holders returns the repositories that show a version held by the physical
// repositories: those, and the virtual repositories that contain them and that the
// caller may read. Local repositories come first, then remote, then virtual; each
// group is sorted.
func (sc Scope) holders(physical []int64) []int64 {
	set := map[int64]bool{}
	for _, id := range physical {
		set[id] = true
		for _, v := range sc.topo.Containers(id) {
			if sc.virtualReadable[v] {
				set[v] = true
			}
		}
	}
	out := make([]int64, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	rank := func(id int64) int {
		c, _ := sc.topo.Class(id)
		switch c {
		case repository.Local:
			return 0
		case repository.Remote:
			return 1
		}
		return 2
	}
	sort.Slice(out, func(i, j int) bool {
		if ri, rj := rank(out[i]), rank(out[j]); ri != rj {
			return ri < rj
		}
		return sc.topo.Less(out[i], out[j])
	})
	return out
}

// ScopeRequest narrows the rows the scope has to decide one by one: the statements
// of the request intersect with it anyway, so a narrower request decides less.
type ScopeRequest struct {
	Formats    []string
	NamePrefix string
	ExactName  string
}

// Scope builds the scope of a caller: the repositories that need no per-row
// decision, and the readable rows of the others. virtualReadable are the virtual
// repositories the caller may read; they decide which of them a version is shown
// in.
func (s *Service) Scope(ctx context.Context, rs authz.ReadScope, topo Topology, virtualReadable []int64, r ScopeRequest) (Scope, error) {
	sc := Scope{topo: topo, virtualReadable: map[int64]bool{}, wiki: rs.MayReadWiki}
	for _, v := range virtualReadable {
		sc.virtualReadable[v] = true
	}
	var rowIDs []int64
	for _, id := range topo.Physical() {
		switch shape := rs.Shape(topo.Name(id)); {
		case shape == authz.ReadAll && !topo.Restricts(id):
			sc.whole = append(sc.whole, id)
		case shape != authz.ReadNone:
			// A repository with include or exclude patterns serves only the names they
			// admit (R-REPO-005), whatever the caller may read: it is decided row by row.
			rowIDs = append(rowIDs, id)
		}
	}
	if len(rowIDs) == 0 {
		return sc, nil
	}
	formats := s.formatsOf(r.Formats)
	if len(formats) == 0 {
		return sc, nil
	}
	var cands []q.PackageViewCandidatesRow
	err := s.read(ctx, func(qs *q.Queries) (err error) {
		cands, err = qs.PackageViewCandidates(ctx, q.PackageViewCandidatesParams{
			Keys: ids(rowIDs), Formats: keys(formats), NamePrefix: r.NamePrefix, ExactName: r.ExactName, MaxRows: rows32(s.maxRows + 1),
		})
		return err
	})
	if err != nil {
		return Scope{}, err
	}
	if len(cands) > s.maxRows {
		return Scope{}, tooBroad("The repositories you may read only in part")
	}
	for _, c := range cands {
		if !topo.Admits(c.RepoID, c.Name) {
			continue
		}
		if name := topo.Name(c.RepoID); rs.Shape(name) == authz.ReadAll || rs.MayRead(name, c.Name, c.Version) {
			sc.rows = append(sc.rows, rowKey(c.RepoID, c.Name, c.Version))
			sc.rowRepos = append(sc.rowRepos, c.RepoID)
		}
	}
	slices.Sort(sc.rowRepos)
	sc.rowRepos = slices.Compact(sc.rowRepos)
	return sc, nil
}

// keys and ids turn a nil slice into an empty one: the driver sends a nil slice as
// NULL, and "= ANY(NULL)" is NULL, which would drop every row of a statement that
// negates it.
func keys(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func ids(in []int64) []int64 {
	if in == nil {
		return []int64{}
	}
	return in
}

// rows32 is a row limit as the statements take it.
func rows32(n int) int32 { return int32(min(max(n, 0), math.MaxInt32)) }

// rowKey is the entry of the readable rows: "<repository id>\x1f<name>\x1f<version>", the form the statements compare
// (packages.repo_id, never the qualified name they return).
func rowKey(repoID int64, name, version string) string {
	return strconv.FormatInt(repoID, 10) + "\x1f" + name + "\x1f" + version
}

// formatsOf narrows the asked formats to those with an adapter; no asked format
// means every format that has one.
func (s *Service) formatsOf(asked []string) []string {
	have := s.adapters.Formats()
	if len(asked) == 0 {
		return have
	}
	var out []string
	for _, f := range asked {
		if slices.Contains(have, f) && !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}

// Filter narrows the list of packages (R-API-029).
type Filter struct {
	Formats []string
	// Repositories are the keys as sent; a virtual repository stands for its
	// members. HasRepositories distinguishes no filter from a filter that names
	// nothing.
	Repositories    []qname.Repo
	HasRepositories bool
	// Project narrows the list to the physical repositories of that project
	// (Q-UI-129); it combines with Repositories by intersection.
	Project     string
	Q           string
	Keyword     string
	NamePrefix  string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
}

// Order is the sort of the list: name, createdAt or updatedAt.
type Order struct {
	Sort string
	Desc bool
}

// After is the position after which a page starts: the sort-key values of the last
// package of the previous page.
type After struct {
	Time   time.Time
	Name   string
	Format string
}

// Key returns the sort-key values of a package in the order the statement compares
// them, for the cursor. The statement compares the normalized name, which the display
// name of a case-preserving format does not equal.
func (o Order) Key(p Package) []string {
	switch o.Sort {
	case "createdAt":
		return []string{p.CreatedAt.UTC().Format(time.RFC3339Nano), p.Key, p.Format}
	case "updatedAt":
		return []string{p.UpdatedAt.UTC().Format(time.RFC3339Nano), p.Key, p.Format}
	}
	return []string{p.Key, p.Format}
}

// ParseAfter reads the cursor values of [Order.Key].
func (o Order) ParseAfter(key []string) (*After, error) {
	switch o.Sort {
	case "createdAt", "updatedAt":
		if len(key) != 3 {
			return nil, errBadCursor
		}
		t, err := time.Parse(time.RFC3339Nano, key[0])
		if err != nil {
			return nil, errBadCursor
		}
		return &After{Time: t, Name: key[1], Format: key[2]}, nil
	}
	if len(key) != 2 {
		return nil, errBadCursor
	}
	return &After{Name: key[0], Format: key[1]}, nil
}

var errBadCursor = errors.New("view: the cursor does not hold the values of the sort")

// IsBadCursor reports whether err came from [Order.ParseAfter].
func IsBadCursor(err error) bool { return errors.Is(err, errBadCursor) }

// stamp renders a time for a statement that casts it to timestamptz.
func stamp(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// reposOf expands the repository filter to physical repositories.
func (sc Scope) reposOf(f Filter) []int64 {
	if !f.HasRepositories {
		return nil
	}
	return sc.topo.Expand(sc.topo.IDs(f.Repositories))
}

// List returns at most limit+1 packages after the position, in the order, so that
// the caller learns whether a next page exists. Every member counts the versions the
// scope holds.
func (s *Service) List(ctx context.Context, sc Scope, f Filter, o Order, after *After, limit int) ([]Package, error) {
	formats := s.formatsOf(f.Formats)
	if len(formats) == 0 {
		return nil, nil
	}
	f = sc.topo.InProject(f)
	repos := sc.reposOf(f)
	universe := intersectOrAll(sc.Repos(), repos, f.HasRepositories)
	var qIDs, kwIDs []int64
	var err error
	if f.Q != "" {
		if qIDs, err = s.find(ctx, formats, Find{Repos: sc.topo.Texts(universe), Text: f.Q}); err != nil {
			return nil, err
		}
	}
	if f.Keyword != "" {
		if kwIDs, err = s.find(ctx, formats, Find{Repos: sc.topo.Texts(universe), Keyword: f.Keyword}); err != nil {
			return nil, err
		}
	}
	withheld, err := s.withheld(ctx, sc, formats, universe)
	if err != nil {
		return nil, err
	}
	p := q.PackageViewListParams{
		Sort: o.Sort, Descending: o.Desc, MaxRows: rows32(limit + 1),
		Withheld: ids(withheld), Formats: keys(formats), HasRepos: f.HasRepositories, Repos: ids(repos),
		NamePrefix: f.NamePrefix, Whole: ids(sc.whole), Rows: keys(sc.rows),
		Q: f.Q, QIds: ids(qIDs), Keyword: f.Keyword, KeywordIds: ids(kwIDs),
		HasFrom: f.CreatedFrom != nil, CreatedFrom: stamp(f.CreatedFrom),
		HasTo: f.CreatedTo != nil, CreatedTo: stamp(f.CreatedTo),
	}
	if after != nil {
		p.HasAfter, p.AfterName, p.AfterFormat = true, after.Name, after.Format
		p.AfterTime = stamp(&after.Time)
	}
	return s.packages(ctx, sc, p, withheld, f.HasRepositories, repos)
}

// Package returns one package, found false when the scope holds no version of it.
func (s *Service) Package(ctx context.Context, sc Scope, format, name string) (Package, bool, error) {
	if len(s.formatsOf([]string{format})) == 0 {
		return Package{}, false, nil
	}
	universe := sc.Repos()
	withheld, err := s.withheld(ctx, sc, []string{format}, universe)
	if err != nil {
		return Package{}, false, err
	}
	p := q.PackageViewListParams{
		Sort: "name", MaxRows: 1, Withheld: ids(withheld), Formats: []string{format},
		ExactFormat: format, ExactName: name, Whole: ids(sc.whole), Rows: keys(sc.rows),
		Repos: []int64{}, QIds: []int64{}, KeywordIds: []int64{},
	}
	ps, err := s.packages(ctx, sc, p, withheld, false, nil)
	if err != nil || len(ps) == 0 {
		return Package{}, false, err
	}
	return ps[0], true, nil
}

func intersectOrAll(all, only []int64, restricted bool) []int64 {
	if !restricted {
		return all
	}
	var out []int64
	for _, k := range all {
		if slices.Contains(only, k) {
			out = append(out, k)
		}
	}
	return out
}

// find asks the adapters of the formats for the versions that match and keeps the
// ones in scope. The list is a superset the statement intersects with the rows the
// caller may read.
func (s *Service) find(ctx context.Context, formats []string, f Find) ([]int64, error) {
	var ids []int64
	for _, fm := range formats {
		a, ok := s.adapters.For(fm)
		if !ok {
			continue
		}
		got, err := a.Find(ctx, f, s.maxMatches)
		if err != nil {
			return nil, err
		}
		ids = append(ids, got...)
	}
	if len(ids) > s.maxMatches {
		return nil, tooBroad("The search term")
	}
	return ids, nil
}

func (s *Service) withheld(ctx context.Context, sc Scope, formats []string, repos []int64) ([]int64, error) {
	texts := sc.topo.Texts(repos)
	var ids []int64
	for _, fm := range formats {
		a, ok := s.adapters.For(fm)
		if !ok {
			continue
		}
		got, err := a.Withheld(ctx, texts)
		if err != nil {
			return nil, err
		}
		ids = append(ids, got...)
	}
	return ids, nil
}

// packages runs the list statement and decorates its rows.
func (s *Service) packages(ctx context.Context, sc Scope, p q.PackageViewListParams, withheld []int64, hasRepos bool, repos []int64) ([]Package, error) {
	var rows []q.PackageViewListRow
	p.AnyFile, p.Document = s.fetchRules()
	if err := s.read(ctx, func(qs *q.Queries) (err error) {
		rows, err = qs.PackageViewList(ctx, p)
		return err
	}); err != nil || len(rows) == 0 {
		return nil, err
	}
	pkgs := make([]Package, len(rows))
	keys := make([]string, len(rows))
	for i, r := range rows {
		pkgs[i] = Package{
			ID: ID(r.Format, r.Name), Format: r.Format, Name: r.DisplayName, Key: r.Name,
			CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time,
			VersionCount: r.VersionCount, DownloadCount: r.Downloads,
		}
		keys[i] = r.Format + "\x1f" + r.Name
	}
	vrows, err := s.versionNames(ctx, sc, keys, withheld, hasRepos, repos)
	if err != nil {
		return nil, err
	}
	byPkg := map[string][]q.PackageViewVersionsRow{}
	for _, v := range vrows {
		k := v.Format + "\x1f" + v.Name
		byPkg[k] = append(byPkg[k], q.PackageViewVersionsRow{
			VersionID: v.VersionID, Format: v.Format, Name: v.Name, Version: v.Version, CreatedAt: v.CreatedAt, RepoID: v.RepoID,
		})
	}
	// The latest version of each package, by the format's order, and the row to
	// describe it by.
	type latest struct {
		version string
		row     q.PackageViewVersionsRow
	}
	latests := make([]latest, len(pkgs))
	byFormat := map[string][]Version{}
	for i := range pkgs {
		vs := byPkg[keys[i]]
		a, _ := s.adapters.For(pkgs[i].Format)
		l, ok := latestOf(a, vs)
		if !ok {
			continue
		}
		latests[i] = latest{version: l.Version, row: l}
		pkgs[i].LatestVersion = l.Version
		held := make([]int64, 0, len(vs))
		for _, v := range vs {
			held = append(held, v.RepoID)
		}
		pkgs[i].Repositories = sc.topo.Names(sc.holders(held))
		byFormat[pkgs[i].Format] = append(byFormat[pkgs[i].Format], Version{
			ID: l.VersionID, Format: l.Format, Repo: sc.topo.Name(l.RepoID).String(), Package: l.Name, Version: l.Version,
		})
	}
	data := map[int64]Data{}
	for format, vs := range byFormat {
		a, _ := s.adapters.For(format)
		got, err := a.Describe(ctx, vs, FieldSummary, func(string) bool { return true })
		if err != nil {
			return nil, err
		}
		for id, d := range got {
			data[id] = d
		}
	}
	for i := range pkgs {
		if d, ok := data[latests[i].row.VersionID]; ok {
			pkgs[i].Keywords = d.Keywords
			if d.Description != "" {
				desc := d.Description
				pkgs[i].Description = &desc
			}
		}
		if pkgs[i].Keywords == nil {
			pkgs[i].Keywords = []string{}
		}
		if pkgs[i].Repositories == nil {
			pkgs[i].Repositories = []qname.Repo{}
		}
	}
	return pkgs, nil
}

// latestOf is the latest visible stable version, or the latest visible pre-release
// while no stable version is visible (R-API-029).
func latestOf(a Adapter, rows []q.PackageViewVersionsRow) (q.PackageViewVersionsRow, bool) {
	var best q.PackageViewVersionsRow
	found, bestPre := false, false
	for _, r := range rows {
		pre := a.Prerelease(r.Version)
		switch {
		case !found:
		case bestPre && !pre:
		case !bestPre && pre:
			continue
		case a.Compare(r.Version, best.Version) <= 0:
			continue
		}
		best, found, bestPre = r, true, pre
	}
	return best, found
}

// fetchRules names the formats whose versions are fetched as any of their files and those fetched as their document; every other
// format counts the lead file (R-API-079). The statements count a version's downloads by them.
func (s *Service) fetchRules() (anyFile, document []string) {
	anyFile, document = []string{}, []string{}
	for _, f := range s.adapters.Formats() {
		a, ok := s.adapters.For(f)
		if !ok {
			continue
		}
		switch a.Fetch() {
		case FetchAnyFile:
			anyFile = append(anyFile, f)
		case FetchDocument:
			document = append(document, f)
		}
	}
	return anyFile, document
}

// versionRows reads the visible rows of the given versions of the packages with the given ids, with the per-row size and downloads:
// the rows of one page of a version list, or of one version (Q-API-417). versions empty reads every version of the packages.
func (s *Service) versionRows(ctx context.Context, sc Scope, pkgIDs []int64, versions []string, withSize bool, withheld []int64) ([]q.PackageViewVersionsRow, error) {
	var rows []q.PackageViewVersionsRow
	anyFile, document := s.fetchRules()
	err := s.read(ctx, func(qs *q.Queries) (err error) {
		rows, err = qs.PackageViewVersions(ctx, q.PackageViewVersionsParams{
			WithSize: withSize, PackageIds: ids(pkgIDs), Versions: keys(versions), Withheld: ids(withheld),
			Whole: ids(sc.whole), Rows: keys(sc.rows), AnyFile: anyFile, Document: document,
		})
		return err
	})
	return rows, err
}

// versionNames reads the visible versions of the given packages ("<format>\x1f<name>") without the per-row size and downloads, one
// cheap row per version per repository, oldest first.
func (s *Service) versionNames(ctx context.Context, sc Scope, pkgs []string, withheld []int64, hasRepos bool, repos []int64) ([]q.PackageViewVersionNamesRow, error) {
	var rows []q.PackageViewVersionNamesRow
	err := s.read(ctx, func(qs *q.Queries) (err error) {
		rows, err = qs.PackageViewVersionNames(ctx, q.PackageViewVersionNamesParams{
			Withheld: ids(withheld), Packages: pkgs, HasRepos: hasRepos, Repos: ids(repos), Whole: ids(sc.whole), Rows: keys(sc.rows),
		})
		return err
	})
	return rows, err
}

// holdersOf reads the packages of a format and a name in the repositories of the scope, one per repository.
func (s *Service) holdersOf(ctx context.Context, sc Scope, format, name string) ([]q.PackageViewHoldersRow, error) {
	var out []q.PackageViewHoldersRow
	err := s.read(ctx, func(qs *q.Queries) (err error) {
		out, err = qs.PackageViewHolders(ctx, q.PackageViewHoldersParams{Format: format, Name: name, Repos: ids(sc.Repos())})
		return err
	})
	return out, err
}

func packageIDs(hs []q.PackageViewHoldersRow) []int64 {
	out := make([]int64, len(hs))
	for i, h := range hs {
		out[i] = h.PackageID
	}
	return out
}

// A VersionSet is the visible rows of one package, grouped per version.
type versionGroup struct {
	version   string
	rows      []q.PackageViewVersionsRow
	published time.Time
}

func (g versionGroup) primary() q.PackageViewVersionsRow { return g.rows[0] }

func (g versionGroup) downloads() (n int64) {
	for _, r := range g.rows {
		n += r.Downloads
	}
	return n
}

// lastDownload is the latest download of the lead file of the version's rows, nil when none of them was fetched (R-API-065).
func (g versionGroup) lastDownload() *time.Time {
	var out *time.Time
	for _, r := range g.rows {
		if r.LastDownloadAt.Valid && (out == nil || r.LastDownloadAt.Time.After(*out)) {
			at := r.LastDownloadAt.Time
			out = &at
		}
	}
	return out
}

func (g versionGroup) repoIDs() []int64 {
	out := make([]int64, 0, len(g.rows))
	for _, r := range g.rows {
		out = append(out, r.RepoID)
	}
	return out
}

// groupRows groups the rows of one package per version, in the order of the rows (by creation, so the first row of a group is its
// oldest).
func groupRows(rows []q.PackageViewVersionsRow) []versionGroup {
	idx := map[string]int{}
	var out []versionGroup
	for _, r := range rows {
		i, ok := idx[r.Version]
		if !ok {
			i = len(out)
			idx[r.Version] = i
			out = append(out, versionGroup{version: r.Version, published: r.CreatedAt.Time})
		}
		out[i].rows = append(out[i].rows, r)
	}
	return out
}

// VersionFilter narrows the versions of a package.
type VersionFilter struct {
	Prerelease      *bool
	Repositories    []qname.Repo
	HasRepositories bool
	// WithDigest asks for the digest of each listed version from the format's metadata (R-UI-052).
	WithDigest bool
	// WithState asks for the state of each listed version from the format's metadata (R-UI-194).
	WithState bool
}

// VersionOrder is the sort of the version list: publishedAt or version.
type VersionOrder struct {
	Sort string
	Desc bool
}

// VersionKey is the sort-key of a version summary for the cursor: the sort value and
// the version.
func (o VersionOrder) VersionKey(v VersionSummary) []string {
	if o.Sort == "version" {
		return []string{v.Version}
	}
	return []string{v.PublishedAt.UTC().Format(time.RFC3339Nano), v.Version}
}

// pageKey is a version of a page of the version list, with the time it is ordered by: its oldest visible row's.
type pageKey struct {
	version   string
	published time.Time
}

// keysetBatch is the least number of versions one statement of a keyset page asks for: a filter the statement cannot apply (the
// format's pre-release rule) drops versions, and the page asks again from the last one read.
const keysetBatch = 64

// Versions returns at most limit+1 versions after the cursor values, found false when the scope holds no version of the package
// (R-API-029, R-API-010). A package that one repository of the scope holds, listed in the order of publication, is read a page at a time
// by a keyset query (keysetPage, Q-API-417); a package several repositories hold, whose version is one entry over their rows, and the
// order of the format's versions, which no statement can sort, read the versions' names and sort them here (sortedPage). Either way only
// the rows of the page's versions are read with their sizes and downloads.
func (s *Service) Versions(ctx context.Context, sc Scope, format, name string, f VersionFilter, o VersionOrder, after []string, limit int) ([]VersionSummary, bool, error) {
	a, ok := s.adapters.For(format)
	if !ok {
		return nil, false, nil
	}
	withheld, err := s.withheld(ctx, sc, []string{format}, sc.Repos())
	if err != nil {
		return nil, false, err
	}
	hs, err := s.holdersOf(ctx, sc, format, name)
	if err != nil || len(hs) == 0 {
		return nil, false, err
	}
	var only []int64
	if f.HasRepositories {
		only = sc.topo.Expand(sc.topo.IDs(f.Repositories))
	}
	var page []pageKey
	var found bool
	if len(hs) == 1 && o.Sort != "version" {
		page, found, err = s.keysetPage(ctx, sc, a, hs[0], name, withheld, f, only, o, after, limit)
	} else {
		page, found, err = s.sortedPage(ctx, sc, a, format, name, withheld, f, only, o, after, limit)
	}
	if err != nil || !found {
		return nil, found, err
	}
	if len(page) == 0 {
		return []VersionSummary{}, true, nil
	}
	versions := make([]string, len(page))
	for i, k := range page {
		versions[i] = k.version
	}
	rows, err := s.versionRows(ctx, sc, packageIDs(hs), versions, true, withheld)
	if err != nil {
		return nil, true, err
	}
	groupOf := map[string]versionGroup{}
	for _, g := range groupRows(rows) {
		groupOf[g.version] = g
	}
	out := make([]VersionSummary, 0, len(page))
	for _, k := range page {
		g, ok := groupOf[k.version]
		if !ok {
			continue // removed between the two reads; the next request is right
		}
		files := g.primary().SizeBytes
		out = append(out, VersionSummary{
			Version: k.version, Prerelease: a.Prerelease(k.version), PublishedAt: k.published,
			SizeBytes: &files, Repositories: sc.topo.Names(sc.holders(g.repoIDs())), DownloadCount: g.downloads(),
			LastDownloadedAt: g.lastDownload(),
		})
	}
	if err := s.adapterSizes(ctx, sc, a, format, groupOf, out); err != nil {
		return nil, true, err
	}
	if f.WithDigest || f.WithState {
		if err := s.fillFromMetadata(ctx, sc, a, format, groupOf, out, f); err != nil {
			return nil, true, err
		}
	}
	return out, true, nil
}

// keyCursor reads the cursor values of the order of publication: the time and the version.
func keyCursor(after []string) (pageKey, error) {
	if len(after) != 2 {
		return pageKey{}, errBadCursor
	}
	t, err := time.Parse(time.RFC3339Nano, after[0])
	if err != nil {
		return pageKey{}, errBadCursor
	}
	return pageKey{version: after[1], published: t}, nil
}

// startOf is the cursor's time of a first page: every version is below 'infinity' and above '-infinity'.
func startOf(o VersionOrder) string {
	if o.Desc {
		return "infinity"
	}
	return "-infinity"
}

// keysetPage reads a page of the versions of the one package that holds them, in the order of publication, by keyset statements of at
// least keysetBatch versions each until limit+1 versions passed the pre-release filter or none is left. The cursor is the version's
// creation time and its name: the statement's order, with the name compared bytewise, so a version keeps its place while others come and
// go. found is false when the first page of the package without its filters is empty.
func (s *Service) keysetPage(ctx context.Context, sc Scope, a Adapter, h q.PackageViewHoldersRow, name string, withheld []int64, f VersionFilter,
	only []int64, o VersionOrder, after []string, limit int,
) ([]pageKey, bool, error) {
	at := startOf(o)
	var cur string
	if after != nil {
		k, err := keyCursor(after)
		if err != nil {
			return nil, true, err
		}
		at, cur = stamp(&k.published), k.version
	}
	whole := slices.Contains(sc.whole, h.RepoID)
	var allowed []string
	if !whole {
		prefix := strconv.FormatInt(h.RepoID, 10) + "\x1f" + name + "\x1f"
		for _, r := range sc.rows {
			if v, ok := strings.CutPrefix(r, prefix); ok {
				allowed = append(allowed, v)
			}
		}
	}
	batch := max(limit+1, keysetBatch)
	// A local repository's versions are its rows of package_versions, a remote OCI repository's tags its cache entries (R-OCI-036);
	// a repository of another class has neither, and the two ordered lists of one batch are merged (keysetBatchOf).
	class, _ := sc.topo.Class(h.RepoID)
	k := keysetQuery{
		holder: h, name: name, desc: o.Desc, remote: class == repository.Remote, withheld: ids(withheld), whole: whole,
		allowed: keys(allowed),
	}
	fetch := func(at, cur string, n int) ([]pageKey, error) { return s.keysetBatchOf(ctx, k, at, cur, n) }
	var page []pageKey
	if !f.HasRepositories || slices.Contains(only, h.RepoID) {
		for len(page) < limit+1 {
			got, err := fetch(at, cur, batch)
			if err != nil {
				return nil, true, err
			}
			for _, k := range got {
				if f.Prerelease == nil || *f.Prerelease == a.Prerelease(k.version) {
					page = append(page, k)
				}
			}
			if len(got) < batch {
				break
			}
			last := got[len(got)-1]
			at, cur = stamp(&last.published), last.version
		}
	}
	if len(page) > limit+1 {
		page = page[:limit+1]
	}
	if len(page) > 0 || after != nil || (f.Prerelease == nil && !f.HasRepositories) {
		return page, len(page) > 0 || after != nil, nil
	}
	// An empty first page under a filter: the package is found when it has a version at all.
	any, err := fetch(startOf(o), "", 1)
	return page, len(any) > 0, err
}

// keysetQuery is what every batch of a keyset page asks of the statements.
type keysetQuery struct {
	holder       q.PackageViewHoldersRow
	name         string
	desc, remote bool
	withheld     []int64
	whole        bool
	allowed      []string
}

// keysetBatchOf reads at most n versions after the cursor (at, cur) in the order of the query: the package's rows and, for a remote
// repository, its tags, merged in that order.
func (s *Service) keysetBatchOf(ctx context.Context, k keysetQuery, at, cur string, n int) ([]pageKey, error) {
	var out []pageKey
	err := s.read(ctx, func(qs *q.Queries) error {
		out = out[:0]
		var err error
		if out, err = keysetRows(ctx, qs, k, at, cur, n, out); err != nil || !k.remote {
			return err
		}
		out, err = keysetTags(ctx, qs, k, at, cur, n, out)
		return err
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(x, y pageKey) int {
		c := x.published.Compare(y.published)
		if c == 0 {
			c = strings.Compare(x.version, y.version)
		}
		if k.desc {
			return -c
		}
		return c
	})
	if len(out) > n {
		out = out[:n]
	}
	return out, nil
}

// keysetRows appends a batch of the package's version rows to out.
func keysetRows(ctx context.Context, qs *q.Queries, k keysetQuery, at, cur string, n int, out []pageKey) ([]pageKey, error) {
	if k.desc {
		rs, err := qs.PackageViewVersionPageDesc(ctx, q.PackageViewVersionPageDescParams{
			PackageID: k.holder.PackageID, AfterAt: at, AfterVersion: cur, Withheld: k.withheld, Whole: k.whole, Allowed: k.allowed, MaxRows: rows32(n),
		})
		for _, r := range rs {
			out = append(out, pageKey{version: r.Version, published: r.CreatedAt.Time})
		}
		return out, err
	}
	rs, err := qs.PackageViewVersionPageAsc(ctx, q.PackageViewVersionPageAscParams{
		PackageID: k.holder.PackageID, AfterAt: at, AfterVersion: cur, Withheld: k.withheld, Whole: k.whole, Allowed: k.allowed, MaxRows: rows32(n),
	})
	for _, r := range rs {
		out = append(out, pageKey{version: r.Version, published: r.CreatedAt.Time})
	}
	return out, err
}

// keysetTags appends a batch of a remote OCI repository's tags of the name to out.
func keysetTags(ctx context.Context, qs *q.Queries, k keysetQuery, at, cur string, n int, out []pageKey) ([]pageKey, error) {
	if k.desc {
		rs, err := qs.PackageViewTagPageDesc(ctx, q.PackageViewTagPageDescParams{
			RepoID: k.holder.RepoID, Name: k.name, AfterAt: at, AfterVersion: cur, Withheld: k.withheld, Whole: k.whole, Allowed: k.allowed, MaxRows: rows32(n),
		})
		for _, r := range rs {
			out = append(out, pageKey{version: r.Version, published: r.CreatedAt.Time})
		}
		return out, err
	}
	rs, err := qs.PackageViewTagPageAsc(ctx, q.PackageViewTagPageAscParams{
		RepoID: k.holder.RepoID, Name: k.name, AfterAt: at, AfterVersion: cur, Withheld: k.withheld, Whole: k.whole, Allowed: k.allowed, MaxRows: rows32(n),
	})
	for _, r := range rs {
		out = append(out, pageKey{version: r.Version, published: r.CreatedAt.Time})
	}
	return out, err
}

// sortedPage reads the names of every visible version of the package, one cheap row per version per repository, groups them, filters
// and sorts them in the format's order or by publication and cuts the page after the cursor. It is the way for a package that several
// repositories of the scope hold and for the order of the format's versions (H-1683).
func (s *Service) sortedPage(ctx context.Context, sc Scope, a Adapter, format, name string, withheld []int64, f VersionFilter,
	only []int64, o VersionOrder, after []string, limit int,
) ([]pageKey, bool, error) {
	rows, err := s.versionNames(ctx, sc, []string{format + "\x1f" + name}, withheld, false, nil)
	if err != nil || len(rows) == 0 {
		return nil, false, err
	}
	idx := map[string]int{}
	var all []pageKey
	inOnly := map[string]bool{}
	for _, r := range rows { // oldest first: a version's first row is its publication
		if _, ok := idx[r.Version]; !ok {
			idx[r.Version] = len(all)
			all = append(all, pageKey{version: r.Version, published: r.CreatedAt.Time})
		}
		if slices.Contains(only, r.RepoID) {
			inOnly[r.Version] = true
		}
	}
	out := all[:0:0]
	for _, k := range all {
		if f.Prerelease != nil && *f.Prerelease != a.Prerelease(k.version) {
			continue
		}
		if f.HasRepositories && !inOnly[k.version] {
			continue
		}
		out = append(out, k)
	}
	cmp := func(x, y pageKey) int {
		var c int
		if o.Sort == "version" {
			c = a.Compare(x.version, y.version)
		} else {
			c = x.published.Compare(y.published)
		}
		if c == 0 {
			c = strings.Compare(x.version, y.version)
		}
		if o.Desc {
			return -c
		}
		return c
	}
	slices.SortFunc(out, cmp)
	if after != nil {
		var pos pageKey
		switch {
		case o.Sort == "version" && len(after) == 1:
			pos.version = after[0]
		case o.Sort != "version":
			if pos, err = keyCursor(after); err != nil {
				return nil, true, err
			}
		default:
			return nil, true, errBadCursor
		}
		i := sort.Search(len(out), func(i int) bool { return cmp(out[i], pos) > 0 })
		out = out[i:]
	}
	if len(out) > limit+1 {
		out = out[:limit+1]
	}
	return out, true, nil
}

// adapterSizes replaces the summed size of the files of the listed versions with the size the format reports for them, when the
// format reports one (an OCI tag's file is the manifest document, its size the image's, R-OCI-031, Q-OCI-130). The adapter is asked
// once for the page, after the cut; an adapter without Sizer is not asked. The version rows and the sizes are read in separate
// transactions, so a version deleted between them, or replaced (a tag re-pushed, the purge of its old row), is absent from the sizes and
// shows the sum of its files in that one answer; the next request is right.
func (s *Service) adapterSizes(ctx context.Context, sc Scope, a Adapter, format string, groupOf map[string]versionGroup, page []VersionSummary) error {
	z, ok := a.(Sizer)
	if !ok || len(page) == 0 {
		return nil
	}
	vs := make([]Version, 0, len(page))
	for _, v := range page {
		pri := groupOf[v.Version].primary()
		vs = append(vs, Version{ID: pri.VersionID, Format: format, Repo: sc.topo.Name(pri.RepoID).String(), Package: pri.Name, Version: v.Version})
	}
	got, err := z.Sizes(ctx, vs)
	if err != nil {
		return err
	}
	for i, v := range vs {
		if n, ok := got[v.ID]; ok {
			page[i].SizeBytes, page[i].SizePartial = sizeOf(n)
		}
	}
	return nil
}

// sizeOf is a size a format reported as the API carries it (R-API-080): no number when it is not known.
func sizeOf(n Size) (*int64, bool) {
	if n.Unknown {
		return nil, false
	}
	b := max(n.Bytes, 0)
	return &b, n.Partial
}

// fillFromMetadata sets the digest and the state of the listed versions from the format's metadata (the members `digest` and `state` of
// Describe's details, R-UI-052, R-UI-194): the adapter is asked once for the page, after the cut; a version absent from the answer, or
// a format whose metadata has no such member, has none. Only a caller that asks pays for the read (VersionFilter.WithDigest,
// VersionFilter.WithState).
func (s *Service) fillFromMetadata(ctx context.Context, sc Scope, a Adapter, format string, groupOf map[string]versionGroup, page []VersionSummary, f VersionFilter) error {
	if len(page) == 0 {
		return nil
	}
	vs := make([]Version, 0, len(page))
	for _, v := range page {
		pri := groupOf[v.Version].primary()
		vs = append(vs, Version{ID: pri.VersionID, Format: format, Repo: sc.topo.Name(pri.RepoID).String(), Package: pri.Name, Version: v.Version})
	}
	got, err := a.Describe(ctx, vs, FieldDetails, func(version string) bool { _, ok := groupOf[version]; return ok })
	if err != nil {
		return err
	}
	for i, v := range vs {
		md := got[v.ID].Metadata
		if digest, ok := md["digest"].(string); ok && f.WithDigest {
			page[i].Digest = digest
		}
		if state, ok := md["state"].(string); ok && f.WithState {
			page[i].State = state
		}
	}
	return nil
}

// Version returns the details of one version, found false when the scope does not
// hold it.
func (s *Service) Version(ctx context.Context, sc Scope, format, name, version string) (VersionDetail, bool, error) {
	g, a, ok, err := s.oneVersion(ctx, sc, format, name, version, true)
	if err != nil || !ok {
		return VersionDetail{}, false, err
	}
	pri := g.primary()
	files := pri.SizeBytes
	d := VersionDetail{
		PackageID: ID(format, name), Format: format, Name: pri.DisplayName, Version: version,
		Prerelease: a.Prerelease(version), PublishedAt: g.published,
		SizeBytes: &files, DownloadCount: g.downloads(), LastDownloadedAt: g.lastDownload(), Repo: sc.topo.Name(pri.RepoID),
		Licenses: []string{}, Metadata: map[string]any{},
	}
	earliest := map[int64]time.Time{}
	var held []int64
	for _, r := range g.rows {
		held = append(held, r.RepoID)
		if t, ok := earliest[r.RepoID]; !ok || r.CreatedAt.Time.Before(t) {
			earliest[r.RepoID] = r.CreatedAt.Time
		}
	}
	// A virtual repository shows the oldest copy of its members.
	for _, id := range sc.holders(held) {
		class, _ := sc.topo.Class(id)
		at, ok := earliest[id]
		if !ok {
			for _, m := range sc.topo.Members(id) {
				if t, found := earliest[m]; found && (at.IsZero() || t.Before(at)) {
					at = t
				}
			}
		}
		d.Repositories = append(d.Repositories, RepositoryEntry{Repo: sc.topo.Name(id), Class: string(class), PublishedAt: at})
	}
	// The visible versions are read when the format asks about one (npm's dist-tags): a format that does not ask reads none of them, so
	// the detail of one tag of an image with many costs no read of every tag (Q-OCI-267).
	var visible map[string]bool
	isVisible := func(v string) bool {
		if visible == nil {
			visible = map[string]bool{}
			if withheld, err := s.withheld(ctx, sc, []string{format}, sc.Repos()); err == nil {
				if all, err := s.versionNames(ctx, sc, []string{format + "\x1f" + name}, withheld, false, nil); err == nil {
					for _, r := range all {
						visible[r.Version] = true
					}
				}
			}
		}
		return visible[v]
	}
	got, err := a.Describe(ctx, []Version{{ID: pri.VersionID, Format: format, Repo: sc.topo.Name(pri.RepoID).String(), Package: pri.Name, Version: version}},
		FieldSummary|FieldDetails, isVisible)
	if err != nil {
		return VersionDetail{}, false, err
	}
	if data, ok := got[pri.VersionID]; ok {
		// The README and the members of the metadata that are wiki content go to a caller who may read the wiki of the repository
		// the version was read from; the artifacts level alone does not give them (R-AUTHZ-034, R-API-058).
		wiki := sc.MayReadWiki(sc.topo.Name(pri.RepoID), name, version)
		d.HasReadme = data.HasReadme && wiki
		if data.Licenses != nil {
			d.Licenses = data.Licenses
		}
		if data.Metadata != nil {
			d.Metadata = data.Metadata
			if !wiki && len(data.WikiMetadata) > 0 {
				d.Metadata = make(map[string]any, len(data.Metadata))
				for k, v := range data.Metadata {
					if !slices.Contains(data.WikiMetadata, k) {
						d.Metadata[k] = v
					}
				}
			}
		}
	}
	if z, ok := a.(Sizer); ok {
		sizes, err := z.Sizes(ctx, []Version{{ID: pri.VersionID, Format: format, Repo: sc.topo.Name(pri.RepoID).String(), Package: pri.Name, Version: version}})
		if err != nil {
			return VersionDetail{}, false, err
		}
		if n, ok := sizes[pri.VersionID]; ok {
			d.SizeBytes, d.SizePartial = sizeOf(n)
		}
	}
	err = s.read(ctx, func(qs *q.Queries) error {
		files, err := qs.PackageViewFiles(ctx, pri.VersionID)
		for _, f := range files {
			d.Files = append(d.Files, File{Path: f.Path, SizeBytes: f.Size.Int64, SHA256: f.Sha256, Repo: sc.topo.Name(pri.RepoID)})
		}
		return err
	})
	if err != nil {
		return VersionDetail{}, false, err
	}
	if d.Files == nil {
		d.Files = []File{}
	}
	if d.Repositories == nil {
		d.Repositories = []RepositoryEntry{}
	}
	return d, true, nil
}

// ReadmeOf returns the README source of a version, found false when the scope does
// not hold the version or it has none.
func (s *Service) ReadmeOf(ctx context.Context, sc Scope, format, name, version string) (Readme, bool, error) {
	g, a, ok, err := s.oneVersion(ctx, sc, format, name, version, false)
	if err != nil || !ok {
		return Readme{}, false, err
	}
	pri := g.primary()
	if !sc.MayReadWiki(sc.topo.Name(pri.RepoID), name, version) {
		return Readme{}, false, nil // a README the caller may not read is one that is not there (R-API-058)
	}
	got, err := a.Describe(ctx, []Version{{ID: pri.VersionID, Format: format, Repo: sc.topo.Name(pri.RepoID).String(), Package: pri.Name, Version: version}},
		FieldDetails|FieldReadme, func(string) bool { return false })
	if err != nil {
		return Readme{}, false, err
	}
	data, ok := got[pri.VersionID]
	if !ok || data.Readme == "" {
		return Readme{}, false, nil
	}
	return Readme{Source: data.Readme, FileName: data.ReadmeFile}, true, nil
}

func (s *Service) oneVersion(ctx context.Context, sc Scope, format, name, version string, withSize bool) (versionGroup, Adapter, bool, error) {
	a, ok := s.adapters.For(format)
	if !ok {
		return versionGroup{}, nil, false, nil
	}
	withheld, err := s.withheld(ctx, sc, []string{format}, sc.Repos())
	if err != nil {
		return versionGroup{}, nil, false, err
	}
	hs, err := s.holdersOf(ctx, sc, format, name)
	if err != nil || len(hs) == 0 {
		return versionGroup{}, nil, false, err
	}
	rows, err := s.versionRows(ctx, sc, packageIDs(hs), []string{version}, withSize, withheld)
	if err != nil {
		return versionGroup{}, nil, false, err
	}
	for _, g := range groupRows(rows) {
		if g.version == version {
			return g, a, true, nil
		}
	}
	return versionGroup{}, nil, false, nil
}
