package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/qname"
)

// The technical tokens of a service account on its three levels (R-AUTH-045, R-API-082): below
// /api/v1/service-accounts/{name}, /api/v1/projects/{projectKey}/service-accounts/{name} and
// /api/v1/projects/{projectKey}/repositories/{repoKey}/service-accounts/{name}. The operations of the three levels share one
// implementation over an accountRoute; the declarations of api/openapi.yaml decide the reads, the change and the delete, and the
// handler decides the creation of a token or a secret, whose callers are the ones of R-AUTH-027 (an administrator, an Owner of
// the account's project with the bounds of Q-AUTHZ-177, a token of the account under the derivation rule).

// accountRoute is the account a route addresses: its level, its project and repository keys and its name within its place.
type accountRoute struct {
	level   string
	project string
	repo    string
	name    string
}

func instanceAccount(name string) accountRoute {
	return accountRoute{level: principal.LevelInstance, name: name}
}

func projectAccount(project, name string) accountRoute {
	return accountRoute{level: principal.LevelProject, project: project, name: name}
}

func repositoryAccount(project, repo, name string) accountRoute {
	return accountRoute{level: principal.LevelRepository, project: project, repo: repo, name: name}
}

// stored is the account's stored principal name, which a token of the account carries as its principal's name.
func (a accountRoute) stored() string {
	switch a.level {
	case principal.LevelProject:
		return a.project + "/" + a.name
	case principal.LevelRepository:
		return a.project + "/" + a.repo + "/" + a.name
	}
	return a.name
}

// path is the account's route below /api/v1.
func (a accountRoute) path() string {
	n := url.PathEscape(a.name)
	switch a.level {
	case principal.LevelProject:
		return "/api/v1/projects/" + a.project + "/service-accounts/" + n
	case principal.LevelRepository:
		return "/api/v1/projects/" + a.project + "/repositories/" + a.repo + "/service-accounts/" + n
	}
	return "/api/v1/service-accounts/" + n
}

// account reads the account of the route: 404 for none, and for an account of another level or place.
func (s *Server) account(ctx context.Context, a accountRoute) (principal.ServiceAccount, error) {
	switch a.level {
	case principal.LevelProject:
		return s.o.Auth.Manager.GetProjectServiceAccount(ctx, qname.Object{Project: a.project, Name: a.name})
	case principal.LevelRepository:
		return s.o.Auth.Manager.GetRepositoryServiceAccount(ctx, principal.RepositoryAccount{Repo: qname.Repo{Project: a.project, Key: a.repo}, Name: a.name})
	}
	return s.globalAccount(ctx, a.name)
}

func toTechnicalAccount(sa principal.ServiceAccount) gen.TechnicalTokenAccount {
	return gen.TechnicalTokenAccount{
		Reference: sa.Reference(), Level: gen.TechnicalTokenAccountLevel(sa.Level()), Project: optString(sa.Project), Repository: optString(sa.Repository),
	}
}

func toTechnicalSecrets(in []token.Secret) []gen.TechnicalTokenSecret {
	out := make([]gen.TechnicalTokenSecret, 0, len(in))
	for _, sec := range in {
		g := gen.TechnicalTokenSecret{
			Id: uuidOf(sec.ID), Hint: sec.Hint, Scope: toScope(sec.Scope), CreatedAt: utc(sec.CreatedAt),
			ExpiresAt: optTime(deref1(sec.ExpiresAt)), RevokedAt: optTime(deref1(sec.RevokedAt)), LastUsedAt: optTime(deref1(sec.LastUsedAt)),
		}
		if sec.RotatedFrom != "" {
			u := uuidOf(sec.RotatedFrom)
			g.RotatedFrom = &u
		}
		out = append(out, g)
	}
	return out
}

// toTechnicalToken renders a technical token. limit is the token's own download-limit rule at its account's place (R-AUTH-045,
// usage-and-limits.md R-LIM-038); null while none is set and on a replica without the limits area.
func toTechnicalToken(sa principal.ServiceAccount, t token.TechnicalToken, limit *gen.TechnicalTokenLimit) gen.TechnicalToken {
	return gen.TechnicalToken{
		Id: uuidOf(t.ID), Name: t.Name, Description: t.Description, Account: toTechnicalAccount(sa), State: gen.TechnicalTokenState(t.State),
		Secrets: toTechnicalSecrets(t.Secrets), Limit: limit, CreatedAt: utc(t.CreatedAt), CreatedBy: optString(t.CreatedBy),
	}
}

// accountLimitPlace is the place a service account's tokens have their own rule at (R-LIM-038): the instance, the account's
// project, or its repository.
func accountLimitPlace(sa principal.ServiceAccount) limits.Place {
	switch sa.Level() {
	case principal.LevelRepository:
		return limits.Place{Kind: limits.PlaceRepository, Repo: qname.Repo{Project: sa.Project, Key: sa.Repository}, ProjectKey: sa.Project}
	case principal.LevelProject:
		return limits.Place{Kind: limits.PlaceProject, ProjectKey: sa.Project}
	}
	return limits.InstancePlace()
}

// tokenLimits are the rules of the account's place that name one of its technical tokens, by the token's ID (R-LIM-038). The
// limits are read from the store, as every read of the limits API is; a replica without the limits area answers none.
func (s *Server) tokenLimits(ctx context.Context, sa principal.ServiceAccount) (map[string]tokenLimit, error) {
	l := s.o.Limits
	if l.Store == nil {
		return nil, nil
	}
	snap, err := l.Store.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	place := accountLimitPlace(sa)
	out := map[string]tokenLimit{}
	for _, r := range snap.Rules {
		if r.WhoKind != limits.WhoTechnicalToken || r.Place.Kind != place.Kind {
			continue
		}
		if (place.Kind == limits.PlaceProject && r.Place.ProjectKey != place.ProjectKey) || (place.Kind == limits.PlaceRepository && r.Place.Repo != place.Repo) {
			continue
		}
		n := 0
		for _, set := range r.Overrides.Set {
			if set {
				n++
			}
		}
		out[r.WhoRef] = tokenLimit{rep: &gen.TechnicalTokenLimit{Rule: uuidOf(r.ID), Profile: r.Profile, Overrides: n}, at: r.UpdatedAt}
	}
	return out, nil
}

// tokenLimit is a technical token's own rule and when it last changed.
type tokenLimit struct {
	rep *gen.TechnicalTokenLimit
	at  time.Time
}

// technicalETag is the tag of a read of a technical token. Its limit changes with a rule, not with the token's revision, and a
// strong validator changes with every observable change of the representation (R-API-012, RFC 9110 section 8.8.1): the tag is
// the revision, then the rule's change time as ".<microseconds>" while the token has a rule. A write accepts both forms and
// compares the revision, as a repository's does (withoutActivity).
func technicalETag(revision int64, l tokenLimit) string {
	tag := ETag(revision, "")
	if l.rep != nil {
		tag = tag[:len(tag)-1] + "." + strconv.FormatInt(l.at.UnixMicro(), 10) + `"`
	}
	return tag
}

// listTechnical answers GET …/technical-tokens.
func (s *Server) listTechnical(w http.ResponseWriter, r *http.Request, a accountRoute) {
	if !s.identityReady(w, r) {
		return
	}
	sa, err := s.account(r.Context(), a)
	var list []token.TechnicalToken
	if err == nil {
		list, err = s.o.Auth.Tokens.ListTechnical(r.Context(), sa.ID)
	}
	var lims map[string]tokenLimit
	if err == nil {
		lims, err = s.tokenLimits(r.Context(), sa)
	}
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	items := make([]gen.TechnicalToken, 0, len(list))
	for _, t := range list {
		items = append(items, toTechnicalToken(sa, t, lims[t.ID].rep))
	}
	writeJSON(w, r, http.StatusOK, gen.TechnicalTokenList{Items: items})
}

// getTechnical answers GET …/technical-tokens/{tokenName}.
func (s *Server) getTechnical(w http.ResponseWriter, r *http.Request, a accountRoute, tokenName string) {
	if !s.identityReady(w, r) {
		return
	}
	sa, err := s.account(r.Context(), a)
	var t token.TechnicalToken
	if err == nil {
		t, err = s.o.Auth.Tokens.GetTechnical(r.Context(), sa.ID, tokenName)
	}
	var lims map[string]tokenLimit
	if err == nil {
		lims, err = s.tokenLimits(r.Context(), sa)
	}
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	tag := technicalETag(t.Revision, lims[t.ID])
	if NotModified(w, r, tag) {
		return
	}
	w.Header().Set("ETag", tag)
	writeJSON(w, r, http.StatusOK, toTechnicalToken(sa, t, lims[t.ID].rep))
}

// technicalPatchBody is a merge patch of a technical token: the description; the read-only members are accepted and ignored,
// the name must stay.
type technicalPatchBody struct {
	Description json.RawMessage `json:"description"`

	ID        json.RawMessage `json:"id"`
	Name      json.RawMessage `json:"name"`
	Account   json.RawMessage `json:"account"`
	State     json.RawMessage `json:"state"`
	Secrets   json.RawMessage `json:"secrets"`
	Limit     json.RawMessage `json:"limit"`
	CreatedAt json.RawMessage `json:"createdAt"`
	CreatedBy json.RawMessage `json:"createdBy"`
}

// updateTechnical answers PATCH …/technical-tokens/{tokenName}.
func (s *Server) updateTechnical(w http.ResponseWriter, r *http.Request, a accountRoute, tokenName string) {
	if !s.identityReady(w, r) {
		return
	}
	pre, err := ParseIfMatch(r)
	pre = withoutActivity(pre)
	var raw []byte
	if err == nil {
		raw, err = ReadMergePatch(w, r, s.bodyLimit())
	}
	var in technicalPatchBody
	if err == nil {
		err = StrictUnmarshal(raw, &in)
	}
	var desc *string
	if err == nil {
		err = errorsFirst(
			func() (e error) { desc, e = patchString(in.Description, "/description"); return },
			func() error { return immutableString(in.Name, "/name", tokenName) },
		)
	}
	var sa principal.ServiceAccount
	if err == nil {
		sa, err = s.account(r.Context(), a)
	}
	var t token.TechnicalToken
	switch {
	case err != nil:
	case desc == nil:
		if t, err = s.o.Auth.Tokens.GetTechnical(r.Context(), sa.ID, tokenName); err == nil && !pre.Matches(t.Revision, "") {
			err = Failed(t.Revision)
		}
	default:
		t, err = s.o.Auth.Tokens.UpdateTechnical(r.Context(), s.caller(r), sa, tokenName, *desc, expectedRevision(pre))
	}
	var lims map[string]tokenLimit
	if err == nil {
		lims, err = s.tokenLimits(r.Context(), sa)
	}
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	SetETag(w, t.Revision, "")
	writeJSON(w, r, http.StatusOK, toTechnicalToken(sa, t, lims[t.ID].rep))
}

// deleteTechnical answers DELETE …/technical-tokens/{tokenName}.
func (s *Server) deleteTechnical(w http.ResponseWriter, r *http.Request, a accountRoute, tokenName string) {
	if !s.identityReady(w, r) {
		return
	}
	sa, err := s.account(r.Context(), a)
	if err == nil {
		err = s.o.Auth.Tokens.DeleteTechnical(r.Context(), s.caller(r), sa, tokenName)
	}
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	noContent(w)
}

// technicalIssuer is who issues a secret of a technical token, decided before the account is looked up so that a caller who may
// not issue one gets the same 403 whether the account exists or not (R-AUTH-027): an administrator of Quader, an Owner of the
// account's project (a delegated token, Q-AUTHZ-177), or a token of the account itself under the derivation rule.
type technicalIssuer struct {
	admin, owner, derived bool
	parent                *token.Info
}

func (s *Server) technicalIssuer(ctx context.Context, a accountRoute) (technicalIssuer, error) {
	p := httpserver.PrincipalFrom(ctx)
	var out technicalIssuer
	out.derived = p.Kind == httpserver.KindServiceAccount && p.Name == a.stored() && tokenAuthenticated(p)
	admin, err := s.holds(ctx, "role:admin")
	if err != nil {
		return out, err
	}
	out.admin = admin
	if !admin && !out.derived && a.level != principal.LevelInstance {
		if pa, ok := s.o.Access.(ProjectAccess); ok {
			if out.owner, err = pa.Owns(ctx, a.project); err != nil {
				return out, err
			}
		}
	}
	if !out.admin && !out.owner && !out.derived {
		detail := "Only an administrator or a token of the account itself issues its technical tokens."
		if a.level != principal.LevelInstance {
			detail = "Only an administrator, an Owner of the account's project, or a token of the account itself issues its technical tokens."
		}
		return out, errs.E(errs.Forbidden, errs.CodeForbidden, detail)
	}
	if out.derived {
		out.parent, err = s.presenting(ctx, p)
	}
	return out, err
}

// secretRequest is the creation request of a secret of a technical token for the issuer (R-AUTH-027).
func (s *Server) secretRequest(r *http.Request, is technicalIssuer, ttl token.TTLRequest, sc *gen.TokenScope, selfRotatable *bool) (token.CreateRequest, error) {
	scope, err := scopeBytes(sc)
	if err == nil {
		err = s.refuseUpperCasePaths(scope)
	}
	return token.CreateRequest{
		Caller: s.caller(r), Scope: scope, TTL: ttl, SelfRotatable: selfRotatable != nil && *selfRotatable,
		Via: via(httpserver.PrincipalFrom(r.Context())), Admin: is.admin && !is.derived, Parent: is.parent,
		Delegated: is.owner && !is.admin && !is.derived,
	}, err
}

func ttlOf(seconds *int, at *gen.NewTechnicalToken) token.TTLRequest {
	var out token.TTLRequest
	if seconds != nil {
		out.Seconds = refTo(int64(*seconds))
	}
	if at != nil && at.ExpiresAt != nil {
		t := at.ExpiresAt.UTC()
		out.ExpiresAt = &t
	}
	return out
}

// createTechnical answers POST …/technical-tokens: a technical token with its first secret.
func (s *Server) createTechnical(w http.ResponseWriter, r *http.Request, a accountRoute) {
	if !s.identityReady(w, r) {
		return
	}
	ctx := r.Context()
	body, err := ReadBody(w, r, MediaJSON, s.bodyLimit())
	var in gen.NewTechnicalToken
	if err == nil {
		err = StrictUnmarshal(body, &in)
	}
	var is technicalIssuer
	if err == nil {
		is, err = s.technicalIssuer(ctx, a)
	}
	var req token.CreateRequest
	if err == nil {
		req, err = s.secretRequest(r, is, ttlOf(in.TtlSeconds, &in), in.Scope, in.SelfRotatable)
	}
	var sa principal.ServiceAccount
	if err == nil {
		sa, err = s.account(ctx, a)
	}
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	s.issueTechnical(w, r, body, a, sa, in.Name, func(ctx context.Context, tx meta.Tx) (token.Created, error) {
		return s.o.Auth.Tokens.CreateTechnicalTx(ctx, tx, sa, token.TechnicalCreate{Request: req, Name: in.Name, Description: derefString(in.Description)})
	})
}

// createTechnicalSecret answers POST …/technical-tokens/{tokenName}/secrets: a new secret of a technical token without a live
// one (Q-AUTH-416).
func (s *Server) createTechnicalSecret(w http.ResponseWriter, r *http.Request, a accountRoute, tokenName string) {
	if !s.identityReady(w, r) {
		return
	}
	ctx := r.Context()
	var in gen.NewTechnicalTokenSecret
	body, err := readOptionalBody(w, r, s.bodyLimit())
	if err == nil && body != nil {
		if err = requireJSON(r, MediaJSON); err == nil {
			err = StrictUnmarshal(body, &in)
		}
	}
	var is technicalIssuer
	if err == nil {
		is, err = s.technicalIssuer(ctx, a)
	}
	var req token.CreateRequest
	if err == nil {
		req, err = s.secretRequest(r, is, ttlOf(in.TtlSeconds, &gen.NewTechnicalToken{ExpiresAt: in.ExpiresAt}), in.Scope, in.SelfRotatable)
	}
	var sa principal.ServiceAccount
	if err == nil {
		sa, err = s.account(ctx, a)
	}
	if err != nil {
		failIdentity(w, r, err)
		return
	}
	s.issueTechnical(w, r, body, a, sa, tokenName, func(ctx context.Context, tx meta.Tx) (token.Created, error) {
		return s.o.Auth.Tokens.NewSecretTx(ctx, tx, sa, tokenName, req)
	})
}

// issueTechnical runs the issue of a secret behind the Idempotency-Key and answers 201 with the technical token as the
// transaction wrote it, the secret and its ID. The secret never goes into the stored outcome.
func (s *Server) issueTechnical(w http.ResponseWriter, r *http.Request, body []byte, a accountRoute, sa principal.ServiceAccount, name string,
	run func(ctx context.Context, tx meta.Tx) (token.Created, error),
) {
	err := s.idem.Do(w, r, body, IdempotentOp{
		Run: func(ctx context.Context, tx meta.Tx) (Outcome, error) {
			created, err := run(ctx, tx)
			if err != nil {
				return Outcome{}, identityErr(err)
			}
			t, err := s.o.Auth.Tokens.GetTechnicalTx(ctx, tx, sa.ID, name)
			if err != nil {
				return Outcome{}, err
			}
			lims, err := s.tokenLimits(ctx, sa)
			if err != nil {
				return Outcome{}, err
			}
			rec := toTechnicalToken(sa, t, lims[t.ID].rep)
			out := gen.TechnicalTokenCreated{
				Id: rec.Id, Name: rec.Name, Description: rec.Description, Account: rec.Account, State: gen.TechnicalTokenCreatedState(rec.State),
				Secrets: rec.Secrets, Limit: rec.Limit, CreatedAt: rec.CreatedAt, CreatedBy: rec.CreatedBy, Token: created.Secret, SecretId: uuidOf(created.Record.ID),
			}
			return Outcome{
				Status: http.StatusCreated, Body: out, SecretID: created.Record.ID,
				Header: map[string]string{"Location": location(r, a.path()+"/technical-tokens/"+url.PathEscape(name))},
			}, nil
		},
		SecretReplay: tokenIssued,
	})
	if err != nil {
		failIdentity(w, r, err)
	}
}

// ListServiceAccountTechnicalTokens implements GET /api/v1/service-accounts/{name}/technical-tokens.
func (s *Server) ListServiceAccountTechnicalTokens(w http.ResponseWriter, r *http.Request, name string) {
	s.listTechnical(w, r, instanceAccount(name))
}

// CreateServiceAccountTechnicalToken implements POST /api/v1/service-accounts/{name}/technical-tokens.
func (s *Server) CreateServiceAccountTechnicalToken(w http.ResponseWriter, r *http.Request, name string, _ gen.CreateServiceAccountTechnicalTokenParams) {
	s.createTechnical(w, r, instanceAccount(name))
}

// GetServiceAccountTechnicalToken implements GET /api/v1/service-accounts/{name}/technical-tokens/{tokenName}.
func (s *Server) GetServiceAccountTechnicalToken(w http.ResponseWriter, r *http.Request, name, tokenName string, _ gen.GetServiceAccountTechnicalTokenParams) {
	s.getTechnical(w, r, instanceAccount(name), tokenName)
}

// UpdateServiceAccountTechnicalToken implements PATCH /api/v1/service-accounts/{name}/technical-tokens/{tokenName}.
func (s *Server) UpdateServiceAccountTechnicalToken(w http.ResponseWriter, r *http.Request, name, tokenName string, _ gen.UpdateServiceAccountTechnicalTokenParams) {
	s.updateTechnical(w, r, instanceAccount(name), tokenName)
}

// DeleteServiceAccountTechnicalToken implements DELETE /api/v1/service-accounts/{name}/technical-tokens/{tokenName}.
func (s *Server) DeleteServiceAccountTechnicalToken(w http.ResponseWriter, r *http.Request, name, tokenName string) {
	s.deleteTechnical(w, r, instanceAccount(name), tokenName)
}

// CreateServiceAccountTechnicalTokenSecret implements POST /api/v1/service-accounts/{name}/technical-tokens/{tokenName}/secrets.
func (s *Server) CreateServiceAccountTechnicalTokenSecret(w http.ResponseWriter, r *http.Request, name, tokenName string, _ gen.CreateServiceAccountTechnicalTokenSecretParams) {
	s.createTechnicalSecret(w, r, instanceAccount(name), tokenName)
}

// ListProjectServiceAccountTechnicalTokens implements GET /api/v1/projects/{projectKey}/service-accounts/{name}/technical-tokens.
func (s *Server) ListProjectServiceAccountTechnicalTokens(w http.ResponseWriter, r *http.Request, projectKey, name string) {
	s.listTechnical(w, r, projectAccount(projectKey, name))
}

// CreateProjectServiceAccountTechnicalToken implements POST /api/v1/projects/{projectKey}/service-accounts/{name}/technical-tokens.
func (s *Server) CreateProjectServiceAccountTechnicalToken(w http.ResponseWriter, r *http.Request, projectKey, name string, _ gen.CreateProjectServiceAccountTechnicalTokenParams) {
	s.createTechnical(w, r, projectAccount(projectKey, name))
}

// GetProjectServiceAccountTechnicalToken implements GET …/projects/{projectKey}/service-accounts/{name}/technical-tokens/{tokenName}.
func (s *Server) GetProjectServiceAccountTechnicalToken(w http.ResponseWriter, r *http.Request, projectKey, name, tokenName string, _ gen.GetProjectServiceAccountTechnicalTokenParams) {
	s.getTechnical(w, r, projectAccount(projectKey, name), tokenName)
}

// UpdateProjectServiceAccountTechnicalToken implements PATCH …/projects/{projectKey}/service-accounts/{name}/technical-tokens/{tokenName}.
func (s *Server) UpdateProjectServiceAccountTechnicalToken(w http.ResponseWriter, r *http.Request, projectKey, name, tokenName string, _ gen.UpdateProjectServiceAccountTechnicalTokenParams) {
	s.updateTechnical(w, r, projectAccount(projectKey, name), tokenName)
}

// DeleteProjectServiceAccountTechnicalToken implements DELETE …/projects/{projectKey}/service-accounts/{name}/technical-tokens/{tokenName}.
func (s *Server) DeleteProjectServiceAccountTechnicalToken(w http.ResponseWriter, r *http.Request, projectKey, name, tokenName string) {
	s.deleteTechnical(w, r, projectAccount(projectKey, name), tokenName)
}

// CreateProjectServiceAccountTechnicalTokenSecret implements POST …/projects/{projectKey}/service-accounts/{name}/technical-tokens/{tokenName}/secrets.
func (s *Server) CreateProjectServiceAccountTechnicalTokenSecret(w http.ResponseWriter, r *http.Request, projectKey, name, tokenName string, _ gen.CreateProjectServiceAccountTechnicalTokenSecretParams) {
	s.createTechnicalSecret(w, r, projectAccount(projectKey, name), tokenName)
}

// ListRepositoryServiceAccountTechnicalTokens implements GET …/repositories/{repoKey}/service-accounts/{name}/technical-tokens.
func (s *Server) ListRepositoryServiceAccountTechnicalTokens(w http.ResponseWriter, r *http.Request, projectKey, repoKey, name string) {
	s.listTechnical(w, r, repositoryAccount(projectKey, repoKey, name))
}

// CreateRepositoryServiceAccountTechnicalToken implements POST …/repositories/{repoKey}/service-accounts/{name}/technical-tokens.
func (s *Server) CreateRepositoryServiceAccountTechnicalToken(w http.ResponseWriter, r *http.Request, projectKey, repoKey, name string, _ gen.CreateRepositoryServiceAccountTechnicalTokenParams) {
	s.createTechnical(w, r, repositoryAccount(projectKey, repoKey, name))
}

// GetRepositoryServiceAccountTechnicalToken implements GET …/repositories/{repoKey}/service-accounts/{name}/technical-tokens/{tokenName}.
func (s *Server) GetRepositoryServiceAccountTechnicalToken(w http.ResponseWriter, r *http.Request, projectKey, repoKey, name, tokenName string, _ gen.GetRepositoryServiceAccountTechnicalTokenParams) {
	s.getTechnical(w, r, repositoryAccount(projectKey, repoKey, name), tokenName)
}

// UpdateRepositoryServiceAccountTechnicalToken implements PATCH …/repositories/{repoKey}/service-accounts/{name}/technical-tokens/{tokenName}.
func (s *Server) UpdateRepositoryServiceAccountTechnicalToken(w http.ResponseWriter, r *http.Request, projectKey, repoKey, name, tokenName string, _ gen.UpdateRepositoryServiceAccountTechnicalTokenParams) {
	s.updateTechnical(w, r, repositoryAccount(projectKey, repoKey, name), tokenName)
}

// DeleteRepositoryServiceAccountTechnicalToken implements DELETE …/repositories/{repoKey}/service-accounts/{name}/technical-tokens/{tokenName}.
func (s *Server) DeleteRepositoryServiceAccountTechnicalToken(w http.ResponseWriter, r *http.Request, projectKey, repoKey, name, tokenName string) {
	s.deleteTechnical(w, r, repositoryAccount(projectKey, repoKey, name), tokenName)
}

// CreateRepositoryServiceAccountTechnicalTokenSecret implements POST …/service-accounts/{name}/technical-tokens/{tokenName}/secrets of a repository.
func (s *Server) CreateRepositoryServiceAccountTechnicalTokenSecret(w http.ResponseWriter, r *http.Request, projectKey, repoKey, name, tokenName string, _ gen.CreateRepositoryServiceAccountTechnicalTokenSecretParams) {
	s.createTechnicalSecret(w, r, repositoryAccount(projectKey, repoKey, name), tokenName)
}
