package iface_test

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/secrets"
)

// The interface's part of a Bastion serving every format (bastion.md R-BAS-052, R-BAS-057, R-BAS-059): the tests carry the
// decisions' IDs, since the requirements are proven by the two sides that use these pieces.

// issuer is the main installation's token service in miniature: a signer over a key ring of one key the Bastion never holds.
func issuer(t *testing.T) *registrytoken.Signer {
	t.Helper()
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i + 1)
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

// tokens returns a registry token of a user and one of the anonymous principal, as the token service issues them.
func tokens(t *testing.T) (user, anonymous string) {
	t.Helper()
	s := issuer(t)
	now := time.Now().Truncate(time.Second)
	grant := registrytoken.Grant{Entries: []registrytoken.Entry{{Repo: qname.Repo{Project: "web", Key: "docker"}, Name: "library/nginx", Actions: registrytoken.Pull}}}
	user, err := s.Issue(registrytoken.Claims{
		PrincipalKind: "user", PrincipalID: "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10",
		Binding:  registrytoken.Binding{TokenID: "0192f0c4-0000-7000-8000-000000000001", TokenType: "pat", SecretSHA256: sha256.Sum256([]byte("qdr_pat_x"))},
		Endpoint: "default", IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute), Grant: grant,
	})
	if err != nil {
		t.Fatal(err)
	}
	anonymous, err = s.Issue(registrytoken.Claims{Anonymous: true, Endpoint: "default", IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute), Grant: grant})
	if err != nil {
		t.Fatal(err)
	}
	return user, anonymous
}

// forged builds a registry token of the form with the given payload and a MAC of 32 zero bytes.
func forged(payload string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return iface.RegistryTokenPrefix + enc([]byte(payload)) + "." + enc(make([]byte, 32))
}

// TestR_BAS_052_ARegistryTokenIsCheckedByItsFormOnly: a Bastion holds no key, so it checks a registry token by its form - the
// prefix, base64url, one dot, base64url, at most 4 KiB - as the main installation's token service writes it (R-BAS-052).
func TestR_BAS_052_ARegistryTokenIsCheckedByItsFormOnly(t *testing.T) {
	if iface.RegistryTokenPrefix != registrytoken.Prefix || iface.MaxRegistryTokenBytes != registrytoken.MaxLength {
		t.Fatalf("the interface's form (%q, %d) is not the token service's (%q, %d)", iface.RegistryTokenPrefix, iface.MaxRegistryTokenBytes, registrytoken.Prefix, registrytoken.MaxLength)
	}
	user, anonymous := tokens(t)
	for name, tok := range map[string]string{"a user's": user, "the anonymous principal's": anonymous, "a forged one of the form": forged("1:k1:{}")} {
		if !iface.RegistryTokenWellFormed(tok) {
			t.Errorf("%s token %q is refused", name, tok)
		}
	}
	for name, tok := range map[string]string{
		"two characters outside base64url": iface.RegistryTokenPrefix + "!!",
		"5 KiB":                            forged("1:k1:" + strings.Repeat("x", 5<<10)),
		"no dot":                           strings.Replace(user, ".", "", 1),
		"two dots":                         user + ".AA",
		"an empty payload":                 iface.RegistryTokenPrefix + "." + strings.SplitN(user, ".", 2)[1],
		"an empty MAC":                     strings.SplitN(user, ".", 2)[0] + ".",
		"padding":                          user + "=",
		"another prefix":                   "qdr_pat_" + strings.TrimPrefix(user, iface.RegistryTokenPrefix),
		"the prefix alone":                 iface.RegistryTokenPrefix,
	} {
		if iface.RegistryTokenWellFormed(tok) {
			t.Errorf("a token with %s is accepted: %q", name, tok)
		}
	}
}

// TestR_BAS_052_AnAnonymousRegistryTokenReadsAsTheAnonymousCaller: the unverified claims of the token the token service issues to
// a request without a credential name the anonymous principal, and nothing else reads so (R-BAS-052).
func TestR_BAS_052_AnAnonymousRegistryTokenReadsAsTheAnonymousCaller(t *testing.T) {
	user, anonymous := tokens(t)
	if !iface.RegistryTokenAnonymous(anonymous) {
		t.Errorf("the anonymous principal's token does not read as anonymous: %q", anonymous)
	}
	for name, tok := range map[string]string{
		"a user's token":                  user,
		"a payload of another version":    forged(`3:k1:{"a":true}`),
		"version 2 without a sealed part": forged(`2:k1:{"a":true}`),
		"version 2 without a key ID":      forged(`2::{"a":true}:c2VhbGVk`),
		"claims that are no JSON":         forged(`1:k1:a=true`),
		"no key ID and claims":            forged(`1:{"a":true}`),
		"claims that say false":           forged(`1:k1:{"a":false,"e":"default"}`),
		"a value outside the form":        iface.RegistryTokenPrefix + "!!",
		"a token of another type":         "qdr_pat_abc",
		"a payload that is no token form": forged("garbage"),
	} {
		if iface.RegistryTokenAnonymous(tok) {
			t.Errorf("%s reads as anonymous: %q", name, tok)
		}
	}
	if !iface.RegistryTokenAnonymous(forged(`1:k1:{"a":true,"e":"default","new":1}`)) {
		t.Error("a member the interface does not know hides the anonymous claim")
	}
}

// TestR_BAS_057_TheHandshakeCarriesTheFormatTable: the handshake's format table and bound repository travel and decode strictly,
// a format the table does not name gets the defaults, and the Bastion refuses a table it cannot rely on (R-BAS-057).
func TestR_BAS_057_TheHandshakeCarriesTheFormatTable(t *testing.T) {
	table := iface.HandshakeFormats{
		{
			ID: "npm", ReplayHeaders: []string{}, RateLimitedStatus: http.StatusTooManyRequests, BoundPrefixes: []string{},
			ErrorShape: iface.ErrorShapeNPM, ReadWithBody: []iface.BodyRoute{{Method: http.MethodPost, Path: "/api/npm/{project}/{repo}/-/npm/v1/security/advisories/bulk"}},
		},
		{
			ID: "oci", RegistryRoot: true, ReplayHeaders: []string{"Docker-Content-Digest", "Link"}, RateLimitedStatus: http.StatusTooManyRequests, BoundPrefixes: []string{"/v2/"},
			ErrorShape: iface.ErrorShapeDistribution, ReadWithBody: []iface.BodyRoute{{Method: http.MethodPost, Path: "/v2/_token"}},
		},
		{ID: "clamav", ReplayHeaders: []string{}, RateLimitedStatus: http.StatusServiceUnavailable, BoundPrefixes: []string{}, ErrorShape: iface.ErrorShapeProblem, ReadWithBody: []iface.BodyRoute{}},
	}
	b, err := iface.Marshal(iface.Handshake{InterfaceVersion: iface.InterfaceVersion, Formats: table, BoundRepository: "web/docker-hub"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"formats":[{"id":"npm","registryRoot":false,"replayHeaders":[],"rateLimitedStatus":429,"retryAfterMaxSeconds":0,"boundPrefixes":[],"errorShape":"npm","readWithBody":[{"method":"POST","path":"/api/npm/{project}/{repo}/-/npm/v1/security/advisories/bulk"}]}`, `"boundRepository":"web/docker-hub"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("the handshake %s lacks %s", b, want)
		}
	}
	var back iface.Handshake
	if err := iface.DecodeStrict(strings.NewReader(string(b)), &back); err != nil || back.BoundRepository != "web/docker-hub" || len(back.Formats) != 3 {
		t.Fatalf("decode: %+v %v", back, err)
	}
	if err := back.Formats.Validate(); err != nil {
		t.Fatal(err)
	}
	unbound, _ := iface.Marshal(iface.Handshake{Formats: table})
	if strings.Contains(string(unbound), "boundRepository") {
		t.Errorf("a host bound to no repository names one: %s", unbound)
	}
	if root, ok := back.Formats.RegistryRoot(); !ok || root.ID != "oci" {
		t.Errorf("RegistryRoot = %+v %v", root, ok)
	}
	if got := back.Formats.Lookup("clamav"); got.RateLimitedStatus != http.StatusServiceUnavailable {
		t.Errorf("Lookup(clamav) = %+v", got)
	}
	if got := back.Formats.Lookup("terraform"); got.ID != "terraform" || got.RateLimitedStatus != http.StatusTooManyRequests || len(got.ReplayHeaders) != 0 || got.RegistryRoot ||
		got.ErrorShape != iface.ErrorShapeProblem || len(got.ReadWithBody) != 0 {
		t.Errorf("a format the table does not name = %+v, want the defaults", got)
	}
	// The longest wait a format's clients are told (R-BAS-058): the format's cap, the true wait without one.
	capped := iface.HandshakeFormat{ID: "nuget", RateLimitedStatus: 429, RetryAfterMaxSeconds: 30}
	if capped.CapRetryAfter(15*time.Minute) != 30*time.Second || capped.CapRetryAfter(7*time.Second) != 7*time.Second ||
		back.Formats.Lookup("terraform").CapRetryAfter(15*time.Minute) != 15*time.Minute {
		t.Error("CapRetryAfter does not tell the format's cap, or the true wait without one")
	}
	if _, ok := (iface.HandshakeFormats{table[0]}).RegistryRoot(); ok {
		t.Error("a table without the registry root names one")
	}
	for name, bad := range map[string]iface.HandshakeFormats{
		"an entry without an ID":           {{RateLimitedStatus: 429, ErrorShape: "problem"}},
		"a format twice":                   {table[0], table[0]},
		"two registry roots":               {table[1], {ID: "other", RegistryRoot: true, RateLimitedStatus: 429, ErrorShape: "problem"}},
		"a rate-limit status of 418":       {{ID: "x", RateLimitedStatus: http.StatusTeapot, ErrorShape: "problem"}},
		"no rate-limit status":             {{ID: "x", ErrorShape: "problem"}},
		"a negative longest wait":          {{ID: "x", RateLimitedStatus: 429, RetryAfterMaxSeconds: -1, ErrorShape: "problem"}},
		"a header that is not canonical":   {{ID: "x", RateLimitedStatus: 429, ReplayHeaders: []string{"docker-content-digest"}, ErrorShape: "problem"}},
		"a header of Quader's":             {{ID: "x", RateLimitedStatus: 429, ReplayHeaders: []string{"Quader-Bastion-Class"}, ErrorShape: "problem"}},
		"a bound prefix without the slash": {{ID: "x", RateLimitedStatus: 429, BoundPrefixes: []string{"v2/"}, ErrorShape: "problem"}},
		"no error shape":                   {{ID: "x", RateLimitedStatus: 429}},
		"a sixth error shape":              {{ID: "x", RateLimitedStatus: 429, ErrorShape: "html"}},
		"a read route with a body by GET":  {{ID: "x", RateLimitedStatus: 429, ErrorShape: "problem", ReadWithBody: []iface.BodyRoute{{Method: "GET", Path: "/a"}}}},
		"a body route with a dot segment":  {{ID: "x", RateLimitedStatus: 429, ErrorShape: "problem", ReadWithBody: []iface.BodyRoute{{Method: "POST", Path: "/a/../b"}}}},
		"a body route with a tail":         {{ID: "x", RateLimitedStatus: 429, ErrorShape: "problem", ReadWithBody: []iface.BodyRoute{{Method: "POST", Path: "/a/{rest...}"}}}},
		"a body route from no root":        {{ID: "x", RateLimitedStatus: 429, ErrorShape: "problem", ReadWithBody: []iface.BodyRoute{{Method: "POST", Path: "a"}}}},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("a table with %s is accepted", name)
		}
	}
}

// TestR_BAS_059_TheMarkingSaysWhenItCountedADownload: Quader-Bastion-Counted: download travels as a member of the marking, a
// marking without it counted nothing, any other value is malformed, and the strip removes it with the rest (R-BAS-059).
func TestR_BAS_059_TheMarkingSaysWhenItCountedADownload(t *testing.T) {
	h := http.Header{}
	iface.Marking{
		Class: iface.ClassShared, Repository: "web/docker", Read: iface.ReadFull, Subject: iface.Subject{Kind: iface.SubjectAnonymous},
		Interface: iface.InterfaceVersion, Counted: true,
	}.Set(h)
	if got := h.Get(iface.HeaderCounted); got != iface.CountedDownload {
		t.Fatalf("%s = %q", iface.HeaderCounted, got)
	}
	m, ok, err := iface.ParseMarking(h)
	if err != nil || !ok || !m.Counted {
		t.Fatalf("ParseMarking: %+v %v %v", m, ok, err)
	}
	plain := http.Header{}
	iface.Marking{Class: iface.ClassArtifact, Subject: iface.Subject{Kind: iface.SubjectAnonymous}, Interface: iface.InterfaceVersion}.Set(plain)
	if plain.Get(iface.HeaderCounted) != "" {
		t.Fatalf("a marking that counted nothing sets %s", iface.HeaderCounted)
	}
	if m, _, _ := iface.ParseMarking(plain); m.Counted {
		t.Fatal("a marking without the header reads as counted")
	}
	for _, v := range [][]string{{"yes"}, {""}, {iface.CountedDownload, iface.CountedDownload}} {
		bad := h.Clone()
		bad[iface.HeaderCounted] = v
		if _, _, err := iface.ParseMarking(bad); !errors.Is(err, iface.ErrMarking) {
			t.Errorf("%s %q: %v, want ErrMarking", iface.HeaderCounted, v, err)
		}
	}
	iface.StripMarking(h)
	if h.Get(iface.HeaderCounted) != "" {
		t.Fatal("the strip leaves the counted marking")
	}
}

// TestR_BAS_095_AReadRouteWithABodyIsMatchedAsTheRouterMatchesIt: a request matches a declared read route that takes a body by
// its method and its escaped path, a placeholder standing for one non-empty segment and a literal for itself, so a spelling the
// table does not name is a write (R-BAS-095).
func TestR_BAS_095_AReadRouteWithABodyIsMatchedAsTheRouterMatchesIt(t *testing.T) {
	npm := iface.HandshakeFormat{ReadWithBody: []iface.BodyRoute{{Method: http.MethodPost, Path: "/api/npm/{project}/{repo}/-/npm/v1/security/advisories/bulk"}}}
	for path, want := range map[string]bool{
		"/api/npm/web/npm-all/-/npm/v1/security/advisories/bulk":   true,
		"/api/npm/web/npm-all/%2D/npm/v1/security/advisories/bulk": false,
		"/api/npm/web//-/npm/v1/security/advisories/bulk":          false,
		"/api/npm/web/npm-all/-/npm/v1/security/advisories/bulk/x": false,
		"/api/npm/web/npm-all/lodash":                              false,
	} {
		if got := npm.ReadsWithBody(http.MethodPost, path); got != want {
			t.Errorf("POST %s: %v, want %v", path, got, want)
		}
	}
	if npm.ReadsWithBody(http.MethodPut, "/api/npm/web/npm-all/-/npm/v1/security/advisories/bulk") {
		t.Error("a PUT on the path of a POST reads with a body")
	}
}

// TestR_BAS_052_AVersion2RegistryTokenReadsOnlyItsReadableClaims: payload version 2 (Q-BAS-339) is read for a, exp and g from its
// readable part; the sealed part is never read, whatever it holds.
func TestR_BAS_052_AVersion2RegistryTokenReadsOnlyItsReadableClaims(t *testing.T) {
	exp := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	v2 := forged(`2:9f3a:{"a":true,"exp":` + strconv.FormatInt(exp.Unix(), 10) + `,"g":[{"r":"web/docker","n":"nginx","a":1}]}:` + "\x00:{\"p\":\"x\"}")
	if !iface.RegistryTokenAnonymous(v2) {
		t.Error("a version 2 anonymous token does not read as anonymous")
	}
	if got, ok := iface.RegistryTokenExpiry(v2); !ok || !got.Equal(exp) {
		t.Errorf("expiry = %v %v", got, ok)
	}
	if g, ok := iface.RegistryTokenGrant(v2); !ok || !g.Pulls("web/docker", "nginx") {
		t.Errorf("grant = %+v %v", g, ok)
	}
	if iface.RegistryTokenAnonymous(forged(`2:9f3a:{"x":1}:{"a":true}`)) {
		t.Error("a claim of the sealed part is read")
	}
}
