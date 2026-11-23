package repository

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/qname"

	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
	"github.com/guided-traffic/quader/internal/signatures/gateset"
	"github.com/guided-traffic/quader/internal/vuln"
)

// Class is the class of a repository (R-REPO-001). It is fixed at creation. The
// catalog owns the type, so a format and the repository model name one.
type Class = catalog.Class

// The classes.
const (
	Local   = catalog.Local
	Remote  = catalog.Remote
	Virtual = catalog.Virtual
)

// FormatGeneric is the format of the generic local repository that M1 proves
// raw transfer on (Q-ROAD-1). Other formats arrive with M2.
const FormatGeneric = "generic"

// A repository key has the grammar of qname.ValidKey (R-REPO-002) and must not be a reserved name; a violation answers 422
// invalid_key or reserved_name.

// ReservedKeys are the literal sub-paths of /api/v1/repositories that a key
// cannot take (R-REPO-002).
func ReservedKeys() []string {
	return []string{"batch-create", "batch-update", "batch-get", "batch-delete", "quick-setup"}
}

// Lifecycle is the state of a repository row (R-REPO-013).
type Lifecycle string

// The lifecycle states.
const (
	Active   Lifecycle = "active"
	Deleting Lifecycle = "deleting"
)

// Repository is the read model other packages use. Config is the document of
// R-REPO-004 as stored, without secrets; the repository owner decodes it.
type Repository struct {
	ID string
	// Name is the qualified name of the repository: its project key and its key (Q-REPO-212).
	Name   qname.Repo
	Class  Class
	Format string
	// Disabled repositories take no part in resolution, download or deploy.
	Disabled bool
	// Visibility is the levels of the artifacts and the wiki (R-AUTHZ-033), read from the row's columns.
	Visibility Visibility
	// HideExistence is "inherit", "true" or "false" (R-AUTHZ-017).
	HideExistence string
	Lifecycle     Lifecycle
	// ProjectID is the ID (uuid text) of the project the repository belongs to
	// (R-AUTHZ-026); Name.Project is its key. Every reader fills both.
	ProjectID string
	// Version is config_version, the ETag source (R-REPO-011).
	Version   int64
	Config    []byte
	CreatedAt time.Time
	UpdatedAt time.Time
	// LastActivityAt is the latest upload or download as the replicas flushed it
	// (R-REPO-012); zero when there was none.
	LastActivityAt time.Time
	// Upstream holds the kind and the state of a repository of the built-in project
	// upstream (R-REPO-066, R-REPO-068); nil for a repository of any other project.
	Upstream *UpstreamFacts

	// adm is the compiled include and exclude lists of Config. The snapshot and
	// the store fill it; a Repository built by hand admits everything.
	adm *Admission
	// remote is the parsed remote section of a remote repository (Remote).
	remote *RemoteSettings

	// PriorityMember is the priorityMember setting (R-REPO-022).
	PriorityMember bool
	// rid is the numeric id; autoClaim is local.autoClaim with the format's default
	// (R-REPO-023); virt is the routing view of a virtual repository (virtual.go).
	rid       int64
	autoClaim bool
	virt      *virtualState
	// vulnerabilities is the member `vulnerabilities` with its defaults, vulnEcosystem the OSV ecosystem of the format's
	// vulnerability adapter, "" for a virtual repository or a format without one (R-VULN-002).
	vulnerabilities vuln.Settings
	vulnEcosystem   string
	// malwareOnlyChecked is the member malware.onlyChecked (R-MAL-020).
	malwareOnlyChecked bool
	// signatureGate is the member signatures.onlyVerified and signatures.anchors (R-SIG-061).
	signatureGate gateset.Setting
	// downloadLinksOff is the member storage.downloadLinks set to false (R-STOR-014).
	downloadLinksOff bool
	// delivery is the compiled delivery mode and rules of a remote repository (remote-curation.md R-CUR-006); nil delivers
	// everything: a remote in mode allExceptBlocked without a rule, and every other class. deliveryRevision is the row's
	// delivery_rules_revision.
	delivery         *rules.Set
	deliveryRevision int64
}

// Delivery is the repository's compiled delivery mode and rules as the replica's snapshot holds them (remote-curation.md R-CUR-003,
// R-CUR-006): the one place every serve point, document and pass of a remote asks, with no statement. Nil delivers everything and
// costs nothing.
func (r Repository) Delivery() *rules.Set { return r.delivery }

// DownloadLinks reports whether the repository lets a download be handed to a signed link of the object store: true unless its
// member storage.downloadLinks says false (R-STOR-014, Q-STOR-282). It reads the replica's snapshot, so the serve point costs no
// query for it; a change is in force on every replica within the propagation bound of the repo_config area.
func (r Repository) DownloadLinks() bool { return !r.downloadLinksOff }

// MalwareOnlyChecked reports the repository's member malware.onlyChecked (R-MAL-020): only checked artifacts are served from it,
// whatever the installation's switch says. It reads the replica's snapshot, so the download gate costs no query for it; a change is
// in force on every replica within the propagation bound of the repo_config area.
func (r Repository) MalwareOnlyChecked() bool { return r.malwareOnlyChecked }

// SignatureGate reports the repository's gate of signatures (R-SIG-061): whether only artifacts with a verified signature are served
// from it, and the anchors it accepts (empty: any anchor of its scope). It reads the replica's snapshot, so the serve point costs no query
// for it; a change is in force on every replica within the propagation bound of the repo_config area.
func (r Repository) SignatureGate() gateset.Setting {
	return gateset.Setting{OnlyVerified: r.signatureGate.OnlyVerified, Anchors: slices.Clone(r.signatureGate.Anchors)}
}

// NumericID is the repository's database ID (ID as a number), 0 when ID is not one.
func (r Repository) NumericID() int64 {
	n, _ := strconv.ParseInt(r.ID, 10, 64)
	return n
}

// VulnerabilitySettings returns the repository's member `vulnerabilities` with the defaults of the members it does not set
// (R-VULN-002). A repository the scanning does not apply to (virtual, or a format without the adapter) returns the defaults.
func (r Repository) VulnerabilitySettings() vuln.Settings {
	if r.vulnEcosystem == "" {
		return vuln.DefaultSettings()
	}
	return r.vulnerabilities
}

// VulnerabilityEcosystem returns the OSV ecosystem the repository's stored versions are matched in, "" when the scanning does
// not apply to the repository: a virtual repository stores nothing, and a format without the vulnerability adapter has none
// (R-VULN-001).
func (r Repository) VulnerabilityEcosystem() string { return r.vulnEcosystem }

// VulnerabilityPolicy resolves the repository's member with the installation's switches (vulnerabilities.enabled and
// vulnerabilities.enforcement) into the policy the download path applies (R-VULN-015). It reads the replica's snapshot, so it
// costs no query; a change is in force on every replica within the propagation bound of the repo_config area.
func (r Repository) VulnerabilityPolicy(installationEnabled bool, enforcement vuln.Enforcement) vuln.Policy {
	return vuln.PolicyFor(r.VulnerabilitySettings(), r.vulnEcosystem, installationEnabled, enforcement)
}

// Admits reports whether a canonical path is admitted by the repository's
// include and exclude patterns (R-REPO-005). It applies to every caller,
// administrators included: the patterns are routing rules. A folder (a path that
// ended in a slash) is admitted when some path below it could be.
func (r Repository) Admits(path string, folder bool) bool {
	if folder {
		return r.adm.AdmitsFolder(path)
	}
	return r.adm.Admits(path)
}

// Restricts reports whether the repository has include or exclude patterns, that is,
// whether Admits can answer false for some name or path. A listing that counts the
// versions of a repository without asking Admits about each is right only for a
// repository that does not.
func (r Repository) Restricts() bool { return r.adm.restricts() }

// Reader is the read interface other packages use. It answers from the
// replica's snapshot, which the change area repo_config keeps current.
type Reader interface {
	// ByName returns the repository, or an *errs.Error of kind NotFound with code
	// repository_not_found. It does not apply authorization: callers decide
	// first (R-AUTHZ-016: authorization runs before the existence lookup).
	ByName(ctx context.Context, name qname.Repo) (Repository, error)
	// ByID returns the repository of a numeric id, or an *errs.Error of kind NotFound with code repository_not_found.
	// A running job holds its repository by the id, which a move does not change (R-REPO-060); the name is only for logs
	// and metrics. Like ByName it applies no authorization.
	ByID(ctx context.Context, id int64) (Repository, error)
	// List returns every repository, sorted by project key and key. Callers filter
	// it through authz; a listing contains only what the caller may read
	// (R-AUTHZ-016).
	List(ctx context.Context) ([]Repository, error)
	// ListProject returns the repositories of one project, sorted by key; none
	// for a project that does not exist.
	ListProject(ctx context.Context, project string) ([]Repository, error)
}
