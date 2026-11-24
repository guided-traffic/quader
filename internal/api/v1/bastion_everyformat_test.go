package v1

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/secrets"
)

// The main installation's interface for a Bastion serving every format (bastion.md R-BAS-052, R-BAS-057): the tests carry the
// decisions' IDs, since the requirements are proven once the Bastion's side uses the table and the check.

// stillFeed is a feed at one position that never moves.
type stillFeed struct{}

func (stillFeed) Position(context.Context) (string, error) {
	return iface.Cursor{At: time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)}.String(), nil
}

func (stillFeed) Read(context.Context, string, int) (iface.FeedAnswer, error) {
	return iface.FeedAnswer{Entries: []iface.FeedEntry{}}, nil
}

// TestR_BAS_057_TheHandshakeCarriesTheFormatTable: the handshake lists every registered format with routes once, with the
// facts it declares - OCI the registry root, its four replay headers and the bound prefix /v2/, the ClamAV definitions 503,
// every other format nothing and 429, the lists empty rather than null - and leaves out generic, which has no family of its
// own at a Bastion; the table passes the rules the Bastion reads it with. The answer validates against the document; its
// boundRepository is the binding the dispatcher reads for the request's host, absent for an unbound host (R-BAS-057).
func TestR_BAS_057_TheHandshakeCarriesTheFormatTable(t *testing.T) {
	table := BastionFormatTable(formats.List())
	if err := table.Validate(); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, f := range formats.List() {
		id := string(f.Descriptor().ID)
		e := table.Lookup(id)
		_, listed := func() (iface.HandshakeFormat, bool) {
			for _, x := range table {
				if x.ID == id {
					return x, true
				}
			}
			return iface.HandshakeFormat{}, false
		}()
		if listed != (len(f.Routes()) > 0) {
			t.Errorf("%s: listed %v with %d routes", id, listed, len(f.Routes()))
			continue
		}
		seen[id] = listed
		d := f.Descriptor()
		if listed && (e.RegistryRoot != d.RegistryRoot || e.RateLimitedStatus != d.Bastion.RateLimited() || e.ReplayHeaders == nil || e.BoundPrefixes == nil) {
			t.Errorf("%s: %+v", id, e)
		}
	}
	for _, id := range []string{"npm", "nuget", "oci", "deb", "ansible", "clamav"} {
		if !seen[id] {
			t.Errorf("%s is not in the table", id)
		}
	}
	if seen["generic"] {
		t.Error("generic has no routes and no entry")
	}
	ociEntry := table.Lookup("oci")
	if !ociEntry.RegistryRoot || len(ociEntry.ReplayHeaders) != 4 || len(ociEntry.BoundPrefixes) != 1 || ociEntry.BoundPrefixes[0] != "/v2/" || ociEntry.RateLimitedStatus != 429 {
		t.Errorf("oci: %+v", ociEntry)
	}
	if table.Lookup("clamav").RateLimitedStatus != http.StatusServiceUnavailable {
		t.Errorf("clamav: %+v", table.Lookup("clamav"))
	}
	if root, ok := table.RegistryRoot(); !ok || root.ID != "oci" {
		t.Errorf("registry root %+v %v", root, ok)
	}
	raw, _ := iface.Marshal(table)
	if !strings.Contains(string(raw), `{"id":"npm","registryRoot":false,"replayHeaders":[],"rateLimitedStatus":429,"boundPrefixes":[]}`) {
		t.Errorf("npm on the wire: %s", raw)
	}

	val := newValidator(t)
	handshake := func(t *testing.T, svc BastionServices) iface.Handshake {
		t.Helper()
		s := newSetup(t, func(o *Options) { o.Bastion = svc }, nil)
		r := httptest.NewRequest(http.MethodGet, iface.PathHandshake, nil)
		r = r.WithContext(bastionmain.WithRecognition(r.Context(), bastionmain.Recognition{
			Bastion: "partners", BastionID: "0192f1c4-0000-7000-8000-0000000000b1", TokenID: bastionTokenID,
			TokenExpiresAt: time.Date(2027, 11, 2, 0, 0, 0, 0, time.UTC), Route: true, Interface: iface.InterfaceVersion,
		}))
		w := httptest.NewRecorder()
		s.handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		if _, err := val.Check(r, w.Code, w.Header(), w.Body.Bytes()); err != nil {
			t.Fatalf("the answer does not match the document: %v\n%s", err, w.Body)
		}
		var h iface.Handshake
		if err := iface.DecodeStrict(w.Body, &h); err != nil {
			t.Fatal(err)
		}
		return h
	}
	base := BastionServices{Feed: stillFeed{}, AnonymousReads: func(context.Context) bool { return false }, Formats: table}
	t.Run("an unbound host", func(t *testing.T) {
		svc := base
		svc.BoundRepository = func(*http.Request) (qname.Repo, bool, error) { return qname.Repo{}, false, nil }
		h := handshake(t, svc)
		if len(h.Formats) != len(table) || h.BoundRepository != "" {
			t.Fatalf("%+v", h)
		}
	})
	t.Run("a host bound to an OCI remote", func(t *testing.T) {
		svc := base
		svc.BoundRepository = func(*http.Request) (qname.Repo, bool, error) {
			return qname.Repo{Project: "web", Key: "docker-hub"}, true, nil
		}
		if h := handshake(t, svc); h.BoundRepository != "web/docker-hub" {
			t.Fatalf("%+v", h)
		}
	})
	t.Run("a replica without the table answers an empty one, never null", func(t *testing.T) {
		svc := base
		svc.Formats = nil
		if h := handshake(t, svc); h.Formats == nil || len(h.Formats) != 0 {
			t.Fatalf("%+v", h.Formats)
		}
	})
	t.Run("a failed lookup of the binding is the handshake's error", func(t *testing.T) {
		svc := base
		svc.BoundRepository = func(*http.Request) (qname.Repo, bool, error) { return qname.Repo{}, false, errors.New("lookup failed") }
		s := newSetup(t, func(o *Options) { o.Bastion = svc }, nil)
		r := httptest.NewRequest(http.MethodGet, iface.PathHandshake, nil)
		r = r.WithContext(bastionmain.WithRecognition(r.Context(), bastionmain.Recognition{Bastion: "partners", Route: true, Interface: iface.InterfaceVersion}))
		w := httptest.NewRecorder()
		s.handler.ServeHTTP(w, r)
		if w.Code < 500 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	})
}

// patStore knows one personal access token of one user, by its string and by the SHA-256 a registry token binds.
type patStore struct {
	raw  string
	info token.Info
}

func (s patStore) Validate(_ context.Context, raw string) (token.Info, error) {
	if raw != s.raw {
		return token.Info{}, token.ErrUnknown
	}
	return s.info, nil
}

func (s patStore) CheckSecretHash(_ context.Context, hash [32]byte, typ token.Type, _ token.Request) (token.Info, error) {
	if hash != sha256.Sum256([]byte(s.raw)) || typ != s.info.Type {
		return token.Info{}, token.ErrUnknown
	}
	return s.info, nil
}

// noPrincipals knows no principal: no groups, not external.
type noPrincipals struct{}

func (noPrincipals) ByID(context.Context, string) (principal.Principal, error) {
	return principal.Principal{}, principal.ErrNotFound
}

func (noPrincipals) ByName(context.Context, principal.Kind, string) (principal.Principal, error) {
	return principal.Principal{}, principal.ErrNotFound
}

func (noPrincipals) Groups(context.Context, string) ([]principal.GroupRef, error) { return nil, nil }

// TestR_BAS_052_TheCheckPresentsARegistryTokenBelowTheRegistryRoot: the check hands a registry token to the real
// authentication stage on /v2/bastion-check, the one kind of path where the stage verifies it (its MAC, expiry, endpoint and
// the token behind it), and every other credential on /api/bastion-check as before. A registry token's subject is the token
// it was issued for with the registry token's own expiry, and its shape on a repository the token behind it reads in full
// is partial (R-BAS-052); the Quader token presented itself stays full. A registry token signed with another key is invalid.
func TestR_BAS_052_TheCheckPresentsARegistryTokenBelowTheRegistryRoot(t *testing.T) {
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	signer, other := registrySigner(t, 1), registrySigner(t, 2)
	pat, err := token.Generate(token.TypePAT)
	if err != nil {
		t.Fatal(err)
	}
	patExpiry := now.Add(30 * 24 * time.Hour)
	store := patStore{raw: pat, info: token.Info{
		ID: callerTokenID, Type: token.TypePAT, ExpiresAt: &patExpiry,
		Owner: principal.Principal{ID: checkedPrincipalID, Kind: principal.KindUser, Name: "alice", Status: principal.StatusActive},
	}}
	stage := auth.New(auth.Options{Tokens: store, Registry: signer, Principals: noPrincipals{}, Now: func() time.Time { return now }})
	rules := auth.EndpointRules{Name: "default", TokenTypes: []token.Type{token.TypePAT}}
	var paths []string
	cs := newCheckSetup(t, func(r *http.Request) (httpserver.Principal, error) {
		paths = append(paths, r.URL.Path)
		res, err := stage.Authenticate(r, rules)
		if err != nil {
			return httpserver.Principal{}, err
		}
		p := httpserver.Principal{
			Kind: httpserver.KindUser, ID: res.Principal.ID, Name: res.Principal.Name,
			Credential: httpserver.Credential{Method: httpserver.AuthMethod(res.Credential.Method), TokenID: res.Credential.TokenID, ExpiresAt: res.Credential.ExpiresAt},
		}
		if res.Credential.Registry != nil {
			p.Credential.Registry = &httpserver.RegistryGrant{}
		}
		return p, nil
	})
	issue := func(s *registrytoken.Signer) string {
		raw, err := s.Issue(registrytoken.Claims{
			PrincipalKind: "user", PrincipalID: checkedPrincipalID, Endpoint: "default", IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute),
			Binding: registrytoken.Binding{TokenID: callerTokenID, TokenType: string(token.TypePAT), SecretSHA256: sha256.Sum256([]byte(pat))},
			Grant:   registrytoken.Grant{Entries: []registrytoken.Entry{{Repo: qname.Repo{Project: "it", Key: "npm-all"}, Name: "library/nginx", Actions: registrytoken.Pull}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	body := func(credential string) string {
		return `{"credential":"` + credential + `","clientAddress":"203.0.113.7","repositories":["it/npm-all","it/npm-local","it/npm-partner"]}`
	}

	ans := decodeCheck(t, cs.post(iface.PathCheck, body(issue(signer))))
	if paths[len(paths)-1] != "/v2/bastion-check" {
		t.Errorf("the registry token was presented on %s", paths[len(paths)-1])
	}
	if s := ans.Subject; s.Kind != iface.SubjectToken || s.TokenID != callerTokenID || s.PrincipalID != checkedPrincipalID || s.ExpiresAt == nil || !s.ExpiresAt.Equal(now.Add(5*time.Minute)) {
		t.Errorf("subject %+v: the token behind the registry token, with the registry token's expiry", s)
	}
	for k, want := range map[string]iface.RepositoryRead{
		"it/npm-all": {Read: iface.ReadPartial}, "it/npm-local": {Read: iface.ReadPartial}, "it/npm-partner": {Read: iface.ReadNone, Status: 404},
	} {
		if ans.Repositories[k] != want {
			t.Errorf("registry token on %s: %+v, want %+v", k, ans.Repositories[k], want)
		}
	}

	ans = decodeCheck(t, cs.post(iface.PathCheck, body(pat)))
	if paths[len(paths)-1] != "/api/bastion-check" || ans.Subject.ExpiresAt == nil || !ans.Subject.ExpiresAt.Equal(patExpiry) || ans.Repositories["it/npm-all"].Read != iface.ReadFull {
		t.Errorf("the Quader token itself on %s: %+v", paths[len(paths)-1], ans)
	}

	if ans := decodeCheck(t, cs.post(iface.PathCheck, body(issue(other)))); ans.Subject.Kind != iface.SubjectInvalid || len(ans.Repositories) != 0 {
		t.Errorf("a registry token of another key: %+v", ans)
	}
}

// registrySigner is a token service's signer over a key ring of one key whose bytes are seed.
func registrySigner(t *testing.T, seed byte) *registrytoken.Signer {
	t.Helper()
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = seed
	}
	p := filepath.Join(t.TempDir(), "k1.key")
	if err := os.WriteFile(p, []byte(base64.StdEncoding.EncodeToString(raw)), 0o600); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.Load(config.Secrets{EncryptionKeyFiles: []string{p}, ActiveKeyID: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := registrytoken.NewSigner(ring, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
