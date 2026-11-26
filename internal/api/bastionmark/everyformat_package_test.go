package bastionmark_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/api/bastionmark"
	"github.com/guided-traffic/quader/internal/api/dispatch"
	"github.com/guided-traffic/quader/internal/api/registry"
	"github.com/guided-traffic/quader/internal/auth"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formathost"
	"github.com/guided-traffic/quader/internal/formats"
	_ "github.com/guided-traffic/quader/internal/formats/all" // every format, the registry root's among them
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/packages"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/secrets"
)

// The package of a response through the main installation's real path below /v2/ (bastion.md R-BAS-081): the real authentication
// stage verifies registry tokens of a real signer, the enforcement point checks their grant on the dispatcher's match
// (registry.Register, the production mount), the dispatcher serves the OCI format's own handlers, and the production host
// (formathost.Host) notes the package of each decision they make; only its item store and its remote engine are stand-ins that
// hold every blob. The marking must name exactly the image the enforcement point checked the grant on.

var (
	local = qname.Repo{Project: "web", Key: "docker-local"}
	hub   = qname.Repo{Project: "web", Key: "docker-hub"}
	npmL  = qname.Repo{Project: "web", Key: "npm-local"}
)

type nopRunner struct{}

func (nopRunner) InTx(context.Context, func(meta.Tx) error) error { return nil }

// allow is a decision point that allows every decision.
type allow struct{ authz.Decider }

func (allow) Begin(context.Context, authz.Subject, ...qname.Repo) (*authz.Index, error) {
	return &authz.Index{}, nil
}

func (allow) DecideOn(context.Context, *authz.Index, authz.Subject, authz.Coordinate) authz.Decision {
	return authz.Decision{Outcome: authz.Allow}
}

type repos struct{ repository.Reader }

func (repos) ByName(_ context.Context, name qname.Repo) (repository.Repository, error) {
	switch name {
	case local:
		return repository.Repository{ID: "5", Name: local, Class: repository.Local, Format: "oci", Lifecycle: repository.Active}, nil
	case hub:
		return repository.Repository{ID: "6", Name: hub, Class: repository.Remote, Format: "oci", Lifecycle: repository.Active}, nil
	case npmL:
		return repository.Repository{ID: "7", Name: npmL, Class: repository.Local, Format: "npm", Lifecycle: repository.Active}, nil
	}
	return repository.Repository{}, errs.E(errs.NotFound, errs.CodeRepositoryNotFound, "no")
}

// servingHost is the production host with an item store and a remote engine that hold every blob: Authorize, PathsAllowed and
// WikiReadable are formathost.Host's own.
type servingHost struct{ *formathost.Host }

func (servingHost) Items() formats.ItemStore      { return blobs{} }
func (servingHost) Remote() formats.RemoteFetcher { return cached{} }

type blobs struct{ formats.ItemStore }

func (blobs) Stat(_ context.Context, repo formats.RepoRef, p formats.Path) (formats.Item, bool, error) {
	return formats.Item{Ref: formats.ItemRef{Repo: repo, Path: p.Path}, Digests: formats.Digests{SHA256: make([]byte, 32)}}, true, nil
}

func (blobs) Serve(_ context.Context, _ *formats.Request, w http.ResponseWriter, _ formats.Item, _ formats.ServeMode) error {
	w.WriteHeader(http.StatusOK)
	_, err := io.WriteString(w, "blob")
	return err
}

type cached struct{ formats.RemoteFetcher }

func (cached) Artifact(_ context.Context, _ *formats.Request, _ formats.Path, _ formats.Digests, w http.ResponseWriter) error {
	w.WriteHeader(http.StatusOK)
	_, err := io.WriteString(w, "blob")
	return err
}

// fullShapes is the decision point's shape of the token behind every registry token: it reads every repository in full.
type fullShapes struct{}

func (fullShapes) Shapes(_ context.Context, _ *authz.Index, _ authz.Subject, keys []qname.Repo) (map[qname.Repo]bastionmain.Shape, error) {
	out := map[qname.Repo]bastionmain.Shape{}
	for _, k := range keys {
		out[k] = bastionmain.Shape{Read: iface.ReadFull}
	}
	return out, nil
}

func (fullShapes) PackageShapes(_ context.Context, _ *authz.Index, _ authz.Subject, _ qname.Repo, pkgs []string) (map[string]iface.Read, error) {
	out := map[string]iface.Read{}
	for _, p := range pkgs {
		out[p] = iface.ReadFull
	}
	return out, nil
}

type tokens struct {
	pat  string
	info token.Info
}

func (tokens) Validate(context.Context, string) (token.Info, error) {
	return token.Info{}, token.ErrUnknown
}

func (f tokens) CheckSecretHash(_ context.Context, h [32]byte, typ token.Type, _ token.Request) (token.Info, error) {
	if h != token.Hash(f.pat) || typ != f.info.Type {
		return token.Info{}, token.ErrUnknown
	}
	return f.info, nil
}

type principals struct{}

func (principals) ByID(context.Context, string) (principal.Principal, error) {
	return principal.Principal{}, principal.ErrNotFound
}

func (principals) ByName(context.Context, principal.Kind, string) (principal.Principal, error) {
	return principal.Principal{}, principal.ErrNotFound
}
func (principals) Groups(context.Context, string) ([]principal.GroupRef, error) { return nil, nil }

var alice = principal.Principal{ID: "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10", Kind: principal.KindUser, Name: "alice", Status: principal.StatusActive}

// endpoint is the default endpoint of the test with a Bastion's requests recognized; hBound puts a request on a host whose root
// is bound to web/docker-local.
type endpoint struct {
	h    http.Handler
	d    *dispatch.Dispatcher
	mint func(registrytoken.Grant) string
}

const hBound = "X-Test-Bound"

func newEndpoint(t *testing.T) *endpoint {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	pk, err := packages.NewStore(nopRunner{})
	must(err)
	core, err := formathost.New(formathost.Options{Runner: nopRunner{}, Decider: allow{}, Items: (*items.Store)(nil), Packages: pk})
	must(err)
	d, err := dispatch.New(dispatch.Options{Host: servingHost{core}, Decider: allow{}, Repos: repos{}})
	must(err)
	oci := d.RegistryFormat()
	if oci == nil {
		t.Fatal("no format declares the registry root")
	}
	enf, err := registry.NewEnforcer(registry.EnforcerOptions{Matcher: d, WriteError: oci.WriteError})
	must(err)
	router := httpserver.NewClientRouter()
	stub := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	must(registry.Register(registry.RegisterOptions{Router: router, Enforcer: enf, Dispatcher: d, Format: oci, Version: stub, Catalog: stub, Token: stub}))
	d.Register(router)

	ring := filepath.Join(t.TempDir(), "k1.key")
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	must(os.WriteFile(ring, []byte(base64.StdEncoding.EncodeToString(key)), 0o600))
	keys, err := secrets.Load(config.Secrets{EncryptionKeyFiles: []string{ring}, ActiveKeyID: "k1"})
	must(err)
	signer, err := registrytoken.NewSigner(keys, 5*time.Minute)
	must(err)
	pat, err := token.Generate(token.TypePAT)
	must(err)
	stage := auth.New(auth.Options{
		Tokens: tokens{pat: pat, info: token.Info{ID: "t1", Type: token.TypePAT, Scope: token.Scope{Inherit: true}, Owner: alice}}, Principals: principals{}, Registry: signer,
	})
	rules := auth.EndpointRules{Name: "default", Anonymous: true, TokenTypes: []token.Type{token.TypePAT}}
	marker := bastionmark.New(bastionmark.Options{Shapes: fullShapes{}})
	h := httpserver.Chain{
		Router: router,
		Authenticate: func(r *http.Request) (httpserver.Principal, error) {
			res, err := stage.Authenticate(r, rules)
			if err != nil {
				return httpserver.Principal{}, err
			}
			if res.Anonymous {
				return httpserver.AnonymousPrincipal(), nil
			}
			p := httpserver.Principal{Kind: httpserver.KindUser, ID: res.Principal.ID, Name: res.Principal.Name, Credential: httpserver.Credential{
				Method: httpserver.AuthMethod(res.Credential.Method), TokenID: res.Credential.TokenID, ExpiresAt: res.Credential.ExpiresAt,
			}}
			if g := res.Credential.Registry; g != nil {
				// The composition root's conversion of the grant (internal/app, registryGrant).
				p.Credential.Registry = &httpserver.RegistryGrant{Catalog: g.Catalog, Digest: g.Digest}
				for _, e := range g.Entries {
					p.Credential.Registry.Entries = append(p.Credential.Registry.Entries, httpserver.RegistryEntry{
						Repo: e.Repo, Name: e.Name, Pull: e.Actions.Has(registrytoken.Pull), Push: e.Actions.Has(registrytoken.Push), Delete: e.Actions.Has(registrytoken.Delete),
					})
				}
			}
			return p, nil
		},
		Bastion: func(next http.Handler) http.Handler {
			next = marker.Stage()(next)
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				info := httpserver.InfoFrom(r.Context())
				info.BaseURL = "https://quader.test"
				if r.Header.Get(hBound) != "" {
					info.RootRepository = local
				}
				ctx := httpserver.WithResolved(r.Context(), info)
				ctx = bastionmain.WithRecognition(ctx, bastionmain.Recognition{Bastion: "edge-1", Interface: iface.InterfaceVersion})
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		},
	}.Handler()
	return &endpoint{h: h, d: d, mint: func(g registrytoken.Grant) string {
		now := time.Now().Truncate(time.Second)
		raw, err := signer.Issue(registrytoken.Claims{
			PrincipalKind: string(alice.Kind), PrincipalID: alice.ID, Endpoint: "default", IssuedAt: now, ExpiresAt: now.Add(time.Minute),
			Binding: registrytoken.Binding{TokenID: "t1", TokenType: "pat", SecretSHA256: token.Hash(pat)}, Grant: g,
		})
		must(err)
		return raw
	}}
}

// get sends a request with the credential through the endpoint and returns the response and the image the enforcement point's match
// names for that request.
func (e *endpoint) get(t *testing.T, path, credential string, bound bool) (*httptest.ResponseRecorder, string) {
	t.Helper()
	build := func() *http.Request {
		r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		if credential != "" {
			r.Header.Set("Authorization", "Bearer "+credential)
		}
		if bound {
			r.Header.Set(hBound, "1")
			r = r.WithContext(httpserver.WithResolved(r.Context(), httpserver.RequestInfo{RootRepository: local}))
		}
		return r
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, build())
	target, _ := e.d.RegistryTarget(build())
	return rec, target.Image
}

// TestR_BAS_081_TheImageTheHandlersAuthorizeIsTheEnforcementPointsImage: a blob read with a registry token granted pull on the image
// is marked with the image as its package, full for the token, and that name is the image the enforcement point's match names, on
// an unbound host, on a host bound to the repository and in the cache view of a remote repository; a token granted another image
// of the repository is refused by the enforcement point and the refusal names no package. npm's registry root and an endpoint npm
// defines and Quader does not serve carry no package.
func TestR_BAS_081_TheImageTheHandlersAuthorizeIsTheEnforcementPointsImage(t *testing.T) {
	e := newEndpoint(t)
	blob := "/blobs/sha256:" + strings.Repeat("ab", 32)
	for _, c := range []struct {
		name, path, image string
		grant             qname.Repo
		bound             bool
	}{
		{"an unbound host", "/v2/web/docker-local/app" + blob, "app", local, false},
		{"a nested image name", "/v2/web/docker-local/team/app" + blob, "team/app", local, false},
		{"a host bound to the repository", "/v2" + "/app" + blob, "app", local, true},
		{"the cache view of a remote repository", "/v2/web/docker-hub~cache/library/alpine" + blob, "library/alpine", hub, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			granted := e.mint(registrytoken.Grant{Entries: []registrytoken.Entry{{Repo: c.grant, Name: c.image, Actions: registrytoken.Pull}}})
			rec, image := e.get(t, c.path, granted, c.bound)
			mk, ok, err := iface.ParseMarking(rec.Header())
			if rec.Code != http.StatusOK || !ok || err != nil {
				t.Fatalf("%d %v %v %v %s", rec.Code, ok, err, rec.Header(), rec.Body)
			}
			if image != c.image || mk.Package != image || mk.PackageRead != iface.ReadFull || mk.Read != iface.ReadPartial || mk.Class != iface.ClassArtifact {
				t.Fatalf("the enforcement point's image %q, the marking %+v", image, mk)
			}
			if mk.Repository != c.grant.String() {
				t.Fatalf("repository %q", mk.Repository)
			}
			other := e.mint(registrytoken.Grant{Entries: []registrytoken.Entry{{Repo: c.grant, Name: "other", Actions: registrytoken.Pull}}})
			rec, _ = e.get(t, c.path, other, c.bound)
			if rec.Code != http.StatusUnauthorized || rec.Header().Get(iface.HeaderPackage) != "" || !strings.Contains(rec.Header().Get("WWW-Authenticate"), "insufficient_scope") {
				t.Fatalf("a token granted another image: %d %v", rec.Code, rec.Header())
			}
		})
	}
	t.Run("npm's registry root and an endpoint npm defines and Quader does not serve name no package", func(t *testing.T) {
		for path, status := range map[string]int{"/api/npm/web/npm-local/": http.StatusOK, "/api/npm/web/npm-local/-/v1/search?text=x": http.StatusNotFound} {
			rec, _ := e.get(t, path, "", false)
			mk, ok, err := iface.ParseMarking(rec.Header())
			if rec.Code != status || !ok || err != nil || mk.Package != "" || rec.Header().Get(iface.HeaderPackageRead) != "" {
				t.Fatalf("%s: %d %+v %v", path, rec.Code, mk, err)
			}
		}
	})
}
