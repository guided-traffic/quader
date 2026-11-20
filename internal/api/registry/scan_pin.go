package registry

import (
	"net/http"

	"github.com/guided-traffic/quader/internal/api/registry/route"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// PinnedRoutes are the routes of the registry-root format that a scan token reaches (R-OCI-037, Q-OCI-227): the manifest by its
// digest and the blob. The composition root names them from the format, as it names the mount route.
type PinnedRoutes struct {
	Manifest string
	Blob     string
}

// decidePinned is the enforcement point's rule for a grant with a digest pin, a scan token's (R-OCI-037 "What it reaches"): GET
// and HEAD of the manifest of the pinned digest under the entry's name, and GET and HEAD of a blob under the entry's name, on the
// repository under its plain key. Everything else - a manifest by tag or by another digest, the tags list, referrers, the catalog,
// the version check, a cache view, a bound host and every write - is the 401 with error="insufficient_scope". The scanner asks for
// no other token, so the refusal names no scope.
func (e *Enforcer) decidePinned(r *http.Request, g *httpserver.RegistryGrant, rt Route, t route.Target) error {
	p := e.o.Pinned
	read := r.Method == http.MethodGet || r.Method == http.MethodHead
	plain := rt.Kind == RouteRepository && !t.CacheView && !t.Bound && !t.Repo.IsZero() && t.Image != ""
	reaches := (p.Manifest != "" && t.Route == p.Manifest && t.Reference == g.Digest) || (p.Blob != "" && t.Route == p.Blob)
	if read && plain && reaches && g.Allows(t.Repo, t.Image, "pull") {
		return nil
	}
	return e.insufficient(r, "")
}
