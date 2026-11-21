package v1

import (
	"net/http"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/repository"
)

// The installation's registry presets (R-REPO-076, R-API-084, Q-API-383): one document, read by a holder of repositories:read-config
// and replaced whole by a holder of repositories:write-config under If-Match; the shared authorization step decided the caller. The
// body is read strictly against a preset's four members, so a credential member is a 400 before anything is checked (Q-REPO-417).

// registryPresetsBody is the document as the API reads and writes it.
type registryPresetsBody struct {
	Presets []registryPresetBody `json:"presets"`
}

type registryPresetBody struct {
	Format      string                   `json:"format"`
	DisplayName string                   `json:"displayName"`
	URL         string                   `json:"url"`
	ExtraHosts  []gen.UpstreamPresetHost `json:"extraHosts"`
}

func toRegistryPresets(list []repository.RegistryPreset) gen.RegistryPresets {
	out := gen.RegistryPresets{Presets: make([]gen.RegistryPreset, 0, len(list))}
	for _, p := range list {
		out.Presets = append(out.Presets, gen.RegistryPreset{
			Format: p.Format, DisplayName: p.DisplayName, Url: p.URL, ExtraHosts: presetHostsOf(p.ExtraHosts),
		})
	}
	return out
}

// presetHostsOf renders further hosts in the shape of an upstream preset's, never null.
func presetHostsOf(in []repository.ExtraHost) []gen.UpstreamPresetHost {
	out := make([]gen.UpstreamPresetHost, 0, len(in))
	for _, h := range in {
		out = append(out, gen.UpstreamPresetHost{Pattern: h.Pattern, SendCredentials: h.SendCredentials})
	}
	return out
}

// GetRegistryPresets implements GET /api/v1/settings/registry-presets. It reads PostgreSQL, so a change another replica committed
// shows at once.
func (s *Server) GetRegistryPresets(w http.ResponseWriter, r *http.Request, _ gen.GetRegistryPresetsParams) {
	svc, err := s.repositoryService()
	var list repository.RegistryPresets
	if err == nil {
		list, err = svc.RegistryPresets(r.Context())
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "registry_presets.get"))
		return
	}
	if NotModified(w, r, ETag(list.Version, "")) {
		return
	}
	SetETag(w, list.Version, "")
	writeJSON(w, r, http.StatusOK, toRegistryPresets(list.Presets))
}

// SetRegistryPresets implements PUT /api/v1/settings/registry-presets.
func (s *Server) SetRegistryPresets(w http.ResponseWriter, r *http.Request, _ gen.SetRegistryPresetsParams) {
	svc, err := s.repositoryService()
	var pre Precondition
	if err == nil {
		pre, err = ParseIfMatch(r)
	}
	var in registryPresetsBody
	if err == nil {
		err = DecodeJSON(w, r, &in, int64(s.o.API.Config.MaxConfigurationBodyBytes))
	}
	if err == nil && in.Presets == nil {
		err = errs.E(errs.Invalid, errs.CodeValidation, "The list is required.", errs.WithFields(required("/presets")))
	}
	var list repository.RegistryPresets
	if err == nil {
		list, err = svc.SetRegistryPresets(r.Context(), repositoryCaller(r), expectedRevision(pre), fromRegistryPresets(in.Presets))
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "registry_presets.set"))
		return
	}
	SetETag(w, list.Version, "")
	writeJSON(w, r, http.StatusOK, toRegistryPresets(list.Presets))
}

func fromRegistryPresets(in []registryPresetBody) []repository.RegistryPreset {
	out := make([]repository.RegistryPreset, 0, len(in))
	for _, p := range in {
		hosts := make([]repository.ExtraHost, 0, len(p.ExtraHosts))
		for _, h := range p.ExtraHosts {
			hosts = append(hosts, repository.ExtraHost{Pattern: h.Pattern, SendCredentials: h.SendCredentials})
		}
		out = append(out, repository.RegistryPreset{Format: p.Format, DisplayName: p.DisplayName, URL: p.URL, ExtraHosts: hosts})
	}
	return out
}
