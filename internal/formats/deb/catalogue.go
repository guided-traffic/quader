package deb

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/deb/control"
	"github.com/guided-traffic/quader/internal/formats/deb/rows"
	"github.com/guided-traffic/quader/internal/formats/deb/version"
	"github.com/guided-traffic/quader/internal/mirror/discover"
	"github.com/guided-traffic/quader/internal/openpgp"
)

// The upstream catalogue of a Debian remote (remote-curation.md R-CUR-007, formats/debian.md R-DEB-036). The archive format has no search
// and no listing: apt search is local, over the Packages indexes. Quader answers the same over the indexes the remote has parsed
// (R-DEB-023): the words AND-ed, each a case-insensitive substring of the name, its Provides or its short description, or of the name
// and Provides only (Q-CUR-16); the levels suite, component and architecture of the releases it holds; a package's rows with their
// sizes and digests. Nothing is read from the upstream but the release of a suite whose level lists nothing, which tells a suite,
// a component or an architecture the upstream does not have from one Quader holds no index of (Q-CUR-165).

// CatalogueStore is the upstream catalogue's part of the data port.
type CatalogueStore interface {
	// CatalogueSuites lists the release directories the remote holds a release of (not recorded absent), each with the binary indexes
	// it has parsed below it.
	CatalogueSuites(ctx context.Context, repoID int64) ([]rows.CatalogueSuite, error)
	// CatalogueSearch lists the distinct names of the parsed binary indexes that the query matches, in byte order after q.After, at
	// most q.Limit.
	CatalogueSearch(ctx context.Context, q rows.CatalogueQuery) ([]rows.CatalogueHit, error)
	// CatalogueRows lists every row of a name in the parsed binary indexes.
	CatalogueRows(ctx context.Context, repoID int64, name string) ([]rows.Entry, error)
}

var _ formats.Catalogue = remoteAdapter{}

// CataloguePattern is the ILIKE pattern of a word: a substring, with the pattern's own wildcards and its escape escaped.
func CataloguePattern(word string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(word) + "%"
}

func (a remoteAdapter) catalogueStore(u discover.Upstream) (CatalogueStore, int64, error) {
	ider, ok := u.(discover.RepositoryIDer)
	st, ok2 := a.f.mirrorStoreOfBound().(CatalogueStore)
	if !ok || !ok2 {
		return nil, 0, errs.E(errs.Internal, errs.CodeInternal, "The catalogue of the Debian format has no data port or no repository to read.")
	}
	return st, ider.RepositoryID(), nil
}

// CatalogueCaps implements [discover.Catalogue].
func (remoteAdapter) CatalogueCaps(context.Context, discover.Upstream) (discover.CatalogueCaps, error) {
	return discover.CatalogueCaps{
		Search: discover.SearchIndex,
		SearchNote: "Quader searches the indexes the remote has read, as apt search does: every word in the name, its Provides or its short " +
			"description; a suite whose indexes are not read is not searched.",
		Levels: []string{"suite", "component", "architecture"}, Sizes: "known",
	}, nil
}

// Search implements [discover.Catalogue]: one page of names by name, after the cursor's last name.
func (a remoteAdapter) Search(ctx context.Context, u discover.Upstream, q discover.SearchQuery) (discover.SearchPage, error) {
	st, repoID, err := a.catalogueStore(u)
	if err != nil {
		return discover.SearchPage{}, err
	}
	words := strings.Fields(strings.ToLower(q.Text))
	if len(words) == 0 || len(words) > 10 {
		return discover.SearchPage{}, errs.E(errs.Invalid, errs.CodeValidation, "A search of a Debian remote takes 1 to 10 words.")
	}
	cq := rows.CatalogueQuery{RepoID: repoID, NamesOnly: q.NamesOnly, After: lastName(q.Cursor), Limit: pageSize(q.Limit, discover.MaxSearchHits)}
	for _, w := range words {
		if utf8.RuneCountInString(w) > 128 {
			return discover.SearchPage{}, errs.E(errs.Invalid, errs.CodeValidation, "A word of a search is at most 128 characters.")
		}
		cq.Patterns = append(cq.Patterns, CataloguePattern(w))
	}
	if len(q.Level.Path) > 0 {
		if cq.Dir, cq.IndexPath, err = indexOfLevel(q.Level); err != nil {
			return discover.SearchPage{}, err
		}
	}
	hits, next, done, err := a.names(ctx, st, cq)
	if err != nil {
		return discover.SearchPage{}, err
	}
	return discover.SearchPage{Hits: hits, Total: -1, Cursor: next, Done: done}, nil
}

// names runs a query one name beyond the page, to know whether another page follows.
func (remoteAdapter) names(ctx context.Context, st CatalogueStore, cq rows.CatalogueQuery) ([]discover.Hit, discover.Cursor, bool, error) {
	limit := cq.Limit
	cq.Limit = limit + 1
	got, err := st.CatalogueSearch(ctx, cq)
	if err != nil {
		return nil, nil, false, err
	}
	more := len(got) > limit
	if more {
		got = got[:limit]
	}
	hits := make([]discover.Hit, 0, len(got))
	for _, h := range got {
		hits = append(hits, discover.Hit{Name: h.Name, Description: h.Description, Latest: newest(h.Versions)})
	}
	if !more || len(hits) == 0 {
		return hits, nil, true, nil
	}
	return hits, nameCursor(hits[len(hits)-1].Name), false, nil
}

// newest is the highest version by dpkg's order.
func newest(vs []string) string {
	best := ""
	for _, v := range vs {
		if best == "" || version.Compare(v, best) > 0 {
			best = v
		}
	}
	return best
}

type nameCursorDoc struct {
	After string `json:"after"`
}

func nameCursor(after string) discover.Cursor {
	b, _ := json.Marshal(nameCursorDoc{After: after}) //nolint:errchkjson // a struct of a string always encodes
	return b
}

func lastName(c discover.Cursor) string {
	var d nameCursorDoc
	if len(c) == 0 || json.Unmarshal(c, &d) != nil {
		return ""
	}
	return d.After
}

func pageSize(limit, bound int) int {
	if limit <= 0 || limit > bound {
		return bound
	}
	return limit
}

// suiteName is the name of a release directory as a level: the suite below dists/, else the directory itself.
func suiteName(dir string) string { return strings.TrimPrefix(dir, "dists/") }

// suiteDir is the release directory of a suite's level name.
func suiteDir(name string) string {
	if strings.Contains(name, "/") && !strings.HasPrefix(name, "dists/") {
		return name
	}
	return "dists/" + strings.TrimPrefix(name, "dists/")
}

// indexOfLevel is the binary index a three-part level names: dists/<suite> and <component>/binary-<architecture>/Packages.
func indexOfLevel(l discover.LevelRef) (dir, index string, err error) {
	if len(l.Path) != 3 {
		return "", "", errs.E(errs.Invalid, errs.CodeValidation, "A search of a Debian remote is restricted to a suite, a component and an architecture.")
	}
	return suiteDir(l.Path[0]), l.Path[1] + "/binary-" + l.Path[2] + "/Packages", nil
}

// Children implements [discover.Catalogue]: the root lists the suites the remote holds a release of and those of its mirror, a suite
// the components of its parsed binary indexes, a component their architectures, and an architecture the names of its index.
func (a remoteAdapter) Children(ctx context.Context, u discover.Upstream, rq discover.LevelRequest) (discover.LevelPage, error) {
	st, repoID, err := a.catalogueStore(u)
	if err != nil {
		return discover.LevelPage{}, err
	}
	if len(rq.Level.Path) == 3 {
		dir, index, err := indexOfLevel(rq.Level)
		if err != nil {
			return discover.LevelPage{}, err
		}
		hits, next, done, err := a.names(ctx, st, rows.CatalogueQuery{
			RepoID: repoID, Dir: dir, IndexPath: index, After: lastName(rq.Cursor), Limit: pageSize(rq.Limit, discover.MaxLevelEntries),
		})
		if err != nil {
			return discover.LevelPage{}, err
		}
		if len(hits) == 0 && len(rq.Cursor) == 0 {
			return levelKnown(ctx, u, rq.Level.Path)
		}
		return discover.LevelPage{Packages: hits, Total: -1, Cursor: next, Done: done}, nil
	}
	suites, err := st.CatalogueSuites(ctx, repoID)
	if err != nil {
		return discover.LevelPage{}, err
	}
	set := map[string]bool{}
	switch len(rq.Level.Path) {
	case 0:
		for _, s := range suites {
			set[suiteName(s.Dir)] = true
		}
		for _, s := range SettingsOf(u.Settings()).Mirror.Suites {
			set[suiteName(s)] = true
		}
	default:
		dir := suiteDir(rq.Level.Path[0])
		for _, s := range suites {
			if s.Dir != dir {
				continue
			}
			for _, idx := range s.Indexes {
				comp, rest, ok := strings.Cut(idx, "/binary-")
				arch, _, ok2 := strings.Cut(rest, "/")
				switch {
				case !ok || !ok2:
				case len(rq.Level.Path) == 1:
					set[comp] = true
				case comp == rq.Level.Path[1]:
					set[arch] = true
				}
			}
		}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) == 0 && len(rq.Level.Path) > 0 {
		return levelKnown(ctx, u, rq.Level.Path)
	}
	page, next, done := pageStrings(names, rq.Cursor, pageSize(rq.Limit, discover.MaxLevelEntries))
	return discover.LevelPage{Levels: page, Total: len(names), Cursor: next, Done: done}, nil
}

// levelKnown is the page of a suite, a component or an architecture that lists nothing (Q-CUR-165): unknown when the upstream has no
// release of the suite (neither InRelease nor Release, the stored copy first, as the remote reads them; at most two requests), or when
// the release's Components or Architectures field does not name the component or the architecture; empty otherwise, and where the
// release does not say (a flat repository has no Components).
func levelKnown(ctx context.Context, u discover.Upstream, path []string) (discover.LevelPage, error) {
	empty := discover.LevelPage{Total: 0, Done: true}
	unknown := discover.LevelPage{Total: 0, Done: true, Unknown: &discover.UnknownLevel{}}
	text, found, err := suiteRelease(ctx, u, suiteDir(path[0]))
	switch {
	case err != nil:
		return discover.LevelPage{}, err
	case !found:
		return unknown, nil
	case len(path) < 2 || text == nil:
		return empty, nil
	}
	p, err := control.Parse(text)
	if err != nil {
		return empty, nil //nolint:nilerr // a release that does not parse says nothing about the level; the remote's own read reports it
	}
	if comps := strings.Fields(p.Value("Components")); len(comps) > 0 && !containsString(comps, path[1]) {
		return unknown, nil
	}
	if archs := strings.Fields(p.Value("Architectures")); len(path) == 3 && len(archs) > 0 && !containsString(archs, path[2]) {
		return unknown, nil
	}
	return empty, nil
}

// suiteRelease reads the release of a suite's directory through the catalogue's upstream: the signed text of its InRelease, else its
// Release; found is false when the upstream has neither. text is nil for an InRelease that is no cleartext message.
func suiteRelease(ctx context.Context, u discover.Upstream, dir string) (text []byte, found bool, err error) {
	doc, found, err := u.Document(ctx, joinDir(dir, FileInRelease), "")
	if err != nil {
		return nil, false, err
	}
	if found {
		text, err := openpgp.ClearText(doc, openpgp.Limits{})
		if err != nil {
			return nil, true, nil //nolint:nilerr // the release is there; whether it reads is the remote's to report
		}
		return text, true, nil
	}
	return u.Document(ctx, joinDir(dir, FileRelease), "")
}

// pageStrings pages a list held whole by an offset cursor.
func pageStrings(all []string, c discover.Cursor, limit int) ([]string, discover.Cursor, bool) {
	from := min(discover.Offset(c), len(all))
	to := min(from+limit, len(all))
	if to < len(all) {
		return all[from:to], discover.OffsetCursor(to), false
	}
	return all[from:to], nil, true
}

// Package implements [discover.Catalogue]: the rows of the name in the parsed binary indexes, one version per row group, newest first by
// dpkg's order, with the size and the digest of its first architecture and every architecture as its platforms.
func (a remoteAdapter) Package(ctx context.Context, u discover.Upstream, name string, rq discover.VersionsRequest) (discover.PackageInfo, error) {
	st, repoID, err := a.catalogueStore(u)
	if err != nil {
		return discover.PackageInfo{}, err
	}
	entries, err := st.CatalogueRows(ctx, repoID, strings.ToLower(strings.TrimSpace(name)))
	if err != nil || len(entries) == 0 {
		return discover.PackageInfo{}, err
	}
	byVersion := map[string]*discover.VersionFacts{}
	var order []string
	for _, e := range entries {
		f, ok := byVersion[e.Version]
		if !ok {
			f = &discover.VersionFacts{Version: e.Version, Size: e.Size, Digest: "sha256:" + hexOf(e.SHA256)}
			byVersion[e.Version] = f
			order = append(order, e.Version)
		}
		if p := "linux/" + e.Architecture; !containsString(f.Platforms, p) {
			f.Platforms = append(f.Platforms, p)
		}
	}
	sort.Slice(order, func(i, j int) bool { return version.Compare(order[i], order[j]) > 0 })
	all := make([]discover.VersionFacts, 0, len(order))
	for _, v := range order {
		f := byVersion[v]
		sort.Strings(f.Platforms)
		all = append(all, *f)
	}
	info := discover.PackageInfo{Found: true, Name: entries[0].Package}
	info.Versions, info.Total, info.Cursor, info.Done = discover.PageVersions(all, rq)
	return info, nil
}

func containsString(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func hexOf(b [32]byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 64)
	for i, c := range b {
		out[2*i], out[2*i+1] = digits[c>>4], digits[c&15]
	}
	return string(out)
}
