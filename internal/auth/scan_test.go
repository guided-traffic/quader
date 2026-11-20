package auth

import (
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/errs"
)

const scanManifestDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func scanRules() EndpointRules {
	return EndpointRules{Name: registrytoken.ScanEndpoint, ScanOnly: true}
}

func TestR_EP_010_TheScanListenerAcceptsAScanTokenAndNothingElse(t *testing.T) {
	f := newRegistryFixture(t)
	scans, err := f.signer.ForScans(10 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	lim := &countingLimiter{n: map[string]int{}}
	var refused []Refusal
	rules := scanRules()
	rules.Refused = func(_ *http.Request, r Refusal) { refused = append(refused, r) }
	s := New(Options{
		Tokens: hashTokens{f.ft}, Principals: fakeStore{}, Registry: scans, Now: func() time.Time { return regNow },
		Limiter: lim, ClientAddr: func(*http.Request) netip.Addr { return netip.MustParseAddr("10.0.0.7") },
	})
	tok, sid, err := scans.IssueScan(regRepo, "app", scanManifestDigest, regNow)
	if err != nil {
		t.Fatal(err)
	}
	manifest := "/v2/web/docker-local/app/manifests/" + scanManifestDigest

	t.Run("R-OCI-037 a scan token is the principal system:image-scanner with its grant, its digest pin and its scan ID", func(t *testing.T) {
		res, err := s.Authenticate(req("GET", manifest, "Authorization", "Bearer "+tok.Reveal()), rules)
		if err != nil {
			t.Fatal(err)
		}
		c := res.Credential
		if res.Anonymous || res.Principal.Kind != principal.KindSystem || res.Principal.Name != registrytoken.ScanSubject || res.Principal.ID != "" ||
			c.Registry == nil || c.Registry.Digest != scanManifestDigest || !c.Registry.Allows(regRepo, "app", registrytoken.Pull) || c.ScanID != sid || c.TokenID != "" {
			t.Fatalf("result %+v %+v", res, c.Registry)
		}
	})
	t.Run("every other credential and none are refused with 401 and no challenge", func(t *testing.T) {
		calls := f.ft.calls.Load()
		client := f.issue(t, false)
		for name, r := range map[string]*http.Request{
			"a personal access token":      req("GET", manifest, "Authorization", "Bearer "+f.pat),
			"a client's registry token":    req("GET", manifest, "Authorization", "Bearer "+client),
			"Basic":                        req("GET", manifest, "Authorization", "Basic eDp4"),
			"the scan token outside /v2/":  req("GET", "/api/npm/web/npm-local/x", "Authorization", "Bearer "+tok.Reveal()),
			"the scan token at the tokens": req("GET", registrytoken.TokenPath, "Authorization", "Bearer "+tok.Reveal()),
		} {
			_, err := s.Authenticate(r, rules)
			if e := wantErr(t, err, 401, errs.CodeInvalidToken); e.Challenge != "" {
				t.Errorf("%s: challenge %q", name, e.Challenge)
			}
		}
		for _, r := range []*http.Request{req("GET", manifest), req("GET", registrytoken.TokenPath)} {
			r.AddCookie(&http.Cookie{Name: SessionCookie, Value: "s"})
			_, err := s.Authenticate(r, rules)
			if e := wantErr(t, err, 401, errs.CodeCredentialsRequired); e.Challenge != "" {
				t.Errorf("challenge %q", e.Challenge)
			}
		}
		if f.ft.calls.Load() != calls {
			t.Fatal("a refusal on the scan listener reached the token lookup")
		}
		if len(refused) == 0 {
			t.Fatal("no refusal was counted for the endpoint")
		}
	})
	t.Run("R-AUTH-038 a forged or expired scan token counts nothing towards the throttle", func(t *testing.T) {
		forged := tok.Reveal()[:20] + string(tok.Reveal()[20]^1) + tok.Reveal()[21:]
		_, err := s.Authenticate(req("GET", manifest, "Authorization", "Bearer "+forged), rules)
		wantErr(t, err, 401, errs.CodeInvalidToken)
		if lim.of("10.0.0.7") != 0 {
			t.Fatalf("failures of the scanner's address: %d", lim.of("10.0.0.7"))
		}
	})
	t.Run("R-ARCH-037 a scan token on another listener is refused by its endpoint claim", func(t *testing.T) {
		_, err := s.Authenticate(req("GET", manifest, "Authorization", "Bearer "+tok.Reveal()), defaultRules())
		wantErr(t, err, 401, errs.CodeInvalidToken)
	})
}
