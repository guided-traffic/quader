package signatures

import (
	"encoding/json"
	"time"
)

// The records of the management side of signatures (R-SIG-015 to R-SIG-021, R-SIG-034, R-SIG-038): what the store answers and the API
// renders. The store is internal/signatures/store; the API's port is v1.SignatureStore.

// Settings is the one row of signature_settings (R-SIG-019) with the state of the public Sigstore root and the backlog (R-SIG-034).
type Settings struct {
	// ProjectAnchors is the switch: project Owners may add trust anchors for their project (default off).
	ProjectAnchors bool
	// AnchorEpoch rises with every write of an anchor, a block, the switch and a stored public root (R-SIG-012).
	AnchorEpoch int64
	Version     int64
	UpdatedAt   time.Time
	// UpdatedBy is the name of the principal of the last change, "" for none.
	UpdatedBy string
	TrustRoot TrustRootState
	Backlog   Backlog
}

// TrustRootState is where the public Sigstore root and npm's keys in use come from (R-SIG-017).
type TrustRootState struct {
	// Source is "refreshed" (the task stored a newer root) or "release" (the release's snapshot).
	Source string
	// FetchedAt is the refresh's time, or the release snapshot's build time; TUFVersion the TUF repository's version of the root in use.
	FetchedAt  time.Time
	TUFVersion int64
	// LastError and LastErrorAt are the last failure of the task, "" for none since the last success.
	LastError   string
	LastErrorAt time.Time
}

// The sources of [TrustRootState].
const (
	TrustRootRefreshed = "refreshed"
	TrustRootRelease   = "release"
)

// Backlog is the work of the job: signatures due or waiting for their check, and trust rows computed under an older epoch.
type Backlog struct {
	Pending int64
	Stale   int64
}

// Fingerprint is what an anchor shows of one piece of its material (R-SIG-015): its kind ("sha256" of a certificate or a key, "openpgp"
// of a key), the value, the subject or user ID, and the expiry.
type Fingerprint struct {
	Kind     string    `json:"kind"`
	Value    string    `json:"value"`
	Subject  string    `json:"subject,omitempty"`
	NotAfter time.Time `json:"notAfter,omitzero"`
}

// AnchorRecord is one row of signature_anchors as the API shows it.
type AnchorRecord struct {
	ID         int64
	Scope      string
	ProjectID  string
	ProjectKey string
	Key        string
	Title      string
	Kind       string
	// Material is the anchor's material, the JSON of anchors.Material.
	Material  json.RawMessage
	Enabled   bool
	Note      string
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
	// CreatedBy is the name of the principal that created it, "" for the release and the configuration.
	CreatedBy    string
	Fingerprints []Fingerprint
	// Signatures is the number of signatures the anchor knows in the installation's scope (signature_trust.anchor_ids).
	Signatures int64
	// Applied is false for a project anchor while the switch is off, and for a switched-off anchor.
	Applied bool
}

// AnchorQuery selects anchors: of a scope ("" every scope of the installation, ScopeProject every project's), or of one project.
type AnchorQuery struct {
	Scope     string
	ProjectID string
}

// AnchorInput is a new anchor (R-SIG-015): its scope (installation or project), project, key, title, kind, the material (the JSON of
// anchors.Material with the kind's member), a note and whether it is switched on (default on).
type AnchorInput struct {
	Scope     string
	ProjectID string
	Key       string
	Title     string
	Kind      string
	Material  json.RawMessage
	Note      string
	Enabled   *bool
}

// AnchorPatch is a change of an anchor: nil members stay. A shipped anchor takes only Enabled; an anchor of the configuration none.
type AnchorPatch struct {
	Title    *string
	Note     *string
	Material json.RawMessage
	Enabled  *bool
}

// BlockRecord is one row of signature_blocks (R-SIG-020).
type BlockRecord struct {
	ID        int64
	Kind      string
	Value     string
	Issuer    string
	Reason    string
	CreatedAt time.Time
	CreatedBy string
	// Signatures is the number of signatures the block matches (trust rows blocked by it in the installation's scope).
	Signatures int64
}

// BlockInput is a new block.
type BlockInput struct {
	Kind   string
	Value  string
	Issuer string
	Reason string
}

// ViewQuery asks for the signatures a reader sees of some subjects (R-SIG-004, R-SIG-034): those attached in the repositories named,
// with their trust in the scope of ProjectID (the project's uuid; "": the installation's).
type ViewQuery struct {
	// RepoIDs are the repositories whose attachments count: the addressed repository, or the members of a virtual repository that serve
	// the subjects to the caller.
	RepoIDs []int64
	// Subjects are the blobs of the files asked about; SubjectNames npm's <name>@<version> of a registry signature, whose subject is the
	// digest of its message (Q-SIG-50).
	Subjects     [][32]byte
	SubjectNames []string
	ProjectID    string
}

// View is one signature as a reader sees it: the row, its object's scheme, media type and size, its attachments in the repositories
// asked, and its trust row in the scope (the project's when the project has one, else the installation's).
type View struct {
	Signature   Signature
	Scheme      string
	MediaType   string
	ObjectSize  int64
	Attachments []Attachment
	// Trust is nil when the signature has no trust row (any state but valid and no_key).
	Trust *Trust
}

// Trust is a trust row as a reader sees it (R-SIG-008, R-SIG-012).
type Trust struct {
	ProjectID  string
	State      string
	Anchors    []AnchorRef
	Block      *BlockRecord
	Epoch      int64
	ComputedAt time.Time
	// Stale says the row was computed under an older anchor epoch: the trust job has not reached it since the last change (H-1175).
	Stale bool
}

// AnchorRef names an anchor at a signature: its ID, title and scope.
type AnchorRef struct {
	ID    int64
	Title string
	Scope string
}

// FindingsQuery selects the security dashboard's signatures (R-SIG-038): invalid signatures with an attachment and the signatures of
// blocked signers, newest first, within the repositories of the scope.
type FindingsQuery struct {
	// Kinds are "invalid" and "blocked"; empty both.
	Kinds []string
	// RepoIDs are the repositories the caller reads some of; a nil slice is every repository.
	RepoIDs []int64
	// RepoID filters one repository, 0 none.
	RepoID int64
	// Q matches the signer's text (subject, identity, key ID), case-insensitive.
	Q string
	// AfterAt and AfterID are the cursor: the found time and the signature of the last row of the previous page.
	AfterAt time.Time
	AfterID int64
	Limit   int
}

// The kinds of a finding.
const (
	FindingInvalid = "invalid"
	FindingBlocked = "blocked"
)

// Finding is one row of the security dashboard's view.
type Finding struct {
	SignatureID int64
	Kind        string
	Scheme      string
	Role        string
	Reason      string
	Signer      json.RawMessage
	Subject     [32]byte
	SubjectName string
	FoundAt     time.Time
	Block       *BlockRecord
	// Holders are the files of the subject in the repositories of the scope that hold an attachment of the signature.
	Holders []Holder
}

// Holder is a file that holds a subject (or a repository that holds an attachment) in a repository.
type Holder struct {
	RepoID     int64
	ProjectKey string
	RepoKey    string
	Format     string
	// Path is the item path of the subject's file; "" when the repository holds only the attachment (npm's registry signature).
	Path string
}

// VersionBlob is the blob of one file of a version in a repository (R-SIG-034, the version list); Version is the version as the list
// names it (its stored or its display form, whichever was asked).
type VersionBlob struct {
	Version string
	RepoID  int64
	Blob    [32]byte
}
