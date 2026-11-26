package view

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/versionkey"
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
	// path is the question of a file of a version by its path (authz.PathReader.MayReadPath); nil answers no.
	path func(repo qname.Repo, pkg, version, path string) bool
	// hold is the question of the serve point of stored content whether a download of the file would be served to the caller now
	// (WithHold); nil asks nothing.
	hold Hold
}

// Hold answers what a download of the stored file at a path of a repository would answer the caller now: the code of the check that
// would refuse it or that it would wait for (the malware gate, the vulnerability block, the delivery rules, the signature gate), ""
// when it would be served (internal/files Service.Hold over sgate.Serve.Hold).
type Hold func(ctx context.Context, repo qname.Repo, path string) (string, error)

// WithHold returns the scope with the question of the serve point (formats/oci.md R-OCI-040): a member the format read from a file's
// content goes to a caller only while a download of every such file would be served to the caller.
func (sc Scope) WithHold(h Hold) Scope {
	sc.hold = h
	return sc
}

// MayReadPath reports whether the caller may read the file at a canonical path of a version in a repository: the decision the
// format's serving of that file makes for a read (formats.ViewData.ContentPaths). A scope without the question answers no.
func (sc Scope) MayReadPath(repo qname.Repo, pkg, version, path string) bool {
	return sc.path != nil && sc.path(repo, pkg, version, path)
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
	if pr, ok := rs.(authz.PathReader); ok {
		sc.path = pr.MayReadPath
	}
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
	// Deprecated keeps the packages whose latest visible version is deprecated (R-API-085).
	Deprecated bool
	// Signed keeps a package whose latest visible version carries a verified signature (R-ANS-057, Q-ANS-122).
	Signed bool
	// DependsOn keeps a package with a visible version that depends on the package of this normalized name, or on a name it provides
	// (R-API-086); the filter takes one format, the name's.
	DependsOn string
}

// Order is the sort of the list: name, createdAt, updatedAt or relevance (the best match first, R-ANS-057).
type Order struct {
	Sort string
	Desc bool
}

// After is the position after which a page starts: the sort-key values of the last
// package of the previous page.
type After struct {
	Time      time.Time
	Name      string
	Format    string
	Relevance int64
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
	case "relevance":
		return []string{strconv.FormatInt(p.Relevance, 10), p.Key, p.Format}
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
	case "relevance":
		if len(key) != 3 {
			return nil, errBadCursor
		}
		r, err := strconv.ParseInt(key[0], 10, 64)
		if err != nil {
			return nil, errBadCursor
		}
		return &After{Relevance: r, Name: key[1], Format: key[2]}, nil
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
	var qRanks []int32
	var err error
	if f.Q != "" {
		if qIDs, qRanks, err = s.findRanked(ctx, formats, Find{Repos: sc.topo.Texts(universe), Text: f.Q}); err != nil {
			return nil, err
		}
	}
	if f.Keyword != "" {
		if kwIDs, err = s.find(ctx, formats, Find{Repos: sc.topo.Texts(universe), Keyword: f.Keyword}); err != nil {
			return nil, err
		}
	}
	var depIDs []int64
	if f.Deprecated {
		if depIDs, err = s.deprecated(ctx, formats, sc.topo.Texts(universe)); err != nil {
			return nil, err
		}
	}
	var dependIDs []int64
	if f.DependsOn != "" {
		if dependIDs, err = s.dependentVersions(ctx, sc, f.DependsOn, sc.ofFormats(universe, formats)); err != nil {
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
		Q: f.Q, QIds: ids(qIDs), QRanks: ranks(qRanks), Keyword: f.Keyword, KeywordIds: ids(kwIDs), Signed: f.Signed,
		Deprecated: f.Deprecated, DeprecatedIds: ids(depIDs), Depends: f.DependsOn != "", DependsIds: ids(dependIDs),
		HasFrom: f.CreatedFrom != nil, CreatedFrom: stamp(f.CreatedFrom),
		HasTo: f.CreatedTo != nil, CreatedTo: stamp(f.CreatedTo),
	}
	if after != nil {
		p.HasAfter, p.AfterName, p.AfterFormat, p.AfterRelevance = true, after.Name, after.Format, after.Relevance
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
		Repos: []int64{}, QIds: []int64{}, QRanks: []int32{}, KeywordIds: []int64{}, DeprecatedIds: []int64{}, DependsIds: []int64{},
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

// deprecated asks the adapters of the formats for the stored versions they call deprecated (R-API-085), bounded as a search is: the
// statement keeps a package whose latest visible version is among them.
func (s *Service) deprecated(ctx context.Context, formats []string, repos []string) ([]int64, error) {
	var ids []int64
	for _, fm := range formats {
		a, ok := s.adapters.For(fm)
		if !ok {
			continue
		}
		got, err := a.Deprecated(ctx, repos, s.maxMatches)
		if err != nil {
			return nil, err
		}
		ids = append(ids, got...)
	}
	if len(ids) > s.maxMatches {
		return nil, tooBroad("The filter deprecated")
	}
	return ids, nil
}

// findRanked is find with the rank of each version (R-ANS-057): an adapter that is a [Ranker] ranks its matches, every other
// adapter's matches rank 0. The two lists are side by side, as the list statement takes them.
func (s *Service) findRanked(ctx context.Context, formats []string, f Find) ([]int64, []int32, error) {
	var out []int64
	var rs []int32
	for _, fm := range formats {
		a, ok := s.adapters.For(fm)
		if !ok {
			continue
		}
		if r, ok := a.(Ranker); ok {
			got, err := r.FindRanked(ctx, f, s.maxMatches)
			if err != nil {
				return nil, nil, err
			}
			for _, m := range got {
				out, rs = append(out, m.ID), append(rs, int32(min(max(m.Rank, 0), 999))) //nolint:gosec // bounded to 0..999
			}
			continue
		}
		got, err := a.Find(ctx, f, s.maxMatches)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, got...)
		rs = append(rs, make([]int32, len(got))...)
	}
	if len(out) > s.maxMatches {
		return nil, nil, tooBroad("The search term")
	}
	return out, rs, nil
}

// ranks turns a nil slice into an empty one, as [ids] does.
func ranks(in []int32) []int32 {
	if in == nil {
		return []int32{}
	}
	return in
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
	// A list of ids (a search's matches, a keyword's, the deprecated versions, the dependents of dependsOn) is planned for its values: a
	// generic plan, which a prepared statement takes after its fifth run, knows neither its length nor the clause it makes void, and the
	// list filtered by 5,000 dependents ran 1.5 s with it and 0.15 s planned for its values (Q-API-459, docs/developer/packages.md).
	custom := len(p.QIds) > 0 || len(p.KeywordIds) > 0 || len(p.DeprecatedIds) > 0 || len(p.DependsIds) > 0
	if err := s.read(ctx, func(qs *q.Queries) (err error) {
		if custom {
			if err := qs.SearchCustomPlans(ctx); err != nil {
				return err
			}
		}
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
			VersionCount: r.VersionCount, DownloadCount: r.Downloads, Relevance: r.Relevance,
		}
		keys[i] = r.Format + "\x1f" + r.Name
	}
	vrows, err := s.latestCandidates(ctx, sc, keys, withheld, hasRepos, repos)
	if err != nil {
		return nil, err
	}
	// The candidates of each package row: its highest release, its highest pre-release and a remote OCI repository's highest tag
	// (PackageViewLatest); a row with a candidate is a repository that holds a visible version.
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
		pkgs[i].LatestVersion, pkgs[i].LatestVersionID, pkgs[i].LatestRepo = l.Version, l.VersionID, sc.topo.Name(l.RepoID)
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
			pkgs[i].Keywords, pkgs[i].Deprecation = d.Keywords, d.Deprecation
			if kind, ok := d.Metadata["kind"].(string); ok {
				pkgs[i].Kind = kind
			}
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

// latestOf is the latest visible version (R-API-029, Q-API-431): the highest stable version, the highest pre-release while no stable version
// is visible, and while the format ranks none of the visible versions the newest of the others by its time (an image whose tags are
// latest and sha-5ccfd1a alone).
func latestOf(a Adapter, rows []q.PackageViewVersionsRow) (q.PackageViewVersionsRow, bool) {
	class := func(v string) int {
		switch {
		case !ranked(a, v):
			return 0
		case a.Prerelease(v):
			return 1
		}
		return 2
	}
	var best q.PackageViewVersionsRow
	found, bestClass := false, 0
	for _, r := range rows {
		c := class(r.Version)
		switch {
		case !found, c > bestClass:
		case c < bestClass:
			continue
		case c > 0 && a.Compare(r.Version, best.Version) <= 0:
			continue
		case c == 0 && newer(best, r):
			continue
		}
		best, found, bestClass = r, true, c
	}
	return best, found
}

// newer reports whether x is not older than y in the unranked part of the order: by time, then by the version's bytes.
func newer(x, y q.PackageViewVersionsRow) bool {
	if c := x.CreatedAt.Time.Compare(y.CreatedAt.Time); c != 0 {
		return c > 0
	}
	return x.Version >= y.Version
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

// latestCandidates reads, for each package row of the given packages ("<format>\x1f<name>") in the scope, the candidates of its latest
// visible version by the format's stored sort key (PackageViewLatest, Q-API-429): one index walk each, no row per version.
func (s *Service) latestCandidates(ctx context.Context, sc Scope, pkgs []string, withheld []int64, hasRepos bool, repos []int64) ([]q.PackageViewLatestRow, error) {
	var rows []q.PackageViewLatestRow
	err := s.read(ctx, func(qs *q.Queries) (err error) {
		rows, err = qs.PackageViewLatest(ctx, q.PackageViewLatestParams{
			Withheld: ids(withheld), Whole: ids(sc.whole), Rows: keys(sc.rows), Packages: pkgs, Scope: ids(sc.Repos()),
			HasRepos: hasRepos, Repos: ids(repos),
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
	// WithKind asks for the kind of each listed version from the format's metadata (an OCI artifact's, formats/oci.md R-OCI-041).
	WithKind bool
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
		return orderKey(v)
	}
	return []string{v.PublishedAt.UTC().Format(time.RFC3339Nano), v.Version}
}

// pageKey is a version of a page of the version list, with what it is ordered by: the time of its oldest visible row in the order
// of publication, the format's stored sort key in the order of the versions.
type pageKey struct {
	version   string
	published time.Time
	key       []byte
	// unranked says that the format does not rank the version: it is in the second part of the order of the versions (Q-API-431).
	unranked bool
	// digests are the contents the version names in its holders (package_versions.document_sha256, a remote tag's manifest), each once:
	// what the list by content groups by (Q-API-432); empty for a version of a format whose version is no document.
	digests [][]byte
	// pos is the version's place in the unranked part of several holders, read whole (walk.sorted).
	pos int
}

// withDigest adds a content to the version's, once.
func (k *pageKey) withDigest(d []byte) {
	if d == nil {
		return
	}
	for _, have := range k.digests {
		if bytes.Equal(have, d) {
			return
		}
	}
	k.digests = append(k.digests, d)
}

// keysetBatch is the least number of versions one statement of a keyset page asks for: a filter the statement cannot apply (the
// format's pre-release rule) drops versions, and the page asks again from the last one read.
const keysetBatch = 64

// Versions returns at most limit+1 versions after the cursor values, found false when the scope holds no version of the package
// (R-API-029, R-API-010). The order of the format's versions is read a page at a time by keyset queries on the stored sort key, for
// one repository or several (keyPage, Q-API-429); a package that one repository of the scope holds, listed in the order of publication,
// by a keyset query on the creation time (keysetPage, Q-API-417); a package several repositories hold, listed in the order of
// publication, whose version is ordered by its oldest row, reads the versions' names and sorts them here (sortedPage, H-1683). Either
// way only the rows of the page's versions are read with their sizes and downloads.
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
	switch {
	case o.Sort == "version":
		page, found, err = s.orderPage(ctx, sc, a, hs, name, withheld, f, only, o, after, limit)
	case len(hs) == 1:
		page, found, err = s.keysetPage(ctx, sc, a, hs[0], name, withheld, f, only, o, after, limit)
	default:
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
			Version: k.version, Prerelease: a.Prerelease(k.version), PublishedAt: g.published,
			SizeBytes: &files, Repositories: sc.topo.Names(sc.holders(g.repoIDs())), DownloadCount: g.downloads(),
			LastDownloadedAt: g.lastDownload(), Unranked: k.unranked,
			VersionID: g.primary().VersionID, Repo: sc.topo.Name(g.primary().RepoID),
		})
	}
	if err := s.adapterSizes(ctx, sc, a, format, groupOf, out); err != nil {
		return nil, true, err
	}
	if err := s.fillFromMetadata(ctx, sc, a, format, groupOf, out, f); err != nil {
		return nil, true, err
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
	batch := max(limit+1, keysetBatch)
	k := keysetQueryOf(sc, h, name, withheld, o)
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

// keysetQueryOf is what the batches of a keyset page ask of the statements for one holder: a local repository's versions are its rows
// of package_versions, a remote OCI repository's tags its cache entries (R-OCI-036), and a remote repository's two ordered lists of
// one batch are merged; the scope is the whole repository or the versions the caller may read of it.
func keysetQueryOf(sc Scope, h q.PackageViewHoldersRow, name string, withheld []int64, o VersionOrder) keysetQuery {
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
	class, _ := sc.topo.Class(h.RepoID)
	return keysetQuery{
		holder: h, name: name, desc: o.Desc, remote: class == repository.Remote, withheld: ids(withheld), whole: whole,
		allowed: keys(allowed),
	}
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

// keyStart is the cursor's key of a first page: every key is above the empty one and below 0x02 (a key starts with its class, 0x00 or
// 0x01, internal/formats/versionkey).
func keyStart(o VersionOrder) []byte {
	if o.Desc {
		return []byte{0x02}
	}
	return []byte{}
}

// keyBatch reads at most n versions of every holder after the key at, in the order of the keys, merges them and takes each version
// once. Of the merged list the first n distinct versions are complete: a version among them is preceded in its holder's list only by
// versions among them. more says that a holder had more than it gave, so the list is cut after n versions; when no holder had more,
// every version after at is in the list. A holder's rows without a stored key come whole, with the key a's format gives them (keyRows,
// Q-API-430), so they count against no limit.
func (s *Service) keyBatch(ctx context.Context, a Adapter, ks []keysetQuery, at []byte, n int) ([]pageKey, bool, error) {
	var out []pageKey
	more := false
	err := s.read(ctx, func(qs *q.Queries) error {
		out, more = out[:0], false
		for _, k := range ks {
			var err error
			var keyed int
			if out, keyed, err = keyRows(ctx, qs, a, k, at, n, out); err != nil {
				return err
			}
			more = more || keyed >= n
			if !k.remote {
				continue
			}
			before := len(out)
			if out, err = keyTags(ctx, qs, k, at, n, out); err != nil {
				return err
			}
			more = more || len(out)-before >= n
		}
		return nil
	})
	if err != nil || len(out) == 0 {
		return nil, false, err
	}
	desc := ks[0].desc
	slices.SortStableFunc(out, func(x, y pageKey) int {
		if desc {
			return bytes.Compare(y.key, x.key)
		}
		return bytes.Compare(x.key, y.key)
	})
	distinct := out[:1]
	for _, k := range out[1:] {
		if last := &distinct[len(distinct)-1]; bytes.Equal(k.key, last.key) {
			for _, d := range k.digests {
				last.withDigest(d)
			}
			continue
		}
		distinct = append(distinct, k)
	}
	if more && len(distinct) > n {
		distinct = distinct[:n]
	}
	return distinct, more, nil
}

// keyRows appends a batch of the package's version rows after the key at, in the order of the keys, to out, and reports how many of
// them carry a stored key. The statement returns the rows without a stored key after them, every visible one of the package (rows
// written before migration 00116 or by a replica of the release before it, until the background migration version_sort_key has filled
// them, Q-API-430): each gets the key a's format gives it, the one the fill stores, and is kept when it lies after at. So such a row
// takes its place in the order of the versions on every page, and none is dropped or listed twice.
func keyRows(ctx context.Context, qs *q.Queries, a Adapter, k keysetQuery, at []byte, n int, out []pageKey) ([]pageKey, int, error) {
	keyed := 0
	add := func(version string, created time.Time, key, digest []byte) {
		p := pageKey{version: version, published: created, key: key}
		p.withDigest(digest)
		if key != nil {
			keyed++
			out = append(out, p)
			return
		}
		// The ranked part of the order alone: a row its format does not rank is read by the unranked part (Q-API-431).
		if p.key = a.SortKey(version); p.key[0] == versionkey.Version && keyAfter(p.key, at, k.desc) {
			out = append(out, p)
		}
	}
	if k.desc {
		rs, err := qs.PackageViewVersionKeyDesc(ctx, q.PackageViewVersionKeyDescParams{
			PackageID: k.holder.PackageID, AfterKey: at, Withheld: k.withheld, Whole: k.whole, Allowed: k.allowed, MaxRows: rows32(n),
		})
		for _, r := range rs {
			add(r.Version, r.CreatedAt.Time, r.SortKey, r.Digest)
		}
		return out, keyed, err
	}
	rs, err := qs.PackageViewVersionKeyAsc(ctx, q.PackageViewVersionKeyAscParams{
		PackageID: k.holder.PackageID, AfterKey: at, Withheld: k.withheld, Whole: k.whole, Allowed: k.allowed, MaxRows: rows32(n),
	})
	for _, r := range rs {
		add(r.Version, r.CreatedAt.Time, r.SortKey, r.Digest)
	}
	return out, keyed, err
}

// keyTags appends a batch of a remote OCI repository's tags of the name after the key at, in the order of the keys, to out.
func keyTags(ctx context.Context, qs *q.Queries, k keysetQuery, at []byte, n int, out []pageKey) ([]pageKey, error) {
	if k.desc {
		rs, err := qs.PackageViewTagKeyDesc(ctx, q.PackageViewTagKeyDescParams{
			RepoID: k.holder.RepoID, Name: k.name, AfterKey: at, Withheld: k.withheld, Whole: k.whole, Allowed: k.allowed, MaxRows: rows32(n),
		})
		for _, r := range rs {
			out = append(out, pageKey{version: r.Version, published: r.CreatedAt.Time, key: r.SortKey, digests: digestsOf(r.Digest)})
		}
		return out, err
	}
	rs, err := qs.PackageViewTagKeyAsc(ctx, q.PackageViewTagKeyAscParams{
		RepoID: k.holder.RepoID, Name: k.name, AfterKey: at, Withheld: k.withheld, Whole: k.whole, Allowed: k.allowed, MaxRows: rows32(n),
	})
	for _, r := range rs {
		out = append(out, pageKey{version: r.Version, published: r.CreatedAt.Time, key: r.SortKey, digests: digestsOf(r.Digest)})
	}
	return out, err
}

// digestsOf is the content of a version row as pageKey.digests holds it.
func digestsOf(d []byte) [][]byte {
	if d == nil {
		return nil
	}
	return [][]byte{d}
}

// sortedPage reads the names of every visible version of the package, one cheap row per version per repository, groups them, filters
// and sorts them by publication (a version by its oldest row) and cuts the page after the cursor. It is the way for a package that
// several repositories of the scope hold, listed in the order of publication (H-1683).
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
		c := x.published.Compare(y.published)
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
		pos, err := keyCursor(after)
		if err != nil {
			return nil, true, err
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

// fillFromMetadata sets the deprecation and the unlisted mark of the listed versions from the format's summary (R-API-085), and their
// digest, state and kind from the format's metadata (the members `digest`, `state` and `kind` of Describe's details, R-UI-052,
// R-UI-194, R-OCI-041): the adapter is asked once for the page, after the cut; a version absent from the answer, or a format whose
// metadata has no such member, has none. The details are read only for a caller that asks for them (VersionFilter.WithDigest,
// VersionFilter.WithState, VersionFilter.WithKind); the summary always.
func (s *Service) fillFromMetadata(ctx context.Context, sc Scope, a Adapter, format string, groupOf map[string]versionGroup, page []VersionSummary, f VersionFilter) error {
	if len(page) == 0 {
		return nil
	}
	vs := make([]Version, 0, len(page))
	for _, v := range page {
		pri := groupOf[v.Version].primary()
		vs = append(vs, Version{ID: pri.VersionID, Format: format, Repo: sc.topo.Name(pri.RepoID).String(), Package: pri.Name, Version: v.Version})
	}
	fields := FieldSummary
	if f.WithDigest || f.WithState {
		fields |= FieldDetails
	}
	got, err := a.Describe(ctx, vs, fields, func(version string) bool { _, ok := groupOf[version]; return ok })
	if err != nil {
		return err
	}
	for i, v := range vs {
		page[i].Deprecation, page[i].Unlisted = got[v.ID].Deprecation, got[v.ID].Unlisted
		md := got[v.ID].Metadata
		if digest, ok := md["digest"].(string); ok && f.WithDigest {
			page[i].Digest = digest
		}
		if state, ok := md["state"].(string); ok && f.WithState {
			page[i].State = state
		}
		if kind, ok := md["kind"].(string); ok && f.WithKind {
			page[i].Kind = kind
		}
	}
	return nil
}

// Version returns the details of one version, found false when the scope does not
// hold it.
func (s *Service) Version(ctx context.Context, sc Scope, format, name, version string) (VersionDetail, bool, error) {
	g, a, one, ok, err := s.oneVersionHeld(ctx, sc, format, name, version, true)
	if err != nil || !ok {
		return VersionDetail{}, false, err
	}
	pri := g.primary()
	files := pri.SizeBytes
	d := VersionDetail{
		PackageID: ID(format, name), Format: format, Name: pri.DisplayName, Version: version,
		Prerelease: a.Prerelease(version), PublishedAt: g.published,
		SizeBytes: &files, DownloadCount: g.downloads(), LastDownloadedAt: g.lastDownload(), Repo: sc.topo.Name(pri.RepoID),
		VersionID: pri.VersionID, Licenses: []string{}, Keywords: []string{}, Metadata: map[string]any{},
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
		FieldSummary|FieldDetails|FieldFacts, isVisible)
	if err != nil {
		return VersionDetail{}, false, err
	}
	if data, ok := got[pri.VersionID]; ok {
		d.Deprecation, d.Unlisted = data.Deprecation, data.Unlisted
		// The README and the members of the metadata that are wiki content go to a caller who may read the wiki of the repository
		// the version was read from; the artifacts level alone does not give them (R-AUTHZ-034, R-API-058).
		wiki := sc.MayReadWiki(sc.topo.Name(pri.RepoID), name, version)
		d.HasReadme = data.HasReadme && wiki
		if data.Licenses != nil {
			d.Licenses = data.Licenses
		}
		if data.Keywords != nil {
			d.Keywords = data.Keywords
		}
		if data.Metadata != nil {
			d.Metadata = readableMetadata(ctx, sc, sc.topo.Name(pri.RepoID), name, version, data, wiki)
		}
	}
	if a.Fetch() == FetchDocument {
		// The other versions that name the version's content (an image's other tags, formats/oci.md R-OCI-041), read and decided as the
		// list by content reads an entry's aliases (Q-API-432).
		if d.Aliases, d.AliasesMore, err = s.otherVersions(ctx, sc, a, one, name, version, d.Metadata); err != nil {
			return VersionDetail{}, false, err
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

// otherVersions are the visible versions of the package, other than version, that name the content the version's metadata names
// (`digest`, "sha256:<hex>"), in the order of the versions, at most maxAliases; more says that there are more. A version whose metadata
// names no content has none.
func (s *Service) otherVersions(ctx context.Context, sc Scope, a Adapter, held oneHolders, name, version string, md map[string]any) ([]string, bool, error) {
	text, _ := md["digest"].(string)
	digest, ok := sha256Digest(text)
	if !ok {
		return []string{}, false, nil
	}
	w, err := s.newWalk(sc, a, held.hs, name, held.withheld, VersionFilter{}, nil, true, nil)
	if err != nil {
		return nil, false, err
	}
	tags, _, more, _, err := s.imageFacts(ctx, w, [][]byte{digest})
	if err != nil {
		return nil, false, err
	}
	out := []string{}
	for _, t := range tags[string(digest)] {
		if t != version {
			out = append(out, t)
		}
	}
	if len(out) > maxAliases {
		return out[:maxAliases], true, nil
	}
	return out, more[string(digest)], nil
}

// readableMetadata is the metadata of a version without the members the caller may not read: the wiki's for a caller who may not read
// the wiki (R-API-058), and those the format read from a file's content beyond the version's document for a caller who may not read
// that file (an image's configuration, formats/oci.md R-OCI-040) or to whom a download of it would not be served now. Such content
// that is held is named by the member contentHeld ({"code": …}, the code a download of the file would answer, see heldCode).
func readableMetadata(ctx context.Context, sc Scope, repo qname.Repo, name, version string, data Data, wiki bool) map[string]any {
	var drop []string
	if !wiki {
		drop = data.WikiMetadata
	}
	held := ""
	if !mayReadAll(sc, repo, name, version, data.ContentPaths) {
		drop = append(slices.Clone(drop), data.ContentMetadata...)
	} else if held = heldCode(ctx, sc, repo, data); held != "" {
		drop = append(slices.Clone(drop), data.ContentMetadata...)
	}
	if len(drop) == 0 {
		return data.Metadata
	}
	out := make(map[string]any, len(data.Metadata)+1)
	for k, v := range data.Metadata {
		if !slices.Contains(drop, k) {
			out[k] = v
		}
	}
	if held != "" {
		out[MemberContentHeld] = map[string]any{"code": held}
	}
	return out
}

// MemberContentHeld is the member of a version's metadata that says why the content the format read from its files is not shown.
const MemberContentHeld = "contentHeld"

// heldCode is the code of the first file of the version's content whose download would not be served to the caller now, "" when
// every one would be (or the scope asks nothing). A question that fails holds the content as unavailable: it fails closed.
func heldCode(ctx context.Context, sc Scope, repo qname.Repo, data Data) string {
	if sc.hold == nil || len(data.ContentMetadata) == 0 {
		return ""
	}
	for _, p := range data.ContentPaths {
		code, err := sc.hold(ctx, repo, p)
		if err != nil {
			if e, ok := errs.As(err); ok && e.Code != "" {
				return e.Code
			}
			return errs.CodeUnavailable
		}
		if code != "" {
			return code
		}
	}
	return ""
}

// sha256Digest reads "sha256:<64 hex digits>"; ok is false for anything else.
func sha256Digest(text string) ([]byte, bool) {
	hexDigest, named := strings.CutPrefix(text, "sha256:")
	digest, err := hex.DecodeString(hexDigest)
	return digest, named && err == nil && len(digest) == 32
}

// mayReadAll reports whether the caller may read the files at every one of the paths of a version.
func mayReadAll(sc Scope, repo qname.Repo, pkg, version string, paths []string) bool {
	for _, p := range paths {
		if !sc.MayReadPath(repo, pkg, version, p) {
			return false
		}
	}
	return true
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
	return Readme{Source: data.Readme, FileName: data.ReadmeFile, HTML: data.ReadmeHTML}, true, nil
}

// EmbeddedOf reads a file the package of a version embeds (formats/nuget.md R-NUGET-038: "icon", "license") from the version as the
// scope holds it, the copy of the repository the details are read from; found false when the scope does not hold the version, its
// format's packages embed nothing, or it embeds no such file. It is artifacts metadata, read with the version and no wiki.
func (s *Service) EmbeddedOf(ctx context.Context, sc Scope, format, name, version, file string) ([]byte, bool, error) {
	g, a, ok, err := s.oneVersion(ctx, sc, format, name, version, false)
	if err != nil || !ok {
		return nil, false, err
	}
	em, ok := a.(Embedder)
	if !ok {
		return nil, false, nil
	}
	pri := g.primary()
	return em.Embedded(ctx, Version{ID: pri.VersionID, Format: format, Repo: sc.topo.Name(pri.RepoID).String(), Package: pri.Name, Version: version}, file)
}

// oneHolders are the holders of a version's package and the withheld versions of the scope, as oneVersion read them.
type oneHolders struct {
	hs       []q.PackageViewHoldersRow
	withheld []int64
}

func (s *Service) oneVersion(ctx context.Context, sc Scope, format, name, version string, withSize bool) (versionGroup, Adapter, bool, error) {
	g, a, _, ok, err := s.oneVersionHeld(ctx, sc, format, name, version, withSize)
	return g, a, ok, err
}

func (s *Service) oneVersionHeld(ctx context.Context, sc Scope, format, name, version string, withSize bool) (versionGroup, Adapter, oneHolders, bool, error) {
	a, ok := s.adapters.For(format)
	if !ok {
		return versionGroup{}, nil, oneHolders{}, false, nil
	}
	withheld, err := s.withheld(ctx, sc, []string{format}, sc.Repos())
	if err != nil {
		return versionGroup{}, nil, oneHolders{}, false, err
	}
	hs, err := s.holdersOf(ctx, sc, format, name)
	if err != nil || len(hs) == 0 {
		return versionGroup{}, nil, oneHolders{}, false, err
	}
	rows, err := s.versionRows(ctx, sc, packageIDs(hs), []string{version}, withSize, withheld)
	if err != nil {
		return versionGroup{}, nil, oneHolders{}, false, err
	}
	for _, g := range groupRows(rows) {
		if g.version == version {
			return g, a, oneHolders{hs: hs, withheld: withheld}, true, nil
		}
	}
	return versionGroup{}, nil, oneHolders{}, false, nil
}
