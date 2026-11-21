package formats

import (
	"context"
	"io"
	"time"

	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// SignatureAdapter is what a format says about the signatures of its ecosystem (signatures.md R-SIG-002, R-SIG-022): a static
// declaration of the kinds it knows, or none with the reason, and a detector over what a commit or a remote's fill stores. Every format
// returns one from [Format.Signatures]; [Base] returns nil and [Register] refuses nil, so a format that does not declare does not start
// (Q-SIG-43).
type SignatureAdapter interface {
	// Declaration is static: the kinds the format knows, or none with the reason.
	Declaration() SignatureDeclaration
	// Detect names the signatures a committed item or a stored document carries or points at. It reads only the bytes it is handed
	// (ev.Content, bounded) and runs no SQL; the core resolves what a candidate names by path, stores it and checks it later. An error
	// never fails the commit: the core records it and stores the content without candidates (Q-SIG-47).
	Detect(ctx context.Context, ev SignatureEvent) ([]SignatureCandidate, error)
}

// SignatureDeclaration is the row of a format in the table of R-SIG-002.
type SignatureDeclaration struct {
	None  string // non-empty: the format has no signatures, and why
	Kinds []SignatureKind
}

// SignatureKind is one kind of signature a format knows.
type SignatureKind struct {
	Name     string   // "Notation", "Sigstore bundle", "registry signature", …
	Schemes  []string // R-SIG-001's scheme names (the SignatureScheme constants)
	Role     string   // publisher, provenance, registry, attestation (the SignatureRole constants)
	Location string   // where it lives, in the format's words
	Covers   string   // what it covers
	Client   string   // how a client gets it
}

// SignatureCandidate is one signature a detector names: the original, the subject it covers, and where it reaches the subject in the
// event's repository (the attachment of R-SIG-004). The core resolves the paths a candidate names in the event's repository only, never
// in another, and drops a candidate whose object or subject is named by a path that holds nothing: that path names it again when it is
// stored (Q-SIG-47).
type SignatureCandidate struct {
	Object SignatureLocator // the original: a stored file's blob with a locator, or bytes of at most MaxSignatureRaw
	// Part is the signature within the object: an OpenPGP packet, a layer of an older cosign signature, a countersignature (R-SIG-003).
	Part        int
	Subject     [32]byte // the SHA-256 of the covered blob; zero when SubjectPath names it
	SubjectPath string   // a stored file or document of the event's repository whose blob is the subject (a generic file beside its signature)
	SubjectName string   // npm: <name>@<version>
	Role        string
	SchemeHint  string
	Source      string // referrer, referrers_tag, cosign_tag, index_entry, document, embedded, sibling, release, upload
	Location    string // the attachment's location (R-SIG-004): an item path, or path#pointer in a document
	// SignedContent is what a keyless scheme or NuGet hashes: the subject, an entry of it, its unsigned view, or the message itself
	// (npm's registry signature: Raw). The zero locator is the subject's own bytes.
	SignedContent SignatureLocator
}

// SignatureLocator names bytes for the signature store: by a path of the event's repository, by a blob's digest, or as the bytes
// themselves. Exactly one of Path, Blob and Raw is set; a zero locator names nothing (for SignedContent: the subject's own bytes).
type SignatureLocator struct {
	// Path names a stored file or document of the event's repository whose blob holds the bytes; the event's own path names the
	// committed content.
	Path string
	// Blob is the SHA-256 of a blob that holds the bytes, when the detector knows it by digest (an OCI layer a stored manifest names).
	Blob [32]byte
	// Locator is where inside the blob: "" for the whole blob, a zip entry, a JSON pointer "#/…", or one of the SignatureLocator
	// constants.
	Locator string
	// SHA256 is the digest of the object's own bytes when they are a part of the blob (an entry of an archive); zero for the whole blob.
	SHA256 [32]byte
	// Size is the size of the object's bytes; 0 when it is the whole blob, which the core then knows.
	Size      int64
	MediaType string
	// Raw are the bytes themselves when no stored file keeps them (an entry of a document): at most MaxSignatureRaw.
	Raw []byte
}

// SignatureEvent is what a detector is handed: a committed item, or a document a remote's fill stored (Item.ID is 0 then).
type SignatureEvent struct {
	Origin Origin
	Repo   RepoRef
	// Item is the committed item; the zero ItemRef for a stored document of a remote, which is no item.
	Item ItemRef
	// Lead is the lead item when Item is the file of a [Companion]; the zero ItemRef otherwise.
	Lead      ItemRef
	Path      Path
	Variant   string // the variant of a stored document; "" for an item
	MediaType string
	Digests   Digests
	// Content reads the stored bytes, bounded; a detector reads only what it needs and only through it.
	Content SignatureContent
}

// SignatureContent is the stored bytes of a [SignatureEvent] as a detector reads them: from the replica's spool where the commit had
// one, otherwise from the object store.
type SignatureContent interface {
	// Size is the length of the content.
	Size() int64
	// Bytes returns the whole content when it is at most max bytes long, and an error otherwise; it reads nothing then.
	Bytes(max int64) ([]byte, error)
	// ReaderAt reads parts of the content (the central directory of a package archive).
	ReaderAt() (io.ReaderAt, error)
}

// The roles of a signature (R-SIG-001).
const (
	SignatureRolePublisher   = "publisher"
	SignatureRoleProvenance  = "provenance"
	SignatureRoleRegistry    = "registry"
	SignatureRoleAttestation = "attestation"
)

// The schemes of a signature (R-SIG-001).
const (
	SignatureSchemeSigstoreBundle = "sigstore-bundle"
	SignatureSchemeCosignLegacy   = "cosign-legacy"
	SignatureSchemeNotationJWS    = "notation-jws"
	SignatureSchemeNotationCOSE   = "notation-cose"
	SignatureSchemeNuGetCMS       = "nuget-cms"
	SignatureSchemeOpenPGP        = "openpgp"
	SignatureSchemeNPMRegistry    = "npm-registry"
	SignatureSchemePublicKey      = "public-key"
	SignatureSchemeUnknown        = "unknown"
)

// The sources of an attachment: how a signature reaches its subject in a repository (R-SIG-004).
const (
	SignatureSourceReferrer     = "referrer"
	SignatureSourceReferrersTag = "referrers_tag"
	SignatureSourceCosignTag    = "cosign_tag"
	SignatureSourceIndexEntry   = "index_entry"
	SignatureSourceDocument     = "document"
	SignatureSourceEmbedded     = "embedded"
	SignatureSourceSibling      = "sibling"
	SignatureSourceRelease      = "release"
	SignatureSourceUpload       = "upload"
)

// The locators of signed content that is not the subject's bytes as stored (Q-SIG-48): a zip or tar entry is named "entry:<name>".
const (
	// SignatureLocatorUnsigned is a NuGet package without its signature entry (R-SIG-006).
	SignatureLocatorUnsigned = "unsigned"
	// SignatureLocatorCleartext is the text an inline OpenPGP signature carries (Debian's InRelease).
	SignatureLocatorCleartext = "cleartext"
	// SignatureLocatorEntryPrefix starts the locator of an entry of an archive: "entry:MANIFEST.json".
	SignatureLocatorEntryPrefix = "entry:"
)

// MaxSignatureRaw is the most bytes of a signature object kept as bytes in the store (R-SIG-003, Q-SIG-22).
const MaxSignatureRaw = 256 << 10

// SignatureRoleValid reports whether role is one of R-SIG-001's roles.
func SignatureRoleValid(role string) bool {
	switch role {
	case SignatureRolePublisher, SignatureRoleProvenance, SignatureRoleRegistry, SignatureRoleAttestation:
		return true
	}
	return false
}

// SignatureSchemeValid reports whether scheme is one of R-SIG-001's schemes.
func SignatureSchemeValid(scheme string) bool {
	switch scheme {
	case SignatureSchemeSigstoreBundle, SignatureSchemeCosignLegacy, SignatureSchemeNotationJWS, SignatureSchemeNotationCOSE,
		SignatureSchemeNuGetCMS, SignatureSchemeOpenPGP, SignatureSchemeNPMRegistry, SignatureSchemePublicKey, SignatureSchemeUnknown:
		return true
	}
	return false
}

// SignatureSourceValid reports whether source is one of R-SIG-004's sources.
func SignatureSourceValid(source string) bool {
	switch source {
	case SignatureSourceReferrer, SignatureSourceReferrersTag, SignatureSourceCosignTag, SignatureSourceIndexEntry,
		SignatureSourceDocument, SignatureSourceEmbedded, SignatureSourceSibling, SignatureSourceRelease, SignatureSourceUpload:
		return true
	}
	return false
}

// SignatureFetcher is the optional interface of a format's remote adapter whose signatures do not come with what a client fetches
// anyway (signatures.md R-SIG-026, Q-SIG-33): OCI's referrers and attachment tags, npm's attestations and keys. The job
// signature-fetch (internal/signatures/fetch) hands it the upstream of one remote repository for one run (the background requests of a
// mirror's pass, without the pass) and a target the format named when its data port queued the fetch; the format asks the upstream for what its protocol defines as the
// target's signatures and stores what it finds through the upstream's Document and Artifact, so that the commit hook detects it as it
// detects a client's fill (R-SIG-011). A mirror's plan calls the same code with the pass's upstream (R-SIG-025). A target that names
// nothing the format knows is no error: there is nothing to fetch.
type SignatureFetcher interface {
	FetchSignatures(ctx context.Context, u discover.Upstream, target string) error
}

// SignatureVerdicts is what a format asks of the verdicts of signatures it serves (signatures.md R-SIG-028, Q-SIG-35): an npm local
// repository serves a version's provenance bundle to npm only when it verified against the public Sigstore, because npm checks
// provenance against that root alone. The trust step (internal/signatures, package 2 of stage 1) implements it; a host without it
// answers [NoSignatureVerdicts], so a format never meets nil and serves nothing it cannot vouch for.
type SignatureVerdicts interface {
	// PublicProvenance reports, for stored files of a repository (provenance bundles by their canonical paths), the predicate type of
	// each whose signature is valid and trusted by a shipped public anchor in the installation's scope; a path absent from the answer
	// is not served.
	PublicProvenance(ctx context.Context, repo RepoRef, paths []string) (map[string]string, error)
}

// NoSignatureVerdicts is the [SignatureVerdicts] of a host without the trust step: nothing is vouched for.
var NoSignatureVerdicts SignatureVerdicts = noVerdicts{}

type noVerdicts struct{}

func (noVerdicts) PublicProvenance(context.Context, RepoRef, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

// SignatureCoverage is the optional interface of a format whose ecosystem signs an index that names a package and not the package
// itself (Debian, signatures.md R-SIG-030): it answers which signed documents of a repository name a stored file by its SHA-256, so that
// the details of the file list each covering document with its signatures. A format without it covers nothing.
type SignatureCoverage interface {
	// Covering answers for the stored file at a canonical path of the repository with its SHA-256; data is the format's data port.
	Covering(ctx context.Context, data any, repo RepoRef, path string, sha256 [32]byte) ([]CoveringDocument, error)
}

// CoveringDocument is a signed document that names a stored file (R-SIG-030): a Debian release whose index lists the package.
type CoveringDocument struct {
	// Path is the canonical path of the signed document ("dists/bookworm/InRelease"); SHA256 its digest, the subject of its signatures.
	Path   string
	SHA256 [32]byte
	// Name is what the document is named by in the format's words: a release's suite ("bookworm").
	Name string
	// Date is the document's own date (a release's Date), zero when it states none.
	Date time.Time
	// Verifier is the fingerprint of the key that verified the document when it was stored (deb.upstreamKeys), "" for none.
	Verifier string
}

// NoSignatures is the adapter of a format whose ecosystem has no signatures: its declaration says why, and it detects nothing. A future
// format without signatures returns it; "This format has no signatures" then stands in its details (R-SIG-002).
func NoSignatures(reason string) SignatureAdapter { return noSignatures(reason) }

type noSignatures string

func (n noSignatures) Declaration() SignatureDeclaration {
	return SignatureDeclaration{None: string(n)}
}

func (noSignatures) Detect(context.Context, SignatureEvent) ([]SignatureCandidate, error) {
	return nil, nil
}
