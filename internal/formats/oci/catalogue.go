package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/oci/tagorder"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// The upstream catalogue of an OCI remote (remote-curation.md R-CUR-007, formats/oci.md R-OCI-038). The distribution specification has no
// search and no listing, so what a registry offers beyond the tags list is its operator's, and the capabilities are the preset's
// (Q-CUR-15): Docker Hub's repository search (undocumented, with the fallback of R-CUR-008), its namespaces and tags on its Hub API
// (hub.docker.com, a catalogue host of the preset, without a credential); Quay's API on its own host; GitLab's project registry on
// gitlab.com; ghcr.io nothing but the tags list; a registry without a preset the catalog of the distribution API where it answers.

var _ formats.Catalogue = remoteAdapter{}

// The presets whose operators' APIs the catalogue reads.
const (
	presetDockerHub = "docker-hub"
	presetQuay      = "quay"
	presetGHCR      = "ghcr"
	presetGitLab    = "gitlab"
	// hubAPI and gitlabAPI are the catalogue hosts of the Docker Hub and GitLab presets.
	hubAPI    = "hub.docker.com"
	gitlabAPI = "gitlab.com"
	// hubPageMax and quayPageMax bound a page the operators' APIs answer.
	hubPageMax   = 100
	quayPageMax  = 100
	quayFindPage = 10
	// hubShape is the sentence of Docker Hub's search when it does not answer as expected (R-CUR-008).
	hubShape = "Docker Hub's search did not answer as expected; look an image up by its name or list a namespace."
)

// presetOf names the preset whose URL the repository has, "" for a registry without one (a URL with a namespace prefix included).
func presetOf(base *url.URL) string {
	for _, p := range upstreamPresets() {
		if discover.SameUpstream(base, p.URL) {
			return p.ID
		}
	}
	return ""
}

// CatalogueCaps implements [discover.Catalogue]: the preset's capabilities; a registry without a preset is asked once whether it
// answers the catalog of the distribution API, whose names are its root.
func (remoteAdapter) CatalogueCaps(ctx context.Context, u discover.Upstream) (discover.CatalogueCaps, error) {
	switch presetOf(u.Base()) {
	case presetDockerHub:
		return discover.CatalogueCaps{
			Search: discover.SearchUpstream, SearchHint: discover.SearchHintDockerHub,
			Levels: []string{"namespace"}, Sizes: "known", Hosts: []string{hubAPI},
		}, nil
	case presetQuay:
		return discover.CatalogueCaps{
			Search: discover.SearchUpstream, SearchHint: discover.SearchHintQuay,
			Levels: []string{"namespace"}, Sizes: "perVersion",
		}, nil
	case presetGHCR:
		return discover.CatalogueCaps{
			Search: discover.SearchNone, SearchHint: discover.SearchHintGHCR,
			LevelsReason: "ghcr.io offers no search and no listing of images; look an image up by its full name.", Sizes: "unknown",
		}, nil
	case presetGitLab:
		return discover.CatalogueCaps{
			Search: discover.SearchNone, SearchHint: discover.SearchHintGitLab,
			Levels: []string{"project"}, Sizes: "unknown", Hosts: []string{gitlabAPI},
		}, nil
	}
	caps := discover.CatalogueCaps{Search: discover.SearchNone, SearchHint: discover.SearchHintOCIRegistry, Sizes: "unknown"}
	offered, err := catalogOffered(ctx, u)
	if err != nil {
		return discover.CatalogueCaps{}, err
	}
	if !offered {
		caps.LevelsReason = "This registry lists no images; look an image up by its full name."
	}
	return caps, nil
}

// pageOf reads a page number from a cursor written by discover.OffsetCursor: 1 for the first page.
func pageOf(c discover.Cursor) int { return max(discover.Offset(c), 1) }

func clampLimit(limit, bound int) int {
	if limit <= 0 || limit > bound {
		return bound
	}
	return limit
}

// getJSON reads a JSON answer of an operator's API through the catalogue's upstream; found is false for a 404.
func getJSON(ctx context.Context, u discover.Upstream, target *url.URL, into any) (found bool, header http.Header, err error) {
	found, resp, err := getJSONAnswer(ctx, u, target, into)
	if resp == nil {
		return false, nil, err
	}
	return found, resp.Header, err
}

// getJSONAnswer is getJSON with the whole answer, whose body a 404 may still tell something by (GitLab's "404 Project Not Found").
func getJSONAnswer(ctx context.Context, u discover.Upstream, target *url.URL, into any) (found bool, resp *discover.Response, err error) {
	resp, err = u.Get(ctx, target, "application/json", 0)
	if err != nil {
		return false, nil, err
	}
	switch {
	case resp.Status == http.StatusNotFound:
		return false, resp, nil
	case resp.Status != http.StatusOK:
		return false, resp, errs.E(errs.UpstreamFailed, errs.CodeUpstreamError, fmt.Sprintf("The registry's API answered %d.", resp.Status))
	}
	lim := untrusted.DefaultLimits().Tighten(untrusted.Limits{DocumentBytes: int64(len(resp.Body)) + 1})
	if err := untrusted.UnmarshalJSON(resp.Body, lim, into); err != nil {
		return false, resp, errs.E(errs.UpstreamFailed, errs.CodeUpstreamError, "The registry's API answer is not the expected JSON.")
	}
	return true, resp, nil
}

// Search implements [discover.Catalogue]: Docker Hub's and Quay's search; every other upstream answers discover.ErrNotOffered.
func (remoteAdapter) Search(ctx context.Context, u discover.Upstream, q discover.SearchQuery) (discover.SearchPage, error) {
	switch presetOf(u.Base()) {
	case presetDockerHub:
		return hubSearch(ctx, u, q)
	case presetQuay:
		return quaySearch(ctx, u, q)
	}
	return discover.SearchPage{}, discover.ErrNotOffered
}

// hubSearch reads Docker Hub's repository search, `GET /v2/search/repositories/` on the Hub API. An answer that is not a page of the
// expected shape turns the search off for the cache period (R-CUR-008).
func hubSearch(ctx context.Context, u discover.Upstream, q discover.SearchQuery) (discover.SearchPage, error) {
	limit := clampLimit(q.Limit, discover.MaxSearchHits)
	page := pageOf(q.Cursor)
	target := &url.URL{Scheme: "https", Host: hubAPI, Path: "/v2/search/repositories/", RawQuery: url.Values{
		"query": {strings.TrimSpace(q.Text)}, "page_size": {strconv.Itoa(limit)}, "page": {strconv.Itoa(page)},
	}.Encode()}
	resp, err := u.Get(ctx, target, "application/json", 0)
	if err != nil {
		return discover.SearchPage{}, err
	}
	var a struct {
		Count   *int    `json:"count"`
		Next    *string `json:"next"`
		Results *[]struct {
			RepoName         string `json:"repo_name"`
			ShortDescription string `json:"short_description"`
			IsOfficial       bool   `json:"is_official"`
		} `json:"results"`
	}
	if resp.Status != http.StatusOK || json.Unmarshal(resp.Body, &a) != nil || a.Results == nil {
		return discover.SearchPage{}, &discover.UnavailableError{Reason: hubShape, Hint: discover.SearchHintDockerHubUnavailable, Shape: true}
	}
	out := discover.SearchPage{Total: -1}
	if a.Count != nil {
		out.Total = *a.Count
	}
	for _, r := range *a.Results {
		name := hubName(r.RepoName)
		if !validName(name) {
			continue
		}
		h := discover.Hit{Name: name, Description: r.ShortDescription}
		if r.IsOfficial {
			h.Flags = append(h.Flags, "official")
		}
		out.Hits = append(out.Hits, h)
	}
	if a.Next == nil || *a.Next == "" || len(*a.Results) == 0 {
		out.Done = true
	} else {
		out.Cursor = discover.OffsetCursor(page + 1)
	}
	return out, nil
}

// hubName is the name of a Docker Hub repository as the cache spells it: an official image below library/ (R-OCI-025).
func hubName(repo string) string {
	repo = strings.ToLower(strings.TrimSpace(repo))
	if repo != "" && !strings.Contains(repo, "/") {
		return "library/" + repo
	}
	return repo
}

// validName reports whether s is an image name of R-OCI-002.
func validName(s string) bool { return s != "" && ValidName(s) }

// quaySearch reads Quay's `GET /api/v1/find/repositories`, ten a page; the cursor counts hits, so a page of another size continues.
func quaySearch(ctx context.Context, u discover.Upstream, q discover.SearchQuery) (discover.SearchPage, error) {
	limit := clampLimit(q.Limit, discover.MaxSearchHits)
	offset := discover.Offset(q.Cursor)
	page, skip := offset/quayFindPage+1, offset%quayFindPage
	target := originURL(u.Base(), "/api/v1/find/repositories", url.Values{"query": {strings.TrimSpace(q.Text)}, "page": {strconv.Itoa(page)}})
	var a struct {
		Results []struct {
			Kind        string `json:"kind"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Namespace   struct {
				Name string `json:"name"`
			} `json:"namespace"`
		} `json:"results"`
		HasAdditional bool `json:"has_additional"`
	}
	if _, _, err := getJSON(ctx, u, target, &a); err != nil {
		return discover.SearchPage{}, err
	}
	out := discover.SearchPage{Total: -1}
	rows := a.Results
	if skip > len(rows) {
		skip = len(rows)
	}
	rows = rows[skip:]
	taken := min(len(rows), limit)
	for _, r := range rows[:taken] {
		name := strings.ToLower(r.Namespace.Name + "/" + r.Name)
		if r.Kind != "" && r.Kind != "repository" || !validName(name) {
			continue
		}
		out.Hits = append(out.Hits, discover.Hit{Name: name, Description: r.Description})
	}
	next := offset + taken
	if taken == len(rows) && !a.HasAdditional {
		out.Done = true
	} else {
		out.Cursor = discover.OffsetCursor(next)
	}
	return out, nil
}

// originURL is an address on the upstream's origin.
func originURL(base *url.URL, path string, q url.Values) *url.URL {
	return &url.URL{Scheme: base.Scheme, Host: base.Host, Path: path, RawQuery: q.Encode()}
}

// Children implements [discover.Catalogue]: a Docker Hub or Quay namespace, a GitLab project, or the catalog of a registry without a
// preset at its root. The namespaces and projects themselves are not listable: one is entered by its name, and the root lists nothing.
func (remoteAdapter) Children(ctx context.Context, u discover.Upstream, rq discover.LevelRequest) (discover.LevelPage, error) {
	preset := presetOf(u.Base())
	if preset == presetGHCR {
		return discover.LevelPage{}, discover.ErrNotOffered
	}
	if preset == "" {
		if len(rq.Level.Path) > 0 {
			return discover.LevelPage{}, discover.ErrNotOffered
		}
		return catalogChildren(ctx, u, rq)
	}
	if len(rq.Level.Path) == 0 {
		return discover.LevelPage{Total: -1, Done: true}, nil
	}
	if len(rq.Level.Path) != 1 {
		return discover.LevelPage{}, errs.E(errs.Invalid, errs.CodeValidation, "A level of this registry is one namespace or project.")
	}
	level := strings.ToLower(strings.Trim(rq.Level.Path[0], "/"))
	switch preset {
	case presetDockerHub:
		return hubNamespace(ctx, u, level, rq)
	case presetQuay:
		return quayNamespace(ctx, u, level, rq)
	}
	return gitlabProject(ctx, u, level, rq)
}

// catalogChildren pages the catalog of the distribution API (the mirror's listing).
func catalogChildren(ctx context.Context, u discover.Upstream, rq discover.LevelRequest) (discover.LevelPage, error) {
	limit := clampLimit(rq.Limit, discover.MaxLevelEntries)
	var last string
	if len(rq.Cursor) > 0 {
		_ = json.Unmarshal(rq.Cursor, &last) //nolint:errcheck // a cursor that does not read starts at the catalog's first name
	}
	names, resp, offered, err := readCatalog(ctx, u, limit, last)
	if err != nil {
		return discover.LevelPage{}, err
	}
	if !offered {
		return discover.LevelPage{}, discover.ErrNotOffered
	}
	out := discover.LevelPage{Total: -1}
	for _, n := range names {
		if validName(n) && len(out.Packages) < limit {
			out.Packages = append(out.Packages, discover.Hit{Name: n})
		}
	}
	fallback := ""
	if len(names) > 0 {
		fallback = names[len(names)-1]
	}
	next, more := nextLast(resp.Header, fallback)
	if !more || next == "" || next == last {
		out.Done = true
		return out, nil
	}
	b, err := json.Marshal(next)
	if err != nil {
		return discover.LevelPage{}, err
	}
	out.Cursor = b
	return out, nil
}

// hubNameChar is a character a Docker Hub namespace holds: its operator's rule for the name of a user or an organization is lowercase
// letters and digits (4 to 30 of them).
func hubNameChar(r rune) bool { return r >= 'a' && r <= 'z' || r >= '0' && r <= '9' }

// hubCertainChar is a character a name is asked for with: the rule's, and the underscore, which the registry's oldest namespaces may
// hold (its first index took [a-z0-9_]), so that only what no namespace can hold is refused before a request (Q-CUR-165). The length
// is not checked, for the same reason.
func hubCertainChar(r rune) bool { return hubNameChar(r) || r == '_' }

// hubAccount is the Hub API's account of a namespace, `GET /v2/users/{namespace}`: 200 for a user, 308 to /v2/orgs/{namespace} for an
// organization, 404 for a name Docker Hub does not have (recorded on 2026-11-24, testdata/catalogue/README.md). The listing of a
// namespace answers an unknown one with an empty page, not a 404, so the account is what tells the two apart.
func hubAccount(ns string) *url.URL {
	return &url.URL{Scheme: "https", Host: hubAPI, Path: "/v2/users/" + ns}
}

// hubByRule is the page of a namespace whose name holds a character no Docker Hub namespace holds: unknown, asked for nothing, with
// the name without those characters as the suggestion when Docker Hub has it (one request, Q-CUR-165).
func hubByRule(ctx context.Context, u discover.Upstream, ns, bad string) (discover.LevelPage, error) {
	unknown := &discover.UnknownLevel{Rule: discover.NameRuleLowercaseDigits, Forbidden: bad}
	if s := dropRunes(ns, bad); s != "" {
		exists, _, err := discover.Exists(ctx, u, hubAccount(s))
		if err != nil {
			return discover.LevelPage{}, err
		}
		if exists {
			unknown.Suggestion = s
		}
	}
	return discover.LevelPage{Done: true, Unknown: unknown}, nil
}

// dropRunes is s without the characters of drop.
func dropRunes(s, drop string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(drop, r) {
			return -1
		}
		return r
	}, s)
}

// hubNamespace lists a namespace of Docker Hub, `GET /v2/namespaces/{namespace}/repositories` on the Hub API. A name that breaks the
// namespaces' rule is not asked for; a first page that lists nothing asks the namespace's account whether Docker Hub has it.
func hubNamespace(ctx context.Context, u discover.Upstream, ns string, rq discover.LevelRequest) (discover.LevelPage, error) {
	if bad := discover.Forbidden(ns, hubCertainChar); bad != "" {
		return hubByRule(ctx, u, ns, bad)
	}
	limit := clampLimit(rq.Limit, hubPageMax)
	page := pageOf(rq.Cursor)
	target := &url.URL{Scheme: "https", Host: hubAPI, Path: "/v2/namespaces/" + ns + "/repositories", RawQuery: url.Values{
		"page_size": {strconv.Itoa(limit)}, "page": {strconv.Itoa(page)},
	}.Encode()}
	var a struct {
		Count   int     `json:"count"`
		Next    *string `json:"next"`
		Results []struct {
			Name        string `json:"name"`
			Namespace   string `json:"namespace"`
			Description string `json:"description"`
			LastUpdated string `json:"last_updated"`
		} `json:"results"`
	}
	found, _, err := getJSON(ctx, u, target, &a)
	if err != nil {
		return discover.LevelPage{}, err
	}
	if !found {
		return discover.LevelPage{Done: true, Unknown: &discover.UnknownLevel{}}, nil
	}
	if page == 1 && a.Count == 0 && len(a.Results) == 0 {
		return discover.EmptyOrUnknown(ctx, u, hubAccount(ns))
	}
	out := discover.LevelPage{Total: a.Count}
	for _, r := range a.Results {
		name := strings.ToLower(ns + "/" + r.Name)
		if !validName(name) {
			continue
		}
		out.Packages = append(out.Packages, discover.Hit{Name: name, Description: r.Description, Updated: parseTime(r.LastUpdated)})
	}
	if a.Next == nil || *a.Next == "" || len(a.Results) == 0 {
		out.Done = true
	} else {
		out.Cursor = discover.OffsetCursor(page + 1)
	}
	return out, nil
}

// quayCursor is the position in a Quay namespace: the API's page token and the repositories of its page already answered.
type quayCursor struct {
	Token string `json:"token,omitempty"`
	Skip  int    `json:"skip,omitempty"`
}

// quayNamespace lists a namespace of Quay, `GET /api/v1/repository?public=true&namespace=`; the API's pages are of 100, so a smaller
// page continues within one.
func quayNamespace(ctx context.Context, u discover.Upstream, ns string, rq discover.LevelRequest) (discover.LevelPage, error) {
	limit := clampLimit(rq.Limit, quayPageMax)
	var cur quayCursor
	if len(rq.Cursor) > 0 {
		_ = json.Unmarshal(rq.Cursor, &cur) //nolint:errcheck // a cursor that does not read starts at the namespace's first page
	}
	q := url.Values{"namespace": {ns}, "public": {"true"}}
	if cur.Token != "" {
		q.Set("next_page", cur.Token)
	}
	var a struct {
		Repositories []struct {
			Namespace   string `json:"namespace"`
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"repositories"`
		NextPage string `json:"next_page"`
	}
	if _, _, err := getJSON(ctx, u, originURL(u.Base(), "/api/v1/repository", q), &a); err != nil {
		return discover.LevelPage{}, err
	}
	if cur.Token == "" && cur.Skip == 0 && len(a.Repositories) == 0 {
		// The listing answers an unknown namespace with an empty list; Quay's organization and user accounts answer 404 for a name it
		// does not have (recorded on 2026-11-24).
		return discover.EmptyOrUnknown(ctx, u, originURL(u.Base(), "/api/v1/organization/"+url.PathEscape(ns), nil),
			originURL(u.Base(), "/api/v1/users/"+url.PathEscape(ns), nil))
	}
	rows := a.Repositories
	skip := min(cur.Skip, len(rows))
	rows = rows[skip:]
	taken := min(len(rows), limit)
	out := discover.LevelPage{Total: -1}
	for _, r := range rows[:taken] {
		name := strings.ToLower(r.Namespace + "/" + r.Name)
		if validName(name) {
			out.Packages = append(out.Packages, discover.Hit{Name: name, Description: r.Description})
		}
	}
	var next quayCursor
	switch {
	case taken < len(rows):
		next = quayCursor{Token: cur.Token, Skip: skip + taken}
	case a.NextPage != "":
		next = quayCursor{Token: a.NextPage}
	default:
		out.Done = true
		return out, nil
	}
	b, err := json.Marshal(next)
	if err != nil {
		return discover.LevelPage{}, err
	}
	out.Cursor = b
	return out, nil
}

// gitlabProject lists the images of a public GitLab project, `GET /api/v4/projects/{path}/registry/repositories` on gitlab.com; a
// group's listing needs a token and is not offered.
func gitlabProject(ctx context.Context, u discover.Upstream, project string, rq discover.LevelRequest) (discover.LevelPage, error) {
	limit := clampLimit(rq.Limit, hubPageMax)
	page := pageOf(rq.Cursor)
	target := &url.URL{
		Scheme: "https", Host: gitlabAPI,
		Path:     "/api/v4/projects/" + project + "/registry/repositories",
		RawPath:  "/api/v4/projects/" + url.PathEscape(project) + "/registry/repositories",
		RawQuery: url.Values{"per_page": {strconv.Itoa(limit)}, "page": {strconv.Itoa(page)}}.Encode(),
	}
	var rows []struct {
		Path      string `json:"path"`
		CreatedAt string `json:"created_at"`
	}
	found, resp, err := getJSONAnswer(ctx, u, target, &rows)
	if err != nil {
		return discover.LevelPage{}, err
	}
	if !found {
		// GitLab names what it does not have: "404 Project Not Found" for a project it has not or does not show without a token
		// (recorded on 2026-11-24); another 404 is a project whose registry it does not show.
		var m struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(resp.Body, &m) == nil && m.Message == "404 Project Not Found" {
			return discover.LevelPage{Done: true, Unknown: &discover.UnknownLevel{}}, nil
		}
		return discover.LevelPage{}, errs.E(errs.NotFound, errs.CodeCatalogueNotFound, "GitLab has no public project "+project+" with a registry.")
	}
	h := resp.Header
	out := discover.LevelPage{Total: -1}
	if t, err := strconv.Atoi(h.Get("X-Total")); err == nil {
		out.Total = t
	}
	for _, r := range rows {
		if name := strings.ToLower(r.Path); validName(name) {
			out.Packages = append(out.Packages, discover.Hit{Name: name, Updated: parseTime(r.CreatedAt)})
		}
	}
	if n, err := strconv.Atoi(h.Get("X-Next-Page")); err == nil && n > page && len(rows) > 0 {
		out.Cursor = discover.OffsetCursor(n)
	} else {
		out.Done = true
	}
	return out, nil
}

// Package implements [discover.Catalogue]: an image's tags, which are its versions (Q-CUR-10). Docker Hub's and Quay's APIs tell each
// tag's time, digest and size; every other upstream answers the tags list of the distribution API, ordered newest first by the
// format's tag order (tagorder).
func (a remoteAdapter) Package(ctx context.Context, u discover.Upstream, name string, rq discover.VersionsRequest) (discover.PackageInfo, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !validName(name) {
		return discover.PackageInfo{}, nil
	}
	switch presetOf(u.Base()) {
	case presetDockerHub:
		return hubTags(ctx, u, hubName(name), rq)
	case presetQuay:
		return quayTags(ctx, u, name, rq)
	}
	tags, found, err := a.readTags(ctx, u, name)
	if err != nil || !found {
		return discover.PackageInfo{}, err
	}
	sort.Slice(tags, func(i, j int) bool { return tagorder.Compare(tags[i], tags[j]) > 0 })
	all := make([]discover.VersionFacts, 0, len(tags))
	for _, t := range tags {
		f := discover.VersionFacts{Version: t}
		if tagorder.Prerelease(t) {
			f.Flags = append(f.Flags, "prerelease")
		}
		all = append(all, f)
	}
	info := discover.PackageInfo{Found: true, Name: name}
	info.Versions, info.Total, info.Cursor, info.Done = discover.PageVersions(all, rq)
	return info, nil
}

// hubTags reads an image's tags from Docker Hub's API, newest first by the time of their last push, as the API answers them.
func hubTags(ctx context.Context, u discover.Upstream, name string, rq discover.VersionsRequest) (discover.PackageInfo, error) {
	ns, image, _ := strings.Cut(name, "/")
	limit := clampLimit(rq.Limit, hubPageMax)
	page := pageOf(rq.Cursor)
	target := &url.URL{Scheme: "https", Host: hubAPI, Path: "/v2/namespaces/" + ns + "/repositories/" + image + "/tags", RawQuery: url.Values{
		"page_size": {strconv.Itoa(limit)}, "page": {strconv.Itoa(page)},
	}.Encode()}
	var a struct {
		Count   int     `json:"count"`
		Next    *string `json:"next"`
		Results []struct {
			Name        string `json:"name"`
			FullSize    int64  `json:"full_size"`
			LastUpdated string `json:"last_updated"`
			Digest      string `json:"digest"`
			Images      []struct {
				Architecture string  `json:"architecture"`
				OS           string  `json:"os"`
				Variant      *string `json:"variant"`
			} `json:"images"`
		} `json:"results"`
	}
	found, _, err := getJSON(ctx, u, target, &a)
	if err != nil || !found {
		return discover.PackageInfo{}, err
	}
	info := discover.PackageInfo{Found: true, Name: name, Total: a.Count}
	for _, r := range a.Results {
		if !ValidTag(r.Name) {
			continue
		}
		f := discover.VersionFacts{Version: r.Name, Published: parseTime(r.LastUpdated), Size: max(r.FullSize, 0), Digest: r.Digest}
		for _, im := range r.Images {
			p := im.OS + "/" + im.Architecture
			if im.Variant != nil && *im.Variant != "" {
				p += "/" + *im.Variant
			}
			if im.OS != "" && im.Architecture != "" {
				f.Platforms = append(f.Platforms, p)
			}
		}
		if tagorder.Prerelease(r.Name) {
			f.Flags = append(f.Flags, "prerelease")
		}
		info.Versions = append(info.Versions, filterVersion(f, rq.Filter)...)
	}
	if a.Next == nil || *a.Next == "" || len(a.Results) == 0 {
		info.Done = true
	} else {
		info.Cursor = discover.OffsetCursor(page + 1)
	}
	return info, nil
}

// quayTags reads an image's active tags from Quay's API, newest first as the API answers them.
func quayTags(ctx context.Context, u discover.Upstream, name string, rq discover.VersionsRequest) (discover.PackageInfo, error) {
	limit := clampLimit(rq.Limit, quayPageMax)
	page := pageOf(rq.Cursor)
	target := originURL(u.Base(), "/api/v1/repository/"+name+"/tag/", url.Values{
		"limit": {strconv.Itoa(limit)}, "page": {strconv.Itoa(page)}, "onlyActiveTags": {"true"},
	})
	var a struct {
		Tags []struct {
			Name           string `json:"name"`
			ManifestDigest string `json:"manifest_digest"`
			LastModified   string `json:"last_modified"`
			Size           int64  `json:"size"`
		} `json:"tags"`
		HasAdditional bool `json:"has_additional"`
	}
	found, _, err := getJSON(ctx, u, target, &a)
	if err != nil || !found {
		return discover.PackageInfo{}, err
	}
	info := discover.PackageInfo{Found: true, Name: name, Total: -1}
	for _, t := range a.Tags {
		if !ValidTag(t.Name) {
			continue
		}
		f := discover.VersionFacts{Version: t.Name, Size: max(t.Size, 0), Digest: t.ManifestDigest}
		if ts, err := time.Parse("Mon, 02 Jan 2006 15:04:05 -0700", t.LastModified); err == nil {
			f.Published = ts.UTC()
		}
		if tagorder.Prerelease(t.Name) {
			f.Flags = append(f.Flags, "prerelease")
		}
		info.Versions = append(info.Versions, filterVersion(f, rq.Filter)...)
	}
	if !a.HasAdditional || len(a.Tags) == 0 {
		info.Done = true
	} else {
		info.Cursor = discover.OffsetCursor(page + 1)
	}
	return info, nil
}

// filterVersion keeps a version whose tag holds the filter (case-insensitive), as discover.PageVersions does for a list held whole.
func filterVersion(f discover.VersionFacts, filter string) []discover.VersionFacts {
	if filter = strings.ToLower(strings.TrimSpace(filter)); filter != "" && !strings.Contains(strings.ToLower(f.Version), filter) {
		return nil
	}
	return []discover.VersionFacts{f}
}

// parseTime reads an RFC 3339 time of an operator's API; the zero time for none.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
