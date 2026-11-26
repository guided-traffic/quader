package v1

import (
	"net/http"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

// uploadTokenTTL is the lifetime of the token of an upload from the screen (R-ANS-061, R-NUGET-006): its request starts within seconds
// of the issue, and the token is checked when the request arrives, so a long upload outlives it.
const uploadTokenTTL = 5 * time.Minute

// CreateUploadToken implements POST /api/v1/projects/{projectKey}/repositories/{repoKey}/upload-tokens (R-ANS-061, R-NUGET-006,
// Q-ANS-140): a personal access token of the caller for five minutes, with read and deploy on the repository and, for a virtual
// repository, on its deployment target, and nothing else. The browser publishes with it through the client's own routes, which the
// format names (formats.ScreenPublisher) and the role packages serves with the shadowing probes the role api may not make (R-ARCH-001,
// Q-ARCH-301); the session never leaves the role api. Only a session is answered: a client publishes with its own token, and a token
// may not mint the next one. The answer names the token's id, by which the dialog revokes it once the publish answered.
func (s *Server) CreateUploadToken(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey gen.RepoKey) {
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
	addressed := qname.Repo{Project: projectKey, Key: repoKey}
	holder, f, err := s.screenPublishTarget(r, addressed)
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.createUploadToken"))
		return
	}
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
			Description: "Upload of a package from the screen into " + addressed.String(),
			TTL:         token.TTLRequest{Seconds: refTo(int64(uploadTokenTTL / time.Second))},
		})
	}
	if err != nil {
		failIdentity(w, r, errs.Wrap(err, "api.createUploadToken"))
		return
	}
	expires := time.Now().Add(uploadTokenTTL).UTC()
	if created.Record.ExpiresAt != nil {
		expires = created.Record.ExpiresAt.UTC()
	}
	base := qname.ProtocolBase(f.Descriptor().ProtocolBasePath, addressed)
	pkg, symbols := formats.CapabilitiesOf(f).ScreenPublisher.ScreenPublish()
	out := gen.UploadToken{Id: created.Record.ID, Token: created.Secret, ExpiresAt: expires, PublishPath: base + pkg}
	if symbols != "" {
		sp := base + symbols
		out.SymbolsPath = &sp
	}
	writeJSON(w, r, http.StatusCreated, out)
}

// screenPublishTarget is the repository a publish from the screen into the addressed one lands in, and its format: a local repository,
// or the deployment target of a virtual one, of a format that names its publish routes for the screen (formats.ScreenPublisher). Any
// other is 405 not_applicable, and a virtual repository without a deployment target no_deploy_target.
func (s *Server) screenPublishTarget(r *http.Request, addressed qname.Repo) (repository.Repository, formats.Format, error) {
	if s.o.Repositories.Reader == nil {
		return repository.Repository{}, nil, unavailable("The repository operations")
	}
	v, err := s.o.Repositories.Reader.ByName(r.Context(), addressed)
	if err != nil {
		return repository.Repository{}, nil, err
	}
	f, ok := formats.Lookup(v.Format)
	if !ok || formats.CapabilitiesOf(f).ScreenPublisher == nil {
		return repository.Repository{}, nil, errs.E(errs.MethodNotAllowed, errs.CodeNotApplicable,
			"A package of the format "+v.Format+" is not uploaded on the screen; its client publishes it.")
	}
	if v.Class == repository.Virtual {
		t, ok := v.DeployTarget()
		if !ok {
			return repository.Repository{}, nil, errs.E(errs.MethodNotAllowed, errs.CodeNoDeployTarget,
				"The virtual repository "+addressed.String()+" has no deployment target, so nothing is uploaded into it.")
		}
		v = t
	}
	if v.Class != repository.Local {
		return repository.Repository{}, nil, errs.E(errs.MethodNotAllowed, errs.CodeNotApplicable,
			"A package is uploaded into a local repository, or a virtual one with a deployment target; "+addressed.String()+" is a "+
				string(v.Class)+" repository.")
	}
	return v, f, nil
}
