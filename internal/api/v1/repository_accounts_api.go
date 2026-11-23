package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/qname"
)

// The service accounts of a repository (R-AUTH-046, R-AUTHZ-039): /projects/{projectKey}/repositories/{repoKey}/service-accounts.
// The declarations admit an Owner of the project and a holder of identities:read or identities:write (Q-AUTH-417); the handlers
// pass the repository and the name within it to the identity manager (internal/auth/principal/repoaccounts.go).

func repoAccountOf(projectKey, repoKey, name string) principal.RepositoryAccount {
	return principal.RepositoryAccount{Repo: qname.Repo{Project: projectKey, Key: repoKey}, Name: name}
}

func actionStrings[T ~string](in *[]T) []string {
	if in == nil {
		return nil
	}
	out := make([]string, 0, len(*in))
	for _, a := range *in {
		out = append(out, string(a))
	}
	return out
}

// ListRepositoryServiceAccounts implements GET /api/v1/projects/{projectKey}/repositories/{repoKey}/service-accounts.
func (s *Server) ListRepositoryServiceAccounts(w http.ResponseWriter, r *http.Request, projectKey, repoKey string, _ gen.ListRepositoryServiceAccountsParams) {
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
	rows, err := s.o.Auth.Manager.ListRepositoryServiceAccounts(r.Context(), qname.Repo{Project: projectKey, Key: repoKey}, lq.First("q"), after, lq.FetchLimit())
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	role := s.seesAdminRole(r.Context())
	page := NewPage(lq, rows, func(a principal.ServiceAccount) []string { return []string{a.Name} })
	WritePage(w, r, MapPage(page, func(a principal.ServiceAccount) gen.ServiceAccount { return toServiceAccount(a, role) }))
}

// CreateRepositoryServiceAccount implements POST /api/v1/projects/{projectKey}/repositories/{repoKey}/service-accounts.
func (s *Server) CreateRepositoryServiceAccount(w http.ResponseWriter, r *http.Request, projectKey, repoKey string, _ gen.CreateRepositoryServiceAccountParams) {
	if !s.identityReady(w, r) {
		return
	}
	body, err := ReadBody(w, r, MediaJSON, s.bodyLimit())
	var in gen.NewRepositoryServiceAccount
	if err == nil {
		err = StrictUnmarshal(body, &in)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	c := s.caller(r)
	repo := qname.Repo{Project: projectKey, Key: repoKey}
	err = s.idem.Do(w, r, body, IdempotentOp{Run: func(ctx context.Context, tx meta.Tx) (Outcome, error) {
		a, err := s.o.Auth.Manager.CreateRepositoryServiceAccountTx(ctx, tx, c, repo,
			principal.NewServiceAccount{Name: in.Name, Description: derefString(in.Description)}, actionStrings(in.Actions))
		if err != nil {
			return Outcome{}, identityErr(err)
		}
		return Outcome{Status: http.StatusCreated, Body: toServiceAccount(a, s.seesAdminRole(r.Context())), Header: map[string]string{
			"Location": location(r, "/api/v1/projects/"+projectKey+"/repositories/"+repoKey+"/service-accounts/"+url.PathEscape(in.Name)),
		}}, nil
	}})
	if err != nil {
		failIdentity(w, r, err)
	}
}

// GetRepositoryServiceAccount implements GET /api/v1/projects/{projectKey}/repositories/{repoKey}/service-accounts/{name}.
func (s *Server) GetRepositoryServiceAccount(w http.ResponseWriter, r *http.Request, projectKey, repoKey, name string, _ gen.GetRepositoryServiceAccountParams) {
	if !s.identityReady(w, r) {
		return
	}
	a, err := s.o.Auth.Manager.GetRepositoryServiceAccount(r.Context(), repoAccountOf(projectKey, repoKey, name))
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

// patchActions reads the actions of a merge patch of a repository's account: absent is unchanged, null or an empty list is
// refused (an account holds at least one action, R-AUTHZ-039).
func patchActions(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var acts []string
	if err := json.Unmarshal(raw, &acts); err != nil || acts == nil {
		return nil, errs.E(errs.Unprocessable, errs.CodeValidation, "1 field is invalid.",
			errs.WithFields(errs.FieldError{Pointer: "/actions", Code: "invalid_value", Detail: "a list of at least one repository action"}))
	}
	if len(acts) == 0 {
		return nil, errs.E(errs.Unprocessable, errs.CodeValidation, "1 field is invalid.",
			errs.WithFields(errs.FieldError{Pointer: "/actions", Code: "invalid_value", Detail: "a service account of a repository holds at least one action on it"}))
	}
	return acts, nil
}

// UpdateRepositoryServiceAccount implements PATCH /api/v1/projects/{projectKey}/repositories/{repoKey}/service-accounts/{name}.
func (s *Server) UpdateRepositoryServiceAccount(w http.ResponseWriter, r *http.Request, projectKey, repoKey, name string, _ gen.UpdateRepositoryServiceAccountParams) {
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
	var ch principal.RepositoryAccountChange
	if err == nil {
		err = errorsFirst(
			func() (e error) { ch.Description, e = patchString(in.Description, "/description"); return },
			func() (e error) { ch.Actions, e = patchActions(in.Actions); return },
			func() error { return immutableString(in.Name, "/name", name) },
		)
	}
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	ra := repoAccountOf(projectKey, repoKey, name)
	if ch.Description == nil && ch.Actions == nil {
		a, err := s.o.Auth.Manager.GetRepositoryServiceAccount(r.Context(), ra)
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
	a, err := s.o.Auth.Manager.UpdateRepositoryServiceAccount(r.Context(), s.caller(r), ra, ch, expectedRevision(pre))
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	SetETag(w, a.Revision, "")
	writeJSON(w, r, http.StatusOK, toServiceAccount(a, s.seesAdminRole(r.Context())))
}

// DeleteRepositoryServiceAccount implements DELETE /api/v1/projects/{projectKey}/repositories/{repoKey}/service-accounts/{name}.
func (s *Server) DeleteRepositoryServiceAccount(w http.ResponseWriter, r *http.Request, projectKey, repoKey, name string) {
	if !s.identityReady(w, r) {
		return
	}
	if err := s.o.Auth.Manager.DeleteRepositoryServiceAccount(r.Context(), s.caller(r), repoAccountOf(projectKey, repoKey, name)); err != nil {
		failIdentity(w, r, err)
		return
	}
	noContent(w)
}

// DisableRepositoryServiceAccount implements POST …/repositories/{repoKey}/service-accounts/{name}/disable.
func (s *Server) DisableRepositoryServiceAccount(w http.ResponseWriter, r *http.Request, projectKey, repoKey, name string) {
	if !s.identityReady(w, r) {
		return
	}
	var in gen.DisableRequest
	if err := s.readOptionalJSON(w, r, &in); err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	err := s.o.Auth.Manager.DisableRepositoryServiceAccount(r.Context(), s.caller(r), repoAccountOf(projectKey, repoKey, name), in.RevokeTokens != nil && *in.RevokeTokens)
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	noContent(w)
}

// EnableRepositoryServiceAccount implements POST …/repositories/{repoKey}/service-accounts/{name}/enable.
func (s *Server) EnableRepositoryServiceAccount(w http.ResponseWriter, r *http.Request, projectKey, repoKey, name string) {
	if !s.identityReady(w, r) {
		return
	}
	if err := s.o.Auth.Manager.EnableRepositoryServiceAccount(r.Context(), s.caller(r), repoAccountOf(projectKey, repoKey, name)); err != nil {
		failIdentity(w, r, err)
		return
	}
	noContent(w)
}
