package registrytoken

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/qname"
)

// Blob tokens (malware-scanning.md R-MAL-006, Q-MAL-26): the scan token of the kind blob, the credential of the malware scanner's read
// of one blob on the scan listener. A worker signs one in process for each call of the malware scanner, with the key of every registry
// token; it names the system subject malware-scanner-clamav, the scan's ID, the endpoint scan, one blob's digest and the one repository that
// holds it which the job chose (malware_scan.holder_repo_id), and no grant entry and no image. No quader token stands behind it, so
// its verification costs no query. It is good on the scan listener only, on the blob route only, for that digest, while that
// repository holds an item over it (the route checks the last), for malware.read_timeout. It is held in a [ScanToken], which renders
// nothing of it.

// BlobSubject is the system subject of a blob token (R-AUTHZ-002): the claim s, and the name of the principal of kind system the stage
// of the scan listener yields on the malware scanning's routes. It is the name of the unit that reads, malware-scanner-clamav
// (ha-and-helm.md Q-HA-373).
const BlobSubject = "malware-scanner-clamav"

// UpdaterSubject is the system subject the stage of the scan listener yields for the updater's certificate on the database routes
// (R-MAL-032, Q-MAL-101): the name of the unit that reads for its seed, malware-updater-freshclam. No token carries it, and the blob
// route never admits it.
const UpdaterSubject = "malware-updater-freshclam"

// The malware scanning's routes of the scan listener (R-MAL-006, R-ARCH-037), beside the registry's below /v2/. BlobPathPrefix followed
// by the digest, sha256:<64 hex>, is the blob route, which a blob token and nothing else reaches; DatabasePath, and below it
// <version>/<file>, are the database routes, which take no token and admit the malware scanner's certificate (Q-MAL-27). The stage of
// the scan listener, the routes and the malware scanner's reader share them.
const (
	BlobPathPrefix = "/scan/v1/blobs/"
	DatabasePath   = "/scan/v1/malware-database"
)

// BlobPath is the path of the blob route for a digest.
func BlobPath(digest string) string { return BlobPathPrefix + digest }

// IsDatabasePath reports whether a path is one of the database routes': DatabasePath or a path below it.
func IsDatabasePath(p string) bool {
	return p == DatabasePath || strings.HasPrefix(p, DatabasePath+"/")
}

// BlobOfPath returns the digest a path of the blob route names, and false for any other path.
func BlobOfPath(p string) (string, bool) {
	d, ok := strings.CutPrefix(p, BlobPathPrefix)
	if !ok || !ValidDigest(d) {
		return "", false
	}
	return d, true
}

// BlobRef is what a verified blob token reaches: the one repository and the one blob (R-MAL-006). The stage hands it on with the
// principal; it is no secret.
type BlobRef struct {
	Repo   qname.Repo
	Digest string
}

// ErrNoBlobs is the refusal of a blob token by a [Signer] that has no blob lifetime: a replica that does not issue them.
var ErrNoBlobs = errors.New("registrytoken: this signer issues and accepts no blob token")

// ForBlobs returns a signer that issues and verifies blob tokens with the lifetime readTimeout (malware.read_timeout, 1 minute to 1
// hour) besides the tokens of s. A blob token is checked against this bound and never against oci.token_lifetime or the image
// scanning's pull_timeout.
func (s *Signer) ForBlobs(readTimeout time.Duration) (*Signer, error) {
	if readTimeout < time.Minute || readTimeout > MaxLifetime {
		return nil, fmt.Errorf("registrytoken: the blob token lifetime %s is not between 1m and %s", readTimeout, MaxLifetime)
	}
	out := *s
	out.blobLifetime = readTimeout
	return &out, nil
}

// IssueBlob signs the blob token of one blob (R-MAL-006): repo is the qualified name of the repository the job chose as the blob's
// holder (a local or remote repository), digest the blob's, sha256:<64 hex>. It returns the token and the scan's ID, which the worker's
// log line, the malware scanner's and the scan listener's request log share.
func (s *Signer) IssueBlob(repo qname.Repo, digest string, now time.Time) (ScanToken, string, error) {
	if s.blobLifetime == 0 {
		return ScanToken{}, "", ErrNoBlobs
	}
	raw := make([]byte, scanIDBytes)
	if _, err := rand.Read(raw); err != nil {
		return ScanToken{}, "", fmt.Errorf("registrytoken: a scan ID: %w", err)
	}
	sid := base64.RawURLEncoding.EncodeToString(raw)
	now = now.Truncate(time.Second)
	tok, err := s.Issue(Claims{
		System: BlobSubject, ScanID: sid, Digest: digest, Repo: repo, Endpoint: ScanEndpoint,
		IssuedAt: now, ExpiresAt: now.Add(s.blobLifetime),
	})
	if err != nil {
		return ScanToken{}, "", err
	}
	return ScanToken{v: &tok}, sid, nil
}

// IsBlob reports whether the claims are a blob token's.
func (c Claims) IsBlob() bool { return c.System == BlobSubject }

// checkBlob holds the rule both sides apply to a blob token (Q-MAL-26): the system subject malware-scanner-clamav, no principal, no quader
// token, not anonymous, the endpoint scan, no grant entry and no catalog, a blob digest, a scan ID and one valid repository.
func (c Claims) checkBlob() error {
	switch {
	case c.Anonymous || c.PrincipalID != "" || c.PrincipalKind != "" || c.Binding != (Binding{}):
		return fmt.Errorf("%w: a blob token names no principal and no Quader token", ErrMalformed)
	case c.Endpoint != ScanEndpoint:
		return fmt.Errorf("%w: a blob token is issued for the endpoint scan, not %q", ErrMalformed, c.Endpoint)
	case c.Grant.Catalog || len(c.Grant.Entries) != 0:
		return fmt.Errorf("%w: a blob token has no grant entry and no catalog", ErrMalformed)
	case !ValidDigest(c.Digest):
		return fmt.Errorf("%w: a blob token names a blob digest sha256:<64 hex>", ErrMalformed)
	case !validScanID(c.ScanID):
		return fmt.Errorf("%w: a blob token carries a scan ID of 128 bits", ErrMalformed)
	case !c.Repo.Valid():
		return fmt.Errorf("%w: a blob token names the one repository that holds its blob", ErrMalformed)
	}
	return nil
}
