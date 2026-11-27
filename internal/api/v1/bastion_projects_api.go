package v1

import (
	"net/http"
	"strings"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/repository"
)

// What a Bastion may distribute (R-BAS-134): the administrator's allow list of a registration (endpoints:read to read it,
// endpoints:write to change it, as the registration's other members, Q-BAS-325), the Bastions a project's Owner may choose among
// (the Owner, or repositories:read-config), and the addresses of the repository's member bastions held to server.additional_base_urls.
// The declarations are in the document; the rules are those of bastionmain.Registrations.

// ListBastionProjects implements GET /api/v1/bastions/{name}/projects.
func (s *Server) ListBastionProjects(w http.ResponseWriter, r *http.Request, name string, _ gen.ListBastionProjectsParams) {
	if !s.bastionsReady(w, r) {
		return
	}
	lq, err := ParseList(r, ListSpec{Filters: []string{"q"}, Sorts: []string{"project"}}, s.o.API.Config)
	if err == nil && lq.After != nil && len(lq.After) != 1 {
		err = invalidCursor()
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	after := ""
	if lq.After != nil {
		after = lq.After[0]
	}
	page, err := s.o.Bastions.Projects(r.Context(), name, after, lq.First("q"), lq.Limit)
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	out := gen.BastionProjectList{Items: make([]gen.BastionProject, 0, len(page.Items))}
	for _, p := range page.Items {
		out.Items = append(out.Items, gen.BastionProject{
			Project: p.Key, DisplayName: p.DisplayName, AllowedAt: utc(p.AllowedAt), AllowedBy: p.AllowedBy, Repositories: orEmpty(p.Repositories),
		})
	}
	out.Totals.Projects, out.Totals.Repositories = page.Projects, page.Repositories
	if page.HasMore && len(page.Items) > 0 {
		c := lq.EncodeCursor([]string{page.Items[len(page.Items)-1].Key})
		out.NextCursor = &c
		w.Header().Set("Link", "<"+nextURL(r, c)+`>; rel="next"`)
	}
	writeJSON(w, r, http.StatusOK, out)
}

// AllowBastionProject implements PUT /api/v1/bastions/{name}/projects/{projectKey}.
func (s *Server) AllowBastionProject(w http.ResponseWriter, r *http.Request, name string, projectKey gen.ProjectKey) {
	if !s.bastionsReady(w, r) {
		return
	}
	if err := s.o.Bastions.AllowProject(r.Context(), s.caller(r), name, projectKey); err != nil {
		failIdentity(w, r, err)
		return
	}
	noContent(w)
}

// RemoveBastionProject implements DELETE /api/v1/bastions/{name}/projects/{projectKey}.
func (s *Server) RemoveBastionProject(w http.ResponseWriter, r *http.Request, name string, projectKey gen.ProjectKey) {
	if !s.bastionsReady(w, r) {
		return
	}
	if err := s.o.Bastions.RemoveProject(r.Context(), s.caller(r), name, projectKey); err != nil {
		failIdentity(w, r, err)
		return
	}
	noContent(w)
}

// ListProjectBastions implements GET /api/v1/projects/{projectKey}/bastions: the Bastions that allow the project, with the addresses
// that are entries of server.additional_base_urls.
func (s *Server) ListProjectBastions(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey) {
	if !s.bastionsReady(w, r) {
		return
	}
	offered, err := s.o.Bastions.Offered(r.Context(), projectKey)
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	out := gen.ProjectBastionList{Items: make([]gen.ProjectBastion, 0, len(offered))}
	for _, b := range offered {
		out.Items = append(out.Items, gen.ProjectBastion{
			Name: b.Name, Disabled: b.Disabled, WebInterface: b.WebInterface, Addresses: listedAddresses(b.BaseURLs, s.o.Packages.AdditionalBaseURLs),
		})
	}
	writeJSON(w, r, http.StatusOK, out)
}

// listedBastions holds the addresses of a repository's member bastions to server.additional_base_urls (R-BAS-134, as R-BAS-007 holds a
// client setup's): a base URL a deployment reported is shown only when the installation lists it, in the listed spelling.
func (s *Server) listedBastions(st repository.Stored) repository.Stored {
	refs := make([]repository.BastionRef, 0, len(st.Document.Bastions))
	for _, b := range st.Document.Bastions {
		b.Addresses = listedAddresses(b.Addresses, s.o.Packages.AdditionalBaseURLs)
		refs = append(refs, b)
	}
	st.Document.Bastions = refs
	return st
}

// listedAddresses are the reported base URLs that are entries of listed, in the listed spelling without a trailing slash, in the order
// reported.
func listedAddresses(reported, listed []string) []string {
	out := []string{}
	for _, rep := range reported {
		trimmed := strings.TrimSuffix(rep, "/")
		for _, l := range listed {
			if strings.EqualFold(strings.TrimSuffix(l, "/"), trimmed) {
				out = append(out, strings.TrimSuffix(l, "/"))
				break
			}
		}
	}
	return out
}
