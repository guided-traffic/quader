package v1

import (
	"context"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/signatures"
	sgate "github.com/guided-traffic/quader/internal/signatures/gate"
)

// The port of the operations of signatures on artifacts (signatures.md R-SIG-034; the store is internal/signatures/store). The store
// answers errors of internal/errs: NotFound for an anchor or a block that is not there; Conflict with CodeAnchorManaged, CodeAnchorExists
// or CodeBlockExists; Forbidden with CodeProjectAnchorsDisabled; Invalid (validation_failed) with field errors at the member of the
// material (code invalid_value or secret_material, R-SIG-015) or of the block; errs.Stale when the version a write names is not the
// row's any more. Every write records its audit event, raises the anchor epoch and wakes the trust job in its transaction (R-SIG-012,
// R-SIG-039).

// SignatureStore is what the operations of signatures need.
type SignatureStore interface {
	// Settings answers the switch, the anchor epoch, the state of the public Sigstore root and the backlog.
	Settings(ctx context.Context) (signatures.Settings, error)
	// UpdateSettings changes the switch under the row's version.
	UpdateSettings(ctx context.Context, actor audit.Actor, projectAnchors bool, version int64) (signatures.Settings, error)

	// Anchors lists anchors by scope (q.Scope "" every scope of the installation but project; ScopeProject every project's), or one
	// project's (q.ProjectID), in scope order and then by title.
	Anchors(ctx context.Context, q signatures.AnchorQuery) ([]signatures.AnchorRecord, error)
	Anchor(ctx context.Context, id int64) (signatures.AnchorRecord, error)
	// CreateAnchor validates the material (anchors.Validate) and stores the anchor of scope installation or project. A project anchor
	// while the switch is off is Forbidden (CodeProjectAnchorsDisabled).
	CreateAnchor(ctx context.Context, actor audit.Actor, in signatures.AnchorInput) (signatures.AnchorRecord, error)
	// UpdateAnchor changes an anchor under its version (0: no check); a shipped anchor takes only Enabled, a configuration anchor
	// nothing (CodeAnchorManaged); a project anchor while the switch is off is Forbidden.
	UpdateAnchor(ctx context.Context, actor audit.Actor, id int64, p signatures.AnchorPatch, version int64) (signatures.AnchorRecord, error)
	// DeleteAnchor removes an anchor of scope installation or project (CodeAnchorManaged for the others). An administrator removes a
	// project anchor whatever the switch says.
	DeleteAnchor(ctx context.Context, actor audit.Actor, id int64) error

	Blocks(ctx context.Context) ([]signatures.BlockRecord, error)
	CreateBlock(ctx context.Context, actor audit.Actor, in signatures.BlockInput) (signatures.BlockRecord, error)
	DeleteBlock(ctx context.Context, actor audit.Actor, id int64) error

	// FindingHolders answers the repositories that hold a finding of the kinds (empty: both), so that the caller's readable ones scope
	// the list; Findings answers a page of the list within q.RepoIDs (Limit+1 rows when more follow).
	FindingHolders(ctx context.Context, kinds []string) ([]signatures.Holder, error)
	Findings(ctx context.Context, q signatures.FindingsQuery) ([]signatures.Finding, error)

	// Views answers the signatures of subjects attached in the repositories of q, with their trust in q's scope; Counts the same, only
	// what the badge reads, per subject (the member signatures of a list of files or versions).
	Views(ctx context.Context, q signatures.ViewQuery) ([]signatures.View, error)
	Counts(ctx context.Context, q signatures.ViewQuery) (map[[32]byte][]signatures.Counted, error)
	// VersionBlobs answers the blobs of the files of versions of a package in repositories, in one statement: the subjects a page of the
	// version list counts.
	VersionBlobs(ctx context.Context, repoIDs []int64, name string, versions []string) ([]signatures.VersionBlob, error)

	// ObjectHolders answers the repositories that hold an attachment of a signature object (the permission of its download);
	// ObjectBytes its original bytes and media type ("" when none is known), read from PostgreSQL or the object store.
	ObjectHolders(ctx context.Context, sha256 [32]byte) ([]signatures.Holder, error)
	ObjectBytes(ctx context.Context, sha256 [32]byte) ([]byte, string, error)

	// Origin answers the origin of an uploaded item (R-SIG-010); ok is false for an item no upload recorded one of.
	Origin(ctx context.Context, itemID int64) (signatures.StoredOrigin, bool, error)
}

// SignatureServices are what the signature operations need; without Store they answer 503.
type SignatureServices struct {
	Store SignatureStore
	// Enabled is signatures.enabled: false renders no member signatures and the operations that need the checks answer 409
	// signatures_not_enabled.
	Enabled bool
	// Decider decides what a caller may read; Repos resolves repositories and projects.
	Decider authz.Decider
	Repos   repository.Reader
	// Coverage answers the signed documents that name a stored file (Debian's releases, R-SIG-030); nil lists none.
	Coverage SignatureCoverage
	// Gate is the gate per repository of stage 3 (R-SIG-061 to R-SIG-073), nil while the signatures are off: it judges the members
	// refused and gate, the preview, a release and the impact. GateStore keeps the releases and the refused downloads; without it the
	// operations of the gate answer 503. PreviewLimit is signatures.gate.preview_limit (R-SIG-071, R-SIG-072).
	Gate         *sgate.Gate
	GateStore    SignatureGateStore
	PreviewLimit int
}
