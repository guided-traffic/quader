package v1

import (
	"context"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/qname"

	"github.com/guided-traffic/quader/internal/api/edge"
	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/proxy/invalidation"
	"github.com/guided-traffic/quader/internal/repository"
)

// The cache operations of remote and virtual repositories: the invalidation of
// cached metadata and misses (R-OPS-021, R-REPO-045, R-API-024) and the list of the
// stored entries for diagnosis (R-REPO-032). The invalidate operation declares the
// repository action manage, which the shared step decides before the handler runs.
// The list is open to a holder of repositories:read-config or of manage, a
// disjunction the declaration forms cannot express, so it declares only that the
// caller is authenticated and decides in the handler (Q-OPS-155).

// CacheInvalidations is what the cache operations need of the invalidation package;
// *invalidation.Service implements it.
type CacheInvalidations interface {
	Invalidate(ctx context.Context, c repository.Caller, repo repository.Repository, prefix, scope string) (int64, error)
	Entries(ctx context.Context, repoID int64, f invalidation.EntryFilter) ([]invalidation.Entry, error)
}

// CacheRepositories looks a repository up by name; *repository.Service implements
// it.
type CacheRepositories interface {
	Get(ctx context.Context, name qname.Repo) (repository.Stored, error)
}

// CacheServices are the services of the cache operations. A nil member disables
// the operations with 503.
type CacheServices struct {
	Invalidations CacheInvalidations
	Repositories  CacheRepositories
	// Authz is the decision point of the entry list; it decides per request.
	Authz authz.Decider
}

func (s *Server) cacheServices() (CacheServices, error) {
	c := s.o.Cache
	if c.Invalidations == nil || c.Repositories == nil {
		return c, unavailable("The cache service")
	}
	return c, nil
}

// cacheInvalidateRequest is the body of the invalidate operation. The members are
// pointers so that an absent one takes its default and a null is told from it.
type cacheInvalidateRequest struct {
	Path  *string `json:"path"`
	Scope *string `json:"scope"`
}

// InvalidateRepositoryCache implements POST /api/v1/repositories/{repoKey}/cache/invalidate
// (R-OPS-021). manage on the repository, or the administrator role, was decided by
// the shared step.
func (s *Server) InvalidateRepositoryCache(w http.ResponseWriter, r *http.Request, projectKey, repoKeyText string) {
	repoKey := qname.Repo{Project: projectKey, Key: repoKeyText}
	ctx := r.Context()
	svc, err := s.cacheServices()
	var in cacheInvalidateRequest
	if err == nil {
		err = DecodeJSON(w, r, &in, s.bodyLimit())
	}
	var st repository.Stored
	if err == nil {
		st, err = svc.Repositories.Get(ctx, repoKey)
	}
	var n int64
	if err == nil {
		var path, scope string
		if in.Path != nil {
			path = *in.Path
		}
		if in.Scope != nil {
			scope = *in.Scope
		}
		n, err = svc.Invalidations.Invalidate(ctx, repositoryCaller(r), st.Repository, path, scope)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]int64{"invalidated": n})
}

// cacheEntryRep is the representation of a cache entry.
type cacheEntryRep struct {
	Path           string     `json:"path"`
	Variant        string     `json:"variant"`
	Kind           string     `json:"kind"`
	UpstreamURL    string     `json:"upstreamUrl"`
	FetchedAt      time.Time  `json:"fetchedAt"`
	ValidatedAt    time.Time  `json:"validatedAt"`
	ExpiresAt      *time.Time `json:"expiresAt"`
	UpstreamGoneAt *time.Time `json:"upstreamGoneAt"`
	LastUsedAt     time.Time  `json:"lastUsedAt"`
	// Malware is the member of an artifact's verdict (R-MAL-022); absent for a metadata entry and while the scanning is off.
	Malware *edge.MalwareView `json:"malware,omitempty"`
}

func toCacheEntryRep(e invalidation.Entry) cacheEntryRep {
	return cacheEntryRep{
		Path: e.Path, Variant: e.Variant, Kind: e.Kind, UpstreamURL: e.UpstreamURL,
		FetchedAt: e.FetchedAt, ValidatedAt: e.ValidatedAt, ExpiresAt: e.ExpiresAt,
		UpstreamGoneAt: e.UpstreamGoneAt, LastUsedAt: e.LastUsedAt,
	}
}

func cacheEntriesSpec() ListSpec { return ListSpec{Filters: []string{"pathPrefix", "kind"}} }

// requireCacheReader decides who may list the entries of a repository: a holder of
// repositories:read-config, whom the exposure ceiling of the request's endpoint
// binds like everyone, or a caller with manage on the repository. A caller with
// neither is answered by the decision on manage: 403, or 404 where the repository
// is hidden from them (R-AUTHZ-016).
func (s *Server) requireCacheReader(ctx context.Context, svc CacheServices, key qname.Repo) error {
	ctx, acc, err := requestHolder(ctx, svc.Authz, key)
	if err != nil {
		return err
	}
	if acc == nil {
		return errNoDecisionPoint()
	}
	cfg, err := s.holds(ctx, capabilityReadConfig)
	if err != nil {
		return err
	}
	if cfg {
		if len(acc.Exposed([]qname.Repo{key})) == 0 {
			return authz.NotFound()
		}
		return nil
	}
	return acc.Decide(ctx, authz.Coordinate{Repository: key, Kind: authz.KindWrite, Actions: []authz.Action{authz.Manage}}).Err()
}

// ListCacheEntries implements GET /api/v1/repositories/{repoKey}/cache/entries
// (R-REPO-032).
func (s *Server) ListCacheEntries(w http.ResponseWriter, r *http.Request, projectKey, repoKeyText string, _ gen.ListCacheEntriesParams) {
	repoKey := qname.Repo{Project: projectKey, Key: repoKeyText}
	ctx := r.Context()
	svc, err := s.cacheServices()
	var lq ListQuery
	if err == nil {
		lq, err = ParseList(r, cacheEntriesSpec(), s.o.API.Config)
	}
	var f invalidation.EntryFilter
	if err == nil {
		f, err = cacheEntryFilter(lq)
	}
	if err == nil {
		err = s.requireCacheReader(ctx, svc, repoKey)
	}
	var st repository.Stored
	if err == nil {
		st, err = svc.Repositories.Get(ctx, repoKey)
	}
	if err == nil && st.Repository.Class != repository.Remote {
		err = errs.E(errs.Invalid, errs.CodeNotApplicable, "Only a remote repository has cache entries; "+strconv.Quote(repoKey.String())+" is a "+string(st.Repository.Class)+" repository.")
	}
	var id int64
	if err == nil {
		id, err = strconv.ParseInt(st.Repository.ID, 10, 64)
	}
	var rows []invalidation.Entry
	if err == nil {
		rows, err = svc.Invalidations.Entries(ctx, id, f)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.listCacheEntries"))
		return
	}
	page := NewPage(lq, rows, func(e invalidation.Entry) []string { return []string{e.Path, e.Variant} })
	out := MapPage(page, toCacheEntryRep)
	if err := s.addCacheMalware(ctx, repoKey, page.Items, out.Items); err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.listCacheEntries"))
		return
	}
	WritePage(w, r, out)
}

// addCacheMalware adds the member malware to the artifact entries of a page (R-MAL-022): the cache view is gated as the remote
// is (R-MAL-018), so an entry shows the verdict its download meets.
func (s *Server) addCacheMalware(ctx context.Context, key qname.Repo, rows []invalidation.Entry, out []cacheEntryRep) error {
	var hexes []string
	for _, e := range rows {
		if e.Kind == invalidation.EntryKindArtifact {
			hexes = append(hexes, hex.EncodeToString(e.SHA256[:]))
		}
	}
	verdicts, err := s.malwareVerdicts(ctx, hexes)
	if err != nil || verdicts == nil {
		return err
	}
	mc, err := s.malwareContexts(ctx)
	if err != nil {
		return err
	}
	for i, e := range rows {
		if e.Kind == invalidation.EntryKindArtifact {
			if out[i].Malware, err = mc.describe(key, verdicts[blob.Digest(e.SHA256)], false); err != nil {
				return err
			}
		}
	}
	return nil
}

// cacheEntryFilter reads the filters and the cursor of the entry list.
func cacheEntryFilter(lq ListQuery) (invalidation.EntryFilter, error) {
	f := invalidation.EntryFilter{Prefix: lq.First("pathPrefix"), Limit: lq.FetchLimit()}
	if err := invalidation.CheckPrefix(f.Prefix); err != nil {
		return f, badParam("pathPrefix", "must be a path prefix of whole segments, without a leading or trailing slash")
	}
	switch k := lq.First("kind"); k {
	case "", invalidation.EntryKindMetadata:
		f.Kinds = []string{invalidation.EntryKindMetadata}
	case invalidation.EntryKindArtifact:
		f.Kinds = []string{invalidation.EntryKindArtifact}
	default:
		return f, badParam("kind", "must be metadata or artifact")
	}
	if lq.After != nil {
		if len(lq.After) != 2 {
			return f, invalidCursor()
		}
		f.After = &invalidation.EntryKey{Path: lq.After[0], Variant: lq.After[1]}
	}
	return f, nil
}
