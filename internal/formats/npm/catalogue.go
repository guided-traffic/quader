package npm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/npm/name"
	"github.com/guided-traffic/quader/internal/formats/npm/remote"
	"github.com/guided-traffic/quader/internal/formats/npm/semver"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// The upstream catalogue of an npm remote (remote-curation.md R-CUR-007, formats/npm.md R-NPM-049): the registry's search
// (`-/v1/search`), the members of a scope of the public registry (the key range of `_all_docs` on the replication host the mirror reads,
// Q-NPM-72) and the full package document of a package. Every request goes through the upstream the catalogue lends, which stores
// nothing.

var _ formats.Catalogue = remoteAdapter{}

const (
	// searchWindow is the deepest hit the public registry's search reaches: from from=5000 on it answers its first page again.
	searchWindow = 5250
	// searchMinRunes and searchMaxRunes bound the text the registry takes; a shorter one is not sent.
	searchMinRunes = 2
	searchMaxRunes = 64
	// noSearch is the reason of a registry that does not answer the route.
	noSearch = "This npm registry does not answer searches."
)

// CatalogueCaps implements [discover.Catalogue]: every registry is searched by its route until it says it has none (the engine turns the
// capability off for its period); the public registry's scopes are listable through its replication host.
func (remoteAdapter) CatalogueCaps(_ context.Context, u discover.Upstream) (discover.CatalogueCaps, error) {
	caps := discover.CatalogueCaps{
		Search: discover.SearchUpstream, SearchWindow: searchWindow,
		SearchNote: "The registry ranks names, descriptions, keywords and maintainers; a query takes 2 to 64 characters.",
		Sizes:      "unknown",
	}
	if isPublicRegistry(u.Base()) {
		caps.Levels, caps.Hosts = []string{"scope"}, []string{replicationHost}
	} else {
		caps.LevelsReason = "This npm registry lists no scopes."
	}
	return caps, nil
}

// searchAnswer is the part of a `-/v1/search` answer the catalogue reads.
type searchAnswer struct {
	Objects []struct {
		Package struct {
			Name        string `json:"name"`
			Version     string `json:"version"`
			Description string `json:"description"`
			Date        string `json:"date"`
		} `json:"package"`
		Updated string `json:"updated"`
		Flags   struct {
			Insecure json.Number `json:"insecure"`
		} `json:"flags"`
	} `json:"objects"`
	Total *int `json:"total"`
}

// Search implements [discover.Catalogue]: one page of the registry's search, from the cursor's offset; the window ends at 5,250 hits.
func (remoteAdapter) Search(ctx context.Context, u discover.Upstream, q discover.SearchQuery) (discover.SearchPage, error) {
	text := strings.TrimSpace(q.Text)
	if n := utf8.RuneCountInString(text); n < searchMinRunes || n > searchMaxRunes {
		return discover.SearchPage{}, errs.E(errs.Invalid, errs.CodeValidation, "npm's search takes 2 to 64 characters.",
			errs.WithFields(errs.FieldError{Pointer: "/q", Code: errs.CodeValidation, Detail: "2 to 64 characters"}))
	}
	limit := q.Limit
	if limit <= 0 || limit > discover.MaxSearchHits {
		limit = discover.MaxSearchHits
	}
	from := discover.Offset(q.Cursor)
	if from >= searchWindow {
		return discover.SearchPage{Total: -1, Done: true, Truncated: true, Note: "The registry's search reaches 5,250 hits; narrow the query."}, nil
	}
	limit = min(limit, searchWindow-from)
	target := u.Base().ResolveReference(&url.URL{Path: "-/v1/search", RawQuery: "text=" + url.QueryEscape(text) +
		"&size=" + strconv.Itoa(limit) + "&from=" + strconv.Itoa(from)})
	res, err := u.Get(ctx, target, "application/json", 0)
	if err != nil {
		return discover.SearchPage{}, err
	}
	switch res.Status {
	case 200:
	case 404, 405, 501:
		return discover.SearchPage{}, &discover.UnavailableError{Reason: noSearch}
	default:
		return discover.SearchPage{}, errs.E(errs.UpstreamFailed, errs.CodeUpstreamError, fmt.Sprintf("The registry answered %d to the search.", res.Status))
	}
	var a searchAnswer
	if err := json.Unmarshal(res.Body, &a); err != nil {
		return discover.SearchPage{}, errs.E(errs.UpstreamFailed, errs.CodeUpstreamError, "The registry's search answer is not the expected JSON.")
	}
	page := discover.SearchPage{Total: -1}
	for _, o := range a.Objects {
		n, err := name.Parse(o.Package.Name)
		if err != nil {
			continue
		}
		h := discover.Hit{Name: n.String(), Description: o.Package.Description, Latest: o.Package.Version, Updated: remote.ParseTime(o.Package.Date)}
		if v, err := o.Flags.Insecure.Int64(); err == nil && v > 0 {
			h.Flags = append(h.Flags, "insecure")
		}
		page.Hits = append(page.Hits, h)
	}
	next := from + len(a.Objects)
	if a.Total != nil {
		page.Total = *a.Total
	}
	switch {
	case len(a.Objects) < limit, a.Total != nil && next >= *a.Total:
		page.Done = true
	case next >= searchWindow:
		page.Done, page.Truncated = true, true
		page.Note = "The registry's search reaches 5,250 hits; narrow the query."
	default:
		page.Cursor = discover.OffsetCursor(next)
	}
	return page, nil
}

// scopeOf reads a level of the public registry: one part, the scope with or without its @.
func scopeOf(l discover.LevelRef) (string, bool) {
	if len(l.Path) != 1 {
		return "", false
	}
	s := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(l.Path[0])), "@")
	if s == "" || strings.ContainsAny(s, "/@") {
		return "", false
	}
	if _, err := name.Parse("@" + s + "/x"); err != nil {
		return "", false
	}
	return "@" + s, true
}

// scopeCursor is the position in a scope's key range: the last name answered.
type scopeCursor struct {
	After string `json:"after"`
}

// Children implements [discover.Catalogue]: the root of the public registry lists nothing (a scope is entered by its name); a scope lists
// its packages from the replication host's key range, without a credential. Another registry lists no levels.
func (remoteAdapter) Children(ctx context.Context, u discover.Upstream, rq discover.LevelRequest) (discover.LevelPage, error) {
	if !isPublicRegistry(u.Base()) {
		return discover.LevelPage{}, discover.ErrNotOffered
	}
	if len(rq.Level.Path) == 0 {
		return discover.LevelPage{Total: -1, Done: true}, nil
	}
	scope, ok := scopeOf(rq.Level)
	if !ok {
		return discover.LevelPage{}, errs.E(errs.Invalid, errs.CodeValidation, "A level of the npm registry is one scope, such as @types.")
	}
	limit := rq.Limit
	if limit <= 0 || limit > discover.MaxLevelEntries {
		limit = discover.MaxLevelEntries
	}
	var cur scopeCursor
	if len(rq.Cursor) > 0 {
		_ = json.Unmarshal(rq.Cursor, &cur) //nolint:errcheck // a cursor that does not read starts at the scope's first name
	}
	start, ask := scope+"/", limit+1
	if cur.After != "" && strings.HasPrefix(cur.After, scope+"/") {
		start, ask = cur.After, limit+2 // startkey is inclusive: the last name of the previous page comes again
	}
	var docs struct {
		Rows []struct {
			ID string `json:"id"`
		} `json:"rows"`
	}
	q := "startkey=" + url.QueryEscape(jsonString(start)) + "&endkey=" + url.QueryEscape(jsonString(scope+"/￰")) + "&limit=" + strconv.Itoa(ask)
	res, err := u.Get(ctx, replicationURL("/registry/_all_docs", q), "application/json", 0)
	if err != nil {
		return discover.LevelPage{}, err
	}
	if res.Status != 200 {
		return discover.LevelPage{}, errs.E(errs.UpstreamFailed, errs.CodeUpstreamError, fmt.Sprintf("The registry's replication host answered %d to the scope's listing.", res.Status))
	}
	if err := json.Unmarshal(res.Body, &docs); err != nil {
		return discover.LevelPage{}, errs.E(errs.UpstreamFailed, errs.CodeUpstreamError, "The replication host's listing is not the expected JSON.")
	}
	page := discover.LevelPage{Total: -1}
	more := false
	for _, row := range docs.Rows {
		if row.ID == cur.After {
			continue
		}
		if len(page.Packages) == limit {
			more = true
			break
		}
		n, err := name.Parse(row.ID)
		if err != nil || !strings.HasPrefix(n.String(), scope+"/") {
			continue
		}
		page.Packages = append(page.Packages, discover.Hit{Name: n.String()})
	}
	if !more || len(page.Packages) == 0 {
		page.Done = true
		return page, nil
	}
	b, err := json.Marshal(scopeCursor{After: page.Packages[len(page.Packages)-1].Name})
	if err != nil {
		return discover.LevelPage{}, err
	}
	page.Cursor = b
	return page, nil
}

// Package implements [discover.Catalogue]: the full package document, from the store when the remote holds it, read without storing
// otherwise; its versions newest first by SemVer precedence, with the time of each, deprecated and prerelease, the unpacked size where the
// document has it, and the integrity as the digest. The dist-tags are the package's tags.
func (remoteAdapter) Package(ctx context.Context, u discover.Upstream, pkg string, rq discover.VersionsRequest) (discover.PackageInfo, error) {
	n, err := name.Parse(pkg)
	if err != nil {
		return discover.PackageInfo{}, nil //nolint:nilerr // a text that is no npm name names no package of the upstream
	}
	doc, found, err := u.Document(ctx, n.String(), documentVariant)
	if err != nil || !found {
		return discover.PackageInfo{}, err
	}
	parsed, err := remote.Parse(doc)
	if err != nil {
		return discover.PackageInfo{}, errs.E(errs.UpstreamFailed, errs.CodeUpstreamError, "The package document of "+n.String()+" does not read.")
	}
	info := discover.PackageInfo{Found: true, Name: n.String(), Tags: parsed.Tags()}
	var desc string
	if raw, ok := parsed.Top["description"]; ok {
		_ = json.Unmarshal(raw, &desc) //nolint:errcheck // a description that is no string is none
	}
	info.Description = desc
	times := parsed.Times()
	type entry struct {
		v    semver.Version
		text string
	}
	var es []entry
	for text := range parsed.Versions {
		v, err := semver.Parse(text)
		if err != nil || v.Canonical() != text {
			continue
		}
		es = append(es, entry{v, text})
	}
	sort.Slice(es, func(i, j int) bool {
		if c := semver.CompareBuild(es[i].v, es[j].v); c != 0 {
			return c > 0
		}
		return es[i].text > es[j].text
	})
	all := make([]discover.VersionFacts, 0, len(es))
	for _, e := range es {
		all = append(all, versionFacts(e.text, e.v, parsed.Versions[e.text], times[e.text]))
	}
	info.Versions, info.Total, info.Cursor, info.Done = discover.PageVersions(all, rq)
	return info, nil
}

// versionFacts reads what a version entry tells without a download.
func versionFacts(text string, v semver.Version, raw json.RawMessage, published string) discover.VersionFacts {
	var e struct {
		Deprecated json.RawMessage `json:"deprecated"`
		Dist       struct {
			Integrity    string      `json:"integrity"`
			UnpackedSize json.Number `json:"unpackedSize"`
		} `json:"dist"`
	}
	_ = json.Unmarshal(raw, &e) //nolint:errcheck // an entry that does not read tells nothing more than its version
	f := discover.VersionFacts{Version: text, Digest: e.Dist.Integrity}
	if t := remote.ParseTime(published); !t.IsZero() {
		f.Published = t.UTC().Truncate(time.Second)
	}
	if s, err := e.Dist.UnpackedSize.Int64(); err == nil && s > 0 {
		f.UnpackedSize = s
	}
	if dep := strings.TrimSpace(string(e.Deprecated)); dep != "" && dep != "null" && dep != "false" && dep != `""` {
		f.Flags = append(f.Flags, "deprecated")
	}
	if v.IsPrerelease() {
		f.Flags = append(f.Flags, "prerelease")
	}
	return f
}
