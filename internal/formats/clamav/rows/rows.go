// Package rows holds the values of the clamav format's data port (Q-ARCH-193): what a commit of a definition file carries as its
// payload, what the side table clamav_version keeps of a version (formats/clamav.md R-CLAMAV-002, R-CLAMAV-003), and what the package
// view and the endpoint read back. The format (internal/formats/clamav), its data package (internal/formatdata/clamav) and the malware
// scanning's database task (internal/malware/database) share them; the package imports nothing but the standard library.
package rows

import "time"

// Verification is how the database task checked a container before it stored it (R-MAL-033, R-CLAMAV-003).
type Verification string

// The verifications.
const (
	// VerificationSignature: the archive has the header's MD5 and the header's signature verifies against ClamAV's public key.
	VerificationSignature Verification = "signature_verified"
	// VerificationRepository: a container copied from an installation's own repository (R-MAL-012), checked for its header and size
	// only; Container.Source names that repository.
	VerificationRepository Verification = "not_verified_repository"
	// VerificationUnsigned: an unsigned container (.cud), which only an installation's own repository supplies.
	VerificationUnsigned Verification = "unsigned"
)

// Valid reports whether v is one of the verifications.
func (v Verification) Valid() bool {
	return v == VerificationSignature || v == VerificationRepository || v == VerificationUnsigned
}

// Member is one file of a container's archive: its name, size and SHA-256 as the container's index lists them, and the number of its
// lines that are neither empty nor a comment.
type Member struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Lines  int    `json:"lines"`
}

// Container is the payload of the commit of a container (<db>/<version>/<db>.cvd or .cud): every piece of its header, how it was
// verified, the MD5 the task computed over the archive and the member list. The format's OnCommit writes it as the side row of the
// version, and refuses the commit of a container without it.
type Container struct {
	// DB is main, daily or bytecode; Version the header's field 3. Both must agree with the path of the commit.
	DB      string
	Version int
	// BuildTimeText is the header's field 2 as written; BuiltAt the build time (field 9, else field 2).
	BuildTimeText string
	BuiltAt       time.Time
	// Signatures is field 4, FunctionalityLevel field 5, Builder field 8.
	Signatures         int
	FunctionalityLevel int
	Builder            string
	// HeaderMD5 is field 6; ComputedMD5 the MD5 the task computed over the archive (every byte after the header), both in lower-case
	// hex, ComputedMD5 empty where the task computed none.
	HeaderMD5   string
	ComputedMD5 string
	// Verification is how the container was checked; Source the repository a container of VerificationRepository or
	// VerificationUnsigned was copied from ("ops/clamav-db"), empty otherwise.
	Verification Verification
	Source       string
	// Members are the member files, sorted by name; MembersRead is false when the bounds of R-MAL-033 cut the read short, and
	// Members is then empty.
	Members     []Member
	MembersRead bool
}

// Signature is the payload of the commit of a detached signature (<db>/<version>/<db>-<version>.cvd.sign): its database and version,
// and the number of signatures it holds (its lines after #clamsign-1.0). Its name and size are the item's.
type Signature struct {
	DB         string
	Version    int
	Signatures int
}

// State is the state of a version as the format's rows say it (R-CLAMAV-003).
type State string

// The states.
const (
	// StateInUse: the version the set in use names, which the endpoint serves.
	StateInUse State = "in_use"
	// StatePrevious: the version the set before it names, which a put-back needs.
	StatePrevious State = "previous"
	// StateSuperseded: stored, named by neither set, waiting for the cleanup.
	StateSuperseded State = "superseded"
	// StateFilesRemoved: the cleanup removed its files; the row is history (R-MAL-034).
	StateFilesRemoved State = "files_removed"
)

// DBVersion names one version of one database.
type DBVersion struct {
	DB      string
	Version int
}

// Version is a version as the side table keeps it: the container's metadata, its detached signature's facts, and its state.
type Version struct {
	VersionID int64
	DB        string
	Version   int
	// File is the container's file name: main.cvd or main.cud.
	File      string
	Container Container
	// SignName is the detached signature's file name, "" while none was stored; SignSize its size and SignSignatures its count.
	SignName       string
	SignSize       int64
	SignSignatures int
	InUse          bool
	Previous       bool
	StoredAt       time.Time
	// FilesRemovedAt is set once the cleanup removed the version's files (R-MAL-034).
	FilesRemovedAt *time.Time
}

// State derives the state of the version from its row.
func (v Version) State() State {
	switch {
	case v.FilesRemovedAt != nil:
		return StateFilesRemoved
	case v.InUse:
		return StateInUse
	case v.Previous:
		return StatePrevious
	}
	return StateSuperseded
}

// Served is what the endpoint serves of a database (R-CLAMAV-004): the version the set in use names and its container's file name.
type Served struct {
	Version int
	File    string
}
