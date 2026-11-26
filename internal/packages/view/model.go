package view

import (
	"context"
	"time"

	"github.com/guided-traffic/quader/internal/qname"
)

// Adapter is the format's part of the view (formats.ViewAdapter), in the terms of
// this package: a version is named by its row id and its repository key.
type Adapter interface {
	// Compare orders two normalized versions by the format's precedence.
	Compare(a, b string) int
	// SortKey is the format's stored sort key of a version (formats.ViewAdapter.SortKey, Q-API-429): the cursor of the order of the
	// versions is a version, and its key is where the next page starts.
	SortKey(version string) []byte
	// Prerelease reports whether a normalized version is a pre-release.
	Prerelease(version string) bool
	// Describe reads what the screens show of versions the caller may read. visible
	// says which versions of the package the caller may read.
	Describe(ctx context.Context, vs []Version, want Fields, visible func(version string) bool) (map[int64]Data, error)
	// Find returns the ids of at most limit+1 versions whose description or keywords
	// match; the view intersects them with what the caller may read.
	Find(ctx context.Context, f Find, limit int) ([]int64, error)
	// Withheld returns the ids of stored versions the format does not serve.
	Withheld(ctx context.Context, repos []string) ([]int64, error)
	// Deprecated returns the ids of at most limit+1 stored versions in the repositories that are deprecated (R-API-085); none for a
	// format whose versions never are. The view intersects them with what the caller may read.
	Deprecated(ctx context.Context, repos []string, limit int) ([]int64, error)
	// Fetch is the format's rule of which downloads are fetches of a version (R-API-079, formats.VersionFetch).
	Fetch() FetchRule
}

// FetchRule says which downloads are fetches of a version: of its lead file, of any of its files, or of its document by its name
// or its digest (formats.VersionFetch).
type FetchRule string

// The rules, as the statements of the view read them.
const (
	FetchLeadFile FetchRule = "lead-file"
	FetchAnyFile  FetchRule = "any-file"
	FetchDocument FetchRule = "document"
)

// Embedder is the optional part of an Adapter that reads a file a version's package embeds for the screens (formats/nuget.md
// R-NUGET-038: an icon, a licence file): found false for none.
type Embedder interface {
	Embedded(ctx context.Context, v Version, file string) ([]byte, bool, error)
}

// Sizer is the optional part of an Adapter whose format has versions of a size that is not the sum of their files (an OCI tag):
// Sizes returns the size the screens show for the versions, by id; a version absent keeps the sum of its files. The core asks an
// adapter only when it has this method.
type Sizer interface {
	Sizes(ctx context.Context, vs []Version) (map[int64]Size, error)
}

// Size is a version's size as a Sizer reports it (R-API-080): Bytes, which counts only part of the version when Partial, or no
// number when Unknown.
type Size struct {
	Bytes   int64
	Partial bool
	Unknown bool
}

// Adapters finds the adapter of a format; ok is false for a format without
// package screens.
type Adapters interface {
	For(format string) (Adapter, bool)
	// Formats lists the formats that have an adapter, sorted.
	Formats() []string
}

// Version names a stored version for the adapter.
type Version struct {
	ID      int64
	Format  string
	Repo    string
	Package string
	Version string
}

// Fields says how much of a version the adapter reads.
type Fields uint8

// The fields, as in formats.ViewFields.
const (
	FieldSummary Fields = 1 << iota
	FieldDetails
	FieldReadme
	FieldFacts
)

// Data is what the adapter knows of a version.
type Data struct {
	Description string
	Keywords    []string
	// Deprecation and Unlisted are read with the summary (formats.ViewData): a version's deprecation, its own or its package's, and
	// whether the format hides it from its search (R-API-085).
	Deprecation *Deprecation
	Unlisted    bool
	Licenses    []string
	HasReadme   bool
	Metadata    map[string]any
	// WikiMetadata names the members of Metadata that are wiki content (R-AUTHZ-034): the service drops them, and HasReadme,
	// for a caller who may not read the wiki of the repository the version was read from (R-API-058).
	WikiMetadata []string
	// ContentMetadata names the members of Metadata read from the content of the files at ContentPaths: the service drops them for a
	// caller who may not read every one of those paths (formats.ViewData.ContentMetadata).
	ContentMetadata []string
	ContentPaths    []string
	Readme          string
	ReadmeFile      string
	// ReadmeHTML: Readme is an upstream's HTML, sanitized and not rendered (R-ANS-039).
	ReadmeHTML bool
}

// Deprecation is a version's or a package's deprecation (R-API-085, formats.Deprecation): Package says that the whole package is
// deprecated; Message is plain text; Reasons and Alternate are the format's, empty where it has none.
type Deprecation struct {
	Package   bool
	Message   string
	Reasons   []string
	Alternate *Alternate
}

// Alternate is the package a deprecation names in its place, and the range of its versions ("" for any).
type Alternate struct {
	Package string
	Range   string
}

// Ranker is the optional part of an [Adapter] that ranks what it finds (R-ANS-057): FindRanked answers what Find answers, each version
// with its rank from 0 to 999. The list's relevance adds it to the rank of the package's name (Q-ANS-121).
type Ranker interface {
	FindRanked(ctx context.Context, f Find, limit int) ([]Match, error)
}

// Match is a version a [Ranker] found, with its rank.
type Match struct {
	ID   int64
	Rank int
}

// Find is a search the adapter answers: one of Text and Keyword is set.
type Find struct {
	Repos   []string
	Text    string
	Keyword string
}

// Package is a package as the caller sees it: every member is computed from the
// versions the caller may read.
type Package struct {
	ID     string
	Format string
	// Name is the display name; Key the normalized name the list statement sorts on and the
	// cursor carries (R-API-029).
	Name          string
	Key           string
	Description   *string
	LatestVersion string
	// Kind is what the latest version is in the format's words, from the summary's metadata (an OCI artifact's kind: image,
	// helm-chart, …, formats/oci.md R-OCI-041); "" for a format without kinds.
	Kind          string
	Keywords      []string
	Repositories  []qname.Repo
	CreatedAt     time.Time
	UpdatedAt     time.Time
	VersionCount  int64
	DownloadCount int64
	// Deprecation is the deprecation of the package's latest version, which the row names (R-API-085); nil when it is not deprecated.
	Deprecation *Deprecation
	// Relevance is how well the package matches the list's text (R-ANS-057, Q-ANS-121): 3000 for its name, 2000 for a name that
	// starts with the text, 1000 for one that holds it, plus the best rank its format gave a matched version; 0 without a text. The
	// sort relevance orders by it and the cursor carries it.
	Relevance int64
	// LatestVersionID is the id of the version row LatestVersion was read from, the one whose signature the list's badge shows.
	LatestVersionID int64
}

// VersionSummary is one entry of the version list.
type VersionSummary struct {
	Version     string
	Prerelease  bool
	PublishedAt time.Time
	// SizeBytes is the version's size; nil when it is not known (R-API-080). SizePartial says that it counts only the parts the
	// repository holds.
	SizeBytes   *int64
	SizePartial bool
	// Digest is the version's content digest (an OCI tag: `sha256:…` of its manifest), read from the format's metadata when the caller
	// asks (VersionFilter.WithDigest); "" for none.
	Digest string
	// State is the version's state from the format's metadata (the ClamAV definitions: in_use, previous, superseded, files_removed,
	// R-UI-194), read when the caller asks (VersionFilter.WithState); "" for none.
	State string
	// Kind is what the version is in the format's words (an OCI artifact's kind, R-OCI-041), read when the caller asks
	// (VersionFilter.WithKind); "" for none.
	Kind          string
	Repositories  []qname.Repo
	DownloadCount int64
	// LastDownloadedAt is the latest fetch of the version by a client, by its format's rule, in any repository that holds it (R-API-065,
	// R-API-079); nil when no client fetched it.
	LastDownloadedAt *time.Time
	// Unranked says that the format does not rank the version (an image tag such as latest): in the order of the versions it comes after
	// the ranked ones by its time, and a cursor after it carries the time (Q-API-431).
	Unranked bool
	// Aliases are, in the list by content (Q-API-432), the other versions that name the entry's content, in the order of the versions, at
	// most 100; AliasesMore says that it has more. Nil outside that list.
	Aliases     []string
	AliasesMore bool
	// Deprecation and Unlisted are the version's, read from the format's summary of the repository its row was read from (R-API-085).
	Deprecation *Deprecation
	Unlisted    bool
}

// RepositoryEntry is a repository that holds a version.
type RepositoryEntry struct {
	Repo        qname.Repo
	Class       string
	PublishedAt time.Time
}

// File is a file of a version.
type File struct {
	Path      string
	SizeBytes int64
	SHA256    string
	// Repo is the repository whose copy the file is.
	Repo qname.Repo
}

// VersionDetail is a version with everything the details page shows.
type VersionDetail struct {
	PackageID   string
	Format      string
	Name        string
	Version     string
	Prerelease  bool
	PublishedAt time.Time
	Licenses    []string
	// Keywords are the keywords or tags the version declares (npm's keywords, NuGet's tags, an Ansible collection's tags; R-UI-221),
	// empty for a format without them.
	Keywords []string
	// SizeBytes and SizePartial are as in VersionSummary (R-API-080).
	SizeBytes     *int64
	SizePartial   bool
	DownloadCount int64
	// LastDownloadedAt is as in VersionSummary (R-API-065).
	LastDownloadedAt *time.Time
	HasReadme        bool
	Metadata         map[string]any
	// Aliases are the other versions that name the version's content (an image's other tags), in the order of the versions, at most
	// 100, for a format whose version is fetched as its document; AliasesMore says the content has more. Nil for any other format.
	Aliases     []string
	AliasesMore bool
	// Deprecation and Unlisted are the version's in the repository the metadata was read from (R-API-085).
	Deprecation  *Deprecation
	Unlisted     bool
	Repositories []RepositoryEntry
	Files        []File
	// Repo is the repository the files and the metadata were read from: the one
	// that stored the version first.
	Repo qname.Repo
}

// Readme is the README source of a version.
type Readme struct {
	Source   string
	FileName string
	// HTML says that Source is an upstream's HTML: the answer sanitizes it and renders no Markdown (R-ANS-039).
	HTML bool
}
