package v1

import (
	"crypto/sha256"
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/qname"
)

// TestR_BAS_083_TheCheckAnswersEachPairInRequestOrder: one element per pair, in request order; a pair of a repository the subject
// reads in full is full without a question to the decision point, one of a repository it reads none of is partial, and the pairs of
// a repository it reads in part are the decision point's package shapes, asked once for that repository.
func TestR_BAS_083_TheCheckAnswersEachPairInRequestOrder(t *testing.T) {
	cs := newCheckSetup(t, checkedCaller(nil))
	cs.shapes.pkgs = map[string]map[string]iface.Read{"it/npm-local": {"@acme/sdk": iface.ReadFull}, "it/npm-partner": {"@acme/sdk": iface.ReadFull}}
	ans := decodeCheck(t, cs.post(iface.PathCheck, `{"credential":"qdr_sat_x","clientAddress":"203.0.113.7","repositories":["it/npm-local","it/npm-all","it/npm-partner"],`+
		`"packages":[{"repository":"it/npm-partner","package":"@acme/sdk"},{"repository":"it/npm-local","package":"x"},{"repository":"it/npm-local","package":"@acme/sdk"}]}`))
	want := iface.PackageReads{
		{Repository: "it/npm-partner", Package: "@acme/sdk", Read: iface.ReadPartial},
		{Repository: "it/npm-local", Package: "x", Read: iface.ReadPartial},
		{Repository: "it/npm-local", Package: "@acme/sdk", Read: iface.ReadFull},
	}
	if len(ans.Packages) != len(want) {
		t.Fatalf("%+v", ans.Packages)
	}
	for i := range want {
		if ans.Packages[i] != want[i] {
			t.Errorf("%d: %+v, want %+v", i, ans.Packages[i], want[i])
		}
	}
	if len(cs.shapes.pkgAsked) != 2 || cs.shapes.pkgAsked[0] != "it/npm-local|x" || cs.shapes.pkgAsked[1] != "it/npm-local|@acme/sdk" {
		t.Errorf("the decision point was asked %v: only the repository read in part, once", cs.shapes.pkgAsked)
	}
	cs.shapes.pkgAsked = nil
	ans = decodeCheck(t, cs.post(iface.PathCheck, `{"credential":"qdr_sat_x","clientAddress":"203.0.113.7","repositories":["it/npm-all"],"packages":[{"repository":"it/npm-all","package":"x"}]}`))
	if len(ans.Packages) != 1 || ans.Packages[0].Read != iface.ReadFull || len(cs.shapes.pkgAsked) != 0 {
		t.Errorf("a repository read in full: %+v, asked %v", ans.Packages, cs.shapes.pkgAsked)
	}
	t.Run("a decision point that refuses to decide is the check's error", func(t *testing.T) {
		cs := newCheckSetup(t, checkedCaller(nil))
		cs.shapes.pkgErr = errs.E(errs.Unavailable, errs.CodeAuthzStale, "stale")
		w := cs.post(iface.PathCheck, `{"credential":"qdr_sat_x","clientAddress":"203.0.113.7","repositories":["it/npm-local"],"packages":[{"repository":"it/npm-local","package":"x"}]}`)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%d %s", w.Code, w.Body)
		}
	})
}

// TestR_BAS_083_ARegistryTokenIsFullOnlyOnTheImagesOfItsGrant: through the real authentication stage, a registry token of a real
// signer granted pull on web/docker-local/app, whose token reads the repository in full, asked about the packages [app, other]
// answers full and partial; its shape on the repository stays partial (R-BAS-052). The Quader token behind it, presented itself, is
// full on both.
func TestR_BAS_083_ARegistryTokenIsFullOnlyOnTheImagesOfItsGrant(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	signer := registrySigner(t, 3)
	pat, err := token.Generate(token.TypePAT)
	if err != nil {
		t.Fatal(err)
	}
	store := patStore{raw: pat, info: token.Info{
		ID: callerTokenID, Type: token.TypePAT,
		Owner: principal.Principal{ID: checkedPrincipalID, Kind: principal.KindUser, Name: "alice", Status: principal.StatusActive},
	}}
	stage := auth.New(auth.Options{Tokens: store, Registry: signer, Principals: noPrincipals{}, Now: func() time.Time { return now }})
	rules := auth.EndpointRules{Name: "default", TokenTypes: []token.Type{token.TypePAT}}
	cs := newCheckSetup(t, func(r *http.Request) (httpserver.Principal, error) {
		res, err := stage.Authenticate(r, rules)
		if err != nil {
			return httpserver.Principal{}, err
		}
		p := httpserver.Principal{Kind: httpserver.KindUser, ID: res.Principal.ID, Name: res.Principal.Name, Credential: httpserver.Credential{
			Method: httpserver.AuthMethod(res.Credential.Method), TokenID: res.Credential.TokenID, ExpiresAt: res.Credential.ExpiresAt,
		}}
		if g := res.Credential.Registry; g != nil {
			// The composition root's conversion of the grant (internal/app, registryGrant).
			p.Credential.Registry = &httpserver.RegistryGrant{Catalog: g.Catalog, Digest: g.Digest}
			for _, e := range g.Entries {
				p.Credential.Registry.Entries = append(p.Credential.Registry.Entries, httpserver.RegistryEntry{Repo: e.Repo, Name: e.Name, Pull: e.Actions.Has(registrytoken.Pull)})
			}
		}
		return p, nil
	})
	cs.shapes.shapes["web/docker-local"] = bastionmain.Shape{Read: iface.ReadFull}
	raw, err := signer.Issue(registrytoken.Claims{
		PrincipalKind: "user", PrincipalID: checkedPrincipalID, Endpoint: "default", IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute),
		Binding: registrytoken.Binding{TokenID: callerTokenID, TokenType: string(token.TypePAT), SecretSHA256: sha256.Sum256([]byte(pat))},
		Grant:   registrytoken.Grant{Entries: []registrytoken.Entry{{Repo: qname.Repo{Project: "web", Key: "docker-local"}, Name: "app", Actions: registrytoken.Pull}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := func(credential string) string {
		return `{"credential":"` + credential + `","clientAddress":"203.0.113.7","repositories":["web/docker-local"],` +
			`"packages":[{"repository":"web/docker-local","package":"app"},{"repository":"web/docker-local","package":"other"}]}`
	}
	ans := decodeCheck(t, cs.post(iface.PathCheck, body(raw)))
	if ans.Subject.Kind != iface.SubjectToken || ans.Repositories["web/docker-local"].Read != iface.ReadPartial {
		t.Fatalf("%+v", ans)
	}
	if len(ans.Packages) != 2 || ans.Packages[0] != (iface.PackageRead{Repository: "web/docker-local", Package: "app", Read: iface.ReadFull}) ||
		ans.Packages[1] != (iface.PackageRead{Repository: "web/docker-local", Package: "other", Read: iface.ReadPartial}) {
		t.Fatalf("the registry token: %+v", ans.Packages)
	}
	ans = decodeCheck(t, cs.post(iface.PathCheck, body(pat)))
	if len(ans.Packages) != 2 || ans.Packages[0].Read != iface.ReadFull || ans.Packages[1].Read != iface.ReadFull {
		t.Fatalf("the Quader token itself: %+v", ans.Packages)
	}
}
