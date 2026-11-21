// Package signatures is the domain of signatures on artifacts (docs/spec/signatures.md, R-SIG-001 to R-SIG-043; the contract page
// is docs/developer/signatures.md): the states, roles, sources and reasons of a signature, what the commit hook records (a [Batch] of
// [Candidate]s and an [Origin]) and the values the store answers.
//
// The package is the shared vocabulary, not the machinery. It imports the standard library alone, so that the format host (which
// turns a format's candidates into a batch), the store and the job of stage 1's later packages import it without a cycle. The
// machinery sits in sub-packages:
//
//	store   the PostgreSQL store of signature_objects, signatures, signature_attachments and artifact_origins
//
// Stage 1's later packages add the schemes, the trust step, the anchors, the shipped anchors and the job beside it
// (internal/signatures/{scheme,trust,anchors,shipped,job}); only internal/signatures/... imports the verification libraries (R-SIG-014).
package signatures

import (
	"encoding/json"
	"time"
)

// State is the state of a signature's read and integrity check (R-SIG-003).
type State string

// The states of the integrity check.
const (
	StatePending     State = "pending"
	StateValid       State = "valid"
	StateInvalid     State = "invalid"
	StateNoKey       State = "no_key"
	StateUnsupported State = "unsupported"
	StateMalformed   State = "malformed"
	StateFailed      State = "failed"
)

// Valid reports whether s is one of the seven states.
func (s State) Valid() bool {
	switch s {
	case StatePending, StateValid, StateInvalid, StateNoKey, StateUnsupported, StateMalformed, StateFailed:
		return true
	}
	return false
}

// The schemes of a signature object (R-SIG-001): the names formats.SignatureScheme* declare, spelled here for the packages below the
// edge layer, which may not import the format contract.
const (
	SchemeSigstoreBundle = "sigstore-bundle"
	SchemeCosignLegacy   = "cosign-legacy"
	SchemeNotationJWS    = "notation-jws"
	SchemeNotationCOSE   = "notation-cose"
	SchemeNuGetCMS       = "nuget-cms"
	SchemeOpenPGP        = "openpgp"
	SchemeNPMRegistry    = "npm-registry"
	SchemePublicKey      = "public-key"
	SchemeUnknown        = "unknown"
)

// The roles of a signature (R-SIG-001), as formats.SignatureRole* declare them.
const (
	RolePublisher   = "publisher"
	RoleProvenance  = "provenance"
	RoleRegistry    = "registry"
	RoleAttestation = "attestation"
)

// RoleValid reports whether role is one of the four roles.
func RoleValid(role string) bool {
	switch role {
	case RolePublisher, RoleProvenance, RoleRegistry, RoleAttestation:
		return true
	}
	return false
}

// The closed reasons of R-SIG-006.
const (
	ReasonDigestMismatch     = "digest_mismatch"
	ReasonSubjectMismatch    = "subject_mismatch"
	ReasonSignatureMismatch  = "signature_mismatch"
	ReasonChainBroken        = "chain_broken"
	ReasonLogEntryInvalid    = "log_entry_invalid"
	ReasonTimestampInvalid   = "timestamp_invalid"
	ReasonAttributeMissing   = "attribute_missing"
	ReasonWeakAlgorithm      = "weak_algorithm"
	ReasonSchemeUnknown      = "scheme_unknown"
	ReasonVersionUnsupported = "version_unsupported"
	ReasonLimitsExceeded     = "limits_exceeded"
	ReasonSubjectMissing     = "subject_missing"
	ReasonContentUnreadable  = "content_unreadable"
)

// The priorities of a due signature (R-SIG-011): 0 a client's upload, 1 a fill, a pass or a background fetch, 2 a subject that
// arrived after its signature, 3 discovery and the daily retry of failed.
const (
	PriorityUpload    = 0
	PriorityFill      = 1
	PrioritySubject   = 2
	PriorityDiscovery = 3
)

// The bounds of R-SIG-013 the store applies when it records (Q-SIG-45): signatures.max_object_size and signatures.max_per_subject's
// defaults. The configuration that changes them belongs to the job's package (R-SIG-040).
const (
	DefaultMaxObjectSize = 256 << 10
	DefaultMaxPerSubject = 32
	// MaxRaw is the most bytes the store keeps of an object as bytes (signature_objects.raw).
	MaxRaw = 256 << 10
)

// Object is the original of a signature (signature_objects): its own SHA-256, its scheme as far as the detector could tell, and
// either its bytes (Raw) or the blob of the stored file that holds it with the place inside (ItemBlob, Locator).
type Object struct {
	SHA256    [32]byte
	Scheme    string
	MediaType string
	Raw       []byte
	ItemBlob  [32]byte
	Locator   string
	Size      int64
	// FirstSeenAt is Quader's receipt of the object (R-SIG-007); set by the store.
	FirstSeenAt time.Time
}

// Candidate is one signature the commit hook records, every path it named resolved in the repository of its batch (Q-SIG-47).
type Candidate struct {
	Object      Object
	Part        int
	Subject     [32]byte
	SubjectName string
	Role        string
	// Source, Location and Name are the attachment's (R-SIG-004): how the signature reaches its subject, the item path or path#pointer
	// of the carrier, and the package or image name the location belongs to.
	Source   string
	Location string
	Name     string
	// ItemID is the item that carries the original, 0 for a remote's document.
	ItemID int64
	// ContentBlob and ContentLocator are what a keyless scheme or NuGet hashes when it is not the subject's bytes (Q-SIG-48); a zero
	// ContentBlob with a SignedDigest is a message whose digest the row keeps (npm's registry signature).
	ContentBlob    [32]byte
	ContentLocator string
	SignedDigest   []byte
	DigestAlg      string
}

// Batch is what one commit or one stored document carries: the candidates, and the carrier whose attachments they replace.
type Batch struct {
	RepoID int64
	// CarrierItem is the committed item, 0 for a stored document; Document is the stored document's path, "" for an item. The
	// attachments of the carrier and those at every location the batch names that the batch does not make again are removed, of the
	// batch's Sources only.
	CarrierItem int64
	Document    string
	// Sources are the attachment sources the batch speaks for: a format's detector (a commit, a fill, discovery) every source a
	// detector names, a signature a client posts through the management API "upload". A batch replaces attachments of these sources
	// only, so that what one writer recorded is never removed by another's batch (R-SIG-004, Q-SIG-217); none replaces nothing.
	Sources []string
	// Fresh says the carrier is an item the commit created: it carried no attachment before, so a batch without candidates writes
	// nothing.
	Fresh      bool
	Priority   int
	Candidates []Candidate
}

// Stored is what a path of a repository holds now: the blob of a live file or of a remote's stored document, and the file's item.
type Stored struct {
	Blob   [32]byte
	Size   int64
	ItemID int64
}

// Origin is the origin of an uploaded item (R-SIG-010): the principal as the audit records it and the credential's ID. The claims of
// a CI job are read from the issuance record of the credential by the store, in the commit's transaction.
type Origin struct {
	ItemID        int64
	PrincipalKind string
	PrincipalName string
	TokenID       string
}

// StoredOrigin is an origin as the store answers it.
type StoredOrigin struct {
	PrincipalKind string
	PrincipalName string
	TokenID       string
	// CI holds the issuer's name and the allowlisted claims of the issuance record; nil for an upload that was no CI job's.
	CI         json.RawMessage
	UploadedAt time.Time
}

// Signature is one row of signatures: the read and the integrity check of an object against a subject (R-SIG-003, R-SIG-005).
type Signature struct {
	ID             int64
	Object         [32]byte
	Part           int
	Subject        [32]byte
	SubjectName    string
	Role           string
	PredicateType  string
	ContentBlob    [32]byte
	ContentLocator string
	State          State
	Reason         string
	Signer         json.RawMessage
	KeyIDs         []string
	SignedDigest   []byte
	DigestAlg      string
	ClaimedAt      time.Time
	ProvenAt       time.Time
	TimeProof      string
	ReceivedAt     time.Time
	CheckedAt      time.Time
	Due            bool
	Priority       int
	Attempts       int
	NotBefore      time.Time
}

// Attachment is where a signature reaches its subject in a repository (R-SIG-004).
type Attachment struct {
	RepoID      int64
	SignatureID int64
	Location    string
	ItemID      int64
	Source      string
	Name        string
	CreatedAt   time.Time
	// Subject is the signature's subject, filled where a read joins it.
	Subject [32]byte
}

// Result is what the read and the integrity check of one signature found (R-SIG-005, R-SIG-006).
type Result struct {
	State         State
	Reason        string
	Role          string
	PredicateType string
	Signer        json.RawMessage
	KeyIDs        []string
	SignedDigest  []byte
	DigestAlg     string
	ClaimedAt     time.Time
	ProvenAt      time.Time
	TimeProof     string
	// Evidence is what the trust step judges the signature by without reading content (signatures.evidence, Q-SIG-109): the JSON of
	// scheme.Evidence, nil when the state has no trust step.
	Evidence []byte
}
