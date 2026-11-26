//go:build integration

package endpoints

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/mirror/discover"
	"github.com/guided-traffic/quader/internal/proxy/proxytest"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// The cache-fill policy of an endpoint (R-EP-019) through the whole request path: the
// listener's endpoint, the subject's cache-fill flag, the format dispatcher, the Host's populate
// check and the remote engine with a counting upstream. The format is relayFormat, a package
// format of this test with a remote class and nothing else of a registry, so the test needs no
// npm remote class; the npm acceptance criterion of R-EP-019 joins with it.

// relayFormat serves the files of a package from its repository: a local repository answers
// 404, a remote one goes through the remote engine. It is exposable through paths alone.
type relayFormat struct{ formats.Base }

func init() { formats.Register(relayFormat{}) }

func (relayFormat) Descriptor() formats.Descriptor {
	ttl := 60
	return formats.Descriptor{
		Facts: catalog.Facts{
			ID: "relay", DisplayName: "Relay", Classes: []formats.Class{formats.Local, formats.Remote}, Layout: "relay",
			ProtocolBasePath: "/api/relay/{project}/{key}/", PatternSubject: formats.SubjectName, PackageNames: true,
			DefaultMetadataTTLSeconds: &ttl,
		},
		ContractVersion: formats.ContractVersion, Names: foldedNames{}, PropertyPrefix: "relay.", MaxDocumentBytes: 1 << 20,
		NoProbe:      "a format of the endpoint tests, whose routes fetch what they name",
		DownloadUnit: formats.DownloadUnitFile,
		Catalogue: formats.CatalogueDeclaration{
			Lookup: true, NoSearch: "a format of the endpoint tests", NoLevels: "a format of the endpoint tests",
		},
		VersionCanon: anyVersion{},
	}
}

// anyVersion takes every non-empty version as given (R-CUR-006): the endpoint tests compare no version.
type anyVersion struct{}

func (anyVersion) Canonical(v string) (string, bool) { return v, v != "" }

func (f relayFormat) Routes() []formats.Route {
	return []formats.Route{{
		Name: "relay.file.get", Method: http.MethodGet, Template: "/{package}/-/{file}", Examples: []string{"left-pad/-/left-pad-1.0.0.tgz"},
		Action: formats.ActionRead, Kind: formats.Download, Handler: f.file,
	}}
}

func (f relayFormat) Match(m, p string) (*formats.Route, formats.Params, error) {
	return formats.MatchRoutes(f.Routes(), m, p)
}

func (relayFormat) Classify(p formats.Path) formats.PathInfo {
	pkg, rest, _ := strings.Cut(p.Path, "/")
	if strings.HasPrefix(rest, "-/") {
		return formats.PathInfo{Kind: formats.Content, Package: pkg}
	}
	return formats.PathInfo{Kind: formats.PathMetadata, Package: pkg}
}

func (relayFormat) PackageDocumentPath(name string) (formats.Path, bool) {
	return formats.Path{Path: name}, true
}

func (f relayFormat) file(ctx context.Context, rq *formats.Request, w http.ResponseWriter) error {
	if err := rq.Host.Authorize(ctx, rq, formats.Target{Package: rq.Params.Get("package"), Path: rq.Path.Path, Actions: []formats.Action{formats.ActionRead}}); err != nil {
		return err
	}
	if rq.Repo.Class != formats.Remote {
		return errs.E(errs.NotFound, errs.CodeNotFound, "The resource does not exist.")
	}
	return rq.Host.Remote().Artifact(ctx, rq, rq.Path, formats.Digests{}, w)
}

func (relayFormat) Remote() formats.RemoteAdapter { return relayRemote{} }

func (relayFormat) Exposure() formats.ExposureAdapter { return pathsExposure{} }

func (relayFormat) WriteError(w http.ResponseWriter, r *http.Request, err error) {
	opaqueFormat{}.WriteError(w, r, err)
}

// relayRemote fetches a path below the upstream URL.
type relayRemote struct{ formats.RemoteBase }

// The relay's catalogue is the floor R-CUR-007 asks of every format with the class remote (Q-CUR-109): it searches nothing, lists
// no levels and knows no package, since the endpoint tests fetch only what they name.
var _ formats.Catalogue = relayRemote{}

// CatalogueCaps implements discover.Catalogue.
func (relayRemote) CatalogueCaps(context.Context, discover.Upstream) (discover.CatalogueCaps, error) {
	return discover.CatalogueCaps{
		Search: discover.SearchNone, SearchHint: discover.SearchHintNoSearch,
		LevelsReason: "a format of the endpoint tests", Sizes: "unknown",
	}, nil
}

// Search implements discover.Catalogue.
func (relayRemote) Search(context.Context, discover.Upstream, discover.SearchQuery) (discover.SearchPage, error) {
	return discover.SearchPage{}, discover.ErrNotOffered
}

// Children implements discover.Catalogue.
func (relayRemote) Children(context.Context, discover.Upstream, discover.LevelRequest) (discover.LevelPage, error) {
	return discover.LevelPage{}, discover.ErrNotOffered
}

// Package implements discover.Catalogue: the endpoint tests look no package up.
func (relayRemote) Package(_ context.Context, _ discover.Upstream, name string, _ discover.VersionsRequest) (discover.PackageInfo, error) {
	return discover.PackageInfo{Name: name}, nil
}

// VersionProbe implements formats.VersionProber: the endpoint tests download nothing ahead.
func (relayRemote) VersionProbe(context.Context, discover.Upstream, string, string) (formats.Probe, bool, error) {
	return formats.Probe{}, false, nil
}

// ProbeContent implements formats.RemoteAdapter: the files of the probe.
func (relayRemote) ProbeContent(_ context.Context, _ discover.Upstream, p formats.Probe) ([]discover.Item, error) {
	return formats.ProbeFiles(p), nil
}

func (relayRemote) UpstreamRequest(ctx context.Context, rc formats.RemoteContext, p formats.Path, _ string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(rc.Upstream.String(), "/")+"/"+p.Path, nil)
}

func (relayRemote) AcceptResponse(formats.Path, *http.Response) error { return nil }

func (relayRemote) PrepareFill(context.Context, formats.RemoteContext, formats.Path, string, io.ReaderAt, int64) (formats.Prepared, error) {
	return formats.Prepared{}, nil
}

func (relayRemote) OnFill(context.Context, formats.Tx, formats.FillEvent) (formats.FillDecision, error) {
	return formats.FillByFetchTime, nil
}

func (relayRemote) Rewrite(_ context.Context, _ formats.RemoteContext, _ formats.Path, doc []byte, _ formats.LinkBuilder) ([]byte, error) {
	return doc, nil
}

func (relayRemote) TracksVersionRemoval() bool { return false }

// pathsExposure is the exposure adapter of a format whose repositories rules expose by path.
type pathsExposure struct{}

func (pathsExposure) ValidateRange(string) error             { return fmt.Errorf("no versions") }
func (pathsExposure) Prerelease(string) (bool, bool)         { return false, false }
func (pathsExposure) InRange(string, string) bool            { return false }
func (pathsExposure) Dimensions() formats.ExposureDimensions { return formats.ExposureDimensions{} }

// fillEndpoints is server.endpoints of the test: customers with the defaults of a further
// endpoint (cache_fill false), partners with cache_fill true.
var fillEndpoints = fmt.Sprintf(`{"customers":{"listen":"127.0.0.1:0","base_url":"https://%s","surface":["packages"]},`+
	`"partners":{"listen":"127.0.0.1:0","base_url":"https://%s","surface":["packages"],"cache_fill":true}}`, customersHost, partnersHost)

// TestR_EP_019_TheCacheFillPolicyOfAnEndpoint: no request on an endpoint without cache_fill makes
// quader contact an upstream and the refusal is counted; the same
// request through default fills the cache, after which customers is served from it; an endpoint
// with cache_fill true lets a subject that may populate fill.
func TestR_EP_019_TheCacheFillPolicyOfAnEndpoint(t *testing.T) {
	e := newEnv(t, false)
	up := proxytest.NewUpstream(t)
	file := func(v string) string { return "left-pad/-/left-pad-" + v + ".tgz" }
	for _, v := range []string{"1.0.0", "1.1.0"} {
		up.Bytes("/"+file(v), []byte("left-pad "+v), "application/octet-stream")
	}
	settings, err := json.Marshal(map[string]any{"remote": repository.RemoteSettings{
		URL: up.URL + "/", ExtraHosts: []repository.ExtraHost{}, AuthMode: repository.AuthNone, Headers: []repository.Header{},
		OutboundProxy: repository.OutboundProxy{Mode: repository.ProxyDefault}, ConnectTimeoutMs: 10000, ReadTimeoutMs: 15000,
		CacheFillAction: repository.FillRead,
		Cache:           repository.RemoteCache{StoreArtifacts: true, MetadataTTLSeconds: 60, MetadataRefreshTimeoutSeconds: 60, NotFoundTTLSeconds: 1800},
		Integrity:       repository.RemoteIntegrity{ChecksumPolicy: repository.ChecksumVerifyIfAvailable, BlockUnexpectedContentTypes: true, AllowedContentTypes: []string{}},
		Availability:    repository.Availability{Mode: repository.AvailabilityOnline, FailureThreshold: 5, CircuitOpenSeconds: 300},
		Limits:          repository.RemoteLimits{MaxConcurrentFetches: 8},
	}})
	if err != nil {
		t.Fatal(err)
	}
	e.stack.Repository("relay-remote", "remote", "relay", string(settings))
	acme := testenv.Person{ID: e.acmeID, Kind: "service_account"}
	e.stack.Policy("relay-read", []string{"read"}, []string{"relay-remote"}, acme)
	acmeSAT := e.stack.Token(acme, scope.Scope{Inherit: true, Endpoints: []string{"customers", "partners"}})
	ca := filepath.Join(e.stack.Dir(), "relay-upstream-ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: up.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	h := e.stack.Start(t, testenv.ReplicaOptions{Name: "fill", Endpoints: fillEndpoints, Env: []string{
		`QUADER_PROXY_OUTBOUND_TLS_CA_FILES=["` + ca + `"]`,
		`QUADER_PROXY_OUTBOUND_ADDRESS_POLICY_ALLOW_CIDRS=["127.0.0.0/8","::1/128"]`,
	}})
	var epoch string
	for _, ep := range []string{"customers", "partners"} {
		a := e.send(h.Addr("default"), "quader.test", "POST", "/api/v1/endpoints/"+ep+"/exposure-rules", e.opsPAT, []byte(`{"name":"relay","repository":"it/relay-remote"}`))
		if a.Status != http.StatusCreated {
			t.Fatalf("rule on %s: %d %s", ep, a.Status, a.Body)
		}
		epoch = a.Header.Get("Quader-Authz-Epoch")
	}
	get := func(addr, host, tok, path string) answer {
		t.Helper()
		return e.send(addr, host, "GET", "/api/relay/it/relay-remote/"+path, tok, nil, "Quader-Authz-Min-Epoch", epoch)
	}
	refused := func() float64 {
		return testenv.MetricValue(h.Metrics(t), `quader_endpoint_cache_fill_refused_total{endpoint="customers"}`)
	}

	t.Run("R-EP-019 an uncached file on customers: 403 populate_not_permitted, no upstream request, counted", func(t *testing.T) {
		for range 2 {
			a := get(h.Addr("customers"), customersHost, acmeSAT, file("1.0.0"))
			if a.Status != http.StatusForbidden || !strings.Contains(string(a.Body), errs.CodePopulateNotPermitted) {
				t.Fatalf("%d %s", a.Status, a.Body)
			}
		}
		if n := up.Count("", "/"+file("1.0.0")); n != 0 {
			t.Fatalf("the upstream was asked %d times", n)
		}
		if got := refused(); got < 2 {
			t.Fatalf("quader_endpoint_cache_fill_refused_total{customers} = %v", got)
		}
	})
	t.Run("R-EP-019 the same request through default fills the cache", func(t *testing.T) {
		a := get(h.Addr("default"), "quader.test", e.opsPAT, file("1.0.0"))
		if a.Status != http.StatusOK || string(a.Body) != "left-pad 1.0.0" {
			t.Fatalf("%d %s", a.Status, a.Body)
		}
		if n := up.Count("", "/"+file("1.0.0")); n != 1 {
			t.Fatalf("the upstream was asked %d times", n)
		}
	})
	t.Run("R-EP-019 customers then serves what the cache holds, still without the upstream", func(t *testing.T) {
		before := refused()
		a := get(h.Addr("customers"), customersHost, acmeSAT, file("1.0.0"))
		if a.Status != http.StatusOK || string(a.Body) != "left-pad 1.0.0" {
			t.Fatalf("%d %s", a.Status, a.Body)
		}
		if n := up.Count("", "/"+file("1.0.0")); n != 1 || refused() != before {
			t.Fatalf("upstream asked %d times, refusals %v -> %v", n, before, refused())
		}
	})
	t.Run("R-EP-019 with cache_fill true a subject that may populate fills the cache", func(t *testing.T) {
		a := get(h.Addr("partners"), partnersHost, acmeSAT, file("1.1.0"))
		if a.Status != http.StatusOK || string(a.Body) != "left-pad 1.1.0" {
			t.Fatalf("%d %s", a.Status, a.Body)
		}
		if n := up.Count("", "/"+file("1.1.0")); n != 1 {
			t.Fatalf("the upstream was asked %d times", n)
		}
	})
}

// Signatures declares none: a format of this test (signatures.md R-SIG-022).
func (relayFormat) Signatures() formats.SignatureAdapter {
	return formats.NoSignatures("a format of this test", "a format of this test")
}
