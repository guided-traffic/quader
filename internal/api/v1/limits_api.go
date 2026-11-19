package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/audit"
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

// The operations of the download limits (usage-and-limits.md R-LIM-031 to R-LIM-033, R-LIM-035; R-API-082). The reads ask
// PostgreSQL through the store, never the index of a replica, so an answer after a write shows the write; the index that
// requests are decided on follows within the propagation bound of the area limits (R-ARCH-018).

// LimitsPrincipals looks a user or a group up; *principal.PGStore implements it.
type LimitsPrincipals interface {
	ByID(ctx context.Context, id string) (principal.Principal, error)
	ByName(ctx context.Context, kind principal.Kind, name string) (principal.Principal, error)
	Groups(ctx context.Context, id string) ([]principal.GroupRef, error)
}

// LimitsCIIssuers translates between a CI issuer's name and its immutable ID (Q-LIM-1).
type LimitsCIIssuers interface {
	IssuerID(ctx context.Context, name string) (string, bool, error)
	IssuerName(ctx context.Context, id string) (string, bool, error)
}

// LimitsServices are the services of the limits and usage operations; a nil Store answers them with 503.
type LimitsServices struct {
	Store      *store.Store
	Engine     *limits.Engine
	Usage      usage.Reader
	Principals LimitsPrincipals
	Repos      repository.Reader
	// Projects returns the ID of a project by key; an *errs.Error NotFound when none.
	Projects func(ctx context.Context, key string) (string, error)
	CI       LimitsCIIssuers
	// IsAdministrator reports whether a user holds the role admin now, directly or through a group (R-LIM-012), for the
	// explanation and the usage of a subject the API names.
	IsAdministrator func(ctx context.Context, id string, groups []string) bool
}

// The codes of the limits operations (R-LIM-031 to R-LIM-033).
const (
	codeInvalidFigure     = "invalid_figure"
	codeInvalidWho        = "invalid_who"
	codeLimitAboveMaximum = "limit_above_maximum"
	codeProfileImmutable  = "profile_immutable"
	codeProfileBuiltin    = "profile_builtin"
	codeBlockEnded        = "block_ended"
)

func (s *Server) limitsServices() (LimitsServices, error) {
	l := s.o.Limits
	if l.Store == nil || l.Engine == nil {
		return l, unavailable("The download limits")
	}
	return l, nil
}

// snapshot reads everything of the limits from PostgreSQL and compiles it, for the reads of the API.
func (s *Server) snapshot(ctx context.Context) (LimitsServices, limits.Snapshot, *limits.Index, error) {
	l, err := s.limitsServices()
	if err != nil {
		return l, limits.Snapshot{}, nil, err
	}
	snap, err := l.Store.Snapshot(ctx)
	if err != nil {
		return l, snap, nil, err
	}
	return l, snap, limits.NewIndex(snap), nil
}

func limitsActor(r *http.Request) audit.Actor {
	p := httpserver.PrincipalFrom(r.Context())
	return audit.Actor{Kind: string(p.Kind), ID: principal.IDOrEmpty(p.ID), Name: p.Name}
}

var profileKeyRE = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

// ---- profiles (R-LIM-031)

type profileWrite struct {
	Key         *string         `json:"key"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Figures     json.RawMessage `json:"figures"`
	Builtin     any             `json:"builtin"`
	UsedBy      any             `json:"usedBy"`
	Version     any             `json:"version"`
	CreatedAt   any             `json:"createdAt"`
	CreatedBy   any             `json:"createdBy"`
	UpdatedAt   any             `json:"updatedAt"`
	UpdatedBy   any             `json:"updatedBy"`
}

func figuresOf(raw json.RawMessage) (limits.Figures, error) {
	var f limits.Figures
	if len(raw) > 0 && string(raw) != "null" {
		if err := f.UnmarshalJSON(raw); err != nil {
			return f, errs.E(errs.Invalid, codeInvalidFigure, err.Error(), errs.WithFields(errs.FieldError{Pointer: "/figures", Code: codeInvalidFigure, Detail: err.Error()}))
		}
	}
	if fig, why, ok := f.Validate(); !ok {
		return f, errs.E(errs.Invalid, codeInvalidFigure, fig.String()+" "+why+".",
			errs.WithFields(errs.FieldError{Pointer: "/figures/" + fig.String(), Code: codeInvalidFigure, Detail: why}))
	}
	return f, nil
}

func (s *Server) principalName(ctx context.Context, l LimitsServices, id string) any {
	if id == "" || l.Principals == nil {
		return nil
	}
	p, err := l.Principals.ByID(ctx, id)
	if err != nil {
		return nil
	}
	return p.Name
}

func (s *Server) profileRep(ctx context.Context, l LimitsServices, p limits.Profile, used [2]int64) map[string]any {
	return map[string]any{
		"key": p.Key, "name": p.Name, "description": p.Description, "builtin": p.Builtin, "figures": p.Figures,
		"usedBy": map[string]int64{"assignments": used[0], "projectMaximums": used[1]}, "version": p.Version,
		"createdAt": p.CreatedAt.UTC(), "createdBy": s.principalName(ctx, l, p.CreatedBy),
		"updatedAt": p.UpdatedAt.UTC(), "updatedBy": s.principalName(ctx, l, p.UpdatedBy),
	}
}

func findProfile(snap limits.Snapshot, key string) (limits.Profile, bool) {
	for _, p := range snap.Profiles {
		if p.Key == key {
			return p, true
		}
	}
	return limits.Profile{}, false
}

func profileNotFound() error {
	return errs.E(errs.NotFound, errs.CodeNotFound, "No profile has this key.")
}

// ListLimitProfiles implements GET /api/v1/limits/profiles.
func (s *Server) ListLimitProfiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l, snap, _, err := s.snapshot(ctx)
	var used map[string][2]int64
	if err == nil {
		used, err = l.Store.ProfileUsage(ctx)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(snap.Profiles))
	for _, p := range snap.Profiles {
		items = append(items, s.profileRep(ctx, l, p, used[p.Key]))
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
}

// CreateLimitProfile implements POST /api/v1/limits/profiles.
func (s *Server) CreateLimitProfile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l, err := s.limitsServices()
	var in profileWrite
	if err == nil {
		err = DecodeJSON(w, r, &in, s.bodyLimit())
	}
	var p limits.Profile
	if err == nil {
		p, err = profileFromWrite(in, true)
	}
	if err == nil {
		err = l.Store.CreateProfile(ctx, limitsActor(r), p)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	s.answerProfile(w, r, l, p.Key, http.StatusCreated)
}

func profileFromWrite(in profileWrite, create bool) (limits.Profile, error) {
	p := limits.Profile{Name: in.Name, Description: in.Description}
	if create {
		if in.Key == nil || !profileKeyRE.MatchString(*in.Key) {
			return p, errs.E(errs.Invalid, errs.CodeValidation, "key must match ^[a-z][a-z0-9-]{1,31}$.",
				errs.WithFields(errs.FieldError{Pointer: "/key", Code: "pattern", Detail: "^[a-z][a-z0-9-]{1,31}$"}))
		}
		p.Key = *in.Key
	}
	if n := len([]rune(in.Name)); n < 1 || n > 64 {
		return p, errs.E(errs.Invalid, errs.CodeValidation, "name must have 1 to 64 characters.",
			errs.WithFields(errs.FieldError{Pointer: "/name", Code: "length", Detail: "1 to 64 characters"}))
	}
	if len([]rune(in.Description)) > 1024 {
		return p, errs.E(errs.Invalid, errs.CodeValidation, "description must have at most 1024 characters.",
			errs.WithFields(errs.FieldError{Pointer: "/description", Code: "length", Detail: "at most 1024 characters"}))
	}
	f, err := figuresOf(in.Figures)
	p.Figures = f
	return p, err
}

func (s *Server) answerProfile(w http.ResponseWriter, r *http.Request, l LimitsServices, key string, status int) {
	ctx := r.Context()
	snap, err := l.Store.Snapshot(ctx)
	var used map[string][2]int64
	if err == nil {
		used, err = l.Store.ProfileUsage(ctx)
	}
	p, ok := findProfile(snap, key)
	if err == nil && !ok {
		err = profileNotFound()
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	SetETag(w, p.Version, "")
	if status == http.StatusCreated {
		w.Header().Set("Location", "/api/v1/limits/profiles/"+p.Key)
	}
	writeJSON(w, r, status, s.profileRep(ctx, l, p, used[p.Key]))
}

// GetLimitProfile implements GET /api/v1/limits/profiles/{key}.
func (s *Server) GetLimitProfile(w http.ResponseWriter, r *http.Request, key string) {
	l, err := s.limitsServices()
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	s.answerProfile(w, r, l, key, http.StatusOK)
}

// ReplaceLimitProfile implements PUT /api/v1/limits/profiles/{key}.
func (s *Server) ReplaceLimitProfile(w http.ResponseWriter, r *http.Request, key string, _ gen.ReplaceLimitProfileParams) {
	ctx := r.Context()
	l, snap, _, err := s.snapshot(ctx)
	var pre Precondition
	if err == nil {
		pre, err = requireIfMatch(r)
	}
	cur, ok := findProfile(snap, key)
	if err == nil && !ok {
		err = profileNotFound()
	}
	var in profileWrite
	if err == nil {
		err = DecodeJSON(w, r, &in, s.bodyLimit())
	}
	var p limits.Profile
	if err == nil {
		p, err = profileFromWrite(in, false)
	}
	if err == nil && in.Key != nil && *in.Key != key {
		err = errs.E(errs.Invalid, errs.CodeValidation, "key is immutable.", errs.WithFields(errs.FieldError{Pointer: "/key", Code: "immutable", Detail: "the key cannot change"}))
	}
	if err == nil && cur.Key == limits.ProfileUnlimited && !p.Figures.AllUnlimited() {
		err = errs.E(errs.Conflict, codeProfileImmutable, "The figures of the profile Unlimited stay empty.")
	}
	if err == nil && !pre.Matches(cur.Version, "") {
		err = Failed(cur.Version)
	}
	if err == nil {
		err = l.Store.UpdateProfile(ctx, limitsActor(r), key, cur.Version, p)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	s.answerProfile(w, r, l, key, http.StatusOK)
}

func requireIfMatch(r *http.Request) (Precondition, error) {
	pre, err := ParseIfMatch(r)
	if err == nil {
		err = pre.Require()
	}
	return pre, err
}

// DeleteLimitProfile implements DELETE /api/v1/limits/profiles/{key}.
func (s *Server) DeleteLimitProfile(w http.ResponseWriter, r *http.Request, key string, _ gen.DeleteLimitProfileParams) {
	ctx := r.Context()
	l, snap, _, err := s.snapshot(ctx)
	cur, ok := findProfile(snap, key)
	if err == nil && !ok {
		err = profileNotFound()
	}
	var pre Precondition
	if err == nil {
		pre, err = ParseIfMatch(r)
	}
	if err == nil && cur.Builtin {
		err = errs.E(errs.Conflict, codeProfileBuiltin, "A built-in profile cannot be deleted.")
	}
	if err == nil && !pre.Matches(cur.Version, "") {
		err = Failed(cur.Version)
	}
	if err == nil {
		err = l.Store.DeleteProfile(ctx, limitsActor(r), key, cur.Version)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- rules (R-LIM-032)

// limitPlace is the place an operation addresses, with its names and IDs.
func (s *Server) limitPlace(ctx context.Context, l LimitsServices, projectKey, repoKey string) (limits.Place, repository.Repository, error) {
	switch {
	case repoKey != "":
		repo, err := l.Repos.ByName(ctx, qname.Repo{Project: projectKey, Key: repoKey})
		if err != nil {
			return limits.Place{}, repo, err
		}
		id, err := strconv.ParseInt(repo.ID, 10, 64)
		if err != nil {
			return limits.Place{}, repo, err
		}
		return limits.Place{Kind: limits.PlaceRepository, RepoID: id, Repo: repo.Name, ProjectID: repo.ProjectID, ProjectKey: projectKey}, repo, nil
	case projectKey != "":
		if l.Projects == nil {
			return limits.Place{}, repository.Repository{}, unavailable("The project lookup")
		}
		id, err := l.Projects(ctx, projectKey)
		return limits.Place{Kind: limits.PlaceProject, ProjectID: id, ProjectKey: projectKey}, repository.Repository{}, err
	}
	return limits.InstancePlace(), repository.Repository{}, nil
}

func samePlace(a, b limits.Place) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case limits.PlaceProject:
		return a.ProjectID == b.ProjectID
	case limits.PlaceRepository:
		return a.RepoID == b.RepoID
	}
	return true
}

func placeRep(p limits.Place) map[string]any {
	switch p.Kind {
	case limits.PlaceProject:
		return map[string]any{"kind": "project", "project": p.ProjectKey}
	case limits.PlaceRepository:
		return map[string]any{"kind": "repository", "repository": p.Repo.String()}
	}
	return map[string]any{"kind": "instance"}
}

type ruleWrite struct {
	Who       map[string]any   `json:"who"`
	Profile   string           `json:"profile"`
	Overrides limits.Overrides `json:"overrides"`
	Note      string           `json:"note"`
	ID        any              `json:"id"`
	Place     any              `json:"place"`
	Effective any              `json:"effective"`
	Clamped   any              `json:"clamped"`
	Capped    any              `json:"capped"`
	Builtin   any              `json:"builtin"`
	Version   any              `json:"version"`
	CreatedAt any              `json:"createdAt"`
	CreatedBy any              `json:"createdBy"`
	UpdatedAt any              `json:"updatedAt"`
	UpdatedBy any              `json:"updatedBy"`
}

func invalidWho(detail string) error {
	return errs.E(errs.Invalid, codeInvalidWho, detail, errs.WithFields(errs.FieldError{Pointer: "/who", Code: codeInvalidWho, Detail: detail}))
}

func whoField(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return v
}

// whoOf reads the "who" of a write into the store's kind and reference (R-LIM-011, R-LIM-032).
func (s *Server) whoOf(ctx context.Context, l LimitsServices, place limits.Place, who map[string]any) (limits.WhoKind, string, error) {
	switch whoField(who, "kind") {
	case "user", "group":
		kind, field := principal.KindUser, "user"
		if whoField(who, "kind") == "group" {
			kind, field = principal.KindGroup, "group"
		}
		name := whoField(who, field)
		if name == "" || l.Principals == nil {
			return "", "", invalidWho(field + " names no " + field + ".")
		}
		p, err := l.Principals.ByName(ctx, kind, name)
		if err != nil {
			return "", "", errs.E(errs.NotFound, errs.CodeNotFound, "No "+field+" has this name.")
		}
		if kind == principal.KindGroup {
			return limits.WhoGroup, p.ID, nil
		}
		return limits.WhoUser, p.ID, nil
	case "technicalToken":
		id := whoField(who, "technicalToken")
		if _, err := openapiUUID(id); err != nil {
			return "", "", invalidWho("technicalToken must be the ID of a technical token.")
		}
		return limits.WhoTechnicalToken, id, nil
	case "ciProject":
		issuer, project := whoField(who, "issuer"), whoField(who, "projectId")
		if issuer == "" || project == "" || l.CI == nil {
			return "", "", invalidWho("a CI project names its issuer and its project ID.")
		}
		id, ok, err := l.CI.IssuerID(ctx, issuer)
		if err != nil {
			return "", "", err
		}
		if !ok {
			return "", "", errs.E(errs.NotFound, errs.CodeNotFound, "No CI issuer has this name.")
		}
		return limits.WhoCIProject, id + ":" + project, nil
	case "address":
		p, ok := limits.CanonicalPrefix(whoField(who, "prefix"))
		if !ok {
			return "", "", invalidWho("prefix must be an IPv4 prefix /8 to /32 or an IPv6 prefix /32 to /64.")
		}
		return limits.WhoAddress, p.String(), nil
	case "every":
		k := whoField(who, "callerKind")
		ref := map[string]string{
			"user": limits.EveryUser, "technicalToken": limits.EveryTechnicalToken, "ci": limits.EveryCI,
			"anonymous": limits.EveryAnonymous, "administrator": limits.EveryAdministrator,
		}[k]
		if ref == "" {
			return "", "", invalidWho("callerKind must be user, technicalToken, ci, anonymous or administrator.")
		}
		if ref == limits.EveryAdministrator && place.Kind != limits.PlaceInstance {
			return "", "", invalidWho("Administrators are decided at the instance only.")
		}
		return limits.WhoEvery, ref, nil
	}
	return "", "", invalidWho("kind must be user, technicalToken, ciProject, address, group or every.")
}

func openapiUUID(s string) (openapi_types.UUID, error) {
	var u openapi_types.UUID
	err := u.UnmarshalText([]byte(s))
	return u, err
}

// whoRep renders the "who" of a rule with its read-only names; a user, a group or a technical token that no longer exists is
// marked deleted until limits-cleanup removes the rule (R-LIM-028).
func (s *Server) whoRep(ctx context.Context, l LimitsServices, n store.Names, r limits.Rule) map[string]any {
	out := map[string]any{}
	switch r.WhoKind {
	case limits.WhoUser:
		out["kind"], out["user"], out["deleted"] = "user", r.WhoRef, true
		if u, ok := n.Users[r.WhoRef]; ok {
			out["user"], out["displayName"], out["deleted"] = u.Name, u.Name, false
			if u.DisplayName != "" {
				out["displayName"] = u.DisplayName
			}
		}
	case limits.WhoGroup:
		out["kind"], out["group"], out["deleted"] = "group", r.WhoRef, true
		if l.Principals != nil {
			if p, err := l.Principals.ByID(ctx, r.WhoRef); err == nil {
				out["group"], out["displayName"], out["deleted"] = p.Name, p.Name, false
			}
		}
	case limits.WhoTechnicalToken:
		out["kind"], out["technicalToken"], out["deleted"] = "technicalToken", r.WhoRef, true
		if t, ok := n.Tokens[r.WhoRef]; ok {
			out["name"], out["account"], out["deleted"] = t.Name, "sa:"+t.Account, false
		}
	case limits.WhoCIProject:
		issuer, project, path := s.ciNames(ctx, l, n, r.WhoRef)
		out["kind"], out["issuer"], out["projectId"] = "ciProject", issuer, project
		if path != nil {
			out["projectPath"] = path
		}
	case limits.WhoAddress:
		out["kind"], out["prefix"] = "address", r.WhoRef
	case limits.WhoEvery:
		k := map[string]string{limits.EveryTechnicalToken: "technicalToken"}[r.WhoRef]
		if k == "" {
			k = r.WhoRef
		}
		out["kind"], out["callerKind"] = "every", k
	}
	return out
}

// ruleRef is a rule as the detail and the explanation name it (R-LIM-034, R-LIM-035), with its profile's name: a caller who
// reads its own usage names the profile without reading the profiles (Q-LIM-53).
func (s *Server) ruleRef(ctx context.Context, l LimitsServices, n store.Names, x *limits.Index, r *limits.Rule) any {
	if r == nil {
		return nil
	}
	name := r.Profile
	if p, ok := x.Profile(r.Profile); ok {
		name = p.Name
	}
	return map[string]any{
		"id": r.LogID(), "who": s.whoRep(ctx, l, n, *r), "profile": r.Profile, "profileName": name, "builtin": r.Builtin, "capped": r.Capped,
	}
}

func clampedRep(eff limits.Figures, clamped [limits.NumFigures]bool) map[string]any {
	out := map[string]any{}
	for i, c := range clamped {
		if c {
			if eff[i] > 0 {
				out[limits.Figure(i).String()] = eff[i]
			} else {
				out[limits.Figure(i).String()] = nil
			}
		}
	}
	return out
}

func (s *Server) ruleRep(ctx context.Context, l LimitsServices, n store.Names, x *limits.Index, r limits.Rule) map[string]any {
	eff, clamped := x.EffectiveOf(r)
	return map[string]any{
		"id": r.ID, "place": placeRep(r.Place), "who": s.whoRep(ctx, l, n, r), "profile": r.Profile, "overrides": r.Overrides,
		"effective": eff, "clamped": clampedRep(eff, clamped), "capped": r.Capped, "builtin": r.Builtin, "note": r.Note,
		"version": r.Version, "createdAt": r.CreatedAt.UTC(), "createdBy": s.principalName(ctx, l, r.CreatedBy),
		"updatedAt": r.UpdatedAt.UTC(), "updatedBy": s.principalName(ctx, l, r.UpdatedBy),
	}
}

func ruleNotFound() error {
	return errs.E(errs.NotFound, errs.CodeNotFound, "No rule of this place has this ID.")
}

func (s *Server) listRulesLimit(w http.ResponseWriter, r *http.Request, projectKey, repoKey string) {
	ctx := r.Context()
	l, snap, x, err := s.snapshot(ctx)
	var place limits.Place
	if err == nil {
		place, _, err = s.limitPlace(ctx, l, projectKey, repoKey)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	whoKind, profile := r.URL.Query().Get("whoKind"), r.URL.Query().Get("profile")
	var refs nameRefs
	for _, rule := range snap.Rules {
		if samePlace(rule.Place, place) {
			refs.rule(rule)
		}
	}
	n, err := s.names(ctx, l, refs)
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	items := []map[string]any{}
	for _, rule := range snap.Rules {
		if !samePlace(rule.Place, place) || (profile != "" && rule.Profile != profile) {
			continue
		}
		rep := s.ruleRep(ctx, l, n, x, rule)
		if whoKind != "" && rep["who"].(map[string]any)["kind"] != whoKind {
			continue
		}
		items = append(items, rep)
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
}

// capCheck refuses a capped rule whose figures exceed the cap (R-LIM-016): 422 limit_above_maximum with every figure.
func capCheck(ctx context.Context, l LimitsServices, x *limits.Index, rule limits.Rule) error {
	if !rule.Capped || rule.Place.Kind == limits.PlaceInstance {
		return nil
	}
	var groups []string
	if rule.WhoKind == limits.WhoUser && l.Principals != nil {
		_ = ctx // the current groups are the authentication stage's; the check takes the instance's rule of the user alone
	}
	cap := x.CapFor(rule, groups)
	want, ok := x.FiguresOfProfile(rule.Profile, rule.Overrides)
	if !ok {
		return errs.E(errs.NotFound, errs.CodeNotFound, "No profile has this key.")
	}
	var fields []errs.FieldError
	for i := range want {
		c := cap[i]
		if c <= 0 || (want[i] > 0 && want[i] <= c) {
			continue
		}
		requested := "unlimited"
		if want[i] > 0 {
			requested = strconv.FormatInt(want[i], 10)
		}
		limit := c
		fields = append(fields, errs.FieldError{
			Pointer: "/figures/" + limits.Figure(i).String(), Code: codeLimitAboveMaximum,
			Detail: "requested " + requested + ", at most " + strconv.FormatInt(c, 10), Cap: &limit,
		})
	}
	if len(fields) > 0 {
		return errs.E(errs.Invalid, codeLimitAboveMaximum,
			"The rule is above what the instance gives this caller and above the project's maximum.", errs.WithFields(fields...))
	}
	return nil
}

func (s *Server) createRuleLimit(w http.ResponseWriter, r *http.Request, projectKey, repoKey string) {
	ctx := r.Context()
	l, snap, x, err := s.snapshot(ctx)
	var place limits.Place
	if err == nil {
		place, _, err = s.limitPlace(ctx, l, projectKey, repoKey)
	}
	var in ruleWrite
	if err == nil {
		err = DecodeJSON(w, r, &in, s.bodyLimit())
	}
	rule := limits.Rule{Place: place, Profile: in.Profile, Overrides: in.Overrides, Note: in.Note}
	if err == nil {
		rule.WhoKind, rule.WhoRef, err = s.whoOf(ctx, l, place, in.Who)
	}
	if err == nil {
		err = s.checkRule(ctx, l, snap, x, &rule)
	}
	if err == nil {
		err = l.Store.CreateRule(ctx, limitsActor(r), &rule)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	s.answerRule(w, r, l, place, rule.ID, http.StatusCreated)
}

// checkRule applies the rules of R-LIM-011 and R-LIM-016 to a rule before it is written: the profile exists, the overrides
// are figures, the note's length, and the cap of a caller without limits:write.
func (s *Server) checkRule(ctx context.Context, l LimitsServices, snap limits.Snapshot, x *limits.Index, rule *limits.Rule) error {
	if _, ok := findProfile(snap, rule.Profile); !ok {
		return errs.E(errs.NotFound, errs.CodeNotFound, "No profile has this key.")
	}
	if len([]rune(rule.Note)) > 512 {
		return errs.E(errs.Invalid, errs.CodeValidation, "note must have at most 512 characters.",
			errs.WithFields(errs.FieldError{Pointer: "/note", Code: "length", Detail: "at most 512 characters"}))
	}
	eff, _ := x.FiguresOfProfile(rule.Profile, rule.Overrides)
	if fig, why, ok := eff.Validate(); !ok {
		return errs.E(errs.Invalid, codeInvalidFigure, fig.String()+" "+why+".")
	}
	writer, err := s.holds(ctx, string(authz.LimitsWrite))
	if err != nil {
		return err
	}
	rule.Capped = !writer && rule.Place.Kind != limits.PlaceInstance
	return capCheck(ctx, l, x, *rule)
}

func (s *Server) answerRule(w http.ResponseWriter, r *http.Request, l LimitsServices, place limits.Place, id string, status int) {
	ctx := r.Context()
	snap, err := l.Store.Snapshot(ctx)
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	x := limits.NewIndex(snap)
	for _, rule := range snap.Rules {
		if rule.ID == id && samePlace(rule.Place, place) {
			var refs nameRefs
			refs.rule(rule)
			n, err := s.names(ctx, l, refs)
			if err != nil {
				httpserver.Problem(w, r, err)
				return
			}
			SetETag(w, rule.Version, "")
			writeJSON(w, r, status, s.ruleRep(ctx, l, n, x, rule))
			return
		}
	}
	httpserver.Problem(w, r, ruleNotFound())
}

func findRule(snap limits.Snapshot, place limits.Place, id string) (limits.Rule, bool) {
	for _, rule := range snap.Rules {
		if rule.ID == id && samePlace(rule.Place, place) {
			return rule, true
		}
	}
	return limits.Rule{}, false
}

func (s *Server) getRuleLimit(w http.ResponseWriter, r *http.Request, projectKey, repoKey string, id string) {
	ctx := r.Context()
	l, err := s.limitsServices()
	var place limits.Place
	if err == nil {
		place, _, err = s.limitPlace(ctx, l, projectKey, repoKey)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	s.answerRule(w, r, l, place, id, http.StatusOK)
}

func (s *Server) replaceRuleLimit(w http.ResponseWriter, r *http.Request, projectKey, repoKey string, id string) {
	ctx := r.Context()
	l, snap, x, err := s.snapshot(ctx)
	var place limits.Place
	if err == nil {
		place, _, err = s.limitPlace(ctx, l, projectKey, repoKey)
	}
	var pre Precondition
	if err == nil {
		pre, err = requireIfMatch(r)
	}
	cur, ok := findRule(snap, place, id)
	if err == nil && !ok {
		err = ruleNotFound()
	}
	var in ruleWrite
	if err == nil {
		err = DecodeJSON(w, r, &in, s.bodyLimit())
	}
	next := cur
	next.Profile, next.Overrides, next.Note = in.Profile, in.Overrides, in.Note
	if err == nil && in.Who != nil {
		kind, ref, werr := s.whoOf(ctx, l, place, in.Who)
		if werr != nil || kind != cur.WhoKind || ref != cur.WhoRef {
			err = errs.E(errs.Conflict, store.CodeAssignmentBuiltin, "The caller a rule names is immutable; create a rule of its own.")
		}
	}
	if err == nil {
		err = s.checkRule(ctx, l, snap, x, &next)
	}
	if err == nil && !pre.Matches(cur.Version, "") {
		err = Failed(cur.Version)
	}
	if err == nil {
		err = l.Store.UpdateRule(ctx, limitsActor(r), next, cur.Version)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	s.answerRule(w, r, l, place, cur.ID, http.StatusOK)
}

func (s *Server) deleteRuleLimit(w http.ResponseWriter, r *http.Request, projectKey, repoKey string, id string) {
	ctx := r.Context()
	l, snap, _, err := s.snapshot(ctx)
	var place limits.Place
	if err == nil {
		place, _, err = s.limitPlace(ctx, l, projectKey, repoKey)
	}
	cur, ok := findRule(snap, place, id)
	if err == nil && !ok {
		err = ruleNotFound()
	}
	var pre Precondition
	if err == nil {
		pre, err = ParseIfMatch(r)
	}
	if err == nil && !pre.Matches(cur.Version, "") {
		err = Failed(cur.Version)
	}
	if err == nil {
		err = l.Store.DeleteRule(ctx, limitsActor(r), cur, cur.Version)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListInstanceLimitAssignments implements GET /api/v1/limits/assignments.
func (s *Server) ListInstanceLimitAssignments(w http.ResponseWriter, r *http.Request, _ gen.ListInstanceLimitAssignmentsParams) {
	s.listRulesLimit(w, r, "", "")
}

// CreateInstanceLimitAssignment implements POST /api/v1/limits/assignments.
func (s *Server) CreateInstanceLimitAssignment(w http.ResponseWriter, r *http.Request) {
	s.createRuleLimit(w, r, "", "")
}

// GetInstanceLimitAssignment implements GET /api/v1/limits/assignments/{id}.
func (s *Server) GetInstanceLimitAssignment(w http.ResponseWriter, r *http.Request, id string) {
	s.getRuleLimit(w, r, "", "", id)
}

// ReplaceInstanceLimitAssignment implements PUT /api/v1/limits/assignments/{id}.
func (s *Server) ReplaceInstanceLimitAssignment(w http.ResponseWriter, r *http.Request, id string, _ gen.ReplaceInstanceLimitAssignmentParams) {
	s.replaceRuleLimit(w, r, "", "", id)
}

// DeleteInstanceLimitAssignment implements DELETE /api/v1/limits/assignments/{id}.
func (s *Server) DeleteInstanceLimitAssignment(w http.ResponseWriter, r *http.Request, id string, _ gen.DeleteInstanceLimitAssignmentParams) {
	s.deleteRuleLimit(w, r, "", "", id)
}

// ListProjectLimitAssignments implements GET /api/v1/projects/{projectKey}/limits/assignments.
func (s *Server) ListProjectLimitAssignments(w http.ResponseWriter, r *http.Request, projectKey string, _ gen.ListProjectLimitAssignmentsParams) {
	s.listRulesLimit(w, r, projectKey, "")
}

// CreateProjectLimitAssignment implements POST /api/v1/projects/{projectKey}/limits/assignments.
func (s *Server) CreateProjectLimitAssignment(w http.ResponseWriter, r *http.Request, projectKey string) {
	s.createRuleLimit(w, r, projectKey, "")
}

// GetProjectLimitAssignment implements GET /api/v1/projects/{projectKey}/limits/assignments/{id}.
func (s *Server) GetProjectLimitAssignment(w http.ResponseWriter, r *http.Request, projectKey string, id string) {
	s.getRuleLimit(w, r, projectKey, "", id)
}

// ReplaceProjectLimitAssignment implements PUT /api/v1/projects/{projectKey}/limits/assignments/{id}.
func (s *Server) ReplaceProjectLimitAssignment(w http.ResponseWriter, r *http.Request, projectKey string, id string, _ gen.ReplaceProjectLimitAssignmentParams) {
	s.replaceRuleLimit(w, r, projectKey, "", id)
}

// DeleteProjectLimitAssignment implements DELETE /api/v1/projects/{projectKey}/limits/assignments/{id}.
func (s *Server) DeleteProjectLimitAssignment(w http.ResponseWriter, r *http.Request, projectKey string, id string, _ gen.DeleteProjectLimitAssignmentParams) {
	s.deleteRuleLimit(w, r, projectKey, "", id)
}

// ListRepositoryLimitAssignments implements GET …/repositories/{repoKey}/limits/assignments.
func (s *Server) ListRepositoryLimitAssignments(w http.ResponseWriter, r *http.Request, projectKey, repoKey string, _ gen.ListRepositoryLimitAssignmentsParams) {
	s.listRulesLimit(w, r, projectKey, repoKey)
}

// CreateRepositoryLimitAssignment implements POST …/repositories/{repoKey}/limits/assignments.
func (s *Server) CreateRepositoryLimitAssignment(w http.ResponseWriter, r *http.Request, projectKey, repoKey string) {
	s.createRuleLimit(w, r, projectKey, repoKey)
}

// GetRepositoryLimitAssignment implements GET …/repositories/{repoKey}/limits/assignments/{id}.
func (s *Server) GetRepositoryLimitAssignment(w http.ResponseWriter, r *http.Request, projectKey, repoKey string, id string) {
	s.getRuleLimit(w, r, projectKey, repoKey, id)
}

// ReplaceRepositoryLimitAssignment implements PUT …/repositories/{repoKey}/limits/assignments/{id}.
func (s *Server) ReplaceRepositoryLimitAssignment(w http.ResponseWriter, r *http.Request, projectKey, repoKey string, id string, _ gen.ReplaceRepositoryLimitAssignmentParams) {
	s.replaceRuleLimit(w, r, projectKey, repoKey, id)
}

// DeleteRepositoryLimitAssignment implements DELETE …/repositories/{repoKey}/limits/assignments/{id}.
func (s *Server) DeleteRepositoryLimitAssignment(w http.ResponseWriter, r *http.Request, projectKey, repoKey string, id string, _ gen.DeleteRepositoryLimitAssignmentParams) {
	s.deleteRuleLimit(w, r, projectKey, repoKey, id)
}

// ---- ceilings (R-LIM-015, R-LIM-032)

func ceilingRep(c limits.Ceiling) map[string]any {
	opt := func(v int64) any {
		if v <= 0 {
			return nil
		}
		return v
	}
	return map[string]any{"uncachedBytesPerHour": opt(c.UncachedBytesPerHour), "upstreamRequestsPerHour": opt(c.UpstreamRequestsPerHour)}
}

func (s *Server) ceilingsOf(ctx context.Context, projectKey, repoKey string) (LimitsServices, [2]limits.Ceiling, limits.Place, error) {
	l, snap, _, err := s.snapshot(ctx)
	var out [2]limits.Ceiling
	if err != nil {
		return l, out, limits.Place{}, err
	}
	place, repo, err := s.limitPlace(ctx, l, projectKey, repoKey)
	if err != nil {
		return l, out, place, err
	}
	if repo.Class != repository.Remote {
		return l, out, place, errs.E(errs.Invalid, errs.CodeNotApplicable, "Only a remote repository has ceilings.")
	}
	out[0] = limits.Ceiling{RepoID: place.RepoID, Class: limits.CeilingAnonymous}
	out[1] = limits.Ceiling{RepoID: place.RepoID, Class: limits.CeilingSignedIn}
	for _, c := range snap.Ceilings {
		if c.RepoID != place.RepoID {
			continue
		}
		if c.Class == limits.CeilingAnonymous {
			out[0] = c
		} else {
			out[1] = c
		}
	}
	return l, out, place, nil
}

func ceilingsVersion(c [2]limits.Ceiling) int64 { return max(1, c[0].Version+c[1].Version-1) }

func (s *Server) answerCeilings(w http.ResponseWriter, r *http.Request, projectKey, repoKey string) {
	ctx := r.Context()
	l, c, place, err := s.ceilingsOf(ctx, projectKey, repoKey)
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	v := ceilingsVersion(c)
	SetETag(w, v, "")
	updated := c[0].UpdatedAt
	by := c[0].UpdatedBy
	if c[1].UpdatedAt.After(updated) {
		updated, by = c[1].UpdatedAt, c[1].UpdatedBy
	}
	var at any
	if !updated.IsZero() {
		at = updated.UTC()
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"repository": place.Repo.String(), "anonymous": ceilingRep(c[0]), "signedIn": ceilingRep(c[1]), "version": v,
		"updatedAt": at, "updatedBy": s.principalName(ctx, l, by),
	})
}

// GetLimitCeilings implements GET …/repositories/{repoKey}/limits/ceilings.
func (s *Server) GetLimitCeilings(w http.ResponseWriter, r *http.Request, projectKey, repoKey string) {
	s.answerCeilings(w, r, projectKey, repoKey)
}

type ceilingWrite struct {
	UncachedBytesPerHour    *int64 `json:"uncachedBytesPerHour"`
	UpstreamRequestsPerHour *int64 `json:"upstreamRequestsPerHour"`
}

type ceilingsWrite struct {
	Anonymous  ceilingWrite `json:"anonymous"`
	SignedIn   ceilingWrite `json:"signedIn"`
	Repository any          `json:"repository"`
	Version    any          `json:"version"`
	UpdatedAt  any          `json:"updatedAt"`
	UpdatedBy  any          `json:"updatedBy"`
}

func (c ceilingWrite) ceiling(repo int64, class limits.CeilingClass, pointer string) (limits.Ceiling, error) {
	out := limits.Ceiling{RepoID: repo, Class: class}
	for _, v := range []struct {
		p   *int64
		dst *int64
		f   string
	}{{c.UncachedBytesPerHour, &out.UncachedBytesPerHour, "uncachedBytesPerHour"}, {c.UpstreamRequestsPerHour, &out.UpstreamRequestsPerHour, "upstreamRequestsPerHour"}} {
		if v.p == nil {
			continue
		}
		if *v.p < 1 {
			return out, errs.E(errs.Invalid, codeInvalidFigure, v.f+" must be at least 1, or null for unlimited.",
				errs.WithFields(errs.FieldError{Pointer: pointer + "/" + v.f, Code: codeInvalidFigure, Detail: "at least 1, or null"}))
		}
		*v.dst = *v.p
	}
	return out, nil
}

// ReplaceLimitCeilings implements PUT …/repositories/{repoKey}/limits/ceilings. A ceiling only narrows, so an Owner sets it
// uncapped (R-LIM-016).
func (s *Server) ReplaceLimitCeilings(w http.ResponseWriter, r *http.Request, projectKey, repoKey string, _ gen.ReplaceLimitCeilingsParams) {
	ctx := r.Context()
	l, cur, place, err := s.ceilingsOf(ctx, projectKey, repoKey)
	var pre Precondition
	if err == nil {
		pre, err = requireIfMatch(r)
	}
	var in ceilingsWrite
	if err == nil {
		err = DecodeJSON(w, r, &in, s.bodyLimit())
	}
	var next [2]limits.Ceiling
	if err == nil {
		next[0], err = in.Anonymous.ceiling(place.RepoID, limits.CeilingAnonymous, "/anonymous")
	}
	if err == nil {
		next[1], err = in.SignedIn.ceiling(place.RepoID, limits.CeilingSignedIn, "/signedIn")
	}
	if err == nil && !pre.Matches(ceilingsVersion(cur), "") {
		err = Failed(ceilingsVersion(cur))
	}
	if err == nil {
		err = l.Store.SetCeilings(ctx, limitsActor(r), place.Repo.String(), place.RepoID, cur, next)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	s.answerCeilings(w, r, projectKey, repoKey)
}

// ---- settings and the mode preview (R-LIM-026, R-LIM-033)

func (s *Server) settingsRep(ctx context.Context, l LimitsServices, st limits.Settings) map[string]any {
	return map[string]any{
		"mode": st.Mode, "blockSeconds": st.BlockSeconds, "version": st.Version, "updatedAt": st.UpdatedAt.UTC(),
		"updatedBy": s.principalName(ctx, l, st.UpdatedBy),
	}
}

// GetLimitSettings implements GET /api/v1/limits/settings.
func (s *Server) GetLimitSettings(w http.ResponseWriter, r *http.Request) {
	l, snap, _, err := s.snapshot(r.Context())
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	SetETag(w, snap.Settings.Version, "")
	writeJSON(w, r, http.StatusOK, s.settingsRep(r.Context(), l, snap.Settings))
}

// UpdateLimitSettings implements PATCH /api/v1/limits/settings (merge patch, R-API-013).
func (s *Server) UpdateLimitSettings(w http.ResponseWriter, r *http.Request, _ gen.UpdateLimitSettingsParams) {
	ctx := r.Context()
	l, snap, _, err := s.snapshot(ctx)
	var pre Precondition
	if err == nil {
		pre, err = requireIfMatch(r)
	}
	var patch []byte
	if err == nil {
		patch, err = ReadMergePatch(w, r, s.bodyLimit())
	}
	cur := snap.Settings
	type settingsDoc struct {
		Mode         limits.Mode `json:"mode"`
		BlockSeconds int         `json:"blockSeconds"`
	}
	next := settingsDoc{Mode: cur.Mode, BlockSeconds: cur.BlockSeconds}
	if err == nil {
		next, err = ApplyMergePatch(next, patch)
	}
	if err == nil && next.Mode != limits.ModeAudit && next.Mode != limits.ModeEnforce {
		err = errs.E(errs.Invalid, errs.CodeValidation, "mode must be audit or enforce.", errs.WithFields(errs.FieldError{Pointer: "/mode", Code: "enum", Detail: "audit or enforce"}))
	}
	if err == nil && (next.BlockSeconds < 60 || next.BlockSeconds > 86400) {
		err = errs.E(errs.Invalid, errs.CodeValidation, "blockSeconds must be 60 to 86400.", errs.WithFields(errs.FieldError{Pointer: "/blockSeconds", Code: "range", Detail: "60 to 86400"}))
	}
	if err == nil && !pre.Matches(cur.Version, "") {
		err = Failed(cur.Version)
	}
	after := limits.Settings{Mode: next.Mode, BlockSeconds: next.BlockSeconds}
	if err == nil {
		err = l.Store.UpdateSettings(ctx, limitsActor(r), cur, after, cur.Version)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	s.GetLimitSettings(w, r)
}

// GetLimitModePreview implements GET /api/v1/limits/mode-preview: what enforce would have refused or slowed in the last 24
// hours, from the would-limit events of the usage store (R-LIM-033).
func (s *Server) GetLimitModePreview(w http.ResponseWriter, r *http.Request) {
	l, err := s.limitsServices()
	var evs []usage.PreviewEvent
	var from, to time.Time
	if err == nil {
		evs, from, to, err = l.Usage.ModePreview(r.Context())
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	subjects := map[string]bool{}
	byFigure := map[string]map[string]bool{}
	figureEvents := map[string]int64{}
	byKind := map[string]map[string]bool{}
	var refused, slowed, blocked int64
	for _, e := range evs {
		key := string(e.Kind) + ":" + e.Ref
		subjects[key] = true
		if byFigure[e.Figure] == nil {
			byFigure[e.Figure] = map[string]bool{}
		}
		byFigure[e.Figure][key] = true
		figureEvents[e.Figure] += e.Events
		if byKind[e.Kind.APIName()] == nil {
			byKind[e.Kind.APIName()] = map[string]bool{}
		}
		byKind[e.Kind.APIName()][key] = true
		switch e.Figure {
		case "cachedBytesPerSecond", "uncachedBytesPerSecond":
			slowed += e.Events
		case "refusedPerHour":
			blocked++
		default:
			refused += e.Events
		}
	}
	figures := make([]map[string]any, 0, len(byFigure))
	for f, ss := range byFigure {
		figures = append(figures, map[string]any{"figure": f, "subjects": len(ss), "events": figureEvents[f]})
	}
	sort.Slice(figures, func(i, j int) bool { return figures[i]["figure"].(string) < figures[j]["figure"].(string) })
	kinds := make([]map[string]any, 0, len(byKind))
	for k, ss := range byKind {
		kinds = append(kinds, map[string]any{"kind": k, "subjects": len(ss)})
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i]["kind"].(string) < kinds[j]["kind"].(string) })
	writeJSON(w, r, http.StatusOK, map[string]any{
		"period": map[string]any{"from": from.UTC(), "to": to.UTC()}, "subjects": len(subjects), "refusedRequests": refused,
		"slowedDownloads": slowed, "blockedSubjects": blocked, "byFigure": figures, "byKind": kinds,
	})
}

// ---- a project's maximum (R-LIM-016, R-LIM-033)

func findMaximum(snap limits.Snapshot, projectKey string) (limits.Maximum, bool) {
	for _, m := range snap.Maxima {
		if m.ProjectKey == projectKey {
			return m, true
		}
	}
	return limits.Maximum{}, false
}

func (s *Server) answerMaximum(w http.ResponseWriter, r *http.Request, projectKey string, status int) {
	ctx := r.Context()
	l, snap, x, err := s.snapshot(ctx)
	m, ok := findMaximum(snap, projectKey)
	if err == nil && !ok {
		err = errs.E(errs.NotFound, errs.CodeNotFound, "The project has no maximum.")
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	eff, _ := x.FiguresOfProfile(m.Profile, m.Overrides)
	SetETag(w, m.Version, "")
	writeJSON(w, r, status, map[string]any{
		"project": projectKey, "profile": m.Profile, "overrides": m.Overrides, "effective": eff,
		"version": m.Version, "updatedAt": m.UpdatedAt.UTC(), "updatedBy": s.principalName(ctx, l, m.UpdatedBy),
	})
}

// GetLimitProjectMaximum implements GET /api/v1/projects/{projectKey}/limits/maximum.
func (s *Server) GetLimitProjectMaximum(w http.ResponseWriter, r *http.Request, projectKey string) {
	s.answerMaximum(w, r, projectKey, http.StatusOK)
}

type maximumWrite struct {
	Profile   string           `json:"profile"`
	Overrides limits.Overrides `json:"overrides"`
	Project   any              `json:"project"`
	Effective any              `json:"effective"`
	Version   any              `json:"version"`
	UpdatedAt any              `json:"updatedAt"`
	UpdatedBy any              `json:"updatedBy"`
}

// SetLimitProjectMaximum implements PUT /api/v1/projects/{projectKey}/limits/maximum. The first maximum of a project is created
// without If-Match, since nothing exists whose ETag a client could have loaded; a replace requires it as every PUT of R-API-012
// does, and an If-Match sent for a maximum that does not exist fails, as RFC 9110 section 13.1.1 has it (Q-API-354).
func (s *Server) SetLimitProjectMaximum(w http.ResponseWriter, r *http.Request, projectKey string, _ gen.SetLimitProjectMaximumParams) {
	ctx := r.Context()
	l, snap, _, err := s.snapshot(ctx)
	var place limits.Place
	if err == nil {
		place, _, err = s.limitPlace(ctx, l, projectKey, "")
	}
	cur, exists := findMaximum(snap, projectKey)
	var pre Precondition
	if err == nil {
		pre, err = ParseIfMatch(r)
	}
	switch {
	case err != nil:
	case exists:
		err = pre.Require()
	case pre.Present:
		err = errs.E(errs.PreconditionFailed, errs.CodePreconditionFailed,
			"The project has no maximum that If-Match could name: send none to set a first one.")
	}
	var in maximumWrite
	if err == nil {
		err = DecodeJSON(w, r, &in, s.bodyLimit())
	}
	if _, ok := findProfile(snap, in.Profile); err == nil && !ok {
		err = errs.E(errs.NotFound, errs.CodeNotFound, "No profile has this key.")
	}
	version := int64(0)
	if exists {
		version = cur.Version
		if !pre.Matches(cur.Version, "") && err == nil {
			err = Failed(cur.Version)
		}
	}
	if err == nil {
		err = l.Store.SetMaximum(ctx, limitsActor(r), limits.Maximum{ProjectID: place.ProjectID, ProjectKey: projectKey, Profile: in.Profile, Overrides: in.Overrides}, version)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	status := http.StatusOK
	if !exists {
		status = http.StatusCreated
	}
	s.answerMaximum(w, r, projectKey, status)
}

// DeleteLimitProjectMaximum implements DELETE /api/v1/projects/{projectKey}/limits/maximum.
func (s *Server) DeleteLimitProjectMaximum(w http.ResponseWriter, r *http.Request, projectKey string, _ gen.DeleteLimitProjectMaximumParams) {
	ctx := r.Context()
	l, snap, _, err := s.snapshot(ctx)
	cur, ok := findMaximum(snap, projectKey)
	if err == nil && !ok {
		err = errs.E(errs.NotFound, errs.CodeNotFound, "The project has no maximum.")
	}
	var pre Precondition
	if err == nil {
		pre, err = ParseIfMatch(r)
	}
	if err == nil && !pre.Matches(cur.Version, "") {
		err = Failed(cur.Version)
	}
	if err == nil {
		err = l.Store.DeleteMaximum(ctx, limitsActor(r), cur, cur.Version)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
