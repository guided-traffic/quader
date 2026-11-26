package gui

import (
	"net/url"

	"github.com/guided-traffic/quader/internal/bastion/catalogue"
	pg "github.com/guided-traffic/quader/internal/bastion/gui/page"
)

// docsByPackage are the entries of the documentation the sitemap lists (W-35's B.11, R-BAS-125): what the reader's DocEntries answers
// (the newest version's entries of every public package while docs is on, nothing while it is off), by the package's catalogue ID.
func (h *Handler) docsByPackage() map[string][]catalogue.DocHit {
	hits, err := h.o.Catalogue.DocEntries()
	if err != nil {
		return nil
	}
	out := map[string][]catalogue.DocHit{}
	for _, d := range hits {
		out[d.PackageID] = append(out[d.PackageID], d)
	}
	return out
}

// docRoutes are the routes of a package's documentation pages that the sitemap lists beside the package's page in a project: each
// entry of its newest version's documentation whose tree was read from a repository of that project, as the canonical address of its
// page (the package's route with tab=documentation and doc=<type>/<name>, as the page's head writes it), so a search engine reads
// one address per page.
func docRoutes(docs map[string][]catalogue.DocHit, e catalogue.Entry, project string) []string {
	var out []string
	for _, d := range docs[e.ID] {
		if d.Project != project {
			continue
		}
		q := url.Values{"tab": {"documentation"}, "doc": {d.Doc()}}
		out = append(out, pg.PackageRoute(project, e.Format, e.Name)+"?"+q.Encode())
	}
	return out
}
