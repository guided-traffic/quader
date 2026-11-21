// Package scheme is the contract between the job signature-verify and the verifiers of each scheme (signatures.md R-SIG-005, R-SIG-006,
// R-SIG-007, R-SIG-013, R-SIG-014): what the job hands a verifier for one signature row ([Input]), what the verifier answers ([Output]),
// and the evidence the trust step judges a signature by later without reading any content ([Evidence], Q-SIG-109).
//
// The verifiers sit in sub-packages, one per family of schemes, and only internal/signatures/... imports the verification libraries:
//
//	sigstore   sigstore-bundle and cosign-legacy (sigstore-go), and the judgement of a bundle against an identity anchor
//	notation   notation-jws and notation-cose (notation-core-go, tspclient-go)
//	nugetcms   nuget-cms: the CMS SignedData of a package, its countersignatures and its timestamps (digitorus/pkcs7, digitorus/timestamp)
//	openpgp    openpgp: a signature packet over a content and its trailer (go-crypto)
//	rawkey     npm-registry and public-key: an ECDSA, RSA or Ed25519 signature over a digest (the standard library)
//	all        Check: the dispatch by scheme, with the bounds and the recovery of a panic around every verifier
//
// A verifier reads only through [Input]: the object's bytes, the stored blobs it names (an OCI layer, the subject, the content) and the
// keys it is handed. It writes nothing to disk and runs no SQL; the job stores what it answers.
package scheme

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
)

// ErrBlobMissing is answered by [Blobs.Content] for a digest no stored blob has, or whose content is gone from the object store.
var ErrBlobMissing = errors.New("signatures: the blob is not stored")

// ErrTransient wraps an error of a check that could not run (the object store, a timeout): the job spends an attempt and tries again
// after its back-off (R-SIG-011). Every other outcome is a [signatures.Result].
var ErrTransient = errors.New("signatures: the check could not run")

// Content is a stored blob as a verifier reads it, bounded.
type Content interface {
	// Size is the length of the blob.
	Size() int64
	// Bytes returns the whole blob when it is at most max bytes long, and an error otherwise; it reads nothing then.
	Bytes(max int64) ([]byte, error)
	// Reader streams the whole blob (a content a keyless scheme or NuGet hashes).
	Reader() (io.ReadCloser, error)
	// ReaderAt reads parts of the blob (the central directory of a package).
	ReaderAt() (io.ReaderAt, error)
}

// Blobs opens stored blobs by their SHA-256. An error other than [ErrBlobMissing] is an error of the object store: the verifier wraps it
// in [ErrTransient].
type Blobs interface {
	Content(ctx context.Context, digest [32]byte) (Content, error)
}

// Limits are the bounds of R-SIG-013 (Q-SIG-45) that sit outside the libraries. Zero values are the defaults of [DefaultLimits].
type Limits struct {
	// MaxObjectSize is signatures.max_object_size: one signature object, and one layer an OCI signature manifest names.
	MaxObjectSize int64
	// MaxChain is signatures.max_chain: certificates in a chain; MaxCertificateSize is the size of each.
	MaxChain           int
	MaxCertificateSize int
	// MaxContentSize is signatures.max_content_size: the content a keyless scheme or NuGet reads.
	MaxContentSize int64
	// MinRSABits and MaxRSABits bound an RSA key: below is unsupported (weak_algorithm), above malformed.
	MinRSABits, MaxRSABits int
	// JSONDepth and JSONValues bound a JSON document before any decoder (untrusted.CheckJSON).
	JSONDepth, JSONValues int
	// ASN1Depth bounds the nesting of an ASN.1 or CBOR structure.
	ASN1Depth int
}

// DefaultLimits are the defaults of R-SIG-013.
func DefaultLimits() Limits {
	return Limits{
		MaxObjectSize: signatures.DefaultMaxObjectSize, MaxChain: 8, MaxCertificateSize: 16 << 10, MaxContentSize: 8 << 30,
		MinRSABits: 2048, MaxRSABits: 8192, JSONDepth: 32, JSONValues: 100000, ASN1Depth: 32,
	}
}

// Keys are the keys a keyless scheme (openpgp, npm-registry, public-key, cosign-legacy and a Sigstore bundle signed with a key) can be
// matched with its content by (R-SIG-006): the keys of the anchors of every scope, and, for the integrity check alone, the keys the
// upstreams of the repositories that hold the signature announce (npm's keys of a remote, Debian's deb.upstreamKeys). A key that only an
// upstream announces makes a signature valid and leaves it untrusted (Q-SIG-24): the trust step decides by the anchors alone.
type Keys struct {
	// OpenPGP are public certificates, armored, as internal/openpgp parses them.
	OpenPGP []anchors.OpenPGPCertificate
	// Public are raw public keys, with npm's keyid where the source names one.
	Public []anchors.ParsedKey
}

// SigstoreRoot is a Sigstore trust root a verifier checks the log entries and timestamps of a bundle against at the integrity check
// (R-SIG-006, R-SIG-007): the public root in use (the release's snapshot or the refreshed one, R-SIG-017) and the own root of every
// Sigstore identity anchor of any scope. A log entry or timestamp that a root knows and that does not verify is invalid
// (log_entry_invalid, timestamp_invalid); one that no root knows leaves time_proof unset.
type SigstoreRoot struct {
	// AnchorID is the anchor the root belongs to; 0 for the public root.
	AnchorID int64
	// TrustedRoot is the trusted_root.json document.
	TrustedRoot json.RawMessage
}

// Input is one signature row and what the job hands its verifier.
type Input struct {
	// ID is the signature's row, for the log only.
	ID int64
	// Scheme is the object's scheme as detected; Role the role the detector gave. The verifier may correct both in its Result.
	Scheme string
	Role   string
	// MediaType is the object's media type as detected, "" when none.
	MediaType string
	// Part is the signature within the object: an OpenPGP packet, a layer of an OCI signature manifest, a countersignature (R-SIG-003).
	Part int

	// The object (signature_objects): its own SHA-256, and either its bytes (Raw) or the blob that holds it (ItemBlob) with the place
	// inside (Locator: "" the whole blob, "entry:<name>" an entry of an archive, "#/<pointer>" a JSON pointer). ObjectSize is its size;
	// the job hands no object above Limits.MaxObjectSize (it is malformed, limits_exceeded, before the call).
	ObjectSHA256 [32]byte
	Raw          []byte
	ItemBlob     [32]byte
	Locator      string
	ObjectSize   int64

	// The subject: the SHA-256 of the covered blob, its SHA-512 and size from the blobs table (nil, 0 when no blob has the digest: npm's
	// registry signature, whose subject is the digest of its message, Q-SIG-50), and npm's <name>@<version>.
	Subject       [32]byte
	SubjectSHA512 []byte
	SubjectSize   int64
	SubjectName   string

	// The signed content when it is not the subject's bytes (Q-SIG-48): a blob and a view of it ("unsigned": a NuGet package without its
	// signature entry; "cleartext": the text of an inline OpenPGP signature; "entry:<name>": an entry of an archive). SignedDigest is a
	// message's digest the row keeps (npm), DigestAlg its algorithm.
	ContentBlob    [32]byte
	ContentLocator string
	SignedDigest   []byte
	DigestAlg      string

	// ReceivedAt is the earlier of the object's and the subject's receipt (R-SIG-007): the time of a signature without a proof.
	ReceivedAt time.Time
	// Now is the clock of the check.
	Now time.Time

	Blobs  Blobs
	Keys   Keys
	Roots  []SigstoreRoot
	Limits Limits
}

// Output is what a verifier found.
type Output struct {
	// Result is the read and the integrity check (R-SIG-005, R-SIG-006): the state, the reason, the role and predicate type as the read
	// corrects them, the signer, the key IDs, the committed digest of a keyless scheme, the claimed and the proven time.
	Result signatures.Result
	// Evidence is what the trust step needs to judge the signature against the anchors of a scope without reading any content
	// (R-SIG-012); nil for a state that has no trust step (invalid, unsupported, malformed).
	Evidence *Evidence
	// Parts is the number of signatures the object holds when the read could count them (the packets of an OpenPGP signature, the
	// layers of a cosign signature manifest, a NuGet package's primary signature and its countersignatures); the job adds the rows of the
	// parts a detector could not name (a Debian Release named before its Release.gpg, a referrers tag's manifest). 0 when unknown.
	Parts int
}

// Evidence is what the trust step judges a signature by (stored in signatures.evidence, Q-SIG-109). Every field is optional; a scheme
// fills what its family of trust needs, bounded by the object it came from.
type Evidence struct {
	// Chain is the signer's certificate chain as the signature carries it, leaf first, DER: Notation's, NuGet's, cosign's annotated
	// certificate and chain, a Sigstore bundle's leaf. A CA anchor knows the signer when this chain builds to one of its certificates at
	// the signature's time with the use codeSigning (R-SIG-008).
	Chain [][]byte `json:"chain,omitempty"`
	// Timestamps are the RFC 3161 tokens over the signature value that verified against their message imprint at the check: the
	// authority's chain and the time each states. A token whose authority's chain ends in a CA anchor of the use timestamping proves its
	// time in that scope (R-SIG-007, Q-SIG-41).
	Timestamps []Timestamp `json:"timestamps,omitempty"`
	// Sigstore is a Sigstore bundle (sigstore-bundle, and cosign's older layout converted to one) with the artifact digest it was checked
	// against: a Sigstore identity anchor judges it against its trust root (R-SIG-008).
	Sigstore *SigstoreEvidence `json:"sigstore,omitempty"`
	// Key is a keyless scheme's signature: a key of an anchor verifies it against signatures.signed_digest (Q-SIG-23).
	Key *KeyEvidence `json:"key,omitempty"`
}

// Timestamp is an RFC 3161 token that matched the signature it stamps.
type Timestamp struct {
	// Token is the TimeStampToken, DER.
	Token []byte `json:"token"`
	// Time is the genTime the token states.
	Time time.Time `json:"time"`
	// Chain is the authority's chain as the token carries it, leaf first, DER.
	Chain [][]byte `json:"chain,omitempty"`
}

// SigstoreEvidence is a bundle and what it was checked against.
type SigstoreEvidence struct {
	// Bundle is the bundle's JSON (protobuf JSON of dev.sigstore.bundle.v1.Bundle).
	Bundle json.RawMessage `json:"bundle"`
	// DigestAlg and Digest are the artifact digest the bundle was checked against: the subject's SHA-256, its SHA-512 (npm's
	// provenance names the tarball by SHA-512), or the digest of cosign's simple-signing payload.
	DigestAlg string `json:"digestAlg"`
	Digest    []byte `json:"digest"`
}

// KeyEvidence is a keyless scheme's signature and what a key must be to verify it.
type KeyEvidence struct {
	// Kind is "openpgp" (Signature is the signature packet), "ecdsa", "rsa-pkcs1v15", "rsa-pss" or "ed25519" (Signature is the raw
	// signature over signatures.signed_digest).
	Kind      string `json:"kind"`
	Signature []byte `json:"signature"`
	// KeyID is the issuer's key ID or fingerprint (OpenPGP) or npm's keyid; "" when the signature names none (cosign sign-blob).
	KeyID string `json:"keyId,omitempty"`
	// Hash is the hash of the signed digest ("sha256", "sha512", …).
	Hash string `json:"hash"`
}

// ParseChain parses a DER chain of evidence, at most max certificates.
func ParseChain(chain [][]byte, max int) ([]*x509.Certificate, error) {
	if max > 0 && len(chain) > max {
		return nil, errors.New("signatures: the chain is longer than signatures.max_chain")
	}
	out := make([]*x509.Certificate, 0, len(chain))
	for _, der := range chain {
		c, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}
