package v1

import (
	"context"
	"net/http"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/packages/view"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

// The deprecation of a version or a package (R-API-085). The operation is the API's, the decision the format's: the handler checks the
// request's shape, the repository's class and whether the format carries a deprecation, builds a request of the format for the
// repository and the caller, and hands it to the format's Deprecator, which decides deploy (and npm's ownership) on the package, writes
// its rows, raises the package's revision and records the audit event in one transaction, as a protocol write of its own does.

// DeprecationServices are what the operation needs beyond the repositories and the decision point.
type DeprecationServices struct {
	// Host returns the format host the formats write through; nil while the formats are not wired, and then the operation answers
	// 503.
	Host func() formats.Host
}

// SetPackageDeprecation implements PUT /api/v1/projects/{projectKey}/repositories/{repoKey}/packages/{packageName}/deprecation.
func (s *Server) SetPackageDeprecation(w http.ResponseWriter, r *http.Request, projectKey, repoKeyText, packageName string) {
	body, err := ReadBody(w, r, MediaJSON, s.bodyLimit())
	var in gen.PackageDeprecationInput
	if err == nil {
		err = StrictUnmarshal(body, &in)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	d := &formats.Deprecation{}
	if in.Message != nil {
		d.Message = *in.Message
	}
	if in.Reasons != nil {
		d.Reasons = *in.Reasons
	}
	if a := in.AlternatePackage; a != nil {
		d.Alternate = &formats.DeprecationAlternate{Package: a.Name}
		if a.Range != nil {
			d.Alternate.Range = *a.Range
		}
	}
	version := ""
	if in.Version != nil {
		version = *in.Version
	}
	held, err := s.changeDeprecation(r, qname.Repo{Project: projectKey, Key: repoKeyText}, packageName, version, d)
	if err == nil && held == nil {
		err = errs.E(errs.Internal, errs.CodeInternal, "The format set no deprecation.")
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, deprecationRep(viewDeprecation(held)))
}

// ClearPackageDeprecation implements DELETE /api/v1/projects/{projectKey}/repositories/{repoKey}/packages/{packageName}/deprecation.
func (s *Server) ClearPackageDeprecation(w http.ResponseWriter, r *http.Request, projectKey, repoKeyText, packageName string, params gen.ClearPackageDeprecationParams) {
	version := ""
	if params.Version != nil {
		version = *params.Version
	}
	if _, err := s.changeDeprecation(r, qname.Repo{Project: projectKey, Key: repoKeyText}, packageName, version, nil); err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// changeDeprecation runs one change: the coarse check of the dispatcher (deploy on the repository, decided before the repository is
// looked up, so that a caller who may not learns nothing of it), the repository's class, the format's capability, and the format's own
// decision and write.
func (s *Server) changeDeprecation(r *http.Request, key qname.Repo, pkg, version string, d *formats.Deprecation) (*formats.Deprecation, error) {
	var host formats.Host
	if s.o.Deprecation.Host != nil {
		host = s.o.Deprecation.Host()
	}
	reader := s.o.Repositories.Reader
	if host == nil || reader == nil {
		return nil, unavailable("The deprecation of packages")
	}
	ctx, acc, err := requestHolder(r.Context(), s.o.Authz.Engine, key)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		return nil, errNoDecisionPoint()
	}
	if dec := acc.Decide(ctx, authz.Coordinate{Repository: key, Kind: formats.KindWrite, Actions: []authz.Action{authz.Deploy}}); !dec.Allowed() {
		return nil, dec.Err()
	}
	repo, err := reader.ByName(ctx, key)
	if err != nil {
		return nil, err
	}
	if repo.Lifecycle == repository.Deleting {
		return nil, errs.E(errs.NotFound, errs.CodeRepositoryNotFound, "No repository has the requested key.")
	}
	if repo.Class != repository.Local {
		why := "A remote repository shows the deprecation its upstream records; it is set at the upstream."
		if repo.Class == repository.Virtual {
			why = "A virtual repository shows the deprecations of its members; set it in the local repository that holds the package."
		}
		return nil, errs.E(errs.Conflict, errs.CodeNotApplicable, why)
	}
	dep := formats.CapabilitiesFor(formats.ID(repo.Format)).Deprecator
	if dep == nil {
		return nil, errs.E(errs.Conflict, errs.CodeNotApplicable, noDeprecation(repo.Format))
	}
	rq := &formats.Request{
		Request: r.WithContext(ctx), Repo: &repo,
		Route:   &formats.Route{Name: "api.package.deprecation", Method: r.Method, Action: authz.Deploy, Kind: formats.Admin},
		Subject: acc.Subject, Index: acc.Index(), Endpoint: formats.EndpointFacts{Name: acc.Subject.Endpoint},
		Host: host, Log: httpserver.Logger(ctx),
	}
	defer host.Begin(rq)()
	held, _, err := dep.Deprecate(rq.Context(), rq, formats.DeprecationChange{Package: pkg, Version: version, Deprecation: d})
	return held, err
}

// noDeprecation says why a format has nothing to deprecate (R-API-085).
func noDeprecation(format string) string {
	switch format {
	case "oci":
		return "An OCI image has no deprecation in its format; a Helm chart says it in its Chart.yaml, which is set when the chart is pushed."
	case "deb":
		return "A Debian package has no deprecation in its format; a transitional package says it in its description."
	}
	name := format
	if f, ok := formats.Lookup(format); ok && f.Descriptor().DisplayName != "" {
		name = f.Descriptor().DisplayName
	}
	return "The format " + name + " has no deprecation its clients read."
}

// viewDeprecation is a format's deprecation in the terms of the read model, which the representation is made from.
func viewDeprecation(d *formats.Deprecation) *view.Deprecation {
	if d == nil {
		return nil
	}
	out := &view.Deprecation{Package: d.Package, Message: d.Message, Reasons: d.Reasons}
	if a := d.Alternate; a != nil {
		out.Alternate = &view.Alternate{Package: a.Package, Range: a.Range}
	}
	return out
}

// deprecationRep is the member deprecation of an answer (R-API-085); nil for a version or a package that is not deprecated.
func deprecationRep(d *view.Deprecation) *gen.PackageDeprecation {
	if d == nil {
		return nil
	}
	out := &gen.PackageDeprecation{Scope: gen.PackageDeprecationScopeVersion}
	if d.Package {
		out.Scope = gen.PackageDeprecationScopePackage
	}
	if d.Message != "" {
		msg := d.Message
		out.Message = &msg
	}
	if len(d.Reasons) > 0 {
		reasons := append([]string(nil), d.Reasons...)
		out.Reasons = &reasons
	}
	if a := d.Alternate; a != nil && a.Package != "" {
		out.AlternatePackage = &gen.PackageDeprecationAlternate{Name: a.Package}
		if a.Range != "" {
			rng := a.Range
			out.AlternatePackage.Range = &rng
		}
	}
	return out
}

// linkAlternate names the alternate package of a version's deprecation by its identifier when the caller may read a version of it, so
// that the screen links to it (R-API-085); an alternate package of the same format that the scope does not hold stays a name.
func linkAlternate(ctx context.Context, ps PackageServices, sc view.Scope, format string, d *gen.PackageDeprecation) error {
	if d == nil || d.AlternatePackage == nil {
		return nil
	}
	name, ok := normalizeName(format, d.AlternatePackage.Name)
	if !ok {
		return nil
	}
	p, found, err := ps.View.Package(ctx, sc, format, name)
	if err != nil || !found {
		return err
	}
	id := p.ID
	d.AlternatePackage.PackageId = &id
	return nil
}

// unlistedRep is the member unlisted: true, or absent.
func unlistedRep(u bool) *bool {
	if !u {
		return nil
	}
	return &u
}
