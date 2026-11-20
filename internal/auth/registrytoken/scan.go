package registrytoken

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/qname"
)

// Scan tokens (R-OCI-037, ADR-213): the registry token of the image scanning's pull. A worker signs one in process for each call
// of the scanner, with the key of every registry token; it names the system subject image-scanner, the scan's ID, the endpoint
// scan, one grant entry of the repository and image name with pull, and the manifest's digest. No quader token stands behind it,
// so its verification costs no query, and it is good on the scan listener only, for that one manifest and the blobs of its name,
// for vulnerabilities.images.pull_timeout.

// ScanSubject is the system subject of a scan token (R-AUTHZ-002): the claim s, and the name of the principal of kind system the
// stage of the scan listener yields.
const ScanSubject = "image-scanner"

// ScanEndpoint is the endpoint claim of a scan token: the reserved endpoint of the scan listener (R-EP-001, R-ARCH-037).
const ScanEndpoint = "scan"

// scanIDBytes is the size of a scan's ID: 128 random bits (R-OCI-037).
const scanIDBytes = 16

// ErrNoScans is the refusal of a scan token by a [Signer] that has no scan lifetime: a replica that does not issue them.
var ErrNoScans = errors.New("registrytoken: this signer issues and accepts no scan token")

// ForScans returns a signer that issues and verifies scan tokens with the lifetime pullTimeout
// (vulnerabilities.images.pull_timeout, 1 minute to 1 hour, Q-OCI-229) besides the registry tokens of s. A scan token is checked
// against this bound and never against oci.token_lifetime.
func (s *Signer) ForScans(pullTimeout time.Duration) (*Signer, error) {
	if pullTimeout < time.Minute || pullTimeout > MaxLifetime {
		return nil, fmt.Errorf("registrytoken: the scan lifetime %s is not between 1m and %s", pullTimeout, MaxLifetime)
	}
	out := *s
	out.scanLifetime = pullTimeout
	return &out, nil
}

// boundOf is the longest span the claims may have: the scan lifetime for a scan token of an image, the blob lifetime for a blob
// token, oci.token_lifetime for every other.
func (s *Signer) boundOf(c Claims) time.Duration {
	switch c.System {
	case "":
		return s.lifetime
	case BlobSubject:
		return s.blobLifetime
	}
	return s.scanLifetime
}

// ScanToken is a scan token as the worker holds it, of an image or of a blob (R-OCI-037 "Where it is never written", R-MAL-006):
// String, GoString, LogValue and MarshalJSON render nothing of it, so it reaches no log line, no error text and no document by
// accident. The call's body takes it through [ScanToken.Reveal], explicitly.
type ScanToken struct{ v *string }

const redacted = "[REDACTED]"

// String implements fmt.Stringer and renders nothing of the token.
func (ScanToken) String() string { return redacted }

// GoString implements fmt.GoStringer.
func (ScanToken) GoString() string { return redacted }

// LogValue implements slog.LogValuer.
func (ScanToken) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalJSON renders nothing of the token either: the body of the call reveals it on purpose.
func (ScanToken) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

// Reveal returns the token, for the one place that sends it: the body of the call (R-VULN-041).
func (t ScanToken) Reveal() string {
	if t.v == nil {
		return ""
	}
	return *t.v
}

// IsZero reports whether the token holds nothing.
func (t ScanToken) IsZero() bool { return t.v == nil }

// IssueScan signs the scan token of one manifest (Q-OCI-226): repo is the repository whose row is due (local or remote, never a
// cache view or a virtual repository), name the image and digest the manifest's, sha256:<64 hex>. It returns the token and the
// scan's ID, which the worker's log line, the scanner's and the scan listener's request log share.
func (s *Signer) IssueScan(repo qname.Repo, name, digest string, now time.Time) (ScanToken, string, error) {
	if s.scanLifetime == 0 {
		return ScanToken{}, "", ErrNoScans
	}
	raw := make([]byte, scanIDBytes)
	if _, err := rand.Read(raw); err != nil {
		return ScanToken{}, "", fmt.Errorf("registrytoken: a scan ID: %w", err)
	}
	sid := base64.RawURLEncoding.EncodeToString(raw)
	now = now.Truncate(time.Second)
	tok, err := s.Issue(Claims{
		System: ScanSubject, ScanID: sid, Digest: digest, Endpoint: ScanEndpoint,
		IssuedAt: now, ExpiresAt: now.Add(s.scanLifetime),
		Grant: Grant{Entries: []Entry{{Repo: repo, Name: name, Actions: Pull}}},
	})
	if err != nil {
		return ScanToken{}, "", err
	}
	return ScanToken{v: &tok}, sid, nil
}

// checkScan holds the rule both sides apply to a scan token (R-OCI-037 "Claims"): the system subject image-scanner, no principal,
// no quader token, not anonymous, exactly one entry with pull alone, a digest, a scan ID, no catalog and the endpoint scan.
func (c Claims) checkScan() error {
	switch {
	case c.Anonymous || c.PrincipalID != "" || c.PrincipalKind != "" || c.Binding != (Binding{}):
		return fmt.Errorf("%w: a scan token names no principal and no Quader token", ErrMalformed)
	case c.Endpoint != ScanEndpoint:
		return fmt.Errorf("%w: a scan token is issued for the endpoint scan, not %q", ErrMalformed, c.Endpoint)
	case c.Grant.Catalog || len(c.Grant.Entries) != 1:
		return fmt.Errorf("%w: a scan token has exactly one grant entry and no catalog", ErrMalformed)
	case !ValidDigest(c.Digest):
		return fmt.Errorf("%w: a scan token names a manifest digest sha256:<64 hex>", ErrMalformed)
	case !validScanID(c.ScanID):
		return fmt.Errorf("%w: a scan token carries a scan ID of 128 bits", ErrMalformed)
	}
	e := c.Grant.Entries[0]
	if !e.Repo.Valid() || e.Name == "" || e.Actions != Pull {
		return fmt.Errorf("%w: the entry of a scan token is pull on one image name of one repository", ErrMalformed)
	}
	return nil
}

// ValidDigest reports whether d is a SHA-256 digest as the registry spells it: sha256: and 64 lower-case hex digits.
func ValidDigest(d string) bool {
	h, ok := strings.CutPrefix(d, "sha256:")
	if !ok || len(h) != 64 || strings.ToLower(h) != h {
		return false
	}
	_, err := hex.DecodeString(h)
	return err == nil
}

func validScanID(sid string) bool {
	b, err := base64.RawURLEncoding.Strict().DecodeString(sid)
	return err == nil && len(b) == scanIDBytes
}
