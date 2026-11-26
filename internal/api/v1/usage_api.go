package v1

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/limits/store"
	"github.com/guided-traffic/quader/internal/limits/usage"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

// The usage overview, the blocks, the explanation and the suggestions of subjects (usage-and-limits.md R-LIM-029,
// R-LIM-033 to R-LIM-035; R-API-082). Who sees what is R-LIM-029: a holder of usage:read everything, an Owner of a project
// the rows of its project's repositories, every signed-in user its own subject.

// ---- the place keys of the store

// placeOfKey reads a place key of the store (instance, project:<uuid>, repository:<id>) back.
func placeOfKey(key string) limits.Place {
	switch {
	case strings.HasPrefix(key, "project:"):
		return limits.Place{Kind: limits.PlaceProject, ProjectID: strings.TrimPrefix(key, "project:")}
	case strings.HasPrefix(key, "repository:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(key, "repository:"), 10, 64)
		return limits.Place{Kind: limits.PlaceRepository, RepoID: id}
	}
	return limits.InstancePlace()
}

// placeName renders a place key for the API: names instead of IDs.
func (s *Server) placeName(ctx context.Context, l LimitsServices, key string) map[string]any {
	p := placeOfKey(key)
	switch p.Kind {
	case limits.PlaceRepository:
		if l.Repos != nil {
			if repo, err := l.Repos.ByID(ctx, p.RepoID); err == nil {
				return map[string]any{"kind": "repository", "repository": repo.Name.String()}
			}
		}
		return map[string]any{"kind": "repository", "repository": key}
	case limits.PlaceProject:
		return map[string]any{"kind": "project", "project": s.projectKeyOf(ctx, l, p.ProjectID)}
	}
	return map[string]any{"kind": "instance"}
}

// projectKeyOf finds a project's key from its ID among the repositories (a project without repositories has no rows).
func (s *Server) projectKeyOf(ctx context.Context, l LimitsServices, id string) string {
	if l.Repos != nil {
		if all, err := l.Repos.List(ctx); err == nil {
			for _, r := range all {
				if r.ProjectID == id {
					return r.Name.Project
				}
			}
		}
	}
	return id
}

// ---- blocks (R-LIM-025, R-LIM-033)

// ownerPlaces are the place keys of a project and its repositories, for an Owner's view of blocks.
func (s *Server) ownerPlaces(ctx context.Context, l LimitsServices, projectKey string) ([]string, error) {
	id, err := l.Projects(ctx, projectKey)
	if err != nil {
		return nil, err
	}
	out := []string{"project:" + id}
	all, err := l.Repos.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range all {
		if r.Name.Project == projectKey {
			out = append(out, "repository:"+r.ID)
		}
	}
	return out, nil
}

func (s *Server) owns(ctx context.Context, projectKey string) bool {
	pa, ok := s.o.Access.(ProjectAccess)
	if !ok || projectKey == "" {
		return false
	}
	owns, err := pa.Owns(ctx, projectKey)
	return err == nil && owns
}

// placeParam reads the place filter of the blocks: project:<key> or repository:<project>/<key>, into the project it belongs
// to and its store keys.
func (s *Server) placeParam(ctx context.Context, l LimitsServices, v string) (project string, keys []string, err error) {
	switch {
	case strings.HasPrefix(v, "project:"):
		project = strings.TrimPrefix(v, "project:")
		keys, err = s.ownerPlaces(ctx, l, project)
		return project, keys, err
	case strings.HasPrefix(v, "repository:"):
		name, ok := parseRepoName(strings.TrimPrefix(v, "repository:"))
		if !ok {
			return "", nil, badParam("place", "must be project:<key> or repository:<project>/<key>")
		}
		repo, err := l.Repos.ByName(ctx, name)
		if err != nil {
			return "", nil, err
		}
		return name.Project, []string{"repository:" + repo.ID}, nil
	}
	return "", nil, badParam("place", "must be project:<key> or repository:<project>/<key>")
}

func (s *Server) blockRep(ctx context.Context, l LimitsServices, n store.Names, b store.BlockRow) map[string]any {
	var lifted any
	if b.LiftedAt != nil {
		lifted = b.LiftedAt.UTC()
	}
	return map[string]any{
		"id": b.ID, "subject": s.subjectRep(ctx, l, n, b.Subject.Kind, b.Subject.Ref), "place": s.placeName(ctx, l, b.PlaceKey),
		"startedAt": b.Started.UTC(), "until": b.Until.UTC(), "refused": b.Refused, "limit": b.Limit, "liftedAt": lifted,
		"liftedBy": s.principalName(ctx, l, b.LiftedBy),
	}
}

// ListLimitBlocks implements GET /api/v1/limits/blocks.
func (s *Server) ListLimitBlocks(w http.ResponseWriter, r *http.Request, params gen.ListLimitBlocksParams) {
	ctx := r.Context()
	l, err := s.limitsServices()
	reader := false
	if err == nil {
		reader, err = s.holds(ctx, string(authz.LimitsRead))
	}
	var places []string
	if err == nil && params.Place != nil && *params.Place != "" {
		var project string
		project, places, err = s.placeParam(ctx, l, *params.Place)
		if err == nil && !reader && !s.owns(ctx, project) {
			err = errs.E(errs.Forbidden, errs.CodeForbidden, "An Owner reads the blocks of its own project only.")
		}
	} else if err == nil && !reader {
		err = badParam("place", "is required for an Owner of a project")
	}
	active := params.Active == nil || *params.Active
	limit := 50
	if params.Limit != nil {
		limit = min(200, max(1, *params.Limit))
	}
	var rows []store.BlockRow
	if err == nil {
		rows, err = l.Store.Blocks(ctx, active, places, limit)
	}
	var refs nameRefs
	for _, b := range rows {
		refs.subject(b.Subject.Kind, b.Subject.Ref)
	}
	var n store.Names
	if err == nil {
		n, err = s.names(ctx, l, refs)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, b := range rows {
		items = append(items, s.blockRep(ctx, l, n, b))
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
}

// LiftLimitBlock implements DELETE /api/v1/limits/blocks/{id}: the marker goes, the refused counters of the window are reset
// (R-LIM-025).
func (s *Server) LiftLimitBlock(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	l, err := s.limitsServices()
	var b store.BlockRow
	found := false
	if err == nil {
		b, found, err = l.Store.Block(ctx, id)
	}
	if err == nil && !found {
		err = errs.E(errs.NotFound, errs.CodeNotFound, "No block has this ID.")
	}
	writer := false
	if err == nil {
		writer, err = s.holds(ctx, string(authz.LimitsWrite))
	}
	if err == nil && !writer && !s.ownsPlace(ctx, l, b.PlaceKey) {
		err = errs.E(errs.Forbidden, errs.CodeForbidden, "An Owner lifts the blocks of its own project only.")
	}
	if err == nil && (b.LiftedAt != nil || !b.Until.After(time.Now())) {
		err = errs.E(errs.Conflict, codeBlockEnded, "The block has ended.")
	}
	lifted := false
	if err == nil {
		lifted, err = l.Store.LiftBlock(ctx, limitsActor(r), b)
	}
	if err == nil && !lifted {
		err = errs.E(errs.Conflict, codeBlockEnded, "The block has ended.")
	}
	if err == nil {
		blk := b.Block
		blk.Place = placeOfKey(b.PlaceKey)
		err = l.Engine.LiftBlock(ctx, blk)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ownsPlace reports whether the caller is an Owner of the project a block's place belongs to.
func (s *Server) ownsPlace(ctx context.Context, l LimitsServices, key string) bool {
	p := placeOfKey(key)
	switch p.Kind {
	case limits.PlaceRepository:
		repo, err := l.Repos.ByID(ctx, p.RepoID)
		return err == nil && s.owns(ctx, repo.Name.Project)
	case limits.PlaceProject:
		return s.owns(ctx, s.projectKeyOf(ctx, l, p.ProjectID))
	}
	return false
}

// ---- subjects

// subjectOfParam reads <kind>/<id> of the explanation and the usage paths into a subject (R-LIM-034): user/<username>,
// technicalToken/<ID>, ci/<issuer name>:<project ID>, anonymous/<address or prefix>, system/system.
func (s *Server) subjectOfParam(ctx context.Context, l LimitsServices, kind, id string) (limits.Subject, error) {
	k, ok := limits.ParseSubjectKind(kind)
	if !ok {
		return limits.Subject{}, errs.E(errs.NotFound, errs.CodeNotFound, "No subject of this kind.")
	}
	switch k {
	case limits.KindUser:
		if l.Principals == nil {
			return limits.Subject{}, unavailable("The principal store")
		}
		p, err := l.Principals.ByName(ctx, "user", id)
		if err != nil {
			return limits.Subject{}, errs.E(errs.NotFound, errs.CodeNotFound, "No user has this name.")
		}
		s := limits.Subject{Kind: k, Ref: p.ID}
		if groups, err := l.Principals.Groups(ctx, p.ID); err == nil {
			for _, g := range groups {
				s.Groups = append(s.Groups, g.ID)
			}
		}
		if l.IsAdministrator != nil {
			s.Admin = l.IsAdministrator(ctx, p.ID, s.Groups)
		}
		return s, nil
	case limits.KindCI:
		issuer, project, ok := strings.Cut(id, ":")
		if !ok || l.CI == nil {
			return limits.Subject{}, errs.E(errs.NotFound, errs.CodeNotFound, "No CI project has this name.")
		}
		iid, found, err := l.CI.IssuerID(ctx, issuer)
		if err != nil || !found {
			return limits.Subject{}, errs.E(errs.NotFound, errs.CodeNotFound, "No CI issuer has this name.")
		}
		return limits.Subject{Kind: k, Ref: iid + ":" + project}, nil
	case limits.KindAnonymous:
		if id != limits.UnknownAddress && !strings.Contains(id, "/") {
			if a, err := netipParse(id); err == nil {
				id = limits.AddressPrefix(a)
			}
		}
		return limits.Subject{Kind: k, Ref: id}, nil
	case limits.KindSystem:
		return limits.System(), nil
	case limits.KindTechnicalToken:
		return s.tokenSubject(ctx, l, id), nil
	}
	return limits.Subject{Kind: k, Ref: id}, nil
}

// tokenSubject is a technical token's subject with its account's groups and administrator role, which resolution reads as it
// reads a user's (R-LIM-013, R-LIM-038). A token that no longer exists is its ID alone.
func (s *Server) tokenSubject(ctx context.Context, l LimitsServices, id string) limits.Subject {
	out := limits.Subject{Kind: limits.KindTechnicalToken, Ref: id}
	var refs nameRefs
	refs.subject(limits.KindTechnicalToken, id)
	n, err := s.names(ctx, l, refs)
	t, ok := n.Tokens[id]
	if err != nil || !ok || l.Principals == nil {
		return out
	}
	acct, err := l.Principals.ByName(ctx, principal.KindServiceAccount, t.Account)
	if err != nil {
		return out
	}
	if groups, err := l.Principals.Groups(ctx, acct.ID); err == nil {
		for _, g := range groups {
			out.Groups = append(out.Groups, g.ID)
		}
	}
	if l.IsAdministrator != nil {
		out.Admin = l.IsAdministrator(ctx, acct.ID, out.Groups)
	}
	return out
}

// ---- the explanation (R-LIM-035)

func (s *Server) explain(ctx context.Context, l LimitsServices, subj limits.Subject, repoParam string) (map[string]any, error) {
	_, snap, x, err := s.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	var repo qname.Repo
	var repoID int64
	var class repository.Class
	if repoParam != "" {
		name, ok := parseRepoName(repoParam)
		if !ok {
			return nil, badParam("repository", "must be <projectKey>/<repoKey>")
		}
		stored, err := l.Repos.ByName(ctx, name)
		if err != nil {
			return nil, err
		}
		repo, class = name, stored.Class
		repoID, _ = strconv.ParseInt(stored.ID, 10, 64)
	}
	res := x.Resolve(subj, repo, true)
	var refs nameRefs
	refs.subject(subj.Kind, subj.Ref)
	for _, st := range res.Steps {
		for _, r := range st.Rules {
			refs.rule(*r)
		}
	}
	if res.Rule != nil {
		refs.rule(*res.Rule)
	}
	n, err := s.names(ctx, l, refs)
	if err != nil {
		return nil, err
	}
	steps := make([]map[string]any, 0, len(res.Steps))
	for _, st := range res.Steps {
		step := map[string]any{"place": placeRep(st.Place), "step": st.Step, "outcome": "none"}
		if st.Matched {
			step["outcome"] = "matched"
			rules := make([]any, 0, len(st.Rules))
			for _, r := range st.Rules {
				rules = append(rules, s.ruleRef(ctx, l, n, x, r))
			}
			step["rules"] = rules
		}
		steps = append(steps, step)
	}
	rule := s.ruleRef(ctx, l, n, x, res.Rule)
	sources := map[string]string{}
	for i, src := range res.Sources {
		if src != "" {
			sources[limits.Figure(i).String()] = src
		}
	}
	var ceiling any
	if class == repository.Remote {
		cls := limits.CeilingSignedIn
		if subj.Kind == limits.KindAnonymous {
			cls = limits.CeilingAnonymous
		}
		c, _ := x.Ceiling(repoID, cls)
		rep := ceilingRep(c)
		rep["class"] = map[limits.CeilingClass]string{limits.CeilingAnonymous: "anonymous", limits.CeilingSignedIn: "signedIn"}[cls]
		ceiling = rep
	}
	var repoOut any
	if repoParam != "" {
		repoOut = repo.String()
	}
	return map[string]any{
		"subject": s.subjectRep(ctx, l, n, subj.Kind, subj.Ref), "repository": repoOut, "mode": snap.Settings.Mode,
		"administrator": subj.Admin, "steps": steps, "place": placeRep(res.Place), "rule": rule, "effective": res.Figures,
		"clamped": clampedRep(res.Figures, res.Clamped), "figureSources": sources, "ceiling": ceiling,
	}, nil
}

// ExplainLimits implements GET /api/v1/limits/explain. An Owner asks for a repository of its project.
func (s *Server) ExplainLimits(w http.ResponseWriter, r *http.Request, params gen.ExplainLimitsParams) {
	ctx := r.Context()
	l, err := s.limitsServices()
	reader := false
	if err == nil {
		reader, err = s.holds(ctx, string(authz.LimitsRead))
	}
	repoParam := ""
	if params.Repository != nil {
		repoParam = *params.Repository
	}
	if err == nil && !reader {
		name, ok := parseRepoName(repoParam)
		if !ok || !s.owns(ctx, name.Project) {
			err = errs.E(errs.Forbidden, errs.CodeForbidden, "An Owner asks for a repository of its own project.")
		}
	}
	var subj limits.Subject
	if err == nil {
		kind, id, ok := strings.Cut(limitParam(params.Subject), "/")
		if !ok {
			err = badParam("subject", "must be <kind>/<id>")
		} else {
			subj, err = s.subjectOfParam(ctx, l, kind, id)
		}
	}
	var out map[string]any
	if err == nil {
		out, err = s.explain(ctx, l, subj, repoParam)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, out)
}

func limitParam(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// mySubject is the caller's own subject: a user's, with its groups (R-LIM-029); a technical token or a CI token reads
// nothing of its own here.
func (s *Server) mySubject(ctx context.Context, l LimitsServices) (limits.Subject, error) {
	p := httpserver.PrincipalFrom(ctx)
	if p.Anonymous || (p.Kind != httpserver.KindUser && p.Kind != httpserver.KindBreakGlass && p.Kind != "") {
		return limits.Subject{}, errs.E(errs.Forbidden, errs.CodeForbidden, "Only a user has usage of its own here.")
	}
	s2 := limits.Subject{Kind: limits.KindUser, Ref: p.ID}
	for _, g := range p.Groups {
		s2.Groups = append(s2.Groups, g.ID)
	}
	if l.IsAdministrator != nil {
		s2.Admin = l.IsAdministrator(ctx, p.ID, s2.Groups) || p.Kind == httpserver.KindBreakGlass
	}
	return s2, nil
}

// GetMyLimits implements GET /api/v1/users/me/limits, for a repository the caller can read.
func (s *Server) GetMyLimits(w http.ResponseWriter, r *http.Request, params gen.GetMyLimitsParams) {
	ctx := r.Context()
	l, err := s.limitsServices()
	var subj limits.Subject
	if err == nil {
		subj, err = s.mySubject(ctx, l)
	}
	repoParam := limitParam(params.Repository)
	if err == nil && repoParam != "" {
		name, ok := parseRepoName(repoParam)
		if !ok {
			err = badParam("repository", "must be <projectKey>/<repoKey>")
		} else if _, acc, aerr := requestHolder(ctx, s.o.FilesDecider, name); aerr != nil {
			err = aerr
		} else if acc != nil {
			err = acc.Decide(ctx, authz.Coordinate{Repository: name, Kind: authz.KindMetadata, Actions: []authz.Action{authz.Read}}).Err()
		}
	}
	var out map[string]any
	if err == nil {
		out, err = s.explain(ctx, l, subj, repoParam)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, out)
}

// suggestionLimit is the most suggestions of one answer (R-LIM-034).
const suggestionLimit = 20

// SuggestLimitSubjects implements GET /api/v1/limits/subjects: technical tokens and CI projects by name for the rule dialog
// (R-LIM-034, R-UI-179). The technical tokens are every token whose name or account matches, of the accounts of the instance,
// the projects and the repositories; with project= the accounts of that project and its repositories. The CI projects are the
// ones CI tokens were issued for, by path or ID; an Owner, who names its project, gets the CI projects that downloaded from the
// project's repositories in the last 30 days, since the issuances of other projects are not its to read (Q-LIM-50).
func (s *Server) SuggestLimitSubjects(w http.ResponseWriter, r *http.Request, params gen.SuggestLimitSubjectsParams) {
	ctx := r.Context()
	l, err := s.limitsServices()
	reader := false
	if err == nil {
		reader, err = s.holds(ctx, string(authz.LimitsRead))
	}
	project := limitParam(params.Project)
	if err == nil && !reader && (project == "" || !s.owns(ctx, project)) {
		err = errs.E(errs.Forbidden, errs.CodeForbidden, "An Owner asks for the subjects of its own project.")
	}
	want := map[string]bool{}
	if params.Kinds != nil {
		for _, k := range *params.Kinds {
			for _, one := range strings.Split(string(k), ",") {
				want[one] = true
			}
		}
	}
	all := len(want) == 0
	text := strings.TrimSpace(limitParam(params.Q))
	items := []map[string]any{}
	if err == nil && (all || want["technicalToken"]) {
		var tokens []store.TokenName
		tokens, err = l.Store.SearchTechnicalTokens(ctx, text, project, suggestionLimit)
		for _, t := range tokens {
			items = append(items, tokenSubject(t))
		}
	}
	if err == nil && (all || want["ci"]) && len(items) < suggestionLimit {
		items, err = s.ciSuggestions(ctx, l, text, project, reader, items)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items})
}

// ciSuggestions adds the CI projects of a suggestion (R-LIM-034).
func (s *Server) ciSuggestions(ctx context.Context, l LimitsServices, text, project string, reader bool, items []map[string]any) ([]map[string]any, error) {
	room := suggestionLimit - len(items)
	if reader && project == "" {
		found, err := l.Store.SearchCIProjects(ctx, text, room)
		for _, c := range found {
			items = append(items, ciSubject(c))
		}
		return items, err
	}
	scope, err := s.projectScope(ctx, l, project, "")
	if err != nil {
		return items, err
	}
	rows, err := l.Usage.Subjects(ctx, usage.Period30d, scope, []limits.SubjectKind{limits.KindCI}, "")
	if err != nil {
		return items, err
	}
	var refs nameRefs
	for _, u := range rows {
		refs.subject(u.Kind, u.Ref)
	}
	n, err := s.names(ctx, l, refs)
	if err != nil {
		return items, err
	}
	needle := strings.ToLower(text)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Ref < rows[j].Ref })
	for _, u := range rows {
		rep := s.subjectRep(ctx, l, n, u.Kind, u.Ref)
		path, _ := rep["projectPath"].(string)
		if needle != "" && !strings.Contains(strings.ToLower(path), needle) && rep["projectId"] != text {
			continue
		}
		items = append(items, rep)
		if room--; room == 0 {
			break
		}
	}
	return items, nil
}

// ---- usage (R-LIM-034)

// usageQuery is the parsed parameters of a usage list.
type usageQuery struct {
	period usage.Period
	kinds  []limits.SubjectKind
	state  string
	q      string
	sort   string
	asc    bool
	limit  int
	offset int
}

func parseUsageQuery(r *http.Request) (usageQuery, error) {
	v := r.URL.Query()
	uq := usageQuery{period: usage.PeriodDay, sort: "requests", limit: 50}
	switch p := v.Get("period"); p {
	case "", "day":
	case "hour", "30d":
		uq.period = usage.Period(p)
	default:
		return uq, badParam("period", "must be hour, day or 30d")
	}
	for _, k := range v["kind"] {
		for _, one := range strings.Split(k, ",") {
			sk, ok := limits.ParseSubjectKind(one)
			if !ok {
				return uq, badParam("kind", "must be user, technicalToken, ci, anonymous or system")
			}
			uq.kinds = append(uq.kinds, sk)
		}
	}
	if len(uq.kinds) == 0 {
		uq.kinds = []limits.SubjectKind{limits.KindUser, limits.KindTechnicalToken, limits.KindCI, limits.KindAnonymous, limits.KindSystem}
	}
	uq.state, uq.q = v.Get("state"), strings.ToLower(v.Get("q"))
	switch st := uq.state; st {
	case "", "close", "limitReached", "wouldBeLimited", "blocked":
	default:
		return uq, badParam("state", "must be close, limitReached, wouldBeLimited or blocked")
	}
	if s := v.Get("sort"); s != "" {
		switch s {
		case "requests", "cachedBytes", "uncachedBytes", "upstreamRequests", "refused", "utilisation", "name":
			uq.sort = s
		default:
			return uq, badParam("sort", "is not a sort field of the list")
		}
	}
	switch o := v.Get("order"); o {
	case "", "desc":
	case "asc":
		uq.asc = true
	default:
		return uq, badParam("order", "must be asc or desc")
	}
	if s := v.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return uq, badParam("limit", "must be a positive integer")
		}
		uq.limit = min(n, 200)
	}
	if c := v.Get("cursor"); c != "" {
		raw, err := base64.RawURLEncoding.DecodeString(c)
		n, cerr := strconv.Atoi(strings.TrimPrefix(string(raw), "o:"))
		if err != nil || cerr != nil || n < 0 {
			return uq, invalidCursor()
		}
		uq.offset = n
	}
	return uq, nil
}

// usageRow is one row of the overview with what the list filters and sorts on.
type usageRow struct {
	rep   map[string]any
	name  string
	text  string // what the search q= matches: every name of the subject, lower case
	state string
	u     usage.SubjectUsage
}

// subjectNames are the name a row sorts and exports by and the text its search matches (R-LIM-034: "a search on the subject's
// name or address"): a user's name and display name, a technical token's name and account, a CI project's path and ID.
func subjectNames(rep map[string]any) (name, text string) {
	str := func(k string) string { v, _ := rep[k].(string); return v }
	switch rep["kind"] {
	case "user", "technicalToken":
		name = str("name")
	case "ci":
		if name = str("projectPath"); name == "" {
			name = str("id")
		}
	default:
		name = str("id")
	}
	return name, strings.ToLower(strings.Join([]string{name, str("id"), str("displayName"), str("account"), str("address")}, "\x00"))
}

// usageRows reads, names and states the rows of a scope (R-LIM-034).
func (s *Server) usageRows(ctx context.Context, l LimitsServices, uq usageQuery, scope usage.Scope) ([]usageRow, limits.Mode, time.Time, time.Time, error) {
	mode := l.Engine.Mode()
	rows, err := l.Usage.Subjects(ctx, uq.period, scope, uq.kinds, "")
	if err != nil {
		return nil, mode, time.Time{}, time.Time{}, err
	}
	blocks, err := l.Store.Blocks(ctx, true, nil, 10_000)
	if err != nil {
		return nil, mode, time.Time{}, time.Time{}, err
	}
	blocked := map[string]time.Time{}
	for _, b := range blocks {
		k := string(b.Subject.Kind) + ":" + b.Subject.Ref
		if b.Until.After(blocked[k]) {
			blocked[k] = b.Until
		}
	}
	var refs nameRefs
	for _, u := range rows {
		refs.subject(u.Kind, u.Ref)
	}
	n, err := s.names(ctx, l, refs)
	if err != nil {
		return nil, mode, time.Time{}, time.Time{}, err
	}
	now := time.Now()
	from, _ := uq.period.Window(now)
	out := make([]usageRow, 0, len(rows))
	for _, u := range rows {
		rep := s.subjectRep(ctx, l, n, u.Kind, u.Ref)
		name, text := subjectNames(rep)
		until, isBlocked := blocked[string(u.Kind)+":"+u.Ref]
		state := usageState(u, mode, isBlocked, now)
		var util any
		if u.Figure != "" {
			util = map[string]any{"ratio": u.Utilisation, "figure": u.Figure, "place": s.placeName(ctx, l, u.Place)}
		}
		var blockedUntil any
		if isBlocked {
			blockedUntil = until.UTC()
		}
		out = append(out, usageRow{u: u, name: name, text: text, state: state, rep: map[string]any{
			"subject": rep, "requests": u.Requests, "refused": u.Refused, "cachedBytes": u.Cached, "uncachedBytes": u.Uncached,
			"upstreamRequests": u.Upstream, "limitedEvents": u.Limited, "wouldLimitEvents": u.WouldLimit, "utilisation": util,
			"state": state, "blockedUntil": blockedUntil,
		}})
	}
	return out, mode, from, now, nil
}

// usageState is the state of a row (R-LIM-034).
func usageState(u usage.SubjectUsage, mode limits.Mode, blocked bool, now time.Time) string {
	recent := now.Add(-time.Hour)
	switch {
	case blocked:
		return "blocked"
	case mode == limits.ModeEnforce && (u.LastLimited.After(recent) || u.Utilisation >= 1):
		return "limitReached"
	case mode == limits.ModeAudit && (u.LastWouldLimit.After(recent) || u.Utilisation >= 1):
		return "wouldBeLimited"
	case u.Utilisation >= 0.7:
		return "close"
	case u.Unlimited:
		return "unlimited"
	}
	return "normal"
}

func filterSort(rows []usageRow, uq usageQuery) []usageRow {
	out := rows[:0]
	for _, r := range rows {
		if uq.state != "" && r.state != uq.state {
			continue
		}
		if uq.q != "" && !strings.Contains(r.text, uq.q) {
			continue
		}
		out = append(out, r)
	}
	key := func(r usageRow) float64 {
		switch uq.sort {
		case "cachedBytes":
			return float64(r.u.Cached)
		case "uncachedBytes":
			return float64(r.u.Uncached)
		case "upstreamRequests":
			return float64(r.u.Upstream)
		case "refused":
			return float64(r.u.Refused)
		case "utilisation":
			return r.u.Utilisation
		}
		return float64(r.u.Requests)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if uq.sort == "name" {
			if uq.asc {
				return a.name < b.name
			}
			return a.name > b.name
		}
		ka, kb := key(a), key(b)
		if ka == kb {
			return a.name < b.name
		}
		if uq.asc {
			return ka < kb
		}
		return ka > kb
	})
	return out
}

func (s *Server) listUsage(w http.ResponseWriter, r *http.Request, scope usage.Scope) {
	ctx := r.Context()
	l, err := s.limitsServices()
	var uq usageQuery
	if err == nil {
		uq, err = parseUsageQuery(r)
	}
	var rows []usageRow
	var mode limits.Mode
	var from, to time.Time
	if err == nil {
		rows, mode, from, to, err = s.usageRows(ctx, l, uq, scope)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	rows = filterSort(rows, uq)
	end := min(len(rows), uq.offset+uq.limit)
	items := make([]map[string]any, 0, max(0, end-uq.offset))
	for i := uq.offset; i < end; i++ {
		items = append(items, rows[i].rep)
	}
	var next any
	if end < len(rows) {
		c := base64.RawURLEncoding.EncodeToString([]byte("o:" + strconv.Itoa(end)))
		next = c
		w.Header().Set("Link", "<"+nextURL(r, c)+`>; rel="next"`)
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"period": map[string]any{"from": from.UTC(), "to": to.UTC()}, "mode": mode,
		"items": items, "nextCursor": next,
	})
}

func (s *Server) exportUsage(w http.ResponseWriter, r *http.Request, scope usage.Scope) {
	ctx := r.Context()
	l, err := s.limitsServices()
	var uq usageQuery
	if err == nil {
		uq, err = parseUsageQuery(r)
	}
	var rows []usageRow
	var from, to time.Time
	if err == nil {
		rows, _, from, to, err = s.usageRows(ctx, l, uq, scope)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	rows = filterSort(rows, uq)
	if len(rows) > 100_000 {
		rows = rows[:100_000]
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="usage.csv"`)
	w.WriteHeader(http.StatusOK)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{
		"subject_kind", "subject", "display_name", "account", "from", "to", "requests", "refused", "cached_bytes",
		"uncached_bytes", "upstream_requests", "limited_events", "would_limit_events", "utilisation", "utilisation_figure", "state",
	})
	for _, row := range rows {
		rep := row.rep["subject"].(map[string]any)
		display, _ := rep["displayName"].(string)
		account, _ := rep["account"].(string)
		_ = cw.Write([]string{
			row.u.Kind.APIName(), csvSafe(row.name), csvSafe(display), csvSafe(account), from.UTC().Format(time.RFC3339),
			to.UTC().Format(time.RFC3339), i64(row.u.Requests), i64(row.u.Refused), i64(row.u.Cached), i64(row.u.Uncached),
			i64(row.u.Upstream), i64(row.u.Limited), i64(row.u.WouldLimit), strconv.FormatFloat(row.u.Utilisation, 'f', 3, 64),
			row.u.Figure, row.state,
		})
	}
	cw.Flush()
}

func i64(v int64) string { return strconv.FormatInt(v, 10) }

// projectScope is the repositories of a project, optionally one of them (R-LIM-029: an Owner reads its project's rows).
func (s *Server) projectScope(ctx context.Context, l LimitsServices, projectKey, repoKey string) (usage.Scope, error) {
	all, err := l.Repos.List(ctx)
	if err != nil {
		return usage.Scope{}, err
	}
	scope := usage.Scope{Repos: []int64{}}
	for _, r := range all {
		if r.Name.Project != projectKey || (repoKey != "" && r.Name.Key != repoKey) {
			continue
		}
		if id, err := strconv.ParseInt(r.ID, 10, 64); err == nil {
			scope.Repos = append(scope.Repos, id)
		}
	}
	if len(scope.Repos) == 0 {
		scope.Repos = []int64{-1} // a project without repositories has no rows
	}
	return scope, nil
}

// ListUsage implements GET /api/v1/usage.
func (s *Server) ListUsage(w http.ResponseWriter, r *http.Request, params gen.ListUsageParams) {
	scope, err := s.instanceScope(r, params.Project)
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	s.listUsage(w, withoutQuery(r, "project"), scope)
}

// instanceScope is the instance's overview, or one project's repositories with project= (Q-API-353).
func (s *Server) instanceScope(r *http.Request, project *string) (usage.Scope, error) {
	if project == nil || *project == "" {
		return usage.Scope{}, nil
	}
	l, err := s.limitsServices()
	if err != nil {
		return usage.Scope{}, err
	}
	return s.projectScope(r.Context(), l, *project, "")
}

// ExportUsage implements GET /api/v1/usage/export.
func (s *Server) ExportUsage(w http.ResponseWriter, r *http.Request, params gen.ExportUsageParams) {
	scope, err := s.instanceScope(r, params.Project)
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	s.exportUsage(w, withoutQuery(r, "project"), scope)
}

// ListLimitProjects implements GET /api/v1/limits/projects (Q-API-351): per project its maximum and the number of rules at the
// project and at its repositories, in one read for the projects tab of the download limits.
func (s *Server) ListLimitProjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Only the projects with a maximum or a rule (R-UI-177): a project's repositories alone do not make it a row.
	l, snap, x, err := s.snapshot(ctx)
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	keys := map[string]bool{}
	projectRules, repoRules := map[string]int{}, map[string]int{}
	for _, rule := range snap.Rules {
		switch rule.Place.Kind {
		case limits.PlaceProject:
			keys[rule.Place.ProjectKey] = true
			projectRules[rule.Place.ProjectKey]++
		case limits.PlaceRepository:
			keys[rule.Place.Repo.Project] = true
			repoRules[rule.Place.Repo.Project]++
		}
	}
	maxima := map[string]limits.Maximum{}
	for _, m := range snap.Maxima {
		keys[m.ProjectKey] = true
		maxima[m.ProjectKey] = m
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	items := make([]map[string]any, 0, len(sorted))
	for _, k := range sorted {
		var maximum any
		if m, ok := maxima[k]; ok {
			eff, _ := x.FiguresOfProfile(m.Profile, m.Overrides)
			maximum = map[string]any{
				"project": k, "profile": m.Profile, "overrides": m.Overrides, "effective": eff, "version": m.Version,
				"updatedAt": m.UpdatedAt.UTC(), "updatedBy": s.principalName(ctx, l, m.UpdatedBy),
			}
		}
		items = append(items, map[string]any{"project": k, "maximum": maximum, "projectRules": projectRules[k], "repositoryRules": repoRules[k]})
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
}

// ListProjectUsage implements GET /api/v1/projects/{projectKey}/usage.
func (s *Server) ListProjectUsage(w http.ResponseWriter, r *http.Request, projectKey string, params gen.ListProjectUsageParams) {
	l, err := s.limitsServices()
	var scope usage.Scope
	if err == nil {
		scope, err = s.projectScope(r.Context(), l, projectKey, limitParam(params.Repository))
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	r = withoutQuery(r, "repository")
	s.listUsage(w, r, scope)
}

// ExportProjectUsage implements GET /api/v1/projects/{projectKey}/usage/export.
func (s *Server) ExportProjectUsage(w http.ResponseWriter, r *http.Request, projectKey string, params gen.ExportProjectUsageParams) {
	l, err := s.limitsServices()
	var scope usage.Scope
	if err == nil {
		scope, err = s.projectScope(r.Context(), l, projectKey, limitParam(params.Repository))
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	s.exportUsage(w, r, scope)
}

func withoutQuery(r *http.Request, name string) *http.Request {
	q := r.URL.Query()
	q.Del(name)
	u := *r.URL
	u.RawQuery = q.Encode()
	r2 := r.Clone(r.Context())
	r2.URL = &u
	return r2
}

// detail is a subject's detail in a scope (R-LIM-034).
func (s *Server) detail(ctx context.Context, l LimitsServices, subj limits.Subject, period usage.Period, scope usage.Scope) (map[string]any, error) {
	rows, err := l.Usage.Subjects(ctx, period, scope, []limits.SubjectKind{subj.Kind}, subj.Ref)
	if err != nil {
		return nil, err
	}
	var u usage.SubjectUsage
	for _, row := range rows {
		if row.Kind == subj.Kind && row.Ref == subj.Ref {
			u = row
		}
	}
	history, err := l.Usage.History(ctx, subj.Kind, subj.Ref, scope)
	if err != nil {
		return nil, err
	}
	repos, err := l.Usage.Repositories(ctx, period, subj.Kind, subj.Ref, scope)
	if err != nil {
		return nil, err
	}
	// A user's credentials are its session and its personal tokens, a certificate subject's its certificates by serial (R-LIM-027).
	var cs []usage.CredentialUsage
	if subj.Kind == limits.KindUser || subj.Kind == limits.KindCertificate {
		if cs, err = l.Usage.Credentials(ctx, period, subj.Kind, subj.Ref, scope); err != nil {
			return nil, err
		}
	}
	budgets, err := s.budgetKeys(ctx, l, subj, scope)
	if err != nil {
		return nil, err
	}
	var refs nameRefs
	refs.subject(subj.Kind, subj.Ref)
	for _, b := range budgets {
		if b.res.Rule != nil {
			refs.rule(*b.res.Rule)
		}
	}
	for _, c := range cs {
		if subj.Kind == limits.KindUser && c.Credential != limits.SessionCredential {
			refs.r.APITokens = append(refs.r.APITokens, c.Credential)
		}
	}
	n, err := s.names(ctx, l, refs)
	if err != nil {
		return nil, err
	}
	hist := make([]map[string]any, 0, len(history))
	for _, p := range history {
		hist = append(hist, map[string]any{
			"start": p.Start.UTC(), "requests": p.Requests, "cachedBytes": p.Cached, "uncachedBytes": p.Uncached,
			"upstreamRequests": p.Upstream, "refused": p.Refused, "limitedEvents": p.Limited, "wouldLimitEvents": p.WouldLimit,
		})
	}
	repoRows := make([]map[string]any, 0, len(repos))
	for _, x := range repos {
		name := ""
		if x.ProjectKey != "" {
			name = x.ProjectKey + "/" + x.RepoKey
		}
		repoRows = append(repoRows, map[string]any{
			"repository": name, "requests": x.Requests, "cachedBytes": x.Cached,
			"uncachedBytes": x.Uncached, "upstreamRequests": x.Upstream, "refused": x.Refused,
		})
	}
	creds := make([]map[string]any, 0, len(cs))
	for _, c := range cs {
		cred := map[string]any{"kind": "session"}
		if subj.Kind == limits.KindCertificate {
			cred = map[string]any{"kind": "certificate", "serial": c.Credential}
		} else if c.Credential != limits.SessionCredential {
			cred = map[string]any{"kind": "token", "id": c.Credential, "hint": nil, "description": nil}
			if h, ok := n.APITokens[c.Credential]; ok {
				cred["hint"], cred["description"] = h.Hint, h.Description
			}
		}
		creds = append(creds, map[string]any{"credential": cred, "requests": c.Requests, "cachedBytes": c.Cached, "uncachedBytes": c.Uncached})
	}
	budgetReps := make([]map[string]any, 0, len(budgets))
	for _, b := range budgets {
		budgetReps = append(budgetReps, s.budgetRep(ctx, l, n, b))
	}
	now := time.Now()
	from, _ := period.Window(now)
	return map[string]any{
		"subject": s.subjectRep(ctx, l, n, subj.Kind, subj.Ref), "period": map[string]any{"from": from.UTC(), "to": now.UTC()},
		"mode": l.Engine.Mode(),
		"totals": map[string]any{
			"requests": u.Requests, "refused": u.Refused, "cachedBytes": u.Cached, "uncachedBytes": u.Uncached,
			"upstreamRequests": u.Upstream, "limitedEvents": u.Limited, "wouldLimitEvents": u.WouldLimit,
		},
		"budgets": budgetReps, "history": hist, "repositories": repoRows, "credentials": creds,
	}, nil
}

// budgetKey is one budget key of a subject's detail: its place key, the rule that applies there now, the live estimate of its
// windowed figures and the end of an active block.
type budgetKey struct {
	key     string
	x       *limits.Index
	res     limits.Resolution
	used    limits.Figures
	blocked time.Time
}

// budgetKeys are the subject's budget keys of the last 24 hours in the scope (R-LIM-034): the places its usage rows recorded,
// each with the rule that applies there now from the index (limits.Index.ResolveAt), the live estimate of each windowed figure
// (one pipeline of reads per key) and its active block.
func (s *Server) budgetKeys(ctx context.Context, l LimitsServices, subj limits.Subject, scope usage.Scope) ([]budgetKey, error) {
	places, err := l.Usage.Places(ctx, subj.Kind, subj.Ref, scope)
	if err != nil || len(places) == 0 {
		return nil, err
	}
	_, _, x, err := s.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(places))
	for _, p := range places {
		keys = append(keys, p.Place)
	}
	blocks, err := l.Store.Blocks(ctx, true, keys, 1_000)
	if err != nil {
		return nil, err
	}
	out := make([]budgetKey, 0, len(places))
	for _, p := range places {
		b := budgetKey{key: p.Place, x: x, res: x.ResolveAt(subj, s.fullPlace(ctx, l, p.Place))}
		b.used, _ = l.Engine.Live(ctx, subj.String()+"@"+p.Place)
		for _, blk := range blocks {
			if blk.PlaceKey == p.Place && blk.Subject.Kind == subj.Kind && blk.Subject.Ref == subj.Ref && blk.Until.After(b.blocked) {
				b.blocked = blk.Until
			}
		}
		out = append(out, b)
	}
	sort.SliceStable(out, func(i, j int) bool { return placeOrder(out[i].key) < placeOrder(out[j].key) })
	return out, nil
}

// placeOrder puts the instance first, then the projects, then the repositories.
func placeOrder(key string) int {
	switch {
	case strings.HasPrefix(key, "project:"):
		return 1
	case strings.HasPrefix(key, "repository:"):
		return 2
	}
	return 0
}

// fullPlace reads a place key of the store into the place the index resolves at: a repository by its name, a project by its
// key.
func (s *Server) fullPlace(ctx context.Context, l LimitsServices, key string) limits.Place {
	p := placeOfKey(key)
	switch p.Kind {
	case limits.PlaceRepository:
		if l.Repos != nil {
			if repo, err := l.Repos.ByID(ctx, p.RepoID); err == nil {
				p.Repo, p.ProjectKey, p.ProjectID = repo.Name, repo.Name.Project, repo.ProjectID
			}
		}
	case limits.PlaceProject:
		p.ProjectKey = s.projectKeyOf(ctx, l, p.ProjectID)
	}
	return p
}

// budgetRep renders a budget key with every figure (R-LIM-034): a windowed figure its limit, the live estimate and its window;
// a rate its limit (and the burst of the request rate).
func (s *Server) budgetRep(ctx context.Context, l LimitsServices, n store.Names, b budgetKey) map[string]any {
	limit := func(f limits.Figure) any {
		if v := b.res.Figures[f]; v > 0 {
			return v
		}
		return nil
	}
	windowed := func(f limits.Figure, w int) map[string]any {
		return map[string]any{"limit": limit(f), "used": b.used[f], "windowSeconds": w}
	}
	var blocked any
	if !b.blocked.IsZero() {
		blocked = b.blocked.UTC()
	}
	return map[string]any{
		"place": s.placeName(ctx, l, b.key),
		"rule":  s.ruleRef(ctx, l, n, b.x, b.res.Rule),
		"figures": map[string]any{
			"requestsPerSecond":       map[string]any{"limit": limit(limits.RequestsPerSecond), "burst": limit(limits.RequestsBurst)},
			"requestsPerHour":         windowed(limits.RequestsPerHour, 3600),
			"cachedBytesPerSecond":    map[string]any{"limit": limit(limits.CachedBytesPerSecond)},
			"cachedBytesPerHour":      windowed(limits.CachedBytesPerHour, 3600),
			"uncachedBytesPerSecond":  map[string]any{"limit": limit(limits.UncachedBytesPerSecond)},
			"uncachedBytesPerHour":    windowed(limits.UncachedBytesPerHour, 3600),
			"upstreamRequestsPerHour": windowed(limits.UpstreamRequestsPerHour, 3600),
			"parallelTransfers":       map[string]any{"limit": limit(limits.ParallelTransfers), "used": b.used[limits.ParallelTransfers]},
			"bytesPerDay":             windowed(limits.BytesPerDay, 86400),
			"refusedPerHour":          windowed(limits.RefusedPerHour, 3600),
		},
		"blockedUntil": blocked,
	}
}

// GetUsageSubject implements GET /api/v1/usage/subjects/{kind}/{id}.
func (s *Server) GetUsageSubject(w http.ResponseWriter, r *http.Request, kind, id string, params gen.GetUsageSubjectParams) {
	s.subjectDetail(w, r, kind, id, periodOf(params.Period), usage.Scope{}, false)
}

// GetProjectUsageSubject implements GET /api/v1/projects/{projectKey}/usage/subjects/{kind}/{id}.
func (s *Server) GetProjectUsageSubject(w http.ResponseWriter, r *http.Request, projectKey, kind, id string, params gen.GetProjectUsageSubjectParams) {
	l, err := s.limitsServices()
	var scope usage.Scope
	if err == nil {
		scope, err = s.projectScope(r.Context(), l, projectKey, "")
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	s.subjectDetail(w, r, kind, id, periodOf(params.Period), scope, true)
}

func periodOf[T ~string](p *T) usage.Period {
	if p == nil {
		return usage.PeriodDay
	}
	return usage.Period(*p)
}

func (s *Server) subjectDetail(w http.ResponseWriter, r *http.Request, kind, id string, period usage.Period, scope usage.Scope, _ bool) {
	ctx := r.Context()
	l, err := s.limitsServices()
	var subj limits.Subject
	if err == nil {
		raw, uerr := url.PathUnescape(id)
		if uerr != nil {
			raw = id
		}
		subj, err = s.subjectOfParam(ctx, l, kind, raw)
	}
	var out map[string]any
	if err == nil {
		out, err = s.detail(ctx, l, subj, period, scope)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, out)
}

// parseRepoName reads <projectKey>/<repoKey>.
func parseRepoName(s string) (qname.Repo, bool) {
	r, err := qname.ParseRepo(s)
	return r, err == nil
}

func netipParse(s string) (netip.Addr, error) { return netip.ParseAddr(s) }

// GetMyUsage implements GET /api/v1/users/me/usage.
func (s *Server) GetMyUsage(w http.ResponseWriter, r *http.Request, params gen.GetMyUsageParams) {
	ctx := r.Context()
	l, err := s.limitsServices()
	var subj limits.Subject
	if err == nil {
		subj, err = s.mySubject(ctx, l)
	}
	var out map[string]any
	if err == nil {
		out, err = s.detail(ctx, l, subj, periodOf(params.Period), usage.Scope{})
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, out)
}
