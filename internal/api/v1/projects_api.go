package v1

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/project"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// The project operations (R-AUTHZ-026, R-API-047). The handlers are thin: the rules
// (delete order, members and roles, move, the audit event, the change-log note, the
// epoch) are in internal/project, the conventions (paging, ETag, idempotency, the
// header Quader-Authz-Epoch) in this package.
//
// What each handler adds to its declaration (api/openapi.yaml, docs/developer/projects.md):
//
//   - listProjects and getProject are public in the declaration and decided here: a holder of projects:read sees every
//     project, a member its own, any caller the projects its level admits (R-AUTHZ-035 rule 8); membership=true keeps the
//     caller's own.
//   - the member operations are the project service's (last Owner, group and user only, the audit event);
//     the handler maps the body and the status (201 added, 200 role set).
//   - the writes: the declaration is the capability or an Owner of the project. They answer
//     Quader-Authz-Epoch; createProject is idempotent, replaceProject takes If-Match.

// projects returns the project service, or the 503 of a replica without it.
func (s *Server) projects() (project.Service, error) {
	if s.o.Authz.Projects == nil {
		return nil, unavailable("The project service")
	}
	return s.o.Authz.Projects, nil
}

// toProject renders a project; role is the caller's own role in it, "" for a caller that is no member.
func toProject(p project.Project, role authz.ProjectRole) gen.Project {
	owners := make([]string, 0, len(p.Members))
	for _, m := range p.Members {
		if m.Role == authz.ProjectRoleOwner {
			owners = append(owners, m.Principal.String())
		}
	}
	out := gen.Project{
		Key: p.Key, DisplayName: p.DisplayName, Description: p.Description, Visibility: gen.VisibilityLevel(p.Visibility),
		Members: len(p.Members), Owners: owners, Repositories: p.Repositories, ServiceAccounts: p.ServiceAccounts, Policies: p.Policies,
		LogicalBytes: p.LogicalBytes,
		Version:      p.Version,
		CreatedAt:    utc(p.CreatedAt), CreatedBy: p.CreatedBy, UpdatedAt: utc(p.UpdatedAt), UpdatedBy: p.UpdatedBy,
	}
	if role.Valid() {
		r := gen.ProjectRole(role.String())
		out.Role = &r
	}
	return out
}

func projectHref(r *http.Request, key string) string { return location(r, "/api/v1/projects/"+key) }

// projectMatches is the filter q of the list: a case-insensitive substring of the
// key or the display name.
func projectMatches(p project.Project, q string) bool {
	if q == "" {
		return true
	}
	q = strings.ToLower(q)
	return strings.Contains(p.Key, q) || strings.Contains(strings.ToLower(p.DisplayName), q)
}

// ListProjects implements GET /api/v1/projects (R-AUTHZ-026). The page is built from
// the projects the caller may see, by key; the cursor holds the last key.
func (s *Server) ListProjects(w http.ResponseWriter, r *http.Request, _ gen.ListProjectsParams) {
	svc, err := s.projects()
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	lq, err := ParseList(r, ListSpec{Filters: []string{"q", "membership"}, Sorts: []string{"key"}, DefaultSort: "key"}, s.o.API.Config)
	if err == nil && lq.After != nil && len(lq.After) != 1 {
		err = invalidCursor()
	}
	membership := false
	if err == nil {
		switch lq.First("membership") {
		case "", "false":
		case "true":
			membership = true
		default:
			err = badParam("membership", "must be true or false")
		}
	}
	if err == nil {
		err = s.anonymousGate(r.Context())
	}
	sub := caller(r)
	var sight projectSight
	if err == nil {
		sight, err = s.sight(r.Context())
	}
	var list []project.Project
	if err == nil {
		list, err = svc.List(r.Context())
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "projects.list"))
		return
	}
	rows := make([]project.Project, 0, lq.FetchLimit())
	for _, p := range projectOrder(list, lq.Desc) {
		if !sight.listed(sub, p, membership) || !projectMatches(p, lq.First("q")) {
			continue
		}
		if lq.After != nil && !projectAfter(p.Key, lq.After[0], lq.Desc) {
			continue
		}
		if rows = append(rows, p); len(rows) == lq.FetchLimit() {
			break
		}
	}
	page := NewPage(lq, rows, func(p project.Project) []string { return []string{p.Key} })
	WritePage(w, r, MapPage(page, func(p project.Project) gen.Project { return toProject(p, sight.roles[p.Key]) }))
}

// projectOrder is the order of the list of projects (Q-UI-837): the built-in project upstream first, whatever the sort, then the
// projects by key, ascending or descending. list is sorted by key.
func projectOrder(list []project.Project, desc bool) []project.Project {
	out := make([]project.Project, 0, len(list))
	for i := range list {
		p := list[i]
		if desc {
			p = list[len(list)-1-i]
		}
		if wellknown.IsProject(p.Key) {
			out = append([]project.Project{p}, out...)
			continue
		}
		out = append(out, p)
	}
	return out
}

// projectAfter reports whether a project comes after the cursor's key in the order of projectOrder: after upstream every other
// project does, and upstream comes after none.
func projectAfter(key, cursor string, desc bool) bool {
	switch {
	case wellknown.IsProject(key):
		return false
	case wellknown.IsProject(cursor):
		return true
	case desc:
		return key < cursor
	}
	return key > cursor
}

// levelAdmits reports whether a project's level admits the caller: internal for an internal subject (every authenticated subject
// but an external user, R-AUTHZ-030), public for every request (R-AUTHZ-035 rule 8). The token scope does not narrow it: the
// project is named, its content is decided repository by repository. The level of the built-in project upstream admits nobody: it
// is stored public so that its repositories can take any level (Q-AUTHZ-301).
func levelAdmits(sub authz.Subject, p project.Project) bool {
	if wellknown.IsProject(p.Key) {
		return false
	}
	switch p.Visibility {
	case "public":
		return true
	case "internal":
		return sub.Internal()
	}
	return false
}

// projectSight is what decides which projects the caller of a request sees: its memberships with the role, whether it holds
// projects:read and so sees every project, and whether it reads a repository of the built-in project upstream, which is how a
// caller without the capability sees that project (R-AUTHZ-041, Q-AUTHZ-301).
type projectSight struct {
	roles    map[string]authz.ProjectRole
	all      bool
	upstream bool
}

// listed decides whether the list shows the project: a holder of projects:read sees every project, a member its own, and any
// caller the projects its level admits, without a role, and the project upstream when it reads one of its repositories.
// membership keeps the caller's own projects (R-API-057).
func (v projectSight) listed(sub authz.Subject, p project.Project, membership bool) bool {
	if _, member := v.roles[p.Key]; member {
		return true
	}
	if membership {
		return false
	}
	return v.all || levelAdmits(sub, p) || v.upstream && wellknown.IsProject(p.Key)
}

// sight reads the caller's projectSight on the request's index. Whether the caller reads a repository of the project upstream is
// decided only for a caller that does not see every project.
func (s *Server) sight(ctx context.Context) (projectSight, error) {
	roles, all, err := s.memberProjects(ctx, string(authz.ProjectsRead))
	if err != nil || all {
		return projectSight{roles: roles, all: all}, err
	}
	reads, err := s.readsUpstream(ctx)
	return projectSight{roles: roles, upstream: reads}, err
}

// readsUpstream reports whether the caller reads a repository of the built-in project upstream that serves: one that is switched
// on, not being deleted and no system repository (R-AUTHZ-041, Q-AUTHZ-301). Without a snapshot reader nothing is read.
func (s *Server) readsUpstream(ctx context.Context) (bool, error) {
	reader := s.o.Repositories.Reader
	if reader == nil {
		return false, nil
	}
	repos, err := reader.ListProject(ctx, wellknown.ProjectKey)
	if err != nil {
		return false, err
	}
	names := make([]qname.Repo, 0, len(repos))
	for _, r := range repos {
		if upstreamServes(r) {
			names = append(names, r.Name)
		}
	}
	if len(names) == 0 {
		return false, nil
	}
	ctx, acc, err := requestHolder(ctx, s.o.Repositories.Authz)
	if err != nil {
		return false, err
	}
	visible, err := s.visibleKeys(ctx, acc, names, false)
	return len(visible) > 0, err
}

// memberProjects returns the caller's memberships with its role, and whether the caller holds the
// capability and so sees every project.
func (s *Server) memberProjects(ctx context.Context, capability string) (roles map[string]authz.ProjectRole, all bool, err error) {
	if all, err = s.holds(ctx, capability); err != nil {
		return nil, false, err
	}
	roles = map[string]authz.ProjectRole{}
	if pa, ok := s.o.Access.(ProjectAccess); ok {
		members, err := pa.Memberships(ctx)
		if err != nil {
			return nil, false, err
		}
		for _, m := range members {
			roles[m.Key] = m.Role
		}
	}
	return roles, all, nil
}

// projectBody is what the create and the replace read: the members a client writes,
// with the read-only ones of a representation obtained by GET ignored.
type projectBody = gen.ProjectInput

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func required(pointer string) errs.FieldError {
	return errs.FieldError{Pointer: pointer, Code: "required", Detail: "The member is required."}
}

// CreateProject implements POST /api/v1/projects.
func (s *Server) CreateProject(w http.ResponseWriter, r *http.Request, _ gen.CreateProjectParams) {
	svc, err := s.projects()
	if err == nil && s.idem == nil {
		err = unavailable("The idempotency store")
	}
	var body []byte
	var in projectBody
	if err == nil {
		body, err = ReadBody(w, r, MediaJSON, s.bodyLimit())
	}
	if err == nil {
		err = StrictUnmarshal(body, &in)
	}
	if err == nil && in.Key == nil {
		err = errs.E(errs.Invalid, errs.CodeValidation, "The key is required.", errs.WithFields(required("/key")))
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	input := project.Input{
		Key: *in.Key, DisplayName: derefStr(in.DisplayName), Description: derefStr(in.Description),
	}
	if in.Visibility != nil {
		input.Visibility = string(*in.Visibility)
		// A level named above private asks for a recent authentication (R-API-057); an absent one takes the default, which an
		// administrator set under one.
		if input.Visibility != "private" {
			if err := s.requireRecentAuth(r); err != nil {
				httpserver.Problem(w, r, err)
				return
			}
		}
	}
	if in.Owners != nil {
		input.Owners = *in.Owners
	}
	// An administrator names a level the installation restricts (R-AUTHZ-037, Q-AUTHZ-283).
	if input.MayChooseRestricted, err = s.holds(r.Context(), string(authz.SecuritySettingsWrite)); err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	actor := caller(r)
	var epoch int64
	err = s.idem.Do(w, r, body, IdempotentOp{Run: func(ctx context.Context, tx meta.Tx) (Outcome, error) {
		p, err := svc.CreateIn(ctx, tx, actor, input, &epoch)
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{
			Status: http.StatusCreated, Body: toProject(p, authz.ProjectRoleNone),
			Header: map[string]string{"Location": projectHref(r, p.Key), "ETag": ETag(p.Version, "")},
			Finish: finishEpoch(&epoch),
		}, nil
	}})
	if err != nil {
		httpserver.Problem(w, r, err)
	}
}

// GetProject implements GET /api/v1/projects/{projectKey}. The declaration admits the holder of
// projects:read and a member of the project. The ETag is the version of the PUT fields; members and counts
// are in the body and do not move it (Q-AUTHZ-185), so If-None-Match is not honoured: a 304 would hide a
// member added after the read (Q-AUTHZ-202).
func (s *Server) GetProject(w http.ResponseWriter, r *http.Request, key string) {
	svc, err := s.projects()
	var p project.Project
	if err == nil {
		p, err = svc.Get(r.Context(), key)
	}
	var sight projectSight
	if err == nil {
		err = s.anonymousGate(r.Context())
	}
	if err == nil {
		sight, err = s.sight(r.Context())
	}
	if err == nil && !sight.listed(caller(r), p, false) {
		err = notFoundProject(key)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	SetETag(w, p.Version, "")
	writeJSON(w, r, http.StatusOK, toProject(p, sight.roles[key]))
}

// ReplaceProject implements PUT /api/v1/projects/{key}. The binder answers 428 for a
// missing If-Match before this runs; the service answers 412 for a stale one.
func (s *Server) ReplaceProject(w http.ResponseWriter, r *http.Request, key string, _ gen.ReplaceProjectParams) {
	svc, err := s.projects()
	var pre Precondition
	if err == nil {
		pre, err = ParseIfMatch(r)
	}
	var in projectBody
	if err == nil {
		err = DecodeJSON(w, r, &in, s.bodyLimit())
	}
	var missing []errs.FieldError
	if err == nil {
		if in.DisplayName == nil {
			missing = append(missing, required("/displayName"))
		}
		if len(missing) > 0 {
			err = errs.E(errs.Invalid, errs.CodeValidation, "A replacement names displayName.", errs.WithFields(missing...))
		}
	}
	if err == nil && in.Key != nil {
		err = CheckImmutable(map[string]string{"key": key}, map[string]string{"key": *in.Key}, "key")
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	p, epoch, err := svc.Replace(r.Context(), caller(r), key, expectedRevision(pre), project.Input{
		Key: key, DisplayName: *in.DisplayName, Description: derefStr(in.Description),
	})
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	SetETag(w, p.Version, "")
	setEpoch(w, epoch)
	writeJSON(w, r, http.StatusOK, toProject(p, authz.ProjectRoleNone))
}

// DeleteProject implements DELETE /api/v1/projects/{key}; If-Match is optional.
func (s *Server) DeleteProject(w http.ResponseWriter, r *http.Request, key string, _ gen.DeleteProjectParams) {
	svc, err := s.projects()
	var pre Precondition
	if err == nil {
		pre, err = ParseIfMatch(r)
	}
	var epoch int64
	if err == nil {
		epoch, err = svc.Delete(r.Context(), caller(r), key, expectedRevision(pre))
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	setEpoch(w, epoch)
	w.WriteHeader(http.StatusNoContent)
}

// ListProjectMembers implements GET /api/v1/projects/{projectKey}/members.
func (s *Server) ListProjectMembers(w http.ResponseWriter, r *http.Request, projectKey string) {
	svc, err := s.projects()
	var members []project.Member
	if err == nil {
		members, err = svc.Members(r.Context(), projectKey)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	out := gen.MemberList{Items: make([]gen.Member, 0, len(members))}
	for _, m := range members {
		item := toProjectMember(m)
		item.Id = &m.Principal.ID
		item.AvatarUrl = avatarURL(m.AvatarSHA256)
		if m.DisplayName != "" {
			item.DisplayName = &m.DisplayName
		}
		out.Items = append(out.Items, item)
	}
	writeJSON(w, r, http.StatusOK, out)
}

func toProjectMember(m project.Member) gen.Member {
	return gen.Member{
		Kind: gen.MemberKind(m.Principal.Kind), Name: m.Principal.Name, Principal: m.Principal.String(),
		Project: m.Project, Role: gen.ProjectRole(m.Role.String()),
	}
}

// SetProjectMember implements PUT /api/v1/projects/{projectKey}/members/{principalRef}: 201 when the
// principal became a member, 200 when its role was set.
func (s *Server) SetProjectMember(w http.ResponseWriter, r *http.Request, projectKey, principalRef string) {
	svc, err := s.projects()
	var in gen.MemberInput
	if err == nil {
		err = DecodeJSON(w, r, &in, s.bodyLimit())
	}
	var role authz.ProjectRole
	if err == nil {
		var ok bool
		if role, ok = authz.ParseProjectRole(string(in.Role)); !ok {
			err = errs.E(errs.Invalid, errs.CodeValidation, "The role is reader, developer, maintainer or owner.",
				errs.WithFields(errs.FieldError{Pointer: "/role", Code: "invalid_role", Detail: "not one of reader, developer, maintainer, owner"}))
		}
	}
	var member project.Member
	var created bool
	var epoch int64
	if err == nil {
		member, created, epoch, err = svc.AddMember(r.Context(), caller(r), projectKey, principalRef, role, s.mayBypassLastOwner(r))
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	setEpoch(w, epoch)
	writeJSON(w, r, status, toProjectMember(member))
}

// RemoveProjectMember implements DELETE /api/v1/projects/{projectKey}/members/{principalRef}.
func (s *Server) RemoveProjectMember(w http.ResponseWriter, r *http.Request, projectKey, principalRef string) {
	svc, err := s.projects()
	var epoch int64
	if err == nil {
		epoch, err = svc.RemoveMember(r.Context(), caller(r), projectKey, principalRef, s.mayBypassLastOwner(r))
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	setEpoch(w, epoch)
	w.WriteHeader(http.StatusNoContent)
}

// mayBypassLastOwner reports whether the caller holds projects:write, which may remove or demote the last
// Owner of a project (D6 of Q-AUTHZ-208).
func (s *Server) mayBypassLastOwner(r *http.Request) bool {
	held, err := s.holds(r.Context(), "projects:write")
	return err == nil && held
}

// maxLookupQuery is the longest prefix the lookup accepts (the document's maxLength).
const maxLookupQuery = 128

// LookupProjectPrincipals implements GET /api/v1/projects/{key}/principals
// (Q-AUTHZ-182, R-API-077): a caller with identities:read, or an Owner of the project. The project must exist (404). The
// answer is what a member's row shows and the reference to write: the project's own service accounts, and the instance-wide
// ones for a holder of identities:read, who lists them anyway.
func (s *Server) LookupProjectPrincipals(w http.ResponseWriter, r *http.Request, key string, params gen.LookupProjectPrincipalsParams) {
	svc, err := s.projects()
	if err == nil {
		_, err = svc.Get(r.Context(), key)
	}
	query := project.Lookup{Prefix: params.Q, Project: key}
	if err == nil {
		err = lookupParams(params, &query)
	}
	if err == nil {
		query.InstanceAccounts, err = s.holds(r.Context(), string(authz.IdentitiesRead))
	}
	var found []project.Found
	if err == nil {
		found, err = svc.LookupPrincipals(r.Context(), query)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	out := gen.ProjectPrincipalList{Items: make([]gen.ProjectPrincipal, 0, len(found))}
	for _, p := range found {
		out.Items = append(out.Items, toProjectPrincipal(p))
	}
	writeJSON(w, r, http.StatusOK, out)
}

// lookupParams checks the bounds of the document the generated binding does not (the length of q, the limit) and reads the
// kinds, repeated or comma-separated, into the lookup (422).
func lookupParams(params gen.LookupProjectPrincipalsParams, query *project.Lookup) error {
	var fields []errs.FieldError
	if len(params.Q) > maxLookupQuery {
		fields = append(fields, errs.FieldError{Pointer: "/q", Code: "prefix_too_long", Detail: "The prefix has at most 128 characters."})
	}
	if params.Limit != nil {
		if query.Limit = *params.Limit; query.Limit < 1 || query.Limit > project.MaxLookup {
			fields = append(fields, errs.FieldError{Pointer: "/limit", Code: "invalid_limit", Detail: "The limit is 1 to 20."})
		}
	}
	if params.Kind != nil {
		raw := make([]string, 0, len(*params.Kind))
		for _, k := range *params.Kind {
			raw = append(raw, string(k))
		}
		for _, k := range splitValues(raw) {
			kind := project.PrincipalKind(k)
			if kind != project.KindUser && kind != project.KindGroup && kind != project.KindServiceAccount {
				fields = append(fields, errs.FieldError{Pointer: "/kind", Code: "invalid_kind", Detail: "The kind is user, group or service_account."})
				break
			}
			query.Kinds = append(query.Kinds, kind)
		}
	}
	if len(fields) > 0 {
		return errs.E(errs.Invalid, errs.CodeValidation, fields[0].Detail, errs.WithFields(fields...))
	}
	return nil
}

// toProjectPrincipal renders a principal of the lookup: a user with its display name and what its avatar needs, a service
// account with its project.
func toProjectPrincipal(p project.Found) gen.ProjectPrincipal {
	item := gen.ProjectPrincipal{Kind: gen.ProjectPrincipalKind(p.Kind), Name: p.Name, Principal: p.Ref()}
	switch p.Kind {
	case project.KindUser:
		item.Id = &p.ID
		item.DisplayName = optString(p.DisplayName)
		item.AvatarUrl = avatarURL(p.AvatarSHA256)
	case project.KindServiceAccount:
		item.Project = optString(p.Project)
	}
	return item
}

// notFoundProject is the answer for a project the caller may not see: the same as for one that does not exist.
func notFoundProject(key string) error {
	return errs.E(errs.NotFound, errs.CodeNotFound, "The project "+strconv.Quote(key)+" does not exist.")
}
