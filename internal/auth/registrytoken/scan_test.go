package registrytoken

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

const manifestDigest = "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func scanSigner(t *testing.T) *Signer {
	t.Helper()
	s, err := signer(t, "k1", keyFile(t, t.TempDir(), "k1", 1)).ForScans(10 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func scanClaims() Claims {
	return Claims{
		System: ScanSubject, ScanID: "AAAAAAAAAAAAAAAAAAAAAA", Digest: manifestDigest, Endpoint: ScanEndpoint,
		IssuedAt: t0, ExpiresAt: t0.Add(10 * time.Minute),
		Grant: Grant{Entries: []Entry{{Repo: webRepo, Name: "app", Actions: Pull}}},
	}
}

func TestR_OCI_037_AScanTokenIsIssuedAndVerifiedWithItsClaims(t *testing.T) {
	s := scanSigner(t)
	tok, sid, err := s.IssueScan(webRepo, "app", manifestDigest, t0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sid) != 22 {
		t.Fatalf("scan ID %q is not 128 bits in base64url", sid)
	}
	c, err := s.Verify(tok.Reveal(), ScanEndpoint, t0.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if c.System != ScanSubject || c.ScanID != sid || c.Digest != manifestDigest || c.PrincipalID != "" || c.Anonymous ||
		len(c.Grant.Entries) != 1 || c.Grant.Entries[0] != (Entry{Repo: webRepo, Name: "app", Actions: Pull}) || c.Grant.Catalog {
		t.Fatalf("claims %+v", c)
	}
	if got := c.ExpiresAt.Sub(c.IssuedAt); got != 10*time.Minute {
		t.Fatalf("lifetime %s, want pull_timeout", got)
	}

	t.Run("it lives pull_timeout, checked against that bound and not oci.token_lifetime", func(t *testing.T) {
		if _, err := s.Verify(tok.Reveal(), ScanEndpoint, t0.Add(10*time.Minute)); !errors.Is(err, ErrExpired) {
			t.Fatalf("at its end: %v", err)
		}
		short, err := signer(t, "k1", keyFile(t, t.TempDir(), "k1", 1)).ForScans(time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := short.Verify(tok.Reveal(), ScanEndpoint, t0.Add(time.Second)); !errors.Is(err, ErrLifetime) {
			t.Fatalf("a span above the verifier's pull_timeout: %v", err)
		}
		plain := signer(t, "k1", keyFile(t, t.TempDir(), "k1", 1))
		if _, err := plain.Verify(tok.Reveal(), ScanEndpoint, t0.Add(time.Second)); !errors.Is(err, ErrLifetime) {
			t.Fatalf("a replica that accepts no scan token: %v", err)
		}
		if _, _, err := plain.IssueScan(webRepo, "app", manifestDigest, t0); !errors.Is(err, ErrNoScans) {
			t.Fatalf("a signer without a scan lifetime issued one: %v", err)
		}
		for _, d := range []time.Duration{30 * time.Second, 2 * time.Hour} {
			if _, err := plain.ForScans(d); err == nil {
				t.Errorf("pull_timeout %s accepted", d)
			}
		}
	})
	t.Run("R-ARCH-037 it is refused on every endpoint but scan", func(t *testing.T) {
		for _, ep := range []string{"default", "customers"} {
			if _, err := s.Verify(tok.Reveal(), ep, t0.Add(time.Second)); !errors.Is(err, ErrEndpoint) {
				t.Errorf("%s: %v", ep, err)
			}
		}
	})
	t.Run("it renders nothing of itself", func(t *testing.T) {
		var log strings.Builder
		slog.New(slog.NewTextHandler(&log, nil)).Info("call", "token", tok)
		b, _ := json.Marshal(map[string]any{"token": tok})
		for _, out := range []string{tok.String(), fmt.Sprintf("%v %+v %#v %s", tok, tok, tok, tok), log.String(), string(b)} {
			if strings.Contains(out, Prefix) || !strings.Contains(out, "[REDACTED]") {
				t.Errorf("rendered: %s", out)
			}
		}
	})
}

func TestR_OCI_037_AScanTokenOutsideItsRuleIsRefusedAtIssueAndAtVerification(t *testing.T) {
	s := scanSigner(t)
	cases := map[string]func(c *Claims){
		"two entries": func(c *Claims) {
			c.Grant.Entries = append(c.Grant.Entries, Entry{Repo: webRepo, Name: "other", Actions: Pull})
		},
		"push":             func(c *Claims) { c.Grant.Entries[0].Actions = Pull | Push },
		"the catalog":      func(c *Claims) { c.Grant.Catalog = true },
		"a principal":      func(c *Claims) { c.PrincipalKind, c.PrincipalID = "user", "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10" },
		"a binding":        func(c *Claims) { c.Binding = Binding{TokenID: "t", TokenType: "pat"} },
		"anonymous":        func(c *Claims) { c.Anonymous = true },
		"no digest":        func(c *Claims) { c.Digest = "" },
		"a tag as digest":  func(c *Claims) { c.Digest = "latest" },
		"upper-case hex":   func(c *Claims) { c.Digest = strings.ToUpper(manifestDigest) },
		"no scan ID":       func(c *Claims) { c.ScanID = "" },
		"the endpoint":     func(c *Claims) { c.Endpoint = "default" },
		"another subject":  func(c *Claims) { c.System = "image-database" },
		"a span too long":  func(c *Claims) { c.ExpiresAt = c.IssuedAt.Add(11 * time.Minute) },
		"no image name":    func(c *Claims) { c.Grant.Entries[0].Name = "" },
		"no grant entries": func(c *Claims) { c.Grant.Entries = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := scanClaims()
			mutate(&c)
			if _, err := s.Issue(c); err == nil {
				t.Fatal("issued")
			}
			// The same claims signed by a holder of the key ring that skipped the check: refused at verification.
			raw := forge(t, s, c)
			if _, err := s.Verify(raw, ScanEndpoint, t0.Add(time.Second)); err == nil {
				t.Fatal("verified")
			}
		})
	}
	t.Run("a client's registry token with d or sid is malformed", func(t *testing.T) {
		for name, mutate := range map[string]func(c *Claims){
			"d":   func(c *Claims) { c.Digest = manifestDigest },
			"sid": func(c *Claims) { c.ScanID = "AAAAAAAAAAAAAAAAAAAAAA" },
		} {
			c := patClaims()
			mutate(&c)
			if _, err := s.Issue(c); !errors.Is(err, ErrMalformed) {
				t.Errorf("%s issued: %v", name, err)
			}
			if _, err := s.Verify(forge(t, s, c), "default", t0.Add(time.Second)); !errors.Is(err, ErrMalformed) {
				t.Errorf("%s verified: %v", name, err)
			}
		}
	})
}

// forge signs claims with the signer's active key without the checks of Issue: what a holder of the key ring could make (H-579).
func forge(t *testing.T, s *Signer, c Claims) string {
	t.Helper()
	kid, key := s.keys.Active()
	w := wire{
		Anonymous: c.Anonymous, Kind: c.PrincipalKind, Principal: c.PrincipalID, Endpoint: c.Endpoint,
		IssuedAt: c.IssuedAt.Unix(), ExpiresAt: c.ExpiresAt.Unix(), Catalog: c.Grant.Catalog,
		System: c.System, ScanID: c.ScanID, Digest: c.Digest, TokenID: c.Binding.TokenID, TokenType: c.Binding.TokenType,
	}
	if !c.Repo.IsZero() {
		w.Repo = c.Repo.String()
	}
	if c.Binding.SecretSHA256 != ([32]byte{}) {
		w.TokenHash = b64(c.Binding.SecretSHA256[:])
	}
	for _, e := range c.Grant.Entries {
		w.Grant = append(w.Grant, wireEntry{Repo: e.Repo.String(), Name: e.Name, Actions: e.Actions})
	}
	body, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	signed := Prefix + b64([]byte(payloadVersion+":"+kid+":"+string(body)))
	return signed + "." + b64(mac(key, signed))
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
