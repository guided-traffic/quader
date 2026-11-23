package discover

import (
	"context"
	"errors"
	"time"
)

// The upstream catalogue's view of an upstream (remote-curation.md R-CUR-007, Q-CUR-12, Q-CUR-109): what a person sees of a remote's
// upstream through the management API. It lives in this leaf beside [Lister], for the reason [Lister] does (ADR-152): the catalogue's
// engine (internal/remotecuration/catalogue) and the expansion of a bulk download (internal/remotecuration/queue) are domain packages
// that call a format's catalogue without importing the format contract. The contract adds the one call that names a format's probe,
// formats.Catalogue.VersionProbe, and declares what a format answers (formats.Descriptor.Catalogue).
//
// Every call reads through the [Upstream] it is handed: the catalogue lends one that answers [Upstream.Document] from the repository's
// stored copy when it holds one and otherwise fetches without storing, and refuses [Upstream.Artifact] (R-CUR-008), so browsing never
// fills a cache. Names in the answers are normalized by the format's name rule, versions canonical (R-CUR-006), every text from the
// upstream plain text.

// SearchKind says how a repository's upstream answers a text query (R-CUR-007).
type SearchKind string

// The kinds of search.
const (
	// SearchUpstream: the upstream answers the query (npm's -/v1/search, NuGet's search resource, the Galaxy's keyword search, a
	// preset's registry API).
	SearchUpstream SearchKind = "upstream"
	// SearchIndex: Quader answers the query over an index it holds of the upstream (Debian's Packages and Translation-en).
	SearchIndex SearchKind = "index"
	// SearchNone: no text search; CatalogueCaps.SearchReason says why.
	SearchNone SearchKind = "none"
)

// CatalogueCaps are a repository's catalogue capabilities, read per repository because one format's upstreams differ (R-CUR-007).
type CatalogueCaps struct {
	Search SearchKind
	// SearchWindow is the deepest hit the upstream's search reaches; 0 when it says nothing.
	SearchWindow int
	// SearchNote says what a query matches, one sentence.
	SearchNote string
	// SearchReason says why there is no search, one sentence.
	SearchReason string
	// Levels are the kinds of the levels above a package, outermost first ("scope"; "namespace"; "suite", "component",
	// "architecture"); empty with LevelsReason.
	Levels       []string
	LevelsReason string
	// Sizes is "known", "perVersion" or "unknown": whether the upstream tells a version's size without a download.
	Sizes string
	// Hosts are the hosts besides the upstream's that the calls reach, from the preset's catalogueHosts (R-CUR-008).
	Hosts []string
}

// SearchQuery asks for one page of a text query.
type SearchQuery struct {
	// Text is 1 to 128 characters after trimming.
	Text string
	// NamesOnly matches names only (Debian: apt-cache --names-only).
	NamesOnly bool
	// Level restricts the query to a level where the upstream can.
	Level  LevelRef
	Cursor Cursor
	// Limit is at most 50.
	Limit int
}

// Hit is one package of a search or of a level.
type Hit struct {
	Name, Description, Latest string
	Updated                   time.Time
	// Flags: deprecated, unlisted, official, verified, unsigned-index (Debian).
	Flags []string
}

// SearchPage is one page of a text query.
type SearchPage struct {
	Hits []Hit
	// Total is -1 when the upstream does not say.
	Total  int
	Cursor Cursor
	Done   bool
	// Truncated says that the upstream's window ends before its total; Note says why, one sentence.
	Truncated bool
	Note      string
}

// LevelRef names a level by its path, outermost first (["trixie", "main", "amd64"]); empty for the root.
type LevelRef struct{ Path []string }

// LevelRequest asks for one page of a level.
type LevelRequest struct {
	Level  LevelRef
	Cursor Cursor
	// Limit is at most 200.
	Limit int
}

// LevelPage is one page of a level: the levels below it and the packages directly in it.
type LevelPage struct {
	Levels   []string
	Packages []Hit
	Total    int
	Cursor   Cursor
	Done     bool
}

// VersionsRequest asks for one page of a package's versions.
type VersionsRequest struct {
	Cursor Cursor
	// Limit is at most 100.
	Limit int
	// Filter is a substring of the version, applied by Quader.
	Filter string
}

// VersionFacts is what the upstream tells of one version without a download.
type VersionFacts struct {
	// Version is canonical (R-CUR-006).
	Version   string
	Published time.Time
	// Size is 0 when unknown.
	Size      int64
	Flags     []string
	Platforms []string
	Digest    string
}

// PackageInfo is a package looked up by its exact name and one page of its versions, newest first by the format's order.
type PackageInfo struct {
	Found             bool
	Name, Description string
	// Tags are npm's dist-tags; none for OCI.
	Tags     map[string]string
	Versions []VersionFacts
	Total    int
	Cursor   Cursor
	Done     bool
}

// Catalogue is the part of a format's upstream catalogue that browses (R-CUR-007): a remote adapter that has a catalogue implements it
// as part of formats.Catalogue. Every call reads through u, which stores nothing in the repository's cache (R-CUR-008).
type Catalogue interface {
	CatalogueCaps(ctx context.Context, u Upstream) (CatalogueCaps, error)
	// Search answers one page of a text query; a repository whose caps say search none answers [ErrNotOffered].
	Search(ctx context.Context, u Upstream, q SearchQuery) (SearchPage, error)
	// Children lists one page of a level: the levels below it and the packages directly in it; [ErrNotOffered] without levels.
	Children(ctx context.Context, u Upstream, rq LevelRequest) (LevelPage, error)
	// Package looks a package up by its exact name and answers one page of its versions, newest first by the format's order. A
	// package the upstream does not have is PackageInfo{Found: false} and no error.
	Package(ctx context.Context, u Upstream, name string, rq VersionsRequest) (PackageInfo, error)
}

// ErrNotOffered answers a catalogue call the repository's upstream does not answer (a search where its caps say none, a level where
// it has none); the API answers 400 catalogue_not_offered (R-CUR-009).
var ErrNotOffered = errors.New("discover: the upstream does not offer this catalogue call")
