package formats

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

// ContractVersion changes whenever a method of the contract changes meaning or
// leaves it (R-ARCH-025). The contract is frozen since the end of M3 (E3.3, ADR-141): an
// addition keeps the version, and a breaking change raises it and moves every format in the
// same change. Version 2 is the project model's (ADR-156): the repository is named by its
// qualified name, the read scope, the link builder and the refusals changed. Version 3 is the upload sessions' (ADR-178):
// UploadSessions.Finalize takes the offset of its last bytes and returns the received count with a refusal of it. Version 4 is the
// upstream presets' (ADR-198): catalog.Facts.UpstreamPresets replaces DefaultUpstreamURL, DefaultExtraHosts and
// DefaultCredentialHosts. Version 5 is the fetch of a version's (ADR-205): Descriptor.VersionFetch is required of a format with a
// package view, ViewSizer reports a ViewSize that may be partial or not known, and ItemStore records the download of a document a
// format answers itself. Version 6 is the download limits' (usage-and-limits.md R-LIM-036, Q-LIM-24): Host.Serve may refuse a
// download with the 429 of the limits before its status line, and a route's kind declares what the limits count (Download: admitted,
// paced, a parallel lease; Metadata, Computed, Volatile: documents; Upload, Delete, Admin: writes). Version 7 is the probes'
// (repositories-and-proxy.md R-REPO-065, ADR-207): a format with the class remote declares its probes in [Descriptor.Probes] (or why its
// protocol has none), its RemoteAdapter names a probed version's content (ProbeContent), and a handler tells the host of a probe it
// answered (RemoteFetcher.Probed). Version 8 is the signatures' (signatures.md R-SIG-022, ADR-228): every format declares its
// signatures and detects them ([Format.Signatures]); Base returns nil and Register refuses nil. Version 9 is the gate per repository's
// (signatures.md R-SIG-063, Q-SIG-81, ADR-231): a format's [SignatureAdapter] names what the signature gate judges of each stored item
// it serves (Gate), and [SignatureVerdicts] judges a stored document a format serves without the item store (Refuse). Version 10 is the
// receipt attestations' (signatures.md R-SIG-044, Q-SIG-67, Q-SIG-180): a format's [SignatureAdapter] names the stored items Quader
// attests (AttestationSubjects), its declaration says what it attests or why nothing (Attests, AttestsNone), and NoSignatures takes both
// reasons. Version 11 is the upstream catalogue's (remote-curation.md R-CUR-006, R-CUR-007, Q-CUR-12, ADR-277): a format with the class
// remote declares its catalogue ([Descriptor.Catalogue], the lookup by exact name at least) and its version canon
// ([Descriptor.VersionCanon]). Version 12 is the stored sort key's (api-and-ui.md R-API-029, Q-API-429, ADR-278): a format's
// [ViewAdapter] gives the key of its order of versions (SortKey), which the host stores with every version row. Version 13 is the
// pre-release's (api-and-ui.md R-API-029, Q-API-433): a format's [ViewAdapter] says which of its versions is a pre-release (Prerelease),
// beside its order, and the [ExposureAdapter] no longer does; the catalog's Versioning takes the answer from the view adapter. Version 14
// is the range of a package's allow (remote-curation.md R-CUR-026, Q-CUR-221, Q-CUR-226, ADR-283): ModeReason leaves (an emptied
// document's reason is the rule set's EmptyReason, outsideRange under a range), the catalog's entry carries the view adapter's order
// (catalog.Entry.VersionOrder) and the mirror's upstream says what the rules deliver (discover.Delivering); no format's code changes. A
// format declares the literal it was written against in [Descriptor.ContractVersion], and Register panics on a mismatch.
const ContractVersion = 14

// The types of other packages under the names a format spells them: one type
// each, no mirrors (Q-ARCH-188, Q-ARCH-189). A format imports this package and
// nothing else of the core; it reads rq.Subject, rq.Index and rq.Repo without
// naming their packages.
type (
	// ID is the name of a format: "generic", "npm", "nuget" (R-ARCH-005).
	ID = catalog.ID
	// Class is a repository class a format supports (R-REPO-001).
	Class = catalog.Class
	// PatternSubject says what include and exclude patterns of a format match
	// (R-REPO-005).
	PatternSubject = catalog.PatternSubject
	// RequestKind is how a request behaves in caches (R-REPO-003).
	RequestKind = catalog.RequestKind
	// Action is a repository action (R-AUTHZ-006).
	Action = authz.Action
	// Path is the canonical repository path (R-ARCH-005): Path, "" for the root,
	// and Folder.
	Path = repository.Cleaned
	// Limits are the resource limits of untrusted content (R-ARCH-028).
	Limits = untrusted.Limits
	// Tx is the storage transaction as a format holds it: opaque, with no way to
	// run SQL (meta.Tx's own rule). A format passes it to its data port and never
	// unwraps it.
	Tx = meta.Tx
)

// The classes (R-REPO-001).
const (
	Local   = catalog.Local
	Remote  = catalog.Remote
	Virtual = catalog.Virtual
)

// The pattern subjects.
const (
	// SubjectName: patterns match the normalized package name.
	SubjectName = catalog.SubjectName
	// SubjectPath: patterns match the canonical path.
	SubjectPath = catalog.SubjectPath
)

// The request kinds.
const (
	// KindArtifact is immutable binary content.
	KindArtifact = catalog.KindArtifact
	// KindMetadata is an upstream document that expires.
	KindMetadata = catalog.KindMetadata
	// KindSearch is a query, passed through and never cached.
	KindSearch = catalog.KindSearch
	// KindWrite changes state: a deploy or a delete.
	KindWrite = catalog.KindWrite
	// KindOther is everything else, such as a directory index that quader renders.
	KindOther = catalog.KindOther
)

// The repository actions. They carry a prefix because Delete is also a route
// kind.
const (
	ActionRead      = authz.Read
	ActionAnnotate  = authz.Annotate
	ActionDeploy    = authz.Deploy
	ActionOverwrite = authz.Overwrite
	ActionDelete    = authz.Delete
	ActionManage    = authz.Manage
)

// Format is a package format: protocol, classification, storage hooks, adapters.
// Its methods never touch storage; a format reaches the core only through Host
// (R-ARCH-009). Embed [Base] for the defaults of what a format lacks.
type Format interface {
	Descriptor() Descriptor

	// Routes lists every protocol operation below /api/<id>/<projectKey>/<repoKey>/ (below /v2/<projectKey>/<repoKey>/ for
	// the format that declares [catalog.Facts.RegistryRoot]). It is empty
	// for a format whose protocol is /files/ alone: generic (Q-GENERIC-7).
	Routes() []Route
	// Match resolves method and the repository-relative path, still
	// percent-encoded, to one route. It returns errs.NotFound or
	// errs.MethodNotAllowed (with Allow), and never touches storage. Most formats
	// return MatchRoutes(f.Routes(), method, escapedPath).
	Match(method, escapedPath string) (*Route, Params, error)
	// Classify says how a canonical path behaves in caches and which package it
	// belongs to. It gives a /files/ request its coordinate (R-EP-014) and every
	// request its R-REPO-003 descriptor.
	Classify(p Path) PathInfo
	// PackageDocumentPath returns the canonical path of the document that stands
	// for a normalized package name upstream (R-REPO-029, R-REPO-048); ok is false
	// for a format without package names.
	PackageDocumentPath(name string) (p Path, ok bool)

	// IngestOptions says what the upload pipeline expects of one request.
	IngestOptions(rq *Request) IngestOptions
	// Inspect reads identity and namespaced properties from an upload (FMT-008).
	Inspect(ctx context.Context, in InspectInput) (*Inspection, error)
	// OnCommit runs in the transaction of a commit (storage.md): package and
	// version rows, the lead item.
	OnCommit(ctx context.Context, tx Tx, c CommitEvent) error
	// OnItemsRemoved runs in the transaction that removed items.
	OnItemsRemoved(ctx context.Context, tx Tx, e ItemsEvent) error

	// ServeCheck runs before the core serves a stored item on any route, /files/
	// included; errs.NotFound refuses it because of format state the generic item
	// row does not carry. It reads through h and runs no SQL.
	ServeCheck(ctx context.Context, h Host, repo RepoRef, it ItemRef, mode ServeMode) error

	// Remote is nil exactly when Classes lacks Remote; Register checks.
	Remote() RemoteAdapter
	// Virtual is nil exactly when Classes lacks Virtual.
	Virtual() VirtualAdapter
	// Exposure is nil for a format that is not exposable (R-EP-018).
	Exposure() ExposureAdapter
	// View is nil for a format without package screens (R-API-029): the adapter
	// reads what the generic rows do not hold and orders the versions.
	View() ViewAdapter
	// Signatures declares where a format's signatures live and finds them in what a commit stores
	// (signatures.md R-SIG-022). Every format implements it; Base returns nil, and Register refuses nil.
	Signatures() SignatureAdapter

	// SetupTemplates are the client setup snippets (R-API-030).
	SetupTemplates() []SetupTemplate
	// Guide is the format's guide: what a repository of each class is and how each
	// client uses it (R-UI-049). A format with clients covers every class and client
	// with the required topics (kit check 8).
	Guide() []GuideSection
	// WriteError renders a core error in the protocol's native shape (R-ARCH-023).
	// The dispatcher also uses it for the refusals of the chain's stages on this
	// format's paths (Q-ARCH-214).
	WriteError(w http.ResponseWriter, r *http.Request, err error)
}

// Descriptor is what a format registers: the catalog facts every layer reads
// (Register puts them into the catalog) and what only the contract needs.
type Descriptor struct {
	catalog.Facts
	// ContractVersion is the literal the format was written against; Register
	// panics on a mismatch (R-ARCH-010).
	ContractVersion int
	// Names is the format's one package-name normaliser (R-AUTHZ-005); nil exactly
	// when PackageNames is false.
	Names catalog.Names
	// Clients are the native clients and their tested versions (FMT-002, FMT-003).
	Clients []Client
	// PropertyPrefix is the reserved property namespace, "npm."; Inspect and
	// SetProperties write only below it (FMT-008).
	PropertyPrefix string
	// MaxDocumentBytes bounds a document held in memory; the smaller of it and
	// proxy.max_metadata_bytes applies (R-ARCH-028).
	MaxDocumentBytes int64
	// VerifyAnnouncedDigests says that the format's protocol addresses artifacts by
	// the digests its metadata announces, so a remote fill compares them whatever
	// integrity.checksumPolicy says (R-REPO-041: npm always verifies, R-NPM-025). It
	// is held equal to Facts.ChecksumAlwaysVerified, which the repository validation reads.
	VerifyAnnouncedDigests bool
	// Limits tighten untrusted.DefaultLimits; a zero field keeps the default.
	Limits Limits
	// NotifyAreas are the notification areas the format may name in
	// [PackageWriter.Notify]: signal areas of [catalog.FormatAreas] only. The package
	// writer raises the signal of a changed package itself (Q-ARCH-259); a format
	// declares an area for the change that raised no package row, such as the
	// replacement of a stored remote document. Register refuses any other area, and
	// the host refuses a Notify for an area the format did not declare.
	NotifyAreas []string
	// DataPort is the interface type of the format's side-table port (Q-ARCH-193),
	// for example reflect.TypeFor[npm.Store](); nil for a format without side
	// tables. The composition root binds one value per format and refuses to
	// start when a binding is missing or does not implement the port.
	DataPort reflect.Type
	// VersionFetch says what a fetch of one of the format's versions is: the downloads the package screens count as its
	// downloads and its last download (R-API-079). Required of a format with a package view; Register refuses one without it.
	VersionFetch VersionFetch
	// Probes are the requests of the format's remote and virtual classes that name exactly one version or one file without fetching
	// its content (repositories-and-proxy.md R-REPO-065): their handlers call RemoteFetcher.Probed, which may start the background
	// download of the version, through a virtual repository at the member that serves it (Q-REPO-367). A format with the class
	// remote declares at least one, or says in NoProbe why its protocol has none; Register refuses one that does neither, so that a
	// format cannot land without the rule (W-25).
	Probes []ProbeRoute
	// NoProbe is the reason a format with the class remote has no probe; empty for a format that declares Probes.
	NoProbe string
	// Catalogue declares what the format's remote adapter answers of the upstream catalogue and why it answers no more
	// (remote-curation.md R-CUR-007); required of a format with the class remote, whose floor is the lookup by exact name.
	Catalogue CatalogueDeclaration
	// VersionCanon is the format's canonical form of a version (R-CUR-006): a delivery rule stores it and the decision folds what it is
	// asked with it. Required of a format with the class remote.
	VersionCanon catalog.VersionCanon
}

// ProbeRoute names one probe of a format (R-REPO-065): a route of [Format.Routes] that serves the class remote and the method of
// the request, GET or HEAD (a HEAD reaches the GET route of its path), with what the request names in words.
type ProbeRoute struct {
	Route  string
	Method string
	// What is the request in words: "the manifest of a tag", "a HEAD of the tarball".
	What string
}

// ItemsRestorer is the optional hook of a format that keeps state of its own for
// restored items: it runs in the transaction that restored them, with the writer
// and the port of the event (R-STOR-022). It is not part of [Format]: the restore
// from the trash does not exist yet, so the method could not be implemented as
// frozen (Q-ARCH-256). A format that has it implements the method; [CapabilitiesOf]
// finds it.
type ItemsRestorer interface {
	OnItemsRestored(ctx context.Context, tx Tx, e ItemsEvent) error
}

// SourceSpeller is the optional hook of a format whose items have more than one canonical spelling in a repository (OCI on a Docker Hub remote:
// `nginx` and `library/nginx` are one cached copy). SourceSpellings returns the other paths under which the item at p of a remote repository whose upstream is upstreamURL ("" for another class) is decided; a link
// whose source is that item (a cross-repository mount) must be allowed to read each of them as well as p, so that an exclusion written for either
// spelling holds for a link as it does for a read (Q-OCI-129). [CapabilitiesOf] finds it.
type SourceSpeller interface {
	SourceSpellings(upstreamURL string, p Path) []Path
}

// RemovalLocker is the optional hook of a format whose writers take a lock of their own first (a package row): LockRemoval runs in the
// transaction of a removal, before the item store deletes the rows of the files at paths, with the writer of the transaction, so that the
// removal waits in the order a commit does (OCI: a manifest push takes the package row and then holds the rows of the layers it names).
// It decides nothing and removes nothing; [CapabilitiesOf] finds it (ADR-168).
type RemovalLocker interface {
	LockRemoval(ctx context.Context, tx Tx, w PackageWriter, repo RepoRef, paths []string) error
}

// Capabilities are the optional behaviours of a format, found once, when the format
// registers (R-ARCH-025). A new behaviour is added to the contract as an optional
// interface and a field here; no existing format changes, and ContractVersion stays
// (Q-ARCH-256). The core asks the registry ([CapabilitiesFor]), never the format
// value, so a capability cannot be missed by a call site that forgot to look.
type Capabilities struct {
	// ItemsRestorer is the restore hook; nil for a format without one.
	ItemsRestorer ItemsRestorer
	// RemovalLocker takes the locks a format's writers take first, before a removal deletes item rows; nil for a format without one.
	RemovalLocker RemovalLocker
	// Remote are the optional interfaces of the format's remote adapter (the fill
	// hook and the discovery document); the zero value for a format without them.
	Remote RemoteCaps
	// Vulnerability is the adapter that lets the core match the format's stored versions against advisories
	// (R-VULN-009, ADR-150); nil for a format without one, whose repositories are not_covered.
	Vulnerability VulnerabilityAdapter
	// ImageScanned is the optional interface of a format whose content the image scanning reads (R-VULN-033).
	ImageScanned ImageScanned
	// ServeFacts names the rows the format's serve check decides on for a path, so that the item lookup of a download reads
	// them in its own statement (R-ARCH-027, Q-ARCH-291); nil for a format whose check reads nothing besides the item.
	ServeFacts ServeFactsKeyer
	// SourceSpeller names the other spellings of an item that a link's source is decided on (Q-OCI-129); nil for a format whose items have one.
	SourceSpeller SourceSpeller
	// ViewSizer reports the size of a version that is not the sum of its files (ADR-177); nil for a format whose versions are.
	ViewSizer ViewSizer
	// VersionPath gives the path of the file that holds a version's content (Q-AUTHZ-255); nil for a format whose
	// versions have none.
	VersionPath VersionPather
	// SignatureCoverage answers which signed documents name a stored file (signatures.md R-SIG-030); nil for a format whose signatures
	// cover the artifact itself.
	SignatureCoverage SignatureCoverage
	// AttestationReferrer stores Quader's receipt attestation among an artifact's referrers on the format's own wire (signatures.md
	// R-SIG-054); nil for a format whose attestations the API alone serves.
	AttestationReferrer AttestationReferrer
	// Deprecations finds the stored versions that are deprecated (R-API-085); nil for a format whose versions never are.
	Deprecations DeprecationFinder
	// Deprecator sets and clears a deprecation in a local repository (R-API-085); nil for a format whose protocol carries none
	// that Quader sets.
	Deprecator Deprecator
}

// VersionPather is the optional interface of a format whose versions each have one file that holds their content: npm's
// tarball, NuGet's .nupkg. VersionPath returns its canonical path for a normalized name and a version as the format stores
// it, the path a protocol download of the version is decided on; ok is false for a pair that names no version. The
// decision point decides a version on that path as well as on the package document's (catalog.Entry.VersionPath), so a
// path pattern that hides the file hides the version from listings, documents and the search (R-AUTHZ-005, Q-AUTHZ-255).
// Classify of the returned path gives the name and the version back; the conformance kit checks it.
type VersionPather interface {
	VersionPath(name, version string) (p Path, ok bool)
}

// CapabilitiesOf reads the capabilities off a format. It is the one function of the
// module that asserts a value to an interface of this package (the architecture
// test allows no other), and [Register] and the conformance kit are its callers.
func CapabilitiesOf(f Format) Capabilities {
	var c Capabilities
	if r, ok := f.(ItemsRestorer); ok {
		c.ItemsRestorer = r
	}
	if l, ok := f.(RemovalLocker); ok {
		c.RemovalLocker = l
	}
	if v, ok := f.(VulnerabilityAdapter); ok {
		c.Vulnerability = v
	}
	if i, ok := f.(ImageScanned); ok {
		c.ImageScanned = i
	}
	if s, ok := f.(SourceSpeller); ok {
		c.SourceSpeller = s
	}
	if k, ok := f.(ServeFactsKeyer); ok {
		c.ServeFacts = k
	}
	if z, ok := f.(ViewSizer); ok {
		c.ViewSizer = z
	}
	if v, ok := f.(VersionPather); ok {
		c.VersionPath = v
	}
	if sc, ok := f.(SignatureCoverage); ok {
		c.SignatureCoverage = sc
	}
	if ar, ok := f.(AttestationReferrer); ok {
		c.AttestationReferrer = ar
	}
	if df, ok := f.(DeprecationFinder); ok {
		c.Deprecations = df
	}
	if dp, ok := f.(Deprecator); ok {
		c.Deprecator = dp
	}
	if ra := f.Remote(); ra != nil {
		c.Remote.Discovery, _ = ra.(Discoverer)
		c.Remote.Fill, _ = ra.(FillInspector)
		c.Remote.Mirror, _ = ra.(MirrorLister)
		c.Remote.Gate, _ = ra.(FactsGate)
		c.Remote.Auth, _ = ra.(Authenticator)
		c.Remote.Stood, _ = ra.(StoodChallengeReader)
		c.Remote.Exchange, _ = ra.(CredentialExchanger)
		c.Remote.Revalidate, _ = ra.(Revalidator)
		c.Remote.Names, _ = ra.(NameServer)
		c.Remote.Signatures, _ = ra.(SignatureFetcher)
		c.Remote.Catalogue, _ = ra.(Catalogue)
		c.Remote.VersionProbe, _ = ra.(VersionProber)
	}
	return c
}

// formatRegistry holds the registered formats; the facts live in the catalog.
var formatRegistry = struct {
	mu   sync.RWMutex
	by   map[ID]Format
	caps map[ID]Capabilities
}{by: map[ID]Format{}, caps: map[ID]Capabilities{}}

// Register adds a format to the process catalog ([catalog.Default]) and to the
// registry of formats, and records its [Capabilities]. It panics, and the process exits, on a programming error
// (R-ARCH-010): a duplicate or reserved ID, a ContractVersion mismatch, a route
// set the ambiguity check refuses, a class without its adapter (or an adapter
// without its class), and every rule of [catalog.Registry.Register]. Formats are
// registered at compile time by internal/formats/all.
//
// The exposure facts of the catalog come from the format's [ExposureAdapter]: no
// adapter means the format is not exposable, and the adapter is the one source of
// its dimensions and its version syntax (C-13).
func Register(f Format) {
	d := f.Descriptor()
	if err := Validate(f); err != nil {
		panic(fmt.Sprintf("formats: %v", err))
	}
	formatRegistry.mu.Lock()
	defer formatRegistry.mu.Unlock()
	if _, dup := formatRegistry.by[d.ID]; dup {
		panic(fmt.Sprintf("formats: %q is registered twice", d.ID))
	}
	catalog.Default().Register(CatalogEntry(f))
	formatRegistry.by[d.ID] = f
	formatRegistry.caps[d.ID] = CapabilitiesOf(f)
}

// CapabilitiesFor returns the capabilities recorded when the format registered; the
// zero value for a format that is not registered.
func CapabilitiesFor(id ID) Capabilities {
	formatRegistry.mu.RLock()
	defer formatRegistry.mu.RUnlock()
	return formatRegistry.caps[id]
}

// CatalogEntry derives the catalog entry of a format: its facts, its name rule,
// the paths of its package documents and of its versions' files, and the exposure
// facts of its adapter.
func CatalogEntry(f Format) catalog.Entry {
	d := f.Descriptor()
	e := catalog.Entry{Facts: d.Facts, Names: d.Names, VersionCanon: d.VersionCanon}
	if v := f.View(); v != nil {
		e.VersionOrder = v
	}
	if d.PackageNames {
		e.DocumentPath = func(name string) (string, bool) {
			p, ok := f.PackageDocumentPath(name)
			return p.Path, ok && p.Path != ""
		}
	}
	caps := CapabilitiesOf(f)
	if vp := caps.VersionPath; vp != nil && d.PackageNames {
		e.VersionPath = func(name, version string) (string, bool) {
			p, ok := vp.VersionPath(name, version)
			return p.Path, ok && p.Path != ""
		}
	}
	if v := caps.Vulnerability; v != nil {
		e.Ecosystem = v.Ecosystem()
	}
	if i := caps.ImageScanned; i != nil {
		e.ImageEcosystem = i.ImageEcosystem()
	}
	e.Mirror = caps.Remote.Mirror != nil
	e.ClientDigest = slices.ContainsFunc(f.Routes(), func(r Route) bool { return r.ClientDigest.Checks })
	if x := f.Exposure(); x != nil {
		dim := x.Dimensions()
		e.Exposure = catalog.Exposure{Exposable: true, Packages: dim.Packages, Versions: dim.Versions, Tags: dim.Tags}
		if v := f.View(); dim.Versions && v != nil {
			e.Exposure.Versioning = versioning{x: x, v: v}
		}
	}
	return e
}

// Validate runs the contract checks of Register on a format without registering
// it: the checks that need no registry. The conformance kit runs it too, so a
// format's own tests fail with the message the process would exit with.
func Validate(f Format) error {
	d := f.Descriptor()
	id := d.ID
	switch {
	case !qname.ValidFormat(string(id)):
		return fmt.Errorf("invalid or reserved format ID %q", id)
	case d.ContractVersion != ContractVersion:
		return fmt.Errorf("%q was written against contract version %d, this is %d", id, d.ContractVersion, ContractVersion)
	}
	if d.PropertyPrefix != "" && !strings.HasSuffix(d.PropertyPrefix, ".") {
		return fmt.Errorf("%q: property prefix %q does not end in a dot", id, d.PropertyPrefix)
	}
	if d.VerifyAnnouncedDigests != d.ChecksumAlwaysVerified {
		return fmt.Errorf("%q: VerifyAnnouncedDigests (%t) and Facts.ChecksumAlwaysVerified (%t) must agree", id, d.VerifyAnnouncedDigests, d.ChecksumAlwaysVerified)
	}
	if d.DataPort != nil && d.DataPort.Kind() != reflect.Interface {
		return fmt.Errorf("%q: the data port %v is no interface type", id, d.DataPort)
	}
	for _, a := range d.NotifyAreas {
		if !slices.Contains(catalog.FormatAreas, a) {
			return fmt.Errorf("%q declares notification area %q, which a format may not raise (%s)", id, a, strings.Join(catalog.FormatAreas, ", "))
		}
	}
	if err := checkAdapter(id, "Remote", d.Supports(Remote), f.Remote() != nil); err != nil {
		return err
	}
	if err := checkAdapter(id, "Virtual", d.Supports(Virtual), f.Virtual() != nil); err != nil {
		return err
	}
	if caps := CapabilitiesOf(f).Remote; caps.Stood != nil && caps.Auth == nil {
		// The reading of a stood 401 belongs to a handshake: without an Authenticator the engine never asks it (Q-OCI-127).
		return fmt.Errorf("%q implements StoodChallengeReader on its remote adapter without an Authenticator", id)
	}
	for _, p := range d.UpstreamPresets {
		// A preset's URL is compared with the stored, normalized URL of a repository (R-REPO-062): written any other way it would
		// never match one.
		if norm, why := repository.NormalizeUpstreamURL(p.URL); why != "" || norm != p.URL {
			return fmt.Errorf("%q: the URL %q of the upstream preset %q is not in the form R-REPO-024 stores (%s)", id, p.URL, p.ID, norm+why)
		}
	}
	if d.ProbeChallengeReachable {
		// The upstream test reads a Bearer challenge of the probe resource as reachable: that is true only for a format whose upstream
		// is probed and answers the challenge by a token handshake the format implements (R-REPO-024, Q-REPO-280).
		if !d.Supports(Remote) || d.ProbePath == "" || CapabilitiesOf(f).Remote.Auth == nil {
			return fmt.Errorf("%q declares ProbeChallengeReachable and needs the class remote, a ProbePath and a remote adapter that implements Authenticator", id)
		}
	}
	if x := f.Exposure(); x != nil {
		if dim := x.Dimensions(); (dim.Packages || dim.Versions || dim.Tags) && !d.PackageNames {
			return fmt.Errorf("%q narrows exposure by package, version or tag and has no package names", id)
		} else if dim.Versions && f.View() == nil {
			// Which version is a pre-release is the view adapter's answer (Q-API-433): rules that narrow versions need it.
			return fmt.Errorf("%q narrows exposure by version and has no view adapter that says which version is a pre-release", id)
		}
	}
	for _, check := range []func(Format, Descriptor) error{checkSystemOnly, checkView, checkProbes, checkCatalogue, checkSignatures} {
		if err := check(f, d); err != nil {
			return err
		}
	}
	switch {
	case repository.IsReservedMember(string(id)):
		return fmt.Errorf("%q is named like a member of the repository document, whose member for the format's settings it must be", id)
	case len(d.SettingsSchema) > 0 && !repository.HasFormatMember(string(id)):
		return fmt.Errorf("%q declares a settings schema and the repository document has no %q member for it", id, id)
	}
	return checkRoutes(f, d)
}

// checkSystemOnly refuses an upload route of a system-only format (R-CLAMAV-001, ADR-225): only the product writes its repository.
func checkSystemOnly(f Format, d Descriptor) error {
	if !d.SystemOnly {
		return nil
	}
	for _, r := range f.Routes() {
		if r.Kind == Upload {
			return fmt.Errorf("%q is system only and has the upload route %q: only the product writes its repository", d.ID, r.Name)
		}
	}
	return nil
}

// checkView holds a format with a package view to what the view rests on: package names, the identity of a package, and the rule of
// what a fetch of a version is, by which the screens count its downloads (R-API-079). A format without a view is not asked.
func checkView(f Format, d Descriptor) error {
	if f.View() == nil {
		return nil
	}
	if !d.PackageNames {
		return fmt.Errorf("%q has a package view and no package names", d.ID)
	}
	if d.VersionFetch == FetchUndeclared || d.VersionFetch > FetchDocument {
		return fmt.Errorf("%q has a package view and declares no VersionFetch", d.ID)
	}
	return nil
}

// checkProbes holds a format with the class remote to a declaration of its probes (R-REPO-065): each names a route that serves the
// class remote with GET or HEAD, once; a format without any says why. A format without the class declares no probe.
func checkProbes(f Format, d Descriptor) error {
	if !d.Supports(Remote) {
		if len(d.Probes) > 0 {
			return fmt.Errorf("%q declares probes and has no class remote", d.ID)
		}
		return nil
	}
	switch {
	case len(d.Probes) == 0 && d.NoProbe == "":
		return fmt.Errorf("%q has the class remote and declares neither its probes nor why it has none (NoProbe)", d.ID)
	case len(d.Probes) > 0 && d.NoProbe != "":
		return fmt.Errorf("%q declares probes and a reason it has none", d.ID)
	}
	routes := map[string]Route{}
	for _, r := range f.Routes() {
		routes[r.Name] = r
	}
	seen := map[string]bool{}
	for _, p := range d.Probes {
		r, ok := routes[p.Route]
		switch {
		case !ok:
			return fmt.Errorf("%q declares a probe on route %q, which it does not have", d.ID, p.Route)
		case p.Method != http.MethodGet && p.Method != http.MethodHead:
			return fmt.Errorf("%q declares a probe on route %q with method %q, not GET or HEAD", d.ID, p.Route, p.Method)
		case r.Method != http.MethodGet:
			return fmt.Errorf("%q declares a probe on route %q, whose method is %s", d.ID, p.Route, r.Method)
		case len(r.Classes) > 0 && !slices.Contains(r.Classes, Remote) && !slices.Contains(r.Classes, Virtual):
			return fmt.Errorf("%q declares a probe on route %q, which serves neither the class remote nor the class virtual", d.ID, p.Route)
		case p.What == "":
			return fmt.Errorf("%q declares a probe on route %q without saying what it names", d.ID, p.Route)
		case seen[p.Route+" "+p.Method]:
			return fmt.Errorf("%q declares the probe %s %s twice", d.ID, p.Method, p.Route)
		}
		seen[p.Route+" "+p.Method] = true
	}
	return nil
}

// checkSignatures holds every format to a declaration of its signatures (signatures.md R-SIG-022, Q-SIG-43): an adapter, and a
// declaration that says either why the format has none or every field of each kind it knows, with roles and schemes of R-SIG-001.
func checkSignatures(f Format, d Descriptor) error {
	sa := f.Signatures()
	if sa == nil {
		return fmt.Errorf("%q declares no signatures: Signatures() returns nil (R-SIG-022)", d.ID)
	}
	decl := sa.Declaration()
	switch {
	case decl.None == "" && len(decl.Kinds) == 0:
		return fmt.Errorf("%q declares neither the kinds of its signatures nor why it has none", d.ID)
	case decl.None != "" && len(decl.Kinds) > 0:
		return fmt.Errorf("%q declares kinds of signatures and a reason it has none", d.ID)
	case (decl.Attests == "") == (decl.AttestsNone == ""):
		return fmt.Errorf("%q declares neither or both of what Quader attests and why it attests nothing (R-SIG-044)", d.ID)
	}
	seen := map[string]bool{}
	for _, k := range decl.Kinds {
		switch {
		case k.Name == "" || k.Location == "" || k.Covers == "" || k.Client == "" || len(k.Schemes) == 0:
			return fmt.Errorf("%q declares the signature kind %q without its name, schemes, location, coverage or client", d.ID, k.Name)
		case !SignatureRoleValid(k.Role):
			return fmt.Errorf("%q declares the signature kind %q with the role %q", d.ID, k.Name, k.Role)
		case seen[k.Name]:
			return fmt.Errorf("%q declares the signature kind %q twice", d.ID, k.Name)
		}
		seen[k.Name] = true
		for _, s := range k.Schemes {
			if !SignatureSchemeValid(s) {
				return fmt.Errorf("%q declares the signature kind %q with the scheme %q", d.ID, k.Name, s)
			}
		}
	}
	return nil
}

func checkAdapter(id ID, class string, listed, present bool) error {
	switch {
	case listed && !present:
		return fmt.Errorf("%q lists class %s and %s() returns nil", id, strings.ToLower(class), class)
	case !listed && present:
		return fmt.Errorf("%q returns a %s adapter and does not list class %s", id, strings.ToLower(class), strings.ToLower(class))
	}
	return nil
}

// checkRoutes is the ambiguity check of R-ARCH-010 (kit check 1) and the rules
// that make a route table servable.
func checkRoutes(f Format, d Descriptor) error {
	routes := f.Routes()
	if len(routes) == 0 {
		return nil
	}
	want := "/api/" + string(d.ID) + "/{project}/{key}/"
	if d.RegistryRoot {
		want = catalog.RegistryRootBasePath
	}
	if d.ProtocolBasePath != want {
		return fmt.Errorf("%q has routes and protocol base path %q, not %q", d.ID, d.ProtocolBasePath, want)
	}
	names := map[string]bool{}
	for i := range routes {
		r := &routes[i]
		switch {
		case r.Name == "":
			return fmt.Errorf("%q: route %d has no name", d.ID, i)
		case names[r.Name]:
			return fmt.Errorf("%q: route name %q is used twice", d.ID, r.Name)
		case !slices.Contains([]string{http.MethodGet, http.MethodPut, http.MethodPost, http.MethodDelete, http.MethodPatch}, r.Method):
			return fmt.Errorf("%q: route %q has method %q", d.ID, r.Name, r.Method)
		case r.Template == "":
			return fmt.Errorf("%q: route %q has no template", d.ID, r.Name)
		case r.Handler == nil:
			return fmt.Errorf("%q: route %q has no handler", d.ID, r.Name)
		case !r.Kind.Valid():
			return fmt.Errorf("%q: route %q has no kind", d.ID, r.Name)
		case r.Action == "":
			return fmt.Errorf("%q: route %q has no action", d.ID, r.Name)
		case len(r.Examples) == 0:
			return fmt.Errorf("%q: route %q has no example", d.ID, r.Name)
		}
		if err := checkClientDigest(d.ID, r); err != nil {
			return err
		}
		names[r.Name] = true
		for _, c := range r.Classes {
			if !d.Supports(c) {
				return fmt.Errorf("%q: route %q serves class %q, which the format does not support", d.ID, r.Name, c)
			}
		}
	}
	for i := range routes {
		r := &routes[i]
		for _, ex := range r.Examples {
			if _, err := repository.CleanPath(ex); err != nil {
				return fmt.Errorf("%q: example %q of route %q is no clean path", d.ID, ex, r.Name)
			}
			// Clients never upload derived metadata (R-ARCH-012): what quader renders is
			// changed by a write to the rows it renders from, which is an Admin route.
			if r.Kind == Upload && f.Classify(Path{Path: ex}).Kind == PathComputed {
				return fmt.Errorf("%q: upload route %q accepts a write to %q, which Classify marks computed", d.ID, r.Name, ex)
			}
			for j := range routes {
				o := &routes[j]
				if o.Method != r.Method || !classesOverlap(r, o) {
					continue
				}
				_, ok := o.matcher()(ex)
				switch {
				case i == j && !ok:
					return fmt.Errorf("%q: route %q does not match its own example %q", d.ID, r.Name, ex)
				case i != j && ok:
					return fmt.Errorf("%q: example %q of route %q is also matched by route %q (ambiguous routes)", d.ID, ex, r.Name, o.Name)
				}
			}
		}
	}
	return nil
}

// checkClientDigest refuses a declaration that the clients of a route check a digest (R-STOR-071, ADR-243) where the serve point could
// never hand the download to a link, a route that is no GET of the kind Download, and a declaration that does not say what the clients
// compare against and what shows it: a declaration is a reviewed act with its evidence, never a default.
func checkClientDigest(id ID, r *Route) error {
	cd := r.ClientDigest
	switch {
	case !cd.Checks:
		return nil
	case r.Kind != Download || r.Method != http.MethodGet:
		return fmt.Errorf("%q: route %q (%s %s) declares that its clients check a digest, which only a GET of the kind download can (R-STOR-071)", id, r.Name, r.Method, r.Kind)
	case strings.TrimSpace(cd.Against) == "" || strings.TrimSpace(cd.Evidence) == "":
		return fmt.Errorf("%q: route %q declares that its clients check a digest without saying what they compare against and the evidence that shows it (R-STOR-071)", id, r.Name)
	}
	return nil
}

func classesOverlap(a, b *Route) bool {
	if len(a.Classes) == 0 || len(b.Classes) == 0 {
		return true
	}
	for _, c := range a.Classes {
		if slices.Contains(b.Classes, c) {
			return true
		}
	}
	return false
}

// Lookup returns the format with the given name.
func Lookup(name string) (Format, bool) {
	formatRegistry.mu.RLock()
	defer formatRegistry.mu.RUnlock()
	f, ok := formatRegistry.by[ID(strings.TrimSpace(name))]
	return f, ok
}

// List returns the registered formats sorted by ID.
func List() []Format {
	formatRegistry.mu.RLock()
	defer formatRegistry.mu.RUnlock()
	out := make([]Format, 0, len(formatRegistry.by))
	for _, f := range formatRegistry.by {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Descriptor().ID < out[j].Descriptor().ID })
	return out
}

// Descriptors returns the descriptors of the registered formats sorted by ID,
// which is the body of GET /api/v1/formats (R-REPO-003).
func Descriptors() []Descriptor {
	fs := List()
	out := make([]Descriptor, len(fs))
	for i, f := range fs {
		out[i] = f.Descriptor()
	}
	return out
}
