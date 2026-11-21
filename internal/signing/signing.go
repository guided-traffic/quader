// Package signing is Quader's own signing identity, stage 2 of signatures.md (R-SIG-045 to R-SIG-049, R-SIG-055, R-SIG-057 to
// R-SIG-059): what an identity must be and the checks of its material (material.go), its states and their transitions (states.go),
// npm's keyid and keys document (npm.go), the key request and the import of an uploaded identity (request.go, importkey.go), the
// trusted root and the chain anyone may fetch (root.go), the store of the identities and of the job's work (store.go, work.go), its
// audit events and the replay of what withdrew an identity (events.go, journal.go), and the snapshot every replica reads the public
// parts from (snapshot.go).
//
// One identity per installation (Q-SIG-13): an ECDSA P-256 key with a code-signing certificate of the company's CA. It comes from the
// Helm values (a Secret mounted into the worker pods alone; the private key is never stored, R-SIG-046) or from the admin area (a key
// Quader makes and seals with the key ring, R-SIG-047). The private key is opened by internal/signing/key and held by the worker's job
// (internal/signing/job) alone (R-SIG-049); this package never opens one.
package signing

import (
	"encoding/hex"
	"time"
)

// The sources of an identity (R-SIG-045).
const (
	SourceConfiguration = "configuration"
	SourceUpload        = "upload"
)

// The states of an identity (R-SIG-045).
const (
	StatePending   = "pending"
	StateAnnounced = "announced"
	StateCurrent   = "current"
	StatePrevious  = "previous"
	StateRetired   = "retired"
)

// The installation's state as the API answers it (R-SIG-055): the source of the current identity, or none.
const (
	InstallationNone = "none"
)

// The kinds of work of the job signing (R-SIG-049): npm's registry signature of a local version and the receipt attestation. The
// packages that sign register a handler per kind; the table's CHECK holds the list.
const (
	WorkNPM         = "npm"
	WorkAttestation = "attestation"
)

// The reasons a row of work was written (R-SIG-049).
const (
	ReasonUpload    = "upload"
	ReasonFill      = "fill"
	ReasonChecks    = "checks"
	ReasonMalware   = "malware"
	ReasonRotation  = "rotation"
	ReasonRetired   = "retired"
	ReasonDiscovery = "discovery"
)

// The priorities of work (R-SIG-049): 0 an npm signature of a publish, 1 the attestation of an upload, 2 the attestation of a fill and
// what a retirement left, 3 discovery and what a rotation left.
const (
	PriorityPublish   = 0
	PriorityUpload    = 1
	PriorityFill      = 2
	PriorityDiscovery = 3
)

// MinRemaining is the least validity an identity's certificate must have left when it is installed (R-SIG-045).
const MinRemaining = 24 * time.Hour

// ExpiringWithin is when the admin page shows an identity amber and QuaderSigningIdentityExpiring fires (R-SIG-057).
const ExpiringWithin = 14 * 24 * time.Hour

// Identity is a row of signing_identities without its private part: what every role may read.
type Identity struct {
	ID     int64
	Source string
	State  string
	// PublicKey is the DER SubjectPublicKeyInfo, KeySHA256 its SHA-256, NPMKeyID npm's keyid of it (R-SIG-050).
	PublicKey []byte
	KeySHA256 [32]byte
	NPMKeyID  string
	// CSR is the PEM request of a pending key (R-SIG-047).
	CSR string
	// Certificate is the DER leaf; CertSHA256 its SHA-256, the identity's fingerprint. Chain is the DER intermediates and then the
	// root. All empty while pending.
	Certificate []byte
	CertSHA256  [32]byte
	Chain       [][]byte
	Subject     string
	Issuer      string
	// Names are the alternative names: the URIs first, then the e-mail addresses.
	Names                []string
	NotBefore, NotAfter  time.Time
	AnnouncedAt          time.Time
	CurrentAt            time.Time
	ExpiresAt, RetiredAt time.Time
	CreatedAt            time.Time
	// CreatedBy is "<kind>:<name>" of the principal that made it, "" for the files of the Helm values.
	CreatedBy string
}

// Fingerprint is the lower-case hex SHA-256 of the certificate, "" while pending.
func (i Identity) Fingerprint() string {
	if i.State == StatePending || i.CertSHA256 == ([32]byte{}) {
		return ""
	}
	return hex.EncodeToString(i.CertSHA256[:])
}

// KeyFingerprint is the lower-case hex SHA-256 of the public key.
func (i Identity) KeyFingerprint() string { return hex.EncodeToString(i.KeySHA256[:]) }

// Expired reports whether the certificate's end has passed at now: an expired identity signs nothing, also while current (R-SIG-045).
func (i Identity) Expired(now time.Time) bool { return !i.NotAfter.IsZero() && !now.Before(i.NotAfter) }

// SignsAt reports whether the identity may sign at now: current, with a certificate that has not expired.
func (i Identity) SignsAt(now time.Time) bool { return i.State == StateCurrent && !i.Expired(now) }

// Listed reports whether npm's keys endpoint lists the identity's key while some identity is current (R-SIG-045's table): announced,
// current and previous; never pending or retired.
func (i Identity) Listed() bool {
	return i.State == StateAnnounced || i.State == StateCurrent || i.State == StatePrevious
}
