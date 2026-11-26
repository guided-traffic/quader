package v1

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/api/edge"
	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/mirror/discover"
	"github.com/guided-traffic/quader/internal/qname"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/catalogue"
	"github.com/guided-traffic/quader/internal/remotecuration/queue"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/vuln"
)

// The upstream catalogue's operations (remote-curation.md R-CUR-007 to R-CUR-009; work package R3 of docs/developer/remote-curation.md):
// the capabilities, a search, a level, a package by its exact name, and the vulnerability verdicts of versions not stored. Every
// operation needs read on the repository (R-CUR-005); the engine (internal/remotecuration/catalogue) reads the upstream through an
// upstream that stores nothing, and this file adds the repository's own state of every row at answer time (Quader's state is never
// cached): delivery, stored, queued, request.

// UpstreamCatalogueServices are the services of the catalogue's operations; a nil Engine answers them with 503.
type UpstreamCatalogueServices struct {
	Engine *catalogue.Engine
	// Decider decides delivery; nil delivers everything.
	Decider rc.Decider
	// Queue answers the rows that wait or run and counts them; nil has none.
	Queue *queue.Service
	// Requests answers the open request entries; nil has none (remotecuration.NoRequests).
	Requests rc.Requests
	// OpenRequests counts a repository's open request entries, at most a bound it names; nil counts none.
	OpenRequests func(ctx context.Context, repoID int64) (int, error)
	// Stored reads the versions the repository holds.
	Stored StoredVersions
	// Verdicts are the vulnerability verdicts of versions not stored; the zero value covers no format.
	Verdicts vuln.MissVerdicts
}

// StoredVersions reads which versions of a page's packages a repository holds (catalogue.Stored).
type StoredVersions interface {
	Versions(ctx context.Context, repoID int64, names []string) (map[string][]string, error)
}

// withheldNote is the sentence of the hits a reader does not see in the mode onlyAllowed (Q-CUR-14).
const withheldNote = " hits are not allowed in this repository; look a package up by its exact name to ask for it."

// catalogueCall is what every operation starts with: the decisions, read on the repository, the remote repository, the engine's target.
type catalogueCall struct {
	ctx     context.Context
	acc     *edge.Access
	repo    repository.Repository
	t       catalogue.Target
	manager bool
}

func (s *Server) catalogueSetup(r *http.Request, projectKey, repoKey string) (catalogueCall, error) {
	if s.o.UpstreamCatalogue.Engine == nil {
		return catalogueCall{}, unavailable("The upstream catalogue")
	}
	key := qname.Repo{Project: projectKey, Key: repoKey}
	ctx, acc, err := s.downloadAccess(r.Context(), key)
	if err != nil {
		return catalogueCall{}, err
	}
	if err := downloadReader(ctx, acc, key); err != nil {
		return catalogueCall{}, err
	}
	svc, err := s.repositoryService()
	if err != nil {
		return catalogueCall{}, err
	}
	st, err := svc.Get(ctx, key)
	if err != nil {
		return catalogueCall{}, err
	}
	repo := st.Repository
	rs, ok := repo.Remote()
	if !ok || repo.Class != repository.Remote {
		return catalogueCall{}, errs.E(errs.Invalid, errs.CodeNotApplicable,
			"Only a remote repository has an upstream catalogue; "+strconv.Quote(key.String())+" is a "+string(repo.Class)+" repository.")
	}
	id, err := parseRepositoryID(repo.ID)
	if err != nil {
		return catalogueCall{}, err
	}
	return catalogueCall{
		ctx: ctx, acc: acc, repo: repo, manager: downloadManager(ctx, acc, key),
		t: catalogue.Target{RepoID: id, Repo: key.String(), Format: repo.Format, Upstream: rs.URL, Person: callerID(ctx)},
	}, nil
}

func (s *Server) catalogueDecider() rc.Decider {
	if d := s.o.UpstreamCatalogue.Decider; d != nil {
		return d
	}
	return rc.DeliverAll{}
}

// GetUpstreamCatalogue implements GET …/upstream (R-CUR-009).
func (s *Server) GetUpstreamCatalogue(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey string) {
	c, err := s.catalogueSetup(r, projectKey, repoKey)
	var caps discover.CatalogueCaps
	if err == nil {
		caps, err = s.o.UpstreamCatalogue.Engine.Caps(c.ctx, c.t)
	}
	var counts queue.Counts
	if err == nil && s.o.UpstreamCatalogue.Queue != nil {
		counts, err = s.o.UpstreamCatalogue.Queue.Reader().StateCounts(c.ctx, c.t.RepoID, "")
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.getUpstreamCatalogue"))
		return
	}
	var open any
	if c.manager {
		n, err := s.openRequests(c)
		if err != nil {
			httpserver.Problem(w, r, errs.Wrap(err, "api.getUpstreamCatalogue"))
			return
		}
		open = n
	}
	set := c.repo.Delivery()
	writeJSON(w, r, http.StatusOK, map[string]any{
		"search": caps.Search, "searchWindow": nullInt(caps.SearchWindow), "searchHint": nullString(string(caps.SearchHint)),
		"levels": listOf(caps.Levels), "levelsReason": nullString(caps.LevelsReason),
		"sizes": caps.Sizes, "hosts": listOf(caps.Hosts), "mode": set.Mode(), "rules": set.Len(), "openRequests": open,
		"queue":       map[string]int64{"waiting": counts.Waiting, "running": counts.Running, "failed": counts.Failed, "done": counts.Done},
		"maySetRules": c.manager,
	})
}

// openRequests counts the repository's open request entries (R-CUR-015).
func (s *Server) openRequests(c catalogueCall) (int, error) {
	if s.o.UpstreamCatalogue.OpenRequests == nil {
		return 0, nil
	}
	return s.o.UpstreamCatalogue.OpenRequests(c.ctx, c.t.RepoID)
}

// SearchUpstreamCatalogue implements GET …/upstream/search (R-CUR-007, R-CUR-009).
func (s *Server) SearchUpstreamCatalogue(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey string, p gen.SearchUpstreamCatalogueParams) {
	c, err := s.catalogueSetup(r, projectKey, repoKey)
	q := discover.SearchQuery{Text: p.Q, Limit: intOr(p.Limit, 0)}
	if p.NamesOnly != nil {
		q.NamesOnly = *p.NamesOnly
	}
	if err == nil && p.Level != nil {
		q.Level, err = parseLevelParam(*p.Level)
	}
	if err == nil {
		q.Cursor, err = parseCatalogueCursor(p.Cursor)
	}
	var page discover.SearchPage
	if err == nil {
		page, err = s.o.UpstreamCatalogue.Engine.Search(c.ctx, c.t, q)
	}
	var hits []map[string]any
	var withheld int
	if err == nil {
		hits, withheld, err = s.hitRows(c, page.Hits)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.searchUpstreamCatalogue"))
		return
	}
	out := map[string]any{
		"hits": hits, "total": nullTotal(page.Total), "nextCursor": catalogueCursor(page.Cursor, page.Done), "done": page.Done,
		"truncated": page.Truncated, "note": nullString(page.Note), "withheld": nil, "withheldNote": nil,
	}
	withholding(out, c, withheld)
	writeJSON(w, r, http.StatusOK, out)
}

// ListUpstreamLevel implements GET …/upstream/levels (R-CUR-007, R-CUR-009).
func (s *Server) ListUpstreamLevel(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey string, p gen.ListUpstreamLevelParams) {
	c, err := s.catalogueSetup(r, projectKey, repoKey)
	rq := discover.LevelRequest{Limit: intOr(p.Limit, 0)}
	if err == nil && p.Level != nil {
		rq.Level, err = parseLevelParam(*p.Level)
	}
	if err == nil {
		rq.Cursor, err = parseCatalogueCursor(p.Cursor)
	}
	var page discover.LevelPage
	if err == nil {
		page, err = s.o.UpstreamCatalogue.Engine.Children(c.ctx, c.t, rq)
	}
	var pkgs []map[string]any
	var withheld int
	if err == nil {
		pkgs, withheld, err = s.hitRows(c, page.Packages)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.listUpstreamLevel"))
		return
	}
	out := map[string]any{
		"levels": listOf(page.Levels), "packages": pkgs, "total": nullTotal(page.Total), "nextCursor": catalogueCursor(page.Cursor, page.Done),
		"done": page.Done, "withheld": nil, "withheldNote": nil, "unknown": unknownLevel(c, page.Unknown),
	}
	withholding(out, c, withheld)
	writeJSON(w, r, http.StatusOK, out)
}

// unknownLevel is the answer's `unknown` (Q-CUR-165): nil for a level the upstream has or does not tell apart, else why it lists
// nothing with the name a person knows the upstream by.
func unknownLevel(c catalogueCall, u *discover.UnknownLevel) any {
	if u == nil {
		return nil
	}
	return map[string]any{
		"upstream": upstreamName(c.repo.Format, c.t.Upstream), "rule": nullString(string(u.Rule)),
		"forbidden": nullString(u.Forbidden), "suggestion": nullString(u.Suggestion),
	}
}

// upstreamName is the display name of the format's preset whose URL the repository has ("Docker Hub"), else the host of its URL.
func upstreamName(format, upstream string) string {
	if e, ok := catalog.Default().Lookup(format); ok {
		if p, ok := e.PresetFor(upstream); ok {
			return p.DisplayName
		}
	}
	if u, err := url.Parse(upstream); err == nil && u.Host != "" {
		return u.Host
	}
	return upstream
}

// withholding adds the count and the sentence of the hits a reader does not see (Q-CUR-14).
func withholding(out map[string]any, c catalogueCall, withheld int) {
	if c.manager || c.repo.Delivery().Mode() != rc.OnlyAllowed {
		return
	}
	out["withheld"] = withheld
	if withheld > 0 {
		out["withheldNote"] = strconv.Itoa(withheld) + withheldNote
	}
}

// GetUpstreamPackage implements GET …/upstream/package (R-CUR-007, R-CUR-009).
func (s *Server) GetUpstreamPackage(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey string, p gen.GetUpstreamPackageParams) {
	c, err := s.catalogueSetup(r, projectKey, repoKey)
	rq := discover.VersionsRequest{Limit: intOr(p.Limit, 0)}
	if p.Filter != nil {
		rq.Filter = *p.Filter
	}
	if err == nil {
		rq.Cursor, err = parseCatalogueCursor(p.Cursor)
	}
	var info discover.PackageInfo
	if err == nil {
		info, err = s.o.UpstreamCatalogue.Engine.Package(c.ctx, c.t, p.Name, rq)
	}
	var out map[string]any
	if err == nil {
		out, err = s.packageRow(c, info)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.getUpstreamPackage"))
		return
	}
	writeJSON(w, r, http.StatusOK, out)
}

// EvaluateUpstreamVulnerabilities implements POST …/upstream/vulnerabilities (R-CUR-009).
func (s *Server) EvaluateUpstreamVulnerabilities(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey string) {
	c, err := s.catalogueSetup(r, projectKey, repoKey)
	var in gen.UpstreamVulnerabilitiesRequest
	if err == nil {
		var body []byte
		if body, err = ReadBody(w, r, MediaJSON, s.bodyLimit()); err == nil {
			err = StrictUnmarshal(body, &in)
		}
	}
	if err == nil && (len(in.Versions) == 0 || len(in.Versions) > 50 || in.Name == "") {
		err = errs.E(errs.Invalid, errs.CodeValidation, "The body names a package and 1 to 50 versions.")
	}
	var out map[string]any
	if err == nil {
		out, err = s.verdicts(c, in.Name, in.Versions)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.evaluateUpstreamVulnerabilities"))
		return
	}
	writeJSON(w, r, http.StatusOK, out)
}

func (s *Server) verdicts(c catalogueCall, name string, versions []string) (map[string]any, error) {
	name = canonicalName(c.repo.Format, name)
	canon := make([]string, 0, len(versions))
	for _, v := range versions {
		cv, ok := catalog.Default().CanonicalVersion(c.repo.Format, v)
		if !ok {
			return nil, errs.E(errs.Invalid, errs.CodeInvalidVersion, "The version "+strconv.Quote(v)+" is not one the format reads.")
		}
		canon = append(canon, cv)
	}
	stored, err := s.storedOf(c, []string{name})
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, v := range canon {
		if !slices.Contains(stored[name], v) {
			missing = append(missing, v)
		}
	}
	vs := s.o.UpstreamCatalogue.Verdicts
	got, covered, err := vs.Verdicts(c.ctx, qname.Repo{Project: c.repo.Name.Project, Key: c.repo.Name.Key}, c.repo.Format, name, missing)
	if err != nil {
		return nil, err
	}
	byVersion := map[string]vuln.Block{}
	for _, g := range got {
		byVersion[g.Version] = g.Block
	}
	rows := make([]map[string]any, 0, len(canon))
	for _, v := range canon {
		row := map[string]any{"version": v, "stored": false, "verdict": "none", "reason": nil, "advisoryId": nil, "enforced": false}
		switch b, ok := byVersion[v]; {
		case slices.Contains(stored[name], v):
			row["stored"], row["verdict"] = true, "stored"
		case !covered:
			row["verdict"] = "notCovered"
		case ok && b.Active():
			row["verdict"], row["reason"], row["advisoryId"], row["enforced"] = "blocked", string(b.Reason), nullString(b.AdvisoryID), b.Enforced
		}
		rows = append(rows, row)
	}
	return map[string]any{"notCovered": !covered, "versions": rows}, nil
}

// hitRows adds the repository's state to the hits of a page; in the mode onlyAllowed a caller who may not set rules is answered only
// the delivered packages, and the count of the others.
func (s *Server) hitRows(c catalogueCall, hits []discover.Hit) ([]map[string]any, int, error) {
	names := make([]string, 0, len(hits))
	for _, h := range hits {
		names = append(names, h.Name)
	}
	stored, err := s.storedOf(c, names)
	if err != nil {
		return nil, 0, err
	}
	opened, err := s.requestsOf(c, names)
	if err != nil {
		return nil, 0, err
	}
	restrict := !c.manager && c.repo.Delivery().Mode() == rc.OnlyAllowed
	repoID := strconv.FormatInt(c.t.RepoID, 10)
	out := make([]map[string]any, 0, len(hits))
	withheld := 0
	for _, h := range hits {
		d := s.catalogueDecider().Package(c.ctx, repoID, h.Name)
		if restrict && !d.Delivered {
			withheld++
			continue
		}
		st := "none"
		if len(stored[h.Name]) > 0 {
			st = "partial"
		}
		row := map[string]any{
			"name": h.Name, "description": h.Description, "latest": nullString(h.Latest), "updated": nullTimeValue(h.Updated),
			"flags": listOf(h.Flags), "delivery": toDeliveryDecision(d, c.repo.Delivery().Mode()), "stored": st,
			"request": requestRow(opened[h.Name]),
		}
		popularity(row, h.Popularity)
		switchFacts(c, row, h.Name)
		out = append(out, row)
	}
	return out, withheld, nil
}

// popularity adds what the upstream tells beside a package, each member only where it tells it (R-ANS-047, Q-ANS-129): its tags, its
// downloads at the upstream (named apart from Quader's own count) and its signed state.
func popularity(row map[string]any, p discover.Popularity) {
	if len(p.Keywords) > 0 {
		row["keywords"] = p.Keywords
	}
	if p.Downloads != nil {
		row["downloads"] = *p.Downloads
	}
	if p.Signed != nil {
		row["signed"] = *p.Signed
	}
}

// switchFacts adds what the package's switch shows and asks to the row of a package, for whoever may set rules (R-CUR-024): the
// package level's decision, what it inherits, and the counts of its version rules, from the replica's rule set.
func switchFacts(c catalogueCall, row map[string]any, name string) {
	if !c.manager {
		return
	}
	set := c.repo.Delivery()
	mode := set.Mode()
	allow, block := set.VersionRuleCounts(name)
	row["level"] = toDeliveryDecision(set.Level(name), mode)
	row["inherited"] = toDeliveryDecision(set.LevelInherited(name), mode)
	row["versionRules"] = map[string]int{"allow": allow, "block": block}
}

// packageRow is a package with the repository's state of it and of every version of the page.
func (s *Server) packageRow(c catalogueCall, info discover.PackageInfo) (map[string]any, error) {
	name := info.Name
	stored, err := s.storedOf(c, []string{name})
	if err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(info.Versions))
	for _, v := range info.Versions {
		versions = append(versions, v.Version)
	}
	repoID := strconv.FormatInt(c.t.RepoID, 10)
	queued := map[string]rc.QueuedDownload{}
	if q := s.o.UpstreamCatalogue.Queue; q != nil {
		if queued, err = q.Active(c.ctx, repoID, name, versions); err != nil {
			return nil, err
		}
	}
	requests := s.o.UpstreamCatalogue.Requests
	if requests == nil {
		requests = rc.NoRequests{}
	}
	open, err := requests.Open(c.ctx, repoID, name, versions)
	if err != nil {
		return nil, err
	}
	mode := c.repo.Delivery().Mode()
	held := map[string]bool{}
	for _, v := range stored[name] {
		if cv, ok := catalog.Default().CanonicalVersion(c.repo.Format, v); ok {
			held[cv] = true
		}
		held[v] = true
	}
	rows := make([]map[string]any, 0, len(info.Versions))
	for _, v := range info.Versions {
		st := "none"
		if held[v.Version] {
			st = "whole"
		}
		row := map[string]any{
			"version": v.Version, "published": nullTimeValue(v.Published), "size": nullInt64(v.Size), "unpackedSize": nullInt64(v.UnpackedSize),
			"flags": listOf(v.Flags), "platforms": listOf(v.Platforms), "digest": nullString(v.Digest),
			"delivery": toDeliveryDecision(s.catalogueDecider().Decide(c.ctx, repoID, name, v.Version), mode), "stored": st,
			"queued": queuedRow(queued, v.Version), "request": requestRow(openOf(open, v.Version)),
		}
		if c.manager {
			row["inherited"] = toDeliveryDecision(c.repo.Delivery().Inherited(name, v.Version), mode)
		}
		rows = append(rows, row)
	}
	pst := "none"
	if len(stored[name]) > 0 {
		pst = "partial"
	}
	tags := info.Tags
	if tags == nil {
		tags = map[string]string{}
	}
	out := map[string]any{
		"name": name, "description": info.Description, "tags": tags,
		"delivery": toDeliveryDecision(s.catalogueDecider().Package(c.ctx, repoID, name), mode), "stored": pst,
		"request": requestRow(openOf(open, "")), "versions": rows, "total": nullTotal(info.Total),
		"nextCursor": catalogueCursor(info.Cursor, info.Done), "done": info.Done,
		// The newest version's facts and whether the screen asks for the README (R-CUR-028): the facts are every reader's, the README
		// the wiki's (R-AUTHZ-034).
		"about":  aboutRow(info.About),
		"readme": s.o.UpstreamCatalogue.Engine.ServesReadme(c.repo.Format) && mayReadUpstreamWiki(c, name),
	}
	popularity(out, info.Popularity)
	switchFacts(c, out, name)
	return out, nil
}

func (s *Server) storedOf(c catalogueCall, names []string) (map[string][]string, error) {
	if s.o.UpstreamCatalogue.Stored == nil {
		return map[string][]string{}, nil
	}
	return s.o.UpstreamCatalogue.Stored.Versions(c.ctx, c.t.RepoID, names)
}

// requestsOf reads the open package-level request entry of each name.
func (s *Server) requestsOf(c catalogueCall, names []string) (map[string]*rc.OpenRequest, error) {
	out := map[string]*rc.OpenRequest{}
	requests := s.o.UpstreamCatalogue.Requests
	if requests == nil {
		return out, nil
	}
	repoID := strconv.FormatInt(c.t.RepoID, 10)
	for _, n := range names {
		open, err := requests.Open(c.ctx, repoID, n, nil)
		if err != nil {
			return nil, err
		}
		out[n] = openOf(open, "")
	}
	return out, nil
}

func openOf(m map[string]rc.OpenRequest, version string) *rc.OpenRequest {
	if r, ok := m[version]; ok {
		return &r
	}
	return nil
}

func requestRow(r *rc.OpenRequest) any {
	if r == nil {
		return nil
	}
	return map[string]any{"id": r.ID, "askers": r.Askers, "refusals": r.Refusals}
}

func queuedRow(m map[string]rc.QueuedDownload, version string) any {
	q, ok := m[version]
	if !ok {
		return nil
	}
	var next any
	if !q.NextAttemptAt.IsZero() {
		next = q.NextAttemptAt.UTC()
	}
	return map[string]any{"id": q.ID, "state": q.State, "origin": q.Origin, "attempts": q.Attempts, "maxAttempts": q.MaxAttempts, "nextAttemptAt": next}
}

// canonicalName folds a name by the format's rule; a name the rule refuses stays as given.
func canonicalName(format, name string) string {
	if n, ok := catalog.Default().NormalizeName(format, name); ok {
		return n
	}
	return name
}

// parseLevelParam reads a level as the API spells it.
func parseLevelParam(s string) (discover.LevelRef, error) {
	l, err := discover.ParseLevel(s)
	if err != nil {
		return discover.LevelRef{}, errs.E(errs.Invalid, errs.CodeValidation, "A level is its path joined by /, each part URL-encoded and not empty.",
			errs.WithFields(errs.FieldError{Pointer: "/level", Code: errs.CodeValidation, Detail: "a level path"}))
	}
	return l, nil
}

// parseCatalogueCursor reads a nextCursor: the format's cursor, base64url-encoded.
func parseCatalogueCursor(p *string) (discover.Cursor, error) {
	if p == nil || *p == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(*p)
	if err != nil || len(raw) > discover.MaxCursorBytes {
		return nil, errs.E(errs.Invalid, errs.CodeValidation, "The cursor is not one this call answered.",
			errs.WithFields(errs.FieldError{Pointer: "/cursor", Code: errs.CodeValidation, Detail: "a nextCursor"}))
	}
	return raw, nil
}

// catalogueCursor is the nextCursor of a page: null on the last.
func catalogueCursor(c discover.Cursor, done bool) any {
	if done || len(c) == 0 {
		return nil
	}
	return base64.RawURLEncoding.EncodeToString(c)
}

func intOr(p *int, d int) int {
	if p == nil {
		return d
	}
	return *p
}

func nullInt64(n int64) any {
	if n <= 0 {
		return nil
	}
	return n
}

func nullTotal(n int) any {
	if n < 0 {
		return nil
	}
	return n
}

func nullTimeValue(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

// listOf is a list member that is never null.
func listOf[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
