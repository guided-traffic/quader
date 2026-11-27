package gui

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/catalogue"
	pg "github.com/guided-traffic/quader/internal/bastion/gui/page"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/webui"
)

// What a crawler reads of a page (R-BAS-121, W-35's B.11): the Bastion answers a page's address with the application's index.html and
// fills its two slots from the catalogue's documents, the same documents the application reads a moment later, through the members
// of the main installation's shapes that are the same for every format. Nothing here reads a format's metadata, and nothing reaches
// further than the catalogue's reader.

// maxListed bounds the links of a page's text: a project's packages, a package's versions. The rest is reached through the
// application, the sitemap (the packages) and the pages' own links.
const maxListed = catalogue.MaxLimit

// fill is the page hook of the bundle's handler: the status of a page's address and its head and text. While the replica holds no
// state a page answers 503 with Retry-After and names nothing (R-BAS-117).
func (h *Handler) fill(w http.ResponseWriter, r *http.Request, locale, nonce string) webui.Fill {
	st := h.o.Catalogue.State()
	if !st.Decided || !st.Ready {
		w.Header().Set("Retry-After", strconv.Itoa(int(notReadyRetry/time.Second)))
		return webui.Fill{Status: http.StatusServiceUnavailable, Head: h.pages.Head(pg.Page{Locale: locale, Noindex: true}, nonce)}
	}
	tail := strings.TrimPrefix(r.URL.EscapedPath(), webui.MountPath+locale+"/")
	a := pg.Parse(locale, tail, r.URL.Query())
	p, status := h.describe(r.Context(), a)
	if status != http.StatusOK {
		// A page the application shows as not found is answered so: a crawler drops the address (D15), a person reads the same
		// page as before.
		p = h.notFound(a)
	}
	return webui.Fill{Status: status, Head: h.pages.Head(p, nonce), Body: h.pages.Body(p)}
}

// describe reads what the page of an address shows; a status other than 200 is a page the catalogue does not show.
func (h *Handler) describe(ctx context.Context, a pg.Address) (pg.Page, int) {
	switch a.Kind {
	case pg.Start:
		return h.startPage(a)
	case pg.List:
		return h.listPage(a)
	case pg.Package, pg.Versions, pg.Version:
		return h.packagePage(ctx, a)
	case pg.Legal:
		return h.legalPage(a)
	case pg.Setup:
		return h.setupPage(a)
	}
	return pg.Page{}, http.StatusNotFound
}

// setupPage is a public repository's page Set up client (R-BAS-129): found while the public set holds the repository, never indexed
// (Q-BAS-401: its content is commands per client, which the text never carries, Q-BAS-409), its canonical address without the client.
func (h *Handler) setupPage(a pg.Address) (pg.Page, int) {
	pub, err := h.o.Catalogue.PublicSet()
	if err != nil {
		return pg.Page{}, http.StatusNotFound
	}
	held := false
	for _, r := range pub.Repositories {
		held = held || r.Name == a.Project+"/"+a.Repository
	}
	if !held {
		return pg.Page{}, http.StatusNotFound
	}
	t := pg.TextsOf(a.Locale)
	p := h.base(a, a.Route(), nil)
	p.Title, p.Heading, p.Lead, p.Noindex = t.SetupClient, t.SetupClient, a.Project+"/"+a.Repository, true
	p.LD = webPage(t.SetupClient, p.Canonical)
	p.Crumbs = []pg.Link{
		{Text: t.Projects, Href: local(a.Locale, "")},
		{Text: h.projectName(a.Project).DisplayName, Href: local(a.Locale, pg.Address{Kind: pg.List, Project: a.Project}.Route())},
	}
	return p, http.StatusOK
}

// base is a page with what every page has: its language, its canonical address and the other languages' addresses, the footer's links.
func (h *Handler) base(a pg.Address, route string, query url.Values) pg.Page {
	p := pg.Page{Locale: a.Locale, Canonical: pg.URL(h.o.BaseURL, a.Locale, route, query), Legal: h.legalLinks(a.Locale)}
	for _, l := range pg.Locales() {
		p.Alternates = append(p.Alternates, pg.Alternate{Lang: l, Href: pg.URL(h.o.BaseURL, l, route, query)})
	}
	p.Alternates = append(p.Alternates, pg.Alternate{Lang: "x-default", Href: pg.URL(h.o.BaseURL, "", route, query)})
	return p
}

func (h *Handler) notFound(a pg.Address) pg.Page {
	t := pg.TextsOf(a.Locale)
	return pg.Page{
		Locale: a.Locale, Title: t.NotFound, Heading: t.NotFound, Lead: t.NotFoundText, Noindex: true, Legal: h.legalLinks(a.Locale),
		Crumbs: []pg.Link{{Text: t.Projects, Href: local(a.Locale, "")}},
	}
}

// local is a link of the text: the route's address on the Bastion's own host.
func local(locale, route string) string { return webui.MountPath + locale + "/" + route }

func (h *Handler) legalLinks(locale string) []pg.Link {
	t := pg.TextsOf(locale)
	set := h.o.Legal.Pages(locale)
	var out []pg.Link
	if set.Imprint != nil {
		out = append(out, pg.Link{Text: t.Imprint, Href: local(locale, "legal/imprint")})
	}
	if set.Privacy != nil {
		out = append(out, pg.Link{Text: t.Privacy, Href: local(locale, "legal/privacy")})
	}
	return out
}

func webPage(name, url string) map[string]any {
	return map[string]any{"@context": "https://schema.org", "@type": "WebPage", "name": name, "url": url}
}

// shownProjects are the projects whose list the text links: those of the public set that hold a package the catalogue lists
// (Q-BAS-399), each with its name (R-BAS-128).
func (h *Handler) shownProjects() ([]iface.CatalogueProject, error) {
	pub, err := h.o.Catalogue.PublicSet()
	if err != nil {
		return nil, err
	}
	out := []iface.CatalogueProject{}
	for _, p := range pub.Projects {
		res, err := h.o.Catalogue.Search(catalogue.Query{Project: p.Key, Limit: 1})
		if err != nil {
			return nil, err
		}
		if len(res.Items) > 0 {
			out = append(out, p)
		}
	}
	return out, nil
}

// projectName is a project's display name as the public set names it (R-BAS-128), its key where the set does not hold it.
func (h *Handler) projectName(key string) iface.CatalogueProject {
	if pub, err := h.o.Catalogue.PublicSet(); err == nil {
		for _, p := range pub.Projects {
			if p.Key == key {
				return p
			}
		}
	}
	return iface.CatalogueProject{Key: key, DisplayName: key}
}

func (h *Handler) startPage(a pg.Address) (pg.Page, int) {
	t := pg.TextsOf(a.Locale)
	projects, err := h.shownProjects()
	if err != nil {
		return pg.Page{}, http.StatusNotFound
	}
	p := h.base(a, "", nil)
	p.Title, p.Heading = t.Projects, t.Projects
	p.LD = webPage(t.Projects, p.Canonical)
	s := pg.Section{Heading: t.Projects}
	for _, pr := range projects {
		s.Links = append(s.Links, pg.Link{
			Text: pr.DisplayName, Note: pg.Cut(pr.Description, 160), Href: local(a.Locale, pg.Address{Kind: pg.List, Project: pr.Key}.Route()),
		})
	}
	p.Sections = []pg.Section{s}
	return p, http.StatusOK
}

func (h *Handler) listPage(a pg.Address) (pg.Page, int) {
	t := pg.TextsOf(a.Locale)
	res, err := h.o.Catalogue.Search(catalogue.Query{Project: a.Project, Limit: maxListed})
	if err != nil || len(res.Items) == 0 {
		// A project the start does not offer: not public, or public without a package the catalogue lists (Q-BAS-399).
		return pg.Page{}, http.StatusNotFound
	}
	route := pg.Address{Kind: pg.List, Project: a.Project}.Route()
	p := h.base(a, route, nil)
	project := h.projectName(a.Project)
	p.Title = t.PackagesIn(project.DisplayName)
	p.Heading = project.DisplayName
	p.Lead, p.Description = project.Description, project.Description
	// A list of results is not indexed; its canonical address is the list itself (W-35's B.11).
	p.Noindex = a.Filtered
	p.LD = webPage(p.Title, p.Canonical)
	p.Crumbs = []pg.Link{{Text: t.Projects, Href: local(a.Locale, "")}}
	s := pg.Section{Heading: t.Packages}
	for _, e := range res.Items {
		s.Links = append(s.Links, pg.Link{
			Text: e.Name, Note: pg.Cut(e.Description, 160), Href: local(a.Locale, pg.PackageRoute(a.Project, e.Format, e.Name)),
		})
	}
	p.Sections = []pg.Section{s}
	return p, http.StatusOK
}

func (h *Handler) legalPage(a pg.Address) (pg.Page, int) {
	t := pg.TextsOf(a.Locale)
	set := h.o.Legal.Pages(a.Locale)
	lp, title := set.Imprint, t.Imprint
	if a.LegalPage == "privacy" {
		lp, title = set.Privacy, t.Privacy
	}
	if lp == nil {
		return pg.Page{}, http.StatusNotFound
	}
	p := h.base(a, a.Route(), nil)
	p.Title, p.Heading = title, title
	p.LD = webPage(title, p.Canonical)
	// The operator's text as the Bastion rendered it at start through the README's policy (R-BAS-100).
	p.Readme = template.HTML(lp.HTML) //nolint:gosec // sanitized by internal/bastion/legal at start
	return p, http.StatusOK
}

// The members of the main installation's shapes the text reads (W-35's B.11): the same for every format. A format's metadata is never
// decoded, so nothing of it can reach a pg.
type (
	versionDoc struct {
		Version     string    `json:"version"`
		PublishedAt time.Time `json:"publishedAt"`
		Licenses    []string  `json:"licenses"`
		Keywords    []string  `json:"keywords"`
	}
	readmeDoc struct {
		HTML string `json:"html"`
	}
	docsTree struct {
		Groups []struct {
			Types []struct {
				Type    string `json:"type"`
				Entries []struct {
					Name             string `json:"name"`
					ShortDescription string `json:"shortDescription"`
				} `json:"entries"`
			} `json:"types"`
		} `json:"groups"`
	}
)

// readJSON reads a document of the catalogue into v; ok is false for one it does not hold or show, or that does not decode.
func (h *Handler) readJSON(ctx context.Context, p iface.CataloguePath, v any) bool {
	d, ok, err := h.o.Catalogue.Document(ctx, p.String())
	if err != nil || !ok {
		return false
	}
	return json.Unmarshal(d.Raw, v) == nil
}

// inProject reports whether a package is public in the address's project: one of its public repositories is the project's.
func (h *Handler) inProject(e catalogue.Entry, project string) bool {
	pub, err := h.o.Catalogue.PublicSet()
	if err != nil {
		return false
	}
	held := map[string]bool{}
	for _, r := range e.Repositories {
		held[r] = true
	}
	for _, r := range pub.Repositories {
		if held[r.Name] && strings.HasPrefix(r.Name, project+"/") {
			return true
		}
	}
	return false
}

func (h *Handler) packagePage(ctx context.Context, a pg.Address) (pg.Page, int) {
	t := pg.TextsOf(a.Locale)
	e, ok, err := h.o.Catalogue.Lookup(a.Format, a.Name)
	if err != nil || !ok || !h.inProject(e, a.Project) {
		return pg.Page{}, http.StatusNotFound
	}
	version := a.Version
	if version == "" {
		version = e.LatestVersion
	}
	var v versionDoc
	if !h.readJSON(ctx, iface.CataloguePath{Kind: iface.CatalogueVersion, Package: e.ID, Version: version}, &v) && a.Kind == pg.Version {
		return pg.Page{}, http.StatusNotFound
	}
	route := a.Route()
	pkgRoute := pg.PackageRoute(a.Project, a.Format, a.Name)
	var query url.Values
	entry := h.docEntry(ctx, a, e.ID, version)
	if entry != nil {
		query = url.Values{"tab": {"documentation"}, "doc": {a.Doc}}
	}
	p := h.base(a, route, query)
	// An address that opens the open findings is not indexed, and its canonical address is the page without them (W-35's B.11).
	p.Noindex = a.Findings
	p.Crumbs = []pg.Link{
		{Text: t.Projects, Href: local(a.Locale, "")},
		{Text: h.projectName(a.Project).DisplayName, Href: local(a.Locale, pg.Address{Kind: pg.List, Project: a.Project}.Route())},
	}
	p.Lead = e.Description
	p.Description = e.Description
	switch a.Kind {
	case pg.Versions:
		p.Title = t.VersionsOf(e.Name)
		p.Heading = p.Title
		p.LD = webPage(p.Title, p.Canonical)
		p.Crumbs = append(p.Crumbs, pg.Link{Text: e.Name, Href: local(a.Locale, pkgRoute)})
	case pg.Version:
		p.Title = e.Name + " " + version
		p.Heading = p.Title
		p.Crumbs = append(p.Crumbs, pg.Link{Text: e.Name, Href: local(a.Locale, pkgRoute)})
	default:
		p.Title, p.Heading = e.Name, e.Name
	}
	if a.Kind != pg.Versions {
		p.Facts = facts(t, a, e, v, version)
		p.LD = softwareLD(e, v, version, p.Canonical)
		var rd readmeDoc
		if h.readJSON(ctx, iface.CataloguePath{Kind: iface.CatalogueReadme, Package: e.ID, Version: version}, &rd) {
			p.Readme = template.HTML(rd.HTML) //nolint:gosec // rendered and sanitized by the main installation's README renderer (R-UI-008)
		}
	}
	if entry != nil {
		p.Title = entry.name + " – " + e.Name + " " + version
		p.Heading, p.Lead, p.Description = entry.name, entry.short, entry.short
		p.LD = map[string]any{
			"@context": "https://schema.org", "@type": "TechArticle", "headline": entry.name, "description": entry.short, "url": p.Canonical,
			"about": map[string]any{"@type": "SoftwareApplication", "name": e.Name, "softwareVersion": version},
		}
		p.Readme = ""
	}
	if a.Kind != pg.Version {
		p.Sections = append(p.Sections, h.versionLinks(ctx, t, a, e))
	}
	return p, http.StatusOK
}

// facts are the version's facts every format has: its newest version, when it was published, its licences and keywords.
func facts(t pg.Texts, a pg.Address, e catalogue.Entry, v versionDoc, version string) []pg.Fact {
	var out []pg.Fact
	if a.Kind == pg.Package && version != "" {
		out = append(out, pg.Fact{Term: t.LatestVersion, Text: version, Href: local(a.Locale, pg.VersionRoute(a.Project, a.Format, a.Name, version))})
	}
	if !v.PublishedAt.IsZero() {
		out = append(out, pg.Fact{Term: t.Published, Text: v.PublishedAt.UTC().Format("2006-01-02"), Datetime: v.PublishedAt.UTC().Format(time.RFC3339)})
	}
	if len(v.Licenses) > 0 {
		out = append(out, pg.Fact{Term: t.Licenses, Text: strings.Join(v.Licenses, ", ")})
	}
	keywords := v.Keywords
	if len(keywords) == 0 {
		keywords = e.Keywords
	}
	if len(keywords) > 0 {
		out = append(out, pg.Fact{Term: t.Keywords, Text: strings.Join(keywords, ", ")})
	}
	return out
}

// softwareLD is a package's or a version's JSON-LD (W-35's B.11): SoftwareApplication with what every format has.
func softwareLD(e catalogue.Entry, v versionDoc, version, url string) map[string]any {
	ld := map[string]any{"@context": "https://schema.org", "@type": "SoftwareApplication", "name": e.Name, "url": url}
	if version != "" {
		ld["softwareVersion"] = version
	}
	if e.Description != "" {
		ld["description"] = e.Description
	}
	if len(v.Licenses) > 0 {
		ld["license"] = strings.Join(v.Licenses, ", ")
	}
	keywords := v.Keywords
	if len(keywords) == 0 {
		keywords = e.Keywords
	}
	if len(keywords) > 0 {
		ld["keywords"] = strings.Join(keywords, ", ")
	}
	if !v.PublishedAt.IsZero() {
		ld["datePublished"] = v.PublishedAt.UTC().Format(time.RFC3339)
	}
	return ld
}

// versionLinks is the section of a package's versions, the first page of the main installation's order.
func (h *Handler) versionLinks(ctx context.Context, t pg.Texts, a pg.Address, e catalogue.Entry) pg.Section {
	s := pg.Section{Heading: t.Versions}
	res, err := h.o.Catalogue.Versions(ctx, e.ID, "", maxListed)
	if err != nil {
		return s
	}
	for _, item := range res.Items {
		var v versionDoc
		if json.Unmarshal(item, &v) != nil || v.Version == "" {
			continue
		}
		s.Links = append(s.Links, pg.Link{Text: v.Version, Href: local(a.Locale, pg.VersionRoute(a.Project, a.Format, a.Name, v.Version))})
	}
	return s
}

type docEntryHead struct{ name, short string }

// docEntry is the documentation entry the address opens (`doc=<type>/<name>`), when the catalogue shows the version's documentation
// and its tree holds the entry; nil otherwise, and the address is the version's pg.
func (h *Handler) docEntry(ctx context.Context, a pg.Address, id, version string) *docEntryHead {
	typ, name, ok := strings.Cut(a.Doc, "/")
	if !ok || typ == "" || name == "" || typ == "file" || a.Findings {
		return nil
	}
	var tree docsTree
	if !h.readJSON(ctx, iface.CataloguePath{Kind: iface.CatalogueDocs, Package: id, Version: version}, &tree) {
		return nil
	}
	for _, g := range tree.Groups {
		for _, ty := range g.Types {
			if ty.Type != typ {
				continue
			}
			for _, en := range ty.Entries {
				if en.Name == name {
					return &docEntryHead{name: en.Name, short: en.ShortDescription}
				}
			}
		}
	}
	return nil
}
