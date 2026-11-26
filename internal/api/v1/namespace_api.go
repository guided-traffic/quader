package v1

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

// The page of a name (api-and-ui.md R-UI-220, Q-UI-1290 to Q-UI-1294): what decides a publish into a namespace of a format, made from
// what exists. The packages of the namespace are the package list with the filter namespace; this operation answers the rest, and
// only to a caller who may read a repository's configuration: a holder of repositories:read-config, or an Owner of the repository's
// project (readsConfig). It names no principal and no repository the caller may not read the configuration of.

func namespaceNotFound() error {
	return errs.E(errs.NotFound, errs.CodeNotFound, "The format has no such namespace.")
}

// GetPackageNamespace implements GET /api/v1/packages/namespace (R-UI-220).
func (s *Server) GetPackageNamespace(w http.ResponseWriter, r *http.Request, params gen.GetPackageNamespaceParams) {
	ctx := r.Context()
	if err := s.anonymousGate(ctx); err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	f, ok := formats.Lookup(params.Format)
	var rule *catalog.NamespaceRule
	if ok {
		rule = f.Descriptor().Namespaces
	}
	name := strings.ToLower(params.Name)
	prefix, valid := rule.Prefix(name)
	if !valid {
		httpserver.Problem(w, r, namespaceNotFound())
		return
	}
	project := ""
	if params.Project != nil {
		project = *params.Project
	}
	pub, err := s.namespacePublishing(ctx, params.Format, prefix, rule.Nested, project)
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, gen.PackageNamespace{
		Format: params.Format, Name: name, Kind: gen.PackageNamespaceKind(rule.Kind), Publishing: pub,
	})
}

// namespacePublishing is the member publishing (R-UI-220, Q-UI-1292): the active local repositories of the format, in the project when
// one is given, whose configuration the caller may read and that the request's endpoint exposes, those with a claim that covers names
// of the namespace first; nil for a caller who reads no such configuration.
func (s *Server) namespacePublishing(ctx context.Context, format, prefix string, deep bool, project string) (*gen.NamespacePublishing, error) {
	own, err := s.configScope(ctx)
	if err != nil || (!own.global && !own.anyOwned()) {
		return nil, err
	}
	reader := s.o.Repositories.Reader
	svc, svcErr := s.repositoryService()
	if reader == nil || svcErr != nil {
		return nil, unavailable("The repository service")
	}
	repos, err := reader.List(ctx)
	if err != nil {
		return nil, err
	}
	byName := map[qname.Repo]repository.Repository{}
	var keys []qname.Repo
	for _, rp := range repos {
		if rp.Format != format || rp.Class != repository.Local || rp.Lifecycle != repository.Active ||
			(project != "" && rp.Name.Project != project) || !own.sees(rp.Name.Project) {
			continue
		}
		byName[rp.Name] = rp
		keys = append(keys, rp.Name)
	}
	if len(keys) == 0 {
		return nil, nil
	}
	ctx, acc, err := requestHolder(ctx, s.o.Repositories.Authz, keys...)
	if err != nil {
		return nil, err
	}
	exposed, err := s.visibleKeys(ctx, acc, keys, true)
	if err != nil || len(exposed) == 0 {
		return nil, err
	}
	ids := make([]int64, 0, len(exposed))
	for _, k := range exposed {
		ids = append(ids, byName[k].NumericID())
	}
	claims, err := svc.ClaimsWithin(ctx, ids, prefix, deep)
	if err != nil {
		return nil, err
	}
	out := &gen.NamespacePublishing{Repositories: make([]gen.NamespaceRepository, 0, len(exposed))}
	for _, k := range exposed {
		rp := byName[k]
		entry := gen.NamespaceRepository{
			Project: k.Project, Key: k.Key, AutoClaim: rp.AutoClaim(), Claims: []gen.NamespaceClaim{},
			PackageOwnership: gen.NamespaceRepositoryOwnershipOff,
		}
		if rp.PackageOwnership() == "enforce" {
			entry.PackageOwnership = gen.NamespaceRepositoryOwnershipEnforce
		}
		for _, c := range claims[rp.NumericID()] {
			entry.Claims = append(entry.Claims, gen.NamespaceClaim{
				Pattern: c.Pattern, Scope: gen.NamespaceClaimScope(c.Scope), Source: gen.NamespaceClaimSource(c.Source),
				Covers: gen.NamespaceClaimCovers(c.Covers),
			})
		}
		out.Repositories = append(out.Repositories, entry)
	}
	slices.SortStableFunc(out.Repositories, func(a, b gen.NamespaceRepository) int {
		switch ac, bc := len(a.Claims) > 0, len(b.Claims) > 0; {
		case ac && !bc:
			return -1
		case bc && !ac:
			return 1
		}
		return strings.Compare(a.Project+"/"+a.Key, b.Project+"/"+b.Key)
	})
	return out, nil
}
