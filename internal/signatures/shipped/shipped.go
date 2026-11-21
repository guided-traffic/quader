// Package shipped is the trust anchors Quader ships (signatures.md R-SIG-017, Q-SIG-4, Q-SIG-29): the public Sigstore root, the registry
// signing keys of registry.npmjs.org, the archive keyrings of Debian and Ubuntu, nuget.org's repository signing certificates and the roots
// of the timestamp authorities its countersignatures carry. The release embeds each; the public Sigstore root and npm's keys are TUF
// targets of the public Sigstore TUF repository, read by `make shipped-anchors` and kept current online by the task
// signature-trust-root through [Refresh].
package shipped

import (
	"context"
	"net/http"
	"time"

	"github.com/guided-traffic/quader/internal/signatures/anchors"
)

// The keys of the shipped anchors (R-SIG-017).
const (
	KeySigstorePublic       = "sigstore-public"
	KeyNPMJS                = "npmjs"
	KeyDebianArchive        = "debian-archive"
	KeyUbuntuArchive        = "ubuntu-archive"
	KeyNuGetOrg             = "nuget-org"
	KeyNuGetOrgTimestamping = "nuget-org-timestamping"
)

// The TUF targets the release embeds and the task refreshes.
const (
	TargetTrustedRoot = "trusted_root.json"
	TargetNPMKeys     = "registry.npmjs.org/keys.json"
)

// Anchor is one shipped anchor: its key, title, kind, the preset of the well-known repository that names it ("" for sigstore-public),
// and its material as the release carries it.
type Anchor struct {
	Key      string
	Title    string
	Kind     string
	Preset   string
	Material anchors.Material
}

// Snapshot is the release's snapshot of the two TUF targets: their documents, the TUF repository's version they were read at and when.
type Snapshot struct {
	TrustedRoot []byte
	NPMKeys     []byte
	TUFVersion  int64
	BuiltAt     time.Time
}

// Anchors returns every shipped anchor with the release's material, in a stable order.
func Anchors() []Anchor { return anchorsOf() }

// Release returns the release's snapshot of the TUF targets.
func Release() Snapshot { return release() }

// NPMKeys converts npm's keys.json (the TUF target) or a registry's -/npm/v1/keys document (keyid, key, expires) to a public-key anchor's
// material: one entry per key, a key npm names for two uses once with the later expiry.
func NPMKeys(doc []byte) (*anchors.PublicKeyMaterial, error) { return npmKeys(doc) }

// RefreshOptions configure [Refresh].
type RefreshOptions struct {
	// URL is signatures.trust_root.url: the TUF repository (default https://tuf-repo-cdn.sigstore.dev), or a mirror of the company.
	URL string
	// Client fetches through the outbound client and its egress rules.
	Client *http.Client
	// Now is the clock of the TUF metadata's expiry; the zero time is the wall clock.
	Now time.Time
	// Root is the TUF root to start the chain from; nil is the public Sigstore TUF root sigstore-go embeds. A test passes the root of
	// its own TUF repository (internal/signatures/shipped/tuftest).
	Root []byte
}

// Refreshed is what one refresh read: the two targets, verified through the chain of TUF metadata from the root the release embeds.
type Refreshed struct {
	TrustedRoot []byte
	NPMKeys     []byte
	TUFVersion  int64
}

// Refresh fetches the TUF metadata from o.URL, verifies the chain from the TUF root sigstore-go embeds with go-tuf's updater (the one
// sigstore-go's TUF client wraps; driven directly for its reference time o.Now), and returns the two targets and the TUF snapshot's version.
// It writes nothing to disk. A target not signed by the chain, or a signed target that does not parse, is an error, and the caller keeps
// what it had.
func Refresh(ctx context.Context, o RefreshOptions) (Refreshed, error) { return refresh(ctx, o) }
