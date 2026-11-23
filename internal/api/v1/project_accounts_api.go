package v1

import (
	"context"
	"net/http"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/qname"
)

// The service accounts of a project (Q-AUTHZ-169, Q-AUTHZ-209): /projects/{projectKey}/service-accounts. The
// handlers pass the project and the name within it to the identity manager (internal/auth/principal/projectaccounts.go).

func projectAccountName(project, name string) qname.Object {
	return qname.Object{Project: project, Name: name}
}

// ListProjectServiceAccounts implements GET /api/v1/projects/{projectKey}/service-accounts.
func (s *Server) ListProjectServiceAccounts(w http.ResponseWriter, r *http.Request, projectKey string, _ gen.ListProjectServiceAccountsParams) {
	if !s.identityReady(w, r) {
		return
	}
	lq, err := ParseList(r, ListSpec{Filters: []string{"q"}, Sorts: []string{"name"}, DefaultSort: "name"}, s.o.API.Config)
	if err == nil {
		err = ascending(lq)
	}
	var after string
	if err == nil {
		after, err = afterName(lq)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	rows, err := s.o.Auth.Manager.ListProjectServiceAccounts(r.Context(), projectKey, lq.First("q"), after, lq.FetchLimit())
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	role := s.seesAdminRole(r.Context())
	page := NewPage(lq, rows, func(a principal.ServiceAccount) []string { return []string{a.Name} })
	WritePage(w, r, MapPage(page, func(a principal.ServiceAccount) gen.ServiceAccount { return toServiceAccount(a, role) }))
}

// CreateProjectServiceAccount implements POST /api/v1/projects/{projectKey}/service-accounts.
func (s *Server) CreateProjectServiceAccount(w http.ResponseWriter, r *http.Request, projectKey string, _ gen.CreateProjectServiceAccountParams) {
	if !s.identityReady(w, r) {
		return
	}
	body, err := ReadBody(w, r, MediaJSON, s.bodyLimit())
	var in gen.NewServiceAccount
	if err == nil {
		err = StrictUnmarshal(body, &in)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	c := s.caller(r)
	err = s.idem.Do(w, r, body, IdempotentOp{Run: func(ctx context.Context, tx meta.Tx) (Outcome, error) {
		a, err := s.o.Auth.Manager.CreateProjectServiceAccountTx(ctx, tx, c, projectKey, principal.NewServiceAccount{Name: in.Name, Description: derefString(in.Description)})
		if err != nil {
			return Outcome{}, identityErr(err)
		}
		return Outcome{Status: http.StatusCreated, Body: toServiceAccount(a, s.seesAdminRole(r.Context())), Header: map[string]string{
			"Location": location(r, "/api/v1/projects/"+projectKey+"/service-accounts/"+in.Name),
		}}, nil
	}})
	if err != nil {
		failIdentity(w, r, err)
	}
}

// GetProjectServiceAccount implements GET /api/v1/projects/{projectKey}/service-accounts/{name}.
func (s *Server) GetProjectServiceAccount(w http.ResponseWriter, r *http.Request, projectKey, name string, _ gen.GetProjectServiceAccountParams) {
	if !s.identityReady(w, r) {
		return
	}
	a, err := s.o.Auth.Manager.GetProjectServiceAccount(r.Context(), projectAccountName(projectKey, name))
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	if NotModified(w, r, ETag(a.Revision, "")) {
		return
	}
	SetETag(w, a.Revision, "")
	writeJSON(w, r, http.StatusOK, toServiceAccount(a, s.seesAdminRole(r.Context())))
}

// UpdateProjectServiceAccount implements PATCH /api/v1/projects/{projectKey}/service-accounts/{name}.
func (s *Server) UpdateProjectServiceAccount(w http.ResponseWriter, r *http.Request, projectKey, name string, _ gen.UpdateProjectServiceAccountParams) {
	if !s.identityReady(w, r) {
		return
	}
	pre, err := ParseIfMatch(r)
	var raw []byte
	if err == nil {
		raw, err = ReadMergePatch(w, r, s.bodyLimit())
	}
	var in serviceAccountPatchBody
	if err == nil {
		err = StrictUnmarshal(raw, &in)
	}
	var desc *string
	if err == nil {
		err = errorsFirst(
			func() (e error) { desc, e = patchString(in.Description, "/description"); return },
			func() error { return immutableString(in.Name, "/name", name) },
		)
	}
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	o := projectAccountName(projectKey, name)
	if desc == nil {
		a, err := s.o.Auth.Manager.GetProjectServiceAccount(r.Context(), o)
		if err != nil {
			failIdentity(w, r, err)
			return
		}
		if !pre.Matches(a.Revision, "") {
			failIdentity(w, r, Failed(a.Revision))
			return
		}
		SetETag(w, a.Revision, "")
		writeJSON(w, r, http.StatusOK, toServiceAccount(a, s.seesAdminRole(r.Context())))
		return
	}
	a, err := s.o.Auth.Manager.UpdateProjectServiceAccount(r.Context(), s.caller(r), o, *desc, expectedRevision(pre))
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	SetETag(w, a.Revision, "")
	writeJSON(w, r, http.StatusOK, toServiceAccount(a, s.seesAdminRole(r.Context())))
}

// DeleteProjectServiceAccount implements DELETE /api/v1/projects/{projectKey}/service-accounts/{name}.
func (s *Server) DeleteProjectServiceAccount(w http.ResponseWriter, r *http.Request, projectKey, name string) {
	if !s.identityReady(w, r) {
		return
	}
	if err := s.o.Auth.Manager.DeleteProjectServiceAccount(r.Context(), s.caller(r), projectAccountName(projectKey, name)); err != nil {
		failIdentity(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// DisableProjectServiceAccount implements POST /api/v1/projects/{projectKey}/service-accounts/{name}/disable.
func (s *Server) DisableProjectServiceAccount(w http.ResponseWriter, r *http.Request, projectKey, name string) {
	if !s.identityReady(w, r) {
		return
	}
	var in gen.DisableRequest
	if err := s.readOptionalJSON(w, r, &in); err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	if err := s.o.Auth.Manager.DisableProjectServiceAccount(r.Context(), s.caller(r), projectAccountName(projectKey, name), in.RevokeTokens != nil && *in.RevokeTokens); err != nil {
		failIdentity(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// EnableProjectServiceAccount implements POST /api/v1/projects/{projectKey}/service-accounts/{name}/enable.
func (s *Server) EnableProjectServiceAccount(w http.ResponseWriter, r *http.Request, projectKey, name string) {
	if !s.identityReady(w, r) {
		return
	}
	if err := s.o.Auth.Manager.EnableProjectServiceAccount(r.Context(), s.caller(r), projectAccountName(projectKey, name)); err != nil {
		failIdentity(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// RevokeProjectServiceAccountTokens implements DELETE /api/v1/projects/{projectKey}/service-accounts/{name}/tokens.
func (s *Server) RevokeProjectServiceAccountTokens(w http.ResponseWriter, r *http.Request, projectKey, name string) {
	if !s.identityReady(w, r) {
		return
	}
	sa, err := s.o.Auth.Manager.GetProjectServiceAccount(r.Context(), projectAccountName(projectKey, name))
	if err == nil {
		_, err = s.o.Auth.Tokens.RevokeAll(r.Context(), s.caller(r), sa.ID, token.ReasonAdmin)
	}
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	noContent(w)
}
