package formats

import "context"

// DataPorts is the part of the [Host] that hands out the data port of a format
// (C-3). The package view reads format data through it and needs nothing else of
// the host.
type DataPorts interface {
	// FormatData returns the data port bound to a format, nil for a format
	// without Descriptor.DataPort.
	FormatData(id ID) any
}

// ViewAdapter is a format's part of the package screens (R-API-029): what the
// generic package and version rows do not hold - the description, the keywords,
// the licenses, the format-specific metadata and the README - and the format's
// notion of version order, which includes which of its versions is a pre-release
// (api-and-ui.md Q-API-433): the one place a format says it. The screens, the stored
// rows and the decision point of the exposure rules ([catalog.Versioning], composed by
// [CatalogEntry]) all ask it here. The core answers everything the generic rows carry
// (names, versions, repositories, times, sizes, downloads, files) and asks the
// adapter for the rest; the adapter reads its own side tables through its data
// port and runs no SQL (R-ARCH-008).
//
// A format without a view adapter (View returns nil) has no package screens: its
// packages are not listed.
type ViewAdapter interface {
	// Compare orders two normalized versions by the format's precedence: negative
	// when a is older than b. It is a total order over the versions the format
	// stores; a string that is no version of the format sorts before every version
	// and equal strings compare equal.
	Compare(a, b string) int
	// SortKey is the stored sort key of a version (R-API-029, Q-API-429): bytes.Compare of the keys of two strings has the sign of
	// Compare wherever Compare is not zero, and two different strings have two different keys. The host stores it with every
	// version row, so that the version list in the order of the versions and the latest version of a package are read by an index.
	// It is built with internal/formats/versionkey: a string that is no version is [versionkey.NotVersion] and its bytes. A stored
	// key is recomputed only when it is missing: the background migration version_sort_key fills a row without one (Q-HA-492) and
	// the reads give such a row this key meanwhile (Q-API-430). A change of the encoding is therefore a migration step that sets the
	// format's stored keys to NULL; the next worker that starts queues the fill again.
	SortKey(version string) []byte
	// Prerelease reports whether a version is a pre-release of the format (R-API-029, Q-API-433): a version the format ranks (SortKey of
	// the class [versionkey.Version]) that its ecosystem's clients pass over for a release unless asked for it. It is false for a string
	// that is no version and for every version of a format whose ecosystem has no such notion (Debian, the ClamAV definitions). The host
	// stores it with every version row beside the sort key, the latest version of a package is its highest release before its highest
	// pre-release, and an exposure rule without pre-releases hides a version it answers true for. A change of the answer for a stored
	// version is a migration step that sets the format's stored keys to NULL, as for SortKey.
	Prerelease(version string) bool
	// Describe reads, for versions the caller may read, what the screens show of
	// each. A version the format has no data for is absent from the answer. visible
	// says which versions of the same package the caller may read, so that data
	// that names other versions (npm's dist-tags) leaks none that is hidden.
	Describe(ctx context.Context, ports DataPorts, vs []ViewVersion, want ViewFields, visible func(version string) bool) (map[VersionID]ViewData, error)
	// Find returns the ids of at most limit+1 versions in the repositories (by key)
	// whose description contains Text (case-insensitively) or whose keywords
	// contain Keyword exactly. It does not know what a caller may read: the core
	// intersects the ids with the versions the caller may read before it counts
	// anything, so an id of a hidden version changes no answer.
	Find(ctx context.Context, ports DataPorts, f ViewFind, limit int) ([]VersionID, error)
	// Withheld returns the ids of the stored versions in the repositories (by key)
	// that the format does not serve, for example a version a restore's replay
	// tombstoned (R-NPM-047). The screens list none of them.
	Withheld(ctx context.Context, ports DataPorts, repos []string) ([]VersionID, error)
	// Stored returns at most q.Limit of the versions the repository serves of the
	// package, newest first, and, when q.Tags is set, its tags (R-EP-016, Q-EP-61).
	//
	// "Serves" means answerable by a request for the exact version: an unlisted version
	// the repository still serves counts, so does a deprecated one; a version the format
	// withholds (see Withheld) does not. "Newest first" is arrival time for a hosted
	// repository, publish time for a remote one (unknown times last), and for a format
	// with neither Compare descending.
	//
	// The host calls it for local and remote repositories only. A package the
	// repository does not hold is the empty answer, not an error.
	Stored(ctx context.Context, ports DataPorts, q StoredQuery) (Stored, error)
}

// ViewSizer is the optional interface of a [Format] whose version has a size that is not the sum of the version's files: an OCI
// tag's file is the manifest document, its size the image's. ViewSizes returns, for the given versions, the size the screens show
// as the version's `sizeBytes` (R-API-080): a [ViewSize] whose bytes are not below zero, which may count only part of the version
// or say that its size is not known; a version the format has no size for is absent from the answer and keeps the sum of its files.
// It answers only for the ids it was asked for, in one call however many versions the page holds, and it is not asked for a version
// the caller may not read. [CapabilitiesOf] finds it; a format without it is never asked.
type ViewSizer interface {
	ViewSizes(ctx context.Context, ports DataPorts, vs []ViewVersion) (map[VersionID]ViewSize, error)
}

// ViewSize is the size of a version as a [ViewSizer] reports it (R-API-080, ADR-205).
type ViewSize struct {
	// Bytes is the size in bytes, not below zero; 0 when Unknown.
	Bytes int64
	// Partial says that Bytes counts only the parts of the version the repository holds: an OCI image index whose platforms are
	// not all fetched counts the ones that are (Q-OCI-221).
	Partial bool
	// Unknown says that no part of the version's size is known: an image index none of whose platforms is fetched. The screens
	// show no number for it.
	Unknown bool
}

// VersionFetch is what a format declares a fetch of one of its versions to be (R-API-079, ADR-205): the downloads whose count is
// a version's downloads and whose latest time is its last download, in every class. A download is what R-STOR-015 counts (a whole
// body or a range from byte 0, never a HEAD, a 304 or a metadata document); the rule says which of them belong to a version.
// [Register] refuses a format with a package view ([Format.View]) that declares none.
type VersionFetch uint8

const (
	// FetchUndeclared is the zero value: no declaration.
	FetchUndeclared VersionFetch = iota
	// FetchLeadFile: a download of the version's lead file, the one file that is the version (npm's tarball, NuGet's .nupkg, an
	// Ansible collection archive). A file linked beside it (a provenance bundle, a .nuspec, a symbol package) is no fetch.
	FetchLeadFile
	// FetchAnyFile: a download of any file linked to the version, each of which is the whole version for its platform (a Debian
	// package file per architecture).
	FetchAnyFile
	// FetchDocument: a download of the version's document by the version's name or by its digest. The document is the lead
	// file's content (a local OCI tag item) or, for a version without a lead file, the content its listing names (a remote OCI
	// tag's manifest); its fetch by digest is the download of a file of the package that holds the same bytes and is linked to no
	// other version. The documents below it (an index's platform manifests) and the blobs they name are no fetch of the version.
	FetchDocument
)

// String is the name of the rule as the package view passes it to its statements.
func (v VersionFetch) String() string {
	switch v {
	case FetchLeadFile:
		return "lead-file"
	case FetchAnyFile:
		return "any-file"
	case FetchDocument:
		return "document"
	}
	return "undeclared"
}

// StoredQuery asks what one repository stores under one package name.
type StoredQuery struct {
	// Repo is a local or a remote repository; the host never asks for a virtual one,
	// which stores nothing of its own.
	Repo RepoRef
	// Package is the normalized package name.
	Package string
	// Limit bounds the versions returned; the host asks for one more than it judges
	// so that it can tell a cut list from a complete one.
	Limit int
	// Tags says whether the answer needs the tags. It is true only for a format whose
	// catalog entry has tags (catalog.Exposure.Tags); an adapter reads nothing for
	// them otherwise.
	Tags bool
}

// Stored is the answer of [ViewAdapter.Stored].
type Stored struct {
	// Versions are the normalized versions the repository serves, newest first.
	Versions []string
	// Tags maps a tag to the version it points to; empty unless the query asked.
	Tags map[string]string
}

// ViewVersion names a stored version for the adapter.
type ViewVersion struct {
	ID      VersionID
	Repo    RepoRef
	Package string
	Version string
}

// ViewFields says how much of a version the adapter has to read; a README is up to
// 64 KiB and the list of packages never needs one.
type ViewFields uint8

const (
	// ViewSummary is the description, the keywords, the deprecation and whether the version is unlisted.
	ViewSummary ViewFields = 1 << iota
	// ViewDetails adds the licenses, whether a README exists and the metadata.
	ViewDetails
	// ViewReadme adds the README text and its file name.
	ViewReadme
	// ViewFacts adds, with ViewDetails, what only the version's own page shows and a list never reads: an OCI image's configuration,
	// layers and history (formats/oci.md R-OCI-040), which are larger than a list could carry for every row.
	ViewFacts
)

// Has reports whether f includes every field of o.
func (f ViewFields) Has(o ViewFields) bool { return f&o == o }

// ViewData is what the adapter knows of one version.
type ViewData struct {
	Description string
	Keywords    []string
	// Deprecation is the version's deprecation, its own or its package's (R-API-085); nil when it is not deprecated. It is read with
	// ViewSummary, so that every list marks it.
	Deprecation *Deprecation
	// Unlisted says that the format hides the version from its search while it still serves it by its exact number (NuGet's unlist,
	// R-NUGET-006); read with ViewSummary.
	Unlisted bool
	// Licenses are the declared SPDX identifiers or the expression; nil for none.
	Licenses  []string
	HasReadme bool
	// Metadata is the format-specific object of the version details; a member that
	// does not apply is absent. The format page owns its schema.
	Metadata map[string]any
	// WikiMetadata names the members of Metadata that are wiki content (R-AUTHZ-034); the core drops them, and the README, for a
	// caller who may not read the wiki (R-API-058). HasReadme is wiki content as well.
	WikiMetadata []string
	// ContentMetadata names the members of Metadata that are read from the content of the version's files beyond its document, and
	// ContentPaths the canonical paths of those files: an OCI image's configuration, whose environment and labels can hold what a
	// builder baked into the image, is read from its config blob (formats/oci.md R-OCI-040). The core drops the members for a caller who
	// may not read every one of the paths, the decision the format's serving of the files makes (R-API-058).
	ContentMetadata []string
	ContentPaths    []string
	// Readme and ReadmeFile are the README source and the name of its file; set
	// only with ViewReadme.
	Readme     string
	ReadmeFile string
	// ReadmeHTML says that Readme is HTML an upstream rendered, untrusted, which the
	// host sanitizes as it is instead of rendering it as Markdown (an Ansible role's
	// readme_html, R-ANS-039, R-ANS-056).
	ReadmeHTML bool
}

// ViewFind is a search the adapter answers (ViewAdapter.Find): one of Text and
// Keyword is set.
type ViewFind struct {
	Repos   []string
	Text    string
	Keyword string
}

// ViewRanker is the optional hook of a format whose package search ranks what it finds (formats/ansible.md R-ANS-057, Q-ANS-121,
// ADR-287): FindRanked answers what its view adapter's Find answers, each version with its rank from 0 to 999, higher for a better
// match, so that the package list's sort relevance puts it first. A format without it ranks every match it finds 0; the core ranks a
// package by its name in either case. [CapabilitiesOf] finds it.
type ViewRanker interface {
	FindRanked(ctx context.Context, ports DataPorts, f ViewFind, limit int) ([]RankedVersion, error)
}

// RankedVersion is a version a [ViewRanker] found, with its rank.
type RankedVersion struct {
	ID   VersionID
	Rank int
}

// View is nil: the format has no package screens.
func (Base) View() ViewAdapter { return nil }
