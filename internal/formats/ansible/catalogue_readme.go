package ansible

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/ansible/collection"
	"github.com/guided-traffic/quader/internal/formats/ansible/galaxydoc"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// What the tab Upstream shows of a collection or a role that is not stored (remote-curation.md R-CUR-028, formats/ansible.md R-ANS-047):
// the facts of the highest version from its version document, which the lookup reads for its tags and signed state anyway, and the
// README, read through and not stored: a collection's from the docs-blob of the version (the HTML the Galaxy rendered, `collection_readme`),
// a role's from its content document (`v1/roles/<id>/content/`, readme_html), each one request at the address the remote derives for the
// document as it does for a fill (from the API root, on the upstream's host; R-ANS-035, R-ANS-056). Nothing is stored: the catalogue's
// upstream reads the stored copy where the remote holds one and otherwise reads without storing.

var _ discover.Documented = remoteAdapter{}

// aboutOfVersion reads the facts of a collection version from its version document (`metadata`): the licence (the SPDX list, else the
// licence file's name), the homepage, the repository, the issue tracker and the documentation site, and the collections it depends on.
func aboutOfVersion(doc []byte, ver string) discover.About {
	var d struct {
		Metadata struct {
			License       []string          `json:"license"`
			LicenseFile   string            `json:"license_file"`
			Homepage      string            `json:"homepage"`
			Repository    string            `json:"repository"`
			Issues        string            `json:"issues"`
			Documentation string            `json:"documentation"`
			Dependencies  map[string]string `json:"dependencies"`
		} `json:"metadata"`
	}
	if !readsAs(doc, &d) {
		return discover.About{}
	}
	m := d.Metadata
	a := discover.About{Version: ver, License: strings.Join(m.License, " OR ")}
	if a.License == "" {
		a.License = m.LicenseFile
	}
	for _, l := range []discover.Link{
		{Kind: discover.LinkHomepage, URL: m.Homepage}, {Kind: discover.LinkRepository, URL: m.Repository},
		{Kind: discover.LinkIssues, URL: m.Issues}, {Kind: discover.LinkDocumentation, URL: m.Documentation},
	} {
		if l.URL != "" {
			a.Links = append(a.Links, l)
		}
	}
	names := make([]string, 0, len(m.Dependencies))
	for n := range m.Dependencies {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		a.Dependencies = append(a.Dependencies, discover.Dependency{Name: n, Range: m.Dependencies[n]})
	}
	return a
}

// Readme implements [discover.Documented]: a role's README from its content document, a collection version's from its docs-blob. Without
// a version the collection document names the highest (one more request where the remote holds no copy of it).
func (remoteAdapter) Readme(ctx context.Context, u discover.Upstream, name, ver string) (discover.Readme, error) {
	if id := roleIDOf(name); id != "" {
		return roleReadme(ctx, u, id, ver)
	}
	ns, n, ok := collection.SplitName(strings.ToLower(strings.TrimSpace(name)))
	if !ok {
		return discover.Readme{}, nil
	}
	if ver == "" {
		doc, found, err := u.Document(ctx, CollectionDocumentPath(ns, n), documentVariant)
		if err != nil || !found {
			return discover.Readme{}, err
		}
		var c struct {
			Highest *struct {
				Version string `json:"version"`
			} `json:"highest_version"`
		}
		if !readsAs(doc, &c) || c.Highest == nil {
			return discover.Readme{}, nil
		}
		ver = c.Highest.Version
	}
	if !isVersion(ver) {
		return discover.Readme{}, nil
	}
	r := discover.Readme{Version: ver, HTML: true}
	doc, found, err := u.Document(ctx, DocsDocumentPath(ns, n, ver), documentVariant)
	if host, refused := formats.UnreachedHost(err, nil); refused {
		r.Unreachable = host
		return r, nil
	}
	if err != nil || !found {
		return r, err
	}
	if len(doc) > galaxydoc.MaxDocumentBytes {
		return discover.Readme{}, errs.E(errs.UpstreamFailed, errs.CodeCatalogueAnswerTooLarge,
			"The upstream's documentation of "+ns+"."+n+" "+ver+" is larger than the bound of a docs-blob (16 MiB).")
	}
	var b struct {
		DocsBlob *struct {
			CollectionReadme *struct {
				HTML string `json:"html"`
			} `json:"collection_readme"`
		} `json:"docs_blob"`
	}
	if err := json.Unmarshal(doc, &b); err != nil || b.DocsBlob == nil {
		return discover.Readme{}, invalidDocument("a docs-blob", "it is not a docs_blob object")
	}
	if rd := b.DocsBlob.CollectionReadme; rd != nil && strings.TrimSpace(rd.HTML) != "" {
		r.Found, r.Text = true, rd.HTML
	}
	return discover.CutReadme(r), nil
}

// roleReadme reads a role's README from its content document: the HTML the Galaxy rendered of the role's current README, which is
// the README of its newest version (the Galaxy keeps one).
func roleReadme(ctx context.Context, u discover.Upstream, id, ver string) (discover.Readme, error) {
	r := discover.Readme{Version: ver, HTML: true}
	doc, found, err := u.Document(ctx, RoleContentDocumentPath(id), documentVariant)
	if host, refused := formats.UnreachedHost(err, nil); refused {
		r.Unreachable = host
		return r, nil
	}
	if err != nil || !found {
		return r, err
	}
	c, err := parseRoleContent(doc)
	if err != nil {
		return discover.Readme{}, err
	}
	if strings.TrimSpace(c.HTML) != "" {
		r.Found, r.Text = true, c.HTML
	}
	return discover.CutReadme(r), nil
}
