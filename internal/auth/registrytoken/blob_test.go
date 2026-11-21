package registrytoken

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/qname"
)

const blobDigest = "sha256:" + "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

var npmRepo = qname.Repo{Project: "web", Key: "npm-local"}

func blobSigner(t *testing.T) *Signer {
	t.Helper()
	s, err := signer(t, "k1", keyFile(t, t.TempDir(), "k1", 1)).ForBlobs(10 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func blobClaims() Claims {
	return Claims{
		System: BlobSubject, ScanID: "AAAAAAAAAAAAAAAAAAAAAA", Digest: blobDigest, Repo: npmRepo, Endpoint: ScanEndpoint,
		IssuedAt: t0, ExpiresAt: t0.Add(10 * time.Minute),
	}
}

// R-MAL-006, Q-MAL-26: a blob token names the subject malware-scanner-clamav, the endpoint scan, one digest, one repository and a scan ID,
// and no grant entry; it lives malware.read_timeout, checked against that bound alone, and is refused on every other endpoint and once
// it has expired.
func TestR_MAL_006_ABlobTokenIsIssuedAndVerifiedWithItsClaims(t *testing.T) {
	s := blobSigner(t)
	tok, sid, err := s.IssueBlob(npmRepo, blobDigest, t0)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Verify(tok.Reveal(), ScanEndpoint, t0.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsBlob() || c.System != BlobSubject || c.ScanID != sid || len(sid) != 22 || c.Digest != blobDigest || c.Repo != npmRepo ||
		c.PrincipalID != "" || c.Anonymous || len(c.Grant.Entries) != 0 || c.Grant.Catalog {
		t.Fatalf("claims %+v", c)
	}
	if got := c.ExpiresAt.Sub(c.IssuedAt); got != 10*time.Minute {
		t.Fatalf("lifetime %s, want read_timeout", got)
	}
	if strings.Contains(tok.String(), Prefix) {
		t.Fatal("the token renders itself")
	}

	t.Run("one past its lifetime and one of another endpoint are refused", func(t *testing.T) {
		if _, err := s.Verify(tok.Reveal(), ScanEndpoint, t0.Add(10*time.Minute)); !errors.Is(err, ErrExpired) {
			t.Fatalf("at its end: %v", err)
		}
		for _, ep := range []string{"default", "customers"} {
			if _, err := s.Verify(tok.Reveal(), ep, t0.Add(time.Second)); !errors.Is(err, ErrEndpoint) {
				t.Errorf("%s: %v", ep, err)
			}
		}
	})
	t.Run("its span is checked against read_timeout, not pull_timeout or oci.token_lifetime", func(t *testing.T) {
		short, err := signer(t, "k1", keyFile(t, t.TempDir(), "k1", 1)).ForBlobs(time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := short.Verify(tok.Reveal(), ScanEndpoint, t0.Add(time.Second)); !errors.Is(err, ErrLifetime) {
			t.Fatalf("a span above the verifier's read_timeout: %v", err)
		}
		images, err := signer(t, "k1", keyFile(t, t.TempDir(), "k1", 1)).ForScans(time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := images.Verify(tok.Reveal(), ScanEndpoint, t0.Add(time.Second)); !errors.Is(err, ErrLifetime) {
			t.Fatalf("a replica that accepts scan tokens of images only: %v", err)
		}
		if _, _, err := images.IssueBlob(npmRepo, blobDigest, t0); !errors.Is(err, ErrNoBlobs) {
			t.Fatalf("a signer without a blob lifetime issued one: %v", err)
		}
		for _, d := range []time.Duration{30 * time.Second, 2 * time.Hour} {
			if _, err := images.ForBlobs(d); err == nil {
				t.Errorf("read_timeout %s accepted", d)
			}
		}
	})
	t.Run("a signer for both kinds keeps both bounds", func(t *testing.T) {
		both, err := blobSigner(t).ForScans(5 * time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := both.Verify(forge(t, both, blobClaims()), ScanEndpoint, t0.Add(time.Second)); err != nil {
			t.Fatalf("the blob token: %v", err)
		}
		img := scanClaims()
		img.ExpiresAt = img.IssuedAt.Add(5 * time.Minute)
		if _, err := both.Verify(forge(t, both, img), ScanEndpoint, t0.Add(time.Second)); err != nil {
			t.Fatalf("the image's scan token: %v", err)
		}
	})
}

// Q-MAL-26: a blob token outside its rule is refused when it is issued and, signed by a holder of the key ring that skipped the
// check, when it is verified; an image's scan token cannot carry a repository of its own.
func TestR_MAL_006_ABlobTokenOutsideItsRuleIsRefused(t *testing.T) {
	s, err := blobSigner(t).ForScans(10 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(c *Claims){
		"a grant entry":       func(c *Claims) { c.Grant.Entries = []Entry{{Repo: npmRepo, Name: "x", Actions: Pull}} },
		"the catalog":         func(c *Claims) { c.Grant.Catalog = true },
		"no repository":       func(c *Claims) { c.Repo = qname.Repo{} },
		"no digest":           func(c *Claims) { c.Digest = "" },
		"63 hex digits":       func(c *Claims) { c.Digest = blobDigest[:len(blobDigest)-1] },
		"no scan ID":          func(c *Claims) { c.ScanID = "" },
		"the endpoint":        func(c *Claims) { c.Endpoint = "default" },
		"a principal":         func(c *Claims) { c.PrincipalKind, c.PrincipalID = "user", "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10" },
		"anonymous":           func(c *Claims) { c.Anonymous = true },
		"a span too long":     func(c *Claims) { c.ExpiresAt = c.IssuedAt.Add(11 * time.Minute) },
		"an image's with rp":  func(c *Claims) { *c = scanClaims(); c.Repo = npmRepo },
		"a client's with rp":  func(c *Claims) { *c = patClaims(); c.Repo = npmRepo },
		"an unknown subject":  func(c *Claims) { c.System = "malware-database" },
		"the image's subject": func(c *Claims) { c.System = ScanSubject },
	} {
		t.Run(name, func(t *testing.T) {
			c := blobClaims()
			mutate(&c)
			if _, err := s.Issue(c); err == nil {
				t.Fatal("issued")
			}
			ep := c.Endpoint
			if ep == "" {
				ep = ScanEndpoint
			}
			if _, err := s.Verify(forge(t, s, c), ep, t0.Add(time.Second)); err == nil {
				t.Fatal("verified")
			}
		})
	}
}
