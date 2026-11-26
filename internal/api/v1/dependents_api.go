package v1

import (
	"net/http"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/packages/dependents"
	"github.com/guided-traffic/quader/internal/packages/view"
)

// ListPackageDependents implements GET /api/v1/packages/{packageId}/dependents (R-API-086): the packages that depend on a package, in
// the repositories the caller may read. The scope is the package list's (the endpoint's exposure included, packageScope): the
// repositories are decided before a row is read (view.Service.Dependents), so a dependent the caller may not read is neither listed
// nor counted. A format whose versions name no dependencies answers none.
func (s *Server) ListPackageDependents(w http.ResponseWriter, r *http.Request, id gen.PackageId, params gen.ListPackageDependentsParams) {
	ps, err := s.packageServices()
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	format, name, ok := view.ParseID(id)
	if !ok {
		httpserver.Problem(w, r, packageNotFound())
		return
	}
	project := ""
	if params.Project != nil {
		project = *params.Project
	}
	out := gen.PackageDependents{Items: []gen.PackageDependent{}}
	if !dependents.Has(format) {
		writeJSON(w, r, http.StatusOK, out)
		return
	}
	ctx, sc, err := s.packageScope(r, ps, view.ScopeRequest{Formats: []string{format}})
	var d view.Dependents
	if err == nil {
		d, err = ps.View.Dependents(ctx, sc, format, name, project, view.MaxDependents)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	out.Total, out.Whole = d.Total, d.Whole
	for _, p := range d.Items {
		e := gen.PackageDependent{
			PackageId: p.ID, Format: p.Format, Name: p.Name, Key: p.Key, Version: p.Version,
			Relations: make([]gen.PackageDependentRelation, 0, len(p.Relations)),
		}
		for _, rel := range p.Relations {
			e.Relations = append(e.Relations, gen.PackageDependentRelation{Kind: rel.Kind, Range: rel.Range, Via: rel.Via})
		}
		out.Items = append(out.Items, e)
	}
	if err := s.addDependentMalware(ctx, d.Items, out.Items); err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, out)
}

// dependsOnFilter reads the filter dependsOn of the package list (R-API-086): a package name of the one format asked, whose versions name
// dependencies, read by the format's name rule.
func dependsOnFilter(formatNames []string, raw string) (string, error) {
	if len(formatNames) != 1 || !dependents.Has(formatNames[0]) {
		return "", badParam("dependsOn", "needs exactly one format whose versions name dependencies (npm, nuget, deb, ansible)")
	}
	if len(raw) > 256 {
		return "", badParam("dependsOn", "is at most 256 characters")
	}
	norm, ok := normalizeName(formatNames[0], raw)
	if !ok {
		return "", badParam("dependsOn", "is no package name of the format")
	}
	return norm, nil
}
