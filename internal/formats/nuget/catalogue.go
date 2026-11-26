package nuget

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/nuget/remote"
	"github.com/guided-traffic/quader/internal/formats/nuget/version"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// The upstream catalogue of a NuGet remote (remote-curation.md R-CUR-007, formats/nuget.md R-NUGET-035): the service index's
// SearchQueryService with the query as typed, and an ID's version list with its registration. IDs have no levels. Every request goes
// through the upstream the catalogue lends, which stores nothing.

var _ formats.Catalogue = remoteAdapter{}

const (
	// catalogueSkipMax is the largest skip nuget.org's search takes; the window is that plus one page of 50.
	catalogueSkipMax = 3000
	catalogueWindow  = catalogueSkipMax + discover.MaxSearchHits
	// noLevels is why IDs have no levels.
	noLevels = "NuGet IDs have no levels; search for a prefix with id:."
)

// CatalogueCaps implements [discover.Catalogue]: a search where the service index names a search resource.
func (remoteAdapter) CatalogueCaps(ctx context.Context, u discover.Upstream) (discover.CatalogueCaps, error) {
	caps := discover.CatalogueCaps{LevelsReason: noLevels, Sizes: "unknown"}
	d, err := feedDiscovery(ctx, u)
	if err != nil {
		return discover.CatalogueCaps{}, err
	}
	if d == nil || d.Search == nil {
		caps.Search, caps.SearchHint = discover.SearchNone, discover.SearchHintNuGetNoSearch
		return caps, nil
	}
	caps.Search, caps.SearchWindow = discover.SearchUpstream, catalogueWindow
	caps.SearchHint = discover.SearchHintNuGetSearch
	if host := d.Search.Hostname(); !strings.EqualFold(host, u.Base().Hostname()) {
		caps.Hosts = []string{strings.ToLower(host)}
	}
	return caps, nil
}

// Search implements [discover.Catalogue]: one page of the search resource from the cursor's offset; the window ends at a skip of 3,000.
func (remoteAdapter) Search(ctx context.Context, u discover.Upstream, q discover.SearchQuery) (discover.SearchPage, error) {
	d, err := feedDiscovery(ctx, u)
	if err != nil {
		return discover.SearchPage{}, err
	}
	if d == nil || d.Search == nil {
		return discover.SearchPage{}, discover.ErrNotOffered
	}
	limit := q.Limit
	if limit <= 0 || limit > discover.MaxSearchHits {
		limit = discover.MaxSearchHits
	}
	skip := discover.Offset(q.Cursor)
	if skip > catalogueSkipMax {
		return discover.SearchPage{Total: -1, Done: true, Truncated: true, Note: "The upstream's search reaches 3,050 hits; narrow the query."}, nil
	}
	target, _ := d.SearchURL(url.Values{
		"q": {strings.TrimSpace(q.Text)}, "prerelease": {"true"}, "semVerLevel": {"2.0.0"},
		"take": {strconv.Itoa(limit)}, "skip": {strconv.Itoa(skip)},
	})
	resp, err := u.Get(ctx, target, "application/json", maxDiscoveryBytes)
	if err != nil {
		return discover.SearchPage{}, err
	}
	body, err := fetchResponse(resp, "search")
	if err != nil {
		return discover.SearchPage{}, err
	}
	var res struct {
		TotalHits int `json:"totalHits"`
		Data      []struct {
			ID          string `json:"id"`
			Version     string `json:"version"`
			Description string `json:"description"`
			Verified    bool   `json:"verified"`
			Deprecation *struct {
				Reasons []string `json:"reasons"`
			} `json:"deprecation"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return discover.SearchPage{}, invalidDocument("The upstream's search answer is not a search result: " + err.Error())
	}
	page := discover.SearchPage{Total: max(res.TotalHits, 0)}
	for _, row := range res.Data {
		id, ok := lowerID(row.ID)
		if !ok {
			continue
		}
		h := discover.Hit{Name: id, Description: row.Description}
		if v, err := version.Parse(row.Version); err == nil {
			h.Latest = v.URLForm()
		}
		if row.Verified {
			h.Flags = append(h.Flags, "verified")
		}
		if row.Deprecation != nil {
			h.Flags = append(h.Flags, "deprecated")
		}
		page.Hits = append(page.Hits, h)
	}
	next := skip + len(res.Data)
	switch {
	case len(res.Data) < limit || next >= page.Total:
		page.Done = true
	case next > catalogueSkipMax:
		page.Done, page.Truncated = true, true
		page.Note = "The upstream's search reaches 3,050 hits; narrow the query."
	default:
		page.Cursor = discover.OffsetCursor(next)
	}
	return page, nil
}

// Children implements [discover.Catalogue]: IDs have no levels.
func (remoteAdapter) Children(context.Context, discover.Upstream, discover.LevelRequest) (discover.LevelPage, error) {
	return discover.LevelPage{}, discover.ErrNotOffered
}

// registrationFacts are what a registration leaf tells of a version.
type registrationFacts struct {
	listed     bool
	published  time.Time
	deprecated bool
	vulnerable bool
	size       int64
}

// Package implements [discover.Catalogue]: the version list of the ID, which names unlisted versions too, with what its registration
// tells of each (listed, published, deprecation, vulnerabilities), newest first by NuGet's SemVer 2.0.0 precedence.
func (remoteAdapter) Package(ctx context.Context, u discover.Upstream, name string, rq discover.VersionsRequest) (discover.PackageInfo, error) {
	lower, ok := lowerID(name)
	if !ok {
		return discover.PackageInfo{}, nil
	}
	doc, found, err := u.Document(ctx, PackageDocumentPath(lower), documentVariant)
	if err != nil || !found {
		return discover.PackageInfo{}, err
	}
	list, err := remote.VersionListBounded(doc, maxDiscoveryBytes, remote.MaxLeaves)
	if err != nil {
		return discover.PackageInfo{}, err
	}
	facts, newest, err := registrationOf(ctx, u, lower)
	if err != nil {
		return discover.PackageInfo{}, err
	}
	type entry struct {
		v    version.Version
		text string
	}
	seen := map[string]bool{}
	var es []entry
	for _, s := range list {
		v, err := version.Parse(s)
		if err != nil || seen[v.URLForm()] {
			continue
		}
		seen[v.URLForm()] = true
		es = append(es, entry{v, v.URLForm()})
	}
	sort.SliceStable(es, func(i, j int) bool { return version.Compare(es[i].v, es[j].v) > 0 })
	all := make([]discover.VersionFacts, 0, len(es))
	for _, e := range es {
		f := discover.VersionFacts{Version: e.text}
		if r, ok := facts[e.text]; ok {
			f.Published, f.Size = r.published, r.size
			if !r.listed {
				f.Flags = append(f.Flags, "unlisted")
			}
			if r.deprecated {
				f.Flags = append(f.Flags, "deprecated")
			}
			if r.vulnerable {
				f.Flags = append(f.Flags, "vulnerable")
			}
		} else if facts != nil {
			f.Flags = append(f.Flags, "unlisted") // the version list names it, the registration does not: it is not listed
		}
		if e.v.IsPrerelease() {
			f.Flags = append(f.Flags, "prerelease")
		}
		all = append(all, f)
	}
	// The newest leaf's description and facts (R-CUR-028): the registration was read for the versions anyway.
	info := discover.PackageInfo{Found: true, Name: lower, Description: newest.description, About: newest.about()}
	info.Versions, info.Total, info.Cursor, info.Done = discover.PageVersions(all, rq)
	return info, nil
}

// registrationOf reads the registration index of the ID and the pages it does not inline: the facts of each version by its URL form and
// the newest leaf. A registration the upstream does not have gives nil facts.
func registrationOf(ctx context.Context, u discover.Upstream, lower string) (map[string]registrationFacts, newestLeaf, error) {
	doc, found, err := u.Document(ctx, RegistrationIndexPath(lower), documentVariant)
	if err != nil || !found {
		return nil, newestLeaf{}, err
	}
	budget := remote.NewBudget(maxDiscoveryBytes)
	idx, err := budget.Index(doc)
	if err != nil {
		return nil, newestLeaf{}, err
	}
	out := map[string]registrationFacts{}
	var leaf newestLeaf
	var newest *version.Version
	for _, pg := range idx.Pages {
		leaves := pg.Leaves
		if !pg.Inline {
			lo, ok1 := urlForm(pg.Lower)
			hi, ok2 := urlForm(pg.Upper)
			if !ok1 || !ok2 {
				return nil, newestLeaf{}, invalidDocument("A page of the upstream's registration index has bounds that are no versions.")
			}
			pageDoc, found, err := u.Document(ctx, RegistrationPagePath(lower, lo, hi), documentVariant)
			if err != nil {
				return nil, newestLeaf{}, err
			}
			if !found {
				continue
			}
			if leaves, err = budget.Page(pageDoc); err != nil {
				return nil, newestLeaf{}, err
			}
		}
		for _, l := range leaves {
			var e struct {
				Version         string          `json:"version"`
				Listed          *bool           `json:"listed"`
				Published       string          `json:"published"`
				Description     string          `json:"description"`
				PackageSize     int64           `json:"packageSize"`
				Deprecation     json.RawMessage `json:"deprecation"`
				Vulnerabilities json.RawMessage `json:"vulnerabilities"`
			}
			raw, err := json.Marshal(l.Entry)
			if err != nil || json.Unmarshal(raw, &e) != nil {
				continue
			}
			v, err := version.Parse(e.Version)
			if err != nil {
				continue
			}
			f := registrationFacts{listed: e.Listed == nil || *e.Listed, size: max(e.PackageSize, 0)}
			if t, err := time.Parse(time.RFC3339, e.Published); err == nil && t.Year() > 1900 {
				f.published = t.UTC()
			} else if err == nil {
				f.listed = false // nuget.org dates an unlisted version 1900-01-01
			}
			f.deprecated = len(e.Deprecation) > 0 && string(e.Deprecation) != "null"
			f.vulnerable = len(e.Vulnerabilities) > 2 && string(e.Vulnerabilities) != "null"
			out[v.URLForm()] = f
			if newest == nil || version.Compare(v, *newest) > 0 {
				newest, leaf = &v, newestLeaf{version: v.URLForm(), description: e.Description, entry: raw}
			}
		}
	}
	return out, leaf, nil
}
