package ansible

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/ansible/collection"
	"github.com/guided-traffic/quader/internal/formats/ansible/galaxydoc"
	"github.com/guided-traffic/quader/internal/formats/ansible/version"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// The upstream catalogue of an Ansible Galaxy remote (remote-curation.md R-CUR-007, formats/ansible.md R-ANS-047): the keyword search of
// pulp_ansible and Galaxy NG (`v3/plugin/ansible/search/collection-versions/`), the namespaces of the v3 API and a namespace's collections
// from the collection index the mirror reads with a namespace filter, and a collection's version list read as R-ANS-017 reads it. Every
// request goes through the upstream the catalogue lends, which stores nothing.

var _ formats.Catalogue = remoteAdapter{}

// noGalaxySearch is the reason of a Galaxy that does not answer the keyword search.
const noGalaxySearch = "This Galaxy does not answer the keyword search; list a namespace or look a collection up by its exact name."

// CatalogueCaps implements [discover.Catalogue]: the keyword search until the upstream says it has none, the namespaces as the level.
func (remoteAdapter) CatalogueCaps(context.Context, discover.Upstream) (discover.CatalogueCaps, error) {
	return discover.CatalogueCaps{
		Search: discover.SearchUpstream, SearchHint: discover.SearchHintGalaxy,
		Levels: []string{"namespace"}, Sizes: "perVersion",
	}, nil
}

// apiURL is an address below the repository's API root and the v3 prefix its root document names.
func apiURL(ctx context.Context, u discover.Upstream, rest string, q url.Values) (*url.URL, error) {
	d, err := mirrorDiscovery(ctx, u)
	if err != nil {
		return nil, err
	}
	base := *u.Base()
	base.User, base.Fragment, base.RawFragment, base.RawQuery = nil, "", "", ""
	if !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
	}
	target, err := url.Parse(base.String() + d.v3 + rest)
	if err != nil {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "The Galaxy address does not build.", errs.WithCause(err))
	}
	target.RawQuery = q.Encode()
	return target, nil
}

// galaxyPage is the frame of a v3 list answer.
type galaxyPage struct {
	Meta struct {
		Count int `json:"count"`
	} `json:"meta"`
	Data json.RawMessage `json:"data"`
}

// getPage reads a v3 list answer; found is false for a 404.
func getPage(ctx context.Context, u discover.Upstream, target *url.URL, what string) (galaxyPage, bool, error) {
	resp, err := u.Get(ctx, target, "application/json", maxIndexBytes)
	if err != nil {
		return galaxyPage{}, false, err
	}
	switch {
	case resp.Status == 404:
		return galaxyPage{}, false, nil
	case resp.Status != 200:
		return galaxyPage{}, false, errs.E(errs.UpstreamFailed, errs.CodeUpstreamError, "The Galaxy answered "+strconv.Itoa(resp.Status)+" for "+what+".")
	}
	var p galaxyPage
	if err := json.Unmarshal(resp.Body, &p); err != nil || p.Meta.Count < 0 {
		return galaxyPage{}, false, invalidDocument(what, "it is not a page of the v3 API")
	}
	return p, true, nil
}

func pageLimit(limit, bound int) int {
	if limit <= 0 || limit > bound {
		return bound
	}
	return limit
}

// Search implements [discover.Catalogue]: one page of the keyword search over the highest version of each collection.
func (remoteAdapter) Search(ctx context.Context, u discover.Upstream, q discover.SearchQuery) (discover.SearchPage, error) {
	limit := pageLimit(q.Limit, discover.MaxSearchHits)
	offset := discover.Offset(q.Cursor)
	target, err := apiURL(ctx, u, "plugin/ansible/search/collection-versions/", url.Values{
		"keywords": {strings.TrimSpace(q.Text)}, "is_highest": {"true"}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)},
	})
	if err != nil {
		return discover.SearchPage{}, err
	}
	p, found, err := getPage(ctx, u, target, "its keyword search")
	if err != nil {
		return discover.SearchPage{}, err
	}
	if !found {
		return discover.SearchPage{}, &discover.UnavailableError{Reason: noGalaxySearch, Hint: discover.SearchHintGalaxyNoSearch}
	}
	var rows []struct {
		CollectionVersion struct {
			Namespace   string `json:"namespace"`
			Name        string `json:"name"`
			Version     string `json:"version"`
			Description string `json:"description"`
		} `json:"collection_version"`
		IsDeprecated bool `json:"is_deprecated"`
		IsSigned     bool `json:"is_signed"`
	}
	if err := json.Unmarshal(p.Data, &rows); err != nil {
		return discover.SearchPage{}, invalidDocument("its keyword search", "the data is not a list of collection versions")
	}
	out := discover.SearchPage{Total: p.Meta.Count}
	for _, r := range rows {
		name, ok := fullName(r.CollectionVersion.Namespace, r.CollectionVersion.Name)
		if !ok {
			continue
		}
		h := discover.Hit{Name: name, Description: r.CollectionVersion.Description, Latest: r.CollectionVersion.Version}
		if r.IsDeprecated {
			h.Flags = append(h.Flags, "deprecated")
		}
		out.Hits = append(out.Hits, h)
	}
	next := offset + len(rows)
	if len(rows) == 0 || next >= p.Meta.Count {
		out.Done = true
	} else {
		out.Cursor = discover.OffsetCursor(next)
	}
	return out, nil
}

// fullName is "<namespace>.<name>" when both are names of the format.
func fullName(ns, name string) (string, bool) {
	full := strings.ToLower(ns) + "." + strings.ToLower(name)
	if _, _, ok := collection.SplitName(full); !ok {
		return "", false
	}
	return full, true
}

// namespaceChar is a character a Galaxy namespace holds: lowercase letters, digits and underscores (collection.ValidPart, the rule
// ansible-galaxy and the Galaxy apply to a collection's namespace).
func namespaceChar(r rune) bool { return r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' }

// namespaceAccount is the namespace's own document, `v3/namespaces/{name}/`: 200 for a namespace the Galaxy has, 404 for one it does
// not (recorded on 2026-11-24, testdata/catalogue/README.md). The collection index answers an unknown namespace with an empty page.
func namespaceAccount(ctx context.Context, u discover.Upstream, ns string) (*url.URL, error) {
	return apiURL(ctx, u, "namespaces/"+url.PathEscape(ns)+"/", nil)
}

// namespaceKnown is the page of a namespace whose first page lists nothing: unknown when its document answers 404 (Q-CUR-165). The
// document's address is the listing's, beside its collections/, so the root document is not read again.
func namespaceKnown(ctx context.Context, u discover.Upstream, listing *url.URL, ns string) (discover.LevelPage, error) {
	account := *listing
	account.Path = strings.TrimSuffix(listing.Path, "collections/") + "namespaces/" + ns + "/"
	account.RawPath, account.RawQuery = "", ""
	return discover.EmptyOrUnknown(ctx, u, &account)
}

// namespaceByRule is the page of a namespace whose name holds a character no Galaxy namespace holds: unknown, asked for nothing, with
// the name made to keep the rule as the suggestion when the Galaxy has it (one request, Q-CUR-165). A hyphen becomes an underscore,
// as a GitHub organisation's name becomes a namespace (netbox-community, netbox_community); every other such character is dropped.
func namespaceByRule(ctx context.Context, u discover.Upstream, ns, bad string) (discover.LevelPage, error) {
	unknown := &discover.UnknownLevel{Rule: discover.NameRuleLowercaseDigitsUnderscore, Forbidden: bad}
	s := strings.Map(func(r rune) rune {
		switch {
		case r == '-':
			return '_'
		case namespaceChar(r):
			return r
		}
		return -1
	}, ns)
	if collection.ValidPart(s) {
		account, err := namespaceAccount(ctx, u, s)
		if err != nil {
			return discover.LevelPage{}, err
		}
		exists, _, err := discover.Exists(ctx, u, account)
		if err != nil {
			return discover.LevelPage{}, err
		}
		if exists {
			unknown.Suggestion = s
		}
	}
	return discover.LevelPage{Done: true, Unknown: unknown}, nil
}

// Children implements [discover.Catalogue]: the root lists the Galaxy's namespaces (`v3/namespaces/`), a namespace its collections from
// the collection index with the filter namespace.
func (remoteAdapter) Children(ctx context.Context, u discover.Upstream, rq discover.LevelRequest) (discover.LevelPage, error) {
	limit := pageLimit(rq.Limit, indexPageSize)
	offset := discover.Offset(rq.Cursor)
	q := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	var target *url.URL
	var err error
	switch len(rq.Level.Path) {
	case 0:
		target, err = apiURL(ctx, u, "namespaces/", q)
	case 1:
		ns := strings.ToLower(rq.Level.Path[0])
		if bad := discover.Forbidden(ns, namespaceChar); bad != "" {
			return namespaceByRule(ctx, u, ns, bad)
		}
		q.Set("namespace", ns)
		target, err = apiURL(ctx, u, "collections/", q)
	default:
		return discover.LevelPage{}, errs.E(errs.Invalid, errs.CodeValidation, "A level of a Galaxy is one namespace.")
	}
	if err != nil {
		return discover.LevelPage{}, err
	}
	p, found, err := getPage(ctx, u, target, "a listing")
	if err != nil {
		return discover.LevelPage{}, err
	}
	if !found {
		return discover.LevelPage{}, errs.E(errs.NotFound, errs.CodeCatalogueNotFound, "The Galaxy does not have this level.")
	}
	var rows []struct {
		Namespace   string `json:"namespace"`
		Name        string `json:"name"`
		Deprecated  bool   `json:"deprecated"`
		Description string `json:"description"`
		Highest     *struct {
			Version string `json:"version"`
		} `json:"highest_version"`
	}
	if err := json.Unmarshal(p.Data, &rows); err != nil {
		return discover.LevelPage{}, invalidDocument("a listing", "the data is not a list")
	}
	if len(rq.Level.Path) == 1 && offset == 0 && p.Meta.Count == 0 && len(rows) == 0 {
		return namespaceKnown(ctx, u, target, strings.ToLower(rq.Level.Path[0]))
	}
	out := discover.LevelPage{Total: p.Meta.Count}
	for _, r := range rows {
		if len(rq.Level.Path) == 0 {
			if ns := strings.ToLower(r.Name); collection.ValidPart(ns) {
				out.Levels = append(out.Levels, ns)
			}
			continue
		}
		name, ok := fullName(r.Namespace, r.Name)
		if !ok {
			continue
		}
		h := discover.Hit{Name: name, Description: r.Description}
		if r.Highest != nil {
			h.Latest = r.Highest.Version
		}
		if r.Deprecated {
			h.Flags = append(h.Flags, "deprecated")
		}
		out.Packages = append(out.Packages, h)
	}
	next := offset + len(rows)
	if len(rows) == 0 || next >= p.Meta.Count {
		out.Done = true
	} else {
		out.Cursor = discover.OffsetCursor(next)
	}
	return out, nil
}

// Package implements [discover.Catalogue]: the collection's version list read page by page as R-ANS-017 reads it (from the store when the
// remote holds it), newest first by SemVer precedence, with the time and the required Ansible of each; the collection document's
// deprecated marks every version.
func (remoteAdapter) Package(ctx context.Context, u discover.Upstream, name string, rq discover.VersionsRequest) (discover.PackageInfo, error) {
	if id := roleIDOf(name); id != "" {
		return rolePackageInfo(ctx, u, id, rq) // a role, by the name the rules see (R-ANS-056)
	}
	ns, n, ok := collection.SplitName(strings.ToLower(strings.TrimSpace(name)))
	if !ok {
		return discover.PackageInfo{}, nil
	}
	entries, found, err := catalogueVersions(ctx, u, ns, n)
	if err != nil || !found {
		return discover.PackageInfo{}, err
	}
	deprecated := false
	if doc, ok, err := u.Document(ctx, CollectionDocumentPath(ns, n), documentVariant); err != nil {
		return discover.PackageInfo{}, err
	} else if ok {
		var c struct {
			Deprecated bool `json:"deprecated"`
		}
		_ = json.Unmarshal(doc, &c) //nolint:errcheck // a collection document that does not read marks nothing
		deprecated = c.Deprecated
	}
	type pv struct {
		v version.Semver
		f discover.VersionFacts
	}
	var vs []pv
	for _, e := range entries {
		sv, err := version.Parse(e.Version)
		if err != nil {
			continue
		}
		f := discover.VersionFacts{Version: e.Version, Published: e.CreatedAt.UTC()}
		if e.RequiresAnsible != "" {
			f.Platforms = []string{"ansible " + e.RequiresAnsible}
		}
		if len(sv.Pre) > 0 {
			f.Flags = append(f.Flags, "prerelease")
		}
		if deprecated {
			f.Flags = append(f.Flags, "deprecated")
		}
		vs = append(vs, pv{sv, f})
	}
	sort.SliceStable(vs, func(i, j int) bool { return version.Compare(vs[i].v, vs[j].v) > 0 })
	all := make([]discover.VersionFacts, 0, len(vs))
	for _, p := range vs {
		all = append(all, p.f)
	}
	info := discover.PackageInfo{Found: true, Name: ns + "." + n}
	info.Versions, info.Total, info.Cursor, info.Done = discover.PageVersions(all, rq)
	return info, nil
}

// catalogueVersions reads the version list of a collection page by page, as mirrorVersions does, with what each entry tells; found is
// false when the upstream has no such collection.
func catalogueVersions(ctx context.Context, u discover.Upstream, ns, name string) (list []galaxydoc.VersionEntry, found bool, err error) {
	path := VersionsDocumentPath(ns, name)
	first, found, err := u.Document(ctx, path, offsetVariant(0))
	if err != nil || !found {
		return nil, false, err
	}
	head, err := parsePage(first)
	if err != nil {
		return nil, false, err
	}
	list = append(list, head.entries...)
	size := upstreamListLimit
	if head.count > len(head.entries) && len(head.entries) > 0 {
		size = len(head.entries)
	}
	for start := size; start < head.count; start += size {
		doc, ok, err := u.Document(ctx, path, offsetVariant(start))
		if err != nil {
			return nil, false, err
		}
		if !ok {
			break
		}
		p, err := parsePage(doc)
		if err != nil {
			return nil, false, err
		}
		list = append(list, p.entries...)
	}
	return list, true, nil
}
