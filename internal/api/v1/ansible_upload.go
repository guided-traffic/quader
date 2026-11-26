package v1

import (
	"net/http"
	"net/url"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/qname"
)

// uploadTokenTTL is the lifetime of the token of an upload from the screen (R-ANS-061): its request starts within seconds of the
// issue, and the token is checked when the request arrives, so a long upload outlives it.
const uploadTokenTTL = 5 * time.Minute

// CreateAnsibleUploadToken implements POST /api/v1/projects/{projectKey}/repositories/{repoKey}/ansible/upload-tokens (R-ANS-061,
// Q-ANS-140): a personal access token of the caller for five minutes, with read and deploy on the repository and, for a virtual
// repository, on its deployment target, and nothing else. The browser publishes with it through the client's own route, which the
// role packages serves with the shadowing probes the role api may not make (R-ARCH-001, Q-ARCH-301); the session never leaves the
// role api. Only a session is answered: a client publishes with its own token, and a token may not mint the next one. The answer
// names the token's id, by which the dialog revokes it once the publish answered.
func (s *Server) CreateAnsibleUploadToken(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey gen.RepoKey) {
	if !s.identityReady(w, r) {
		return
	}
	ctx := r.Context()
	p := httpserver.PrincipalFrom(ctx)
	if p.Credential.Method != httpserver.MethodSession {
		// A client publishes with its own token; a token that minted upload tokens could renew itself without end.
		httpserver.Problem(w, r, errs.E(errs.Forbidden, errs.CodeForbidden,
			"The token of an upload from the screen is issued to a browser's session only; a client publishes with its own token."))
		return
	}
	_, holder, ok := s.ansibleTarget(w, r, projectKey, repoKey, true)
	if !ok {
		return
	}
	addressed := qname.Repo{Project: projectKey, Key: repoKey}
	repos := []string{addressed.String()}
	if holder.Name != addressed {
		repos = append(repos, holder.Name.String())
	}
	scope, err := scopeBytes(&gen.TokenScope{Repositories: &[]gen.TokenScopeGrant{{
		Repositories: repos, Actions: []gen.TokenScopeGrantActions{"read", "deploy"},
	}}})
	if err == nil {
		err = refuseBreakGlass(p)
	}
	var parent *token.Info
	if err == nil {
		parent, err = s.presenting(ctx, p)
	}
	var created token.Created
	if err == nil {
		created, err = s.o.Auth.Tokens.Create(ctx, token.CreateRequest{
			Caller: s.caller(r), Type: token.TypePAT, OwnerID: p.ID, Scope: scope, Via: via(p), Parent: parent,
			Description: "Upload of a collection from the screen into " + addressed.String(),
			TTL:         token.TTLRequest{Seconds: refTo(int64(uploadTokenTTL / time.Second))},
		})
	}
	if err != nil {
		failIdentity(w, r, errs.Wrap(err, "api.createAnsibleUploadToken"))
		return
	}
	expires := time.Now().Add(uploadTokenTTL).UTC()
	if created.Record.ExpiresAt != nil {
		expires = created.Record.ExpiresAt.UTC()
	}
	writeJSON(w, r, http.StatusCreated, gen.AnsibleUploadToken{
		Id: created.Record.ID, Token: created.Secret, ExpiresAt: expires,
		PublishPath: "/api/ansible/" + url.PathEscape(projectKey) + "/" + url.PathEscape(repoKey) + "/v3/artifacts/collections/",
	})
}

// GetAnsibleImportTask implements GET /api/v1/projects/{projectKey}/repositories/{repoKey}/ansible/imports/{taskId} (R-ANS-009,
// R-ANS-061): one task of a local repository, of the deployment target for a virtual one.
func (s *Server) GetAnsibleImportTask(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey gen.RepoKey, taskID openapi_types.UUID) {
	store, holder, ok := s.ansibleTarget(w, r, projectKey, repoKey, true)
	if !ok {
		return
	}
	t, found, err := store.Task(r.Context(), holder.NumericID(), taskID.String())
	if err == nil && !found {
		err = errs.E(errs.NotFound, errs.CodeNotFound, "No import task "+taskID.String()+" exists in this repository; a task is kept for a limited time.")
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.getAnsibleImportTask"))
		return
	}
	writeJSON(w, r, http.StatusOK, ansibleTaskRep(t))
}
