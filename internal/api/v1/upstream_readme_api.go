package v1

import (
	"net/http"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/mirror/discover"
	"github.com/guided-traffic/quader/internal/repository"
)

// The README of a package of the upstream, read through and not stored (remote-curation.md R-CUR-028), and the facts of its newest
// version on the package's answer. The facts are no wiki content and every reader of the tab gets them; the README is (R-AUTHZ-034), so
// the handler asks the wiki of the repository for the package before the engine sends anything, and a caller who may not read it gets
// the 404 of a package without a README, as the README of a stored version answers (R-API-029). The operation is authenticated and
// not public, so it carries no x-quader-part (which marks the public GETs whose check runs before the handler, Q-API-260): the check
// is this handler's.

// mayReadUpstreamWiki is the wiki question for a package of the repository's upstream: the question form, nothing audited.
func mayReadUpstreamWiki(c catalogueCall, name string) bool {
	if c.acc == nil {
		return false
	}
	return c.acc.D.WikiReadableOn(c.acc.Index(), c.acc.Subject, authz.Coordinate{
		Repository: c.repo.Name, Class: repository.Remote, Kind: authz.KindMetadata, Package: canonicalName(c.repo.Format, name),
		Actions: []authz.Action{authz.Read},
	})
}

// GetUpstreamReadme implements GET …/upstream/readme (R-CUR-028).
func (s *Server) GetUpstreamReadme(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey string, p gen.GetUpstreamReadmeParams) {
	c, err := s.catalogueSetup(r, projectKey, repoKey)
	var ps PackageServices
	if err == nil {
		ps, err = s.packageServices()
	}
	if err == nil && ps.Readme == nil {
		err = unavailable("The README renderer")
	}
	if err == nil && !mayReadUpstreamWiki(c, p.Name) {
		err = errs.E(errs.NotFound, errs.CodeNotFound, "The package has no README.")
	}
	var rd discover.Readme
	if err == nil {
		version := ""
		if p.Version != nil {
			version = *p.Version
		}
		rd, err = s.o.UpstreamCatalogue.Engine.Readme(c.ctx, c.t, p.Name, version)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.getUpstreamReadme"))
		return
	}
	out := gen.UpstreamReadme{Version: rd.Version, State: gen.UpstreamReadmeNone}
	switch {
	case rd.Unreachable != "":
		host := rd.Unreachable
		out.State, out.Host = gen.UpstreamReadmeUnreachable, &host
	case rd.Found:
		// An upstream's HTML (the Galaxy's docs-blob, a role's readme_html) is sanitized as it is; npm's and NuGet's are Markdown.
		res, contentType := ps.Readme.Render(rd.Text), gen.PackageReadmeContentTypeTextmarkdown
		if rd.HTML {
			res, contentType = ps.Readme.RenderHTML(rd.Text), gen.PackageReadmeContentTypeTexthtml
		}
		out.State = gen.UpstreamReadmeAvailable
		out.Readme = &gen.PackageReadme{
			ContentType: contentType, Html: res.HTML, Truncated: res.Truncated || rd.Truncated, ExternalImagesBlocked: res.ExternalImagesBlocked,
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, r, http.StatusOK, out)
}

// aboutRow is the package answer's about (R-CUR-028): nil where the answers told nothing.
func aboutRow(a discover.About) any {
	if a.Empty() {
		return nil
	}
	links := make([]map[string]any, 0, len(a.Links))
	for _, l := range a.Links {
		links = append(links, map[string]any{"kind": string(l.Kind), "url": l.URL})
	}
	deps := make([]map[string]any, 0, len(a.Dependencies))
	for _, d := range a.Dependencies {
		deps = append(deps, map[string]any{"name": d.Name, "range": nullString(d.Range), "group": nullString(d.Group)})
	}
	var dep any
	if d := a.Deprecation; d != nil {
		dep = map[string]any{
			"message": nullString(d.Message), "reasons": listOf(d.Reasons), "alternate": nullString(d.Alternate),
			"alternateRange": nullString(d.AlternateRange),
		}
	}
	return map[string]any{"version": a.Version, "license": nullString(a.License), "links": links, "dependencies": deps, "deprecation": dep}
}
