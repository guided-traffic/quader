package registry_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guided-traffic/quader/internal/api/dispatch"
	"github.com/guided-traffic/quader/internal/api/registry"
	"github.com/guided-traffic/quader/internal/api/registry/route"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/httpserver"
)

const (
	pinned = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	other  = "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
)

// pinnedMount is the registry root of the scan listener as far as the enforcement point goes: every OCI route and the core routes
// mounted through an enforcer that knows the two routes a scan token reaches, behind a principal of kind system whose grant carries
// the digest pin, as the stage of the scan listener yields it.
func pinnedMount(t *testing.T) http.Handler {
	t.Helper()
	disp, err := dispatch.New(dispatch.Options{Host: stubHost{}, Decider: stubDecider{}, Repos: stubRepos{}})
	if err != nil {
		t.Fatal(err)
	}
	e, err := registry.NewEnforcer(registry.EnforcerOptions{
		Matcher: disp, WriteError: writeError,
		Pinned: registry.PinnedRoutes{Manifest: "oci.manifest.get.digest", Blob: "oci.blob.get"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var oci formats.Format
	for _, f := range formats.List() {
		if f.Descriptor().RegistryRoot {
			oci = f
		}
	}
	router := httpserver.NewClientRouter()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(e.Mount(router, []string{"/v2/{project}/{key}/{rest...}"}, ok200, registry.RepositoryRoutes(oci.Routes(), exampleBase)...))
	must(e.Mount(router, []string{"GET /v2/{$}", "GET /v2"}, ok200, registry.Route{Name: route.NameVersion, Kind: registry.RouteVersion, Method: "GET", Example: "/v2/"}))
	must(e.Mount(router, []string{"GET /v2/_catalog"}, ok200, registry.Route{Name: route.NameCatalog, Kind: registry.RouteCatalog, Method: "GET", Example: "/v2/_catalog"}))
	p := httpserver.Principal{
		Kind: httpserver.KindSystem, Name: "vuln-scanner-trivy",
		Credential: httpserver.Credential{Method: httpserver.MethodBearer, ScanID: "sid", Registry: &httpserver.RegistryGrant{
			Digest: pinned, Entries: []httpserver.RegistryEntry{{Repo: local, Name: "app", Pull: true}},
		}},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		router.ServeHTTP(w, r.WithContext(httpserver.WithPrincipal(r.Context(), p)))
	})
}

func TestR_OCI_037_TheEnforcementPointAdmitsAScanTokenOnItsManifestAndTheBlobsOfItsName(t *testing.T) {
	h := pinnedMount(t)
	send := func(method, path string) (int, string) {
		r := httptest.NewRequestWithContext(context.Background(), method, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Header().Get("X-Code")
	}
	base := "/v2/web/docker-local/app"
	for _, tc := range []struct{ method, path string }{
		{"GET", base + "/manifests/" + pinned},
		{"HEAD", base + "/manifests/" + pinned},
		{"GET", base + "/blobs/" + other},
		{"HEAD", base + "/blobs/" + pinned},
	} {
		if code, _ := send(tc.method, tc.path); code != http.StatusOK {
			t.Errorf("%s %s: %d, want 200", tc.method, tc.path, code)
		}
	}
	for _, tc := range []struct{ name, method, path string }{
		{"the manifest by tag", "GET", base + "/manifests/1.0"},
		{"another digest", "GET", base + "/manifests/" + other},
		{"the tags list", "GET", base + "/tags/list"},
		{"referrers", "GET", base + "/referrers/" + pinned},
		{"the catalog", "GET", "/v2/_catalog"},
		{"the version check", "GET", "/v2/"},
		{"an upload start", "POST", base + "/blobs/uploads/"},
		{"a manifest put", "PUT", base + "/manifests/" + pinned},
		{"a delete", "DELETE", base + "/manifests/" + pinned},
		{"a blob delete", "DELETE", base + "/blobs/" + pinned},
		{"another image name", "GET", "/v2/web/docker-local/other/manifests/" + pinned},
		{"another repository", "GET", "/v2/web/docker-hub/app/manifests/" + pinned},
		{"the cache view", "GET", "/v2/web/docker-local~cache/app/blobs/" + other},
	} {
		if code, ecode := send(tc.method, tc.path); code != http.StatusUnauthorized || ecode != errs.CodeOCIInsufficientScope {
			t.Errorf("%s: %d %s, want 401 insufficient_scope", tc.name, code, ecode)
		}
	}
}
