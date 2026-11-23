// Package deb is the Debian (APT) format (R-DEB-001 to R-DEB-035): the repository layout below /api/deb/<projectKey>/<repoKey>/, the
// grammars of packages, versions, architectures, suites and components, the classification of paths, the settings of a repository
// and the error shape. One format, `deb`, serves Debian, Ubuntu and every archive in the Debian repository format; its clients are
// apt, apt-get and every tool that reads an APT source (Q-DEB-1).
//
// Like every format it imports the format contract and almost nothing else (R-ARCH-008): its tables are reached through the [Store]
// port and its generic package rows through the PackageWriter of the hook. The leaf packages hold the parts that need no host: rows
// (the values of the data port) and, as the work packages land, control (deb822), debfile (the package archive), version (dpkg
// order) and index (the renderers).
//
// This is the skeleton the work packages of T-17 fill (docs/spec/formats/debian.md). It registers, the conformance
// kit runs, the routes of every class are declared (routes.go) and answer 501 deb_not_implemented until a work package registers
// the handler of a route by its name (handlers.go), and the descriptor, the grammars, the layout, the classification, the error shape
// and the settings schema are final. A class joins the format when its class_*.go file registers it: the local class in
// class_local.go, the remote and virtual classes with their adapters (class_remote.go, class_virtual.go).
package deb

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/formats/deb/archivekeys"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
)

// ID is the format name.
const ID formats.ID = "deb"

// Layout is the name of the one layout of the format (R-DEB-008): a package file lives on its canonical pool path, the generated
// files below dists/, and nothing else.
const Layout = "deb"

// upstreamPresets are the public archives of Debian and Ubuntu a remote proposes (R-DEB-001, R-REPO-062, Q-REPO-321), Debian the
// default. Ubuntu's arm64 and other ports are on ports.ubuntu.com, not on the archive. Each carries the archive keys its distribution's
// keyring package ships as deb.upstreamKeys (Q-REPO-379, which supersedes Q-REPO-326): the well-known repositories of the project
// upstream are the product's, and a remote created from a preset's URL takes them as it takes the preset's hosts.
func upstreamPresets() []catalog.UpstreamPreset {
	debian, ubuntu := archiveKeySettings(debianArchiveKeyring), archiveKeySettings(ubuntuArchiveKeyring)
	return []catalog.UpstreamPreset{
		{ID: "debian", DisplayName: "Debian", URL: "https://deb.debian.org/debian/", Default: true, Settings: debian},
		{ID: "debian-security", DisplayName: "Debian security updates", URL: "https://deb.debian.org/debian-security/", Settings: debian},
		{ID: "ubuntu", DisplayName: "Ubuntu", URL: "https://archive.ubuntu.com/ubuntu/", Settings: ubuntu},
		{ID: "ubuntu-security", DisplayName: "Ubuntu security updates", URL: "https://security.ubuntu.com/ubuntu/", Settings: ubuntu},
		{ID: "ubuntu-ports", DisplayName: "Ubuntu ports", URL: "https://ports.ubuntu.com/ubuntu-ports/", Settings: ubuntu},
	}
}

// The archive keys of the presets (Q-REPO-379): the keyring files of the leaf internal/formats/deb/archivekeys, which says what each is
// and where it comes from; the signatures ship the same files as the anchors debian-archive and ubuntu-archive (R-SIG-017).
// TestQ_REPO_379_TheArchiveKeys pins every fingerprint and fails 90 days before a key expires. A key change of either keyring is a
// release of Quader.
var (
	debianArchiveKeyring = string(archivekeys.Debian())
	ubuntuArchiveKeyring = string(archivekeys.Ubuntu())
)

// archiveKeySettings is the deb member a preset carries: its keys as deb.upstreamKeys.
func archiveKeySettings(armored string) json.RawMessage {
	raw, err := json.Marshal(map[string]string{"upstreamKeys": armored})
	if err != nil {
		panic("deb: the archive keys do not marshal: " + err.Error())
	}
	return raw
}

// MaxDocumentBytes bounds an upstream document held in memory: a release document is at most 16 MiB (R-DEB-034) and the largest
// index a remote stores as a metadata document is a compressed Contents file of a few tens of MiB; the parse of a Packages index
// reads a stream (R-DEB-023) and is not bound by this.
const MaxDocumentBytes = 128 << 20

// classes holds the classes that class_*.go files registered, with the setup that gives the format what the class needs (its
// adapter). A file registers its class in init, so a class joins the format by adding a file and no existing file changes.
var classes = struct {
	mu sync.Mutex
	by map[formats.Class]func(*Format)
}{by: map[formats.Class]func(*Format){}}

// registerClass registers a class of the format; setup may be nil. Registering a class twice panics.
func registerClass(c formats.Class, setup func(*Format)) {
	classes.mu.Lock()
	defer classes.mu.Unlock()
	if _, dup := classes.by[c]; dup {
		panic("deb: the class " + string(c) + " is registered twice")
	}
	classes.by[c] = setup
}

// Format implements formats.Format for Debian.
type Format struct {
	formats.Base
	classes []formats.Class
	routes  []formats.Route
	remote  formats.RemoteAdapter
	virtual formats.VirtualAdapter
	view    formats.ViewAdapter

	// replicas and ports bind the format to the Hosts and data ports a process built (Bind): the metrics of the remote class.
	replicas sync.Map
	ports    sync.Map
	// port is the data port bound last, for the parts of a fill that have no Host.
	port atomic.Pointer[portBox]
}

var _ formats.Format = (*Format)(nil)

// New creates the format with the classes registered by the class_*.go files, in the order local, remote, virtual.
func New() *Format {
	f := &Format{}
	classes.mu.Lock()
	for _, c := range []formats.Class{formats.Local, formats.Remote, formats.Virtual} {
		setup, ok := classes.by[c]
		if !ok {
			continue
		}
		f.classes = append(f.classes, c)
		if setup != nil {
			setup(f)
		}
	}
	classes.mu.Unlock()
	f.routes = f.buildRoutes()
	return f
}

// Classes returns the classes the format registers.
func (f *Format) Classes() []formats.Class { return slices.Clone(f.classes) }

// Descriptor returns the catalog entry of R-DEB-001 and the contract facts. The facts that only a remote repository reads (the
// upstream, the periods) are declared from the start, so the catalog answers for the whole format whichever classes are registered.
func (f *Format) Descriptor() formats.Descriptor {
	metadataTTL, notFoundTTL := 600, 300
	return formats.Descriptor{
		Facts: catalog.Facts{
			ID:               ID,
			DisplayName:      "Debian (APT)",
			Classes:          f.Classes(),
			Layout:           Layout,
			ProtocolBasePath: "/api/deb/{project}/{key}/",
			// The upstream test requests the upstream URL itself: there is no probe path (R-DEB-001, R-REPO-024).
			UpstreamPresets:           upstreamPresets(),
			DefaultMetadataTTLSeconds: &metadataTTL,
			DefaultNotFoundTTLSeconds: &notFoundTTL,
			PatternSubject:            formats.SubjectName,
			PackageNames:              true,
			PriorityMembers:           true,
			PinsAndClaims:             true,
			// Writes pass the ownership check with the binary package name as the package (R-DEB-004).
			OwnershipDefault: catalog.OwnershipEnforce,
			SettingsSchema:   settingsSchema,
		},
		ContractVersion: formats.ContractVersion,
		VersionFetch:    formats.FetchAnyFile, // the package file of each architecture is the whole version (R-API-079)
		// A HEAD of a package file is the one request of APT's protocol that names one version's file and fetches none of it (R-REPO-065,
		// Q-DEB-122): apt reads the indexes of whole components and then fetches the files itself.
		Probes: []formats.ProbeRoute{
			{Route: routePoolGet, Method: http.MethodHead, What: "a HEAD of a package file in the pool"},
			{Route: routeFlatFile, Method: http.MethodHead, What: "a HEAD of a package file of a flat repository"},
			{Route: routeMemberGet, Method: http.MethodHead, What: "a HEAD of a package file by the member path of a virtual repository's index"},
		},
		// The upstream catalogue (remote-curation.md R-CUR-007, R-DEB-036): the lookup by exact name is the floor; the search over the
		// indexes held and the levels suite, component and architecture wait for the catalogue's package of T-24.
		Catalogue: formats.CatalogueDeclaration{
			Lookup:   true,
			NoSearch: "The search over the indexes the remote holds (R-DEB-036) is specified and not built yet; look a package up by its exact name.",
			NoLevels: "The levels suite, component and architecture (R-DEB-036) are specified and not built yet; look a package up by its exact name.",
		},
		VersionCanon:     versionCanon{},
		Names:            names{},
		Clients:          clients(),
		PropertyPrefix:   "deb.",
		MaxDocumentBytes: MaxDocumentBytes,
		// A by-hash digest is verified because it addresses the file (R-DEB-021) and a pool file when a parsed index names its digest
		// (R-DEB-022): the format's own gate announces them, not the checksum policy's blanket rule (R-DEB-001).
		VerifyAnnouncedDigests: false,
		// The bounds of R-DEB-034 on a control archive: 1,000 entries, 64 MiB decompressed, a ratio of 100:1 and a control file of
		// at most 1 MiB. The ar archive's own bounds are the reader's (untrusted.NewAr).
		Limits:   untrusted.Limits{Entries: 1000, UncompressedBytes: 64 << 20, Ratio: 100, ManifestEntryBytes: 1 << 20},
		DataPort: reflect.TypeFor[Store](),
		// A remote fill stores a release document without raising a package row, so it signals the early eviction of merged
		// documents itself (R-REPO-020).
		NotifyAreas: []string{catalog.MetadataChangedArea},
	}
}

// versionCanon is the canonical form of a Debian version (R-CUR-006, R-DEB-036): the version as the index row has it,
// [<epoch>:]<upstream>[-<revision>] by the grammar of R-DEB-002.
type versionCanon struct{}

func (versionCanon) Canonical(v string) (string, bool) { return v, ValidVersion(v) }

// Routes returns the endpoint map of R-DEB-003.
func (f *Format) Routes() []formats.Route { return f.routes }

// Remote returns the remote adapter, nil until the remote class registers (class_remote.go).
func (f *Format) Remote() formats.RemoteAdapter { return f.remote }

// Virtual returns the virtual adapter, nil until the virtual class registers (class_virtual.go).
func (f *Format) Virtual() formats.VirtualAdapter { return f.virtual }

// View returns the package screens' adapter, nil until the local class provides it (R-DEB-030).
func (f *Format) View() formats.ViewAdapter { return f.view }
