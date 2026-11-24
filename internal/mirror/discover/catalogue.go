package discover

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
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
	// Unknown says that the upstream does not have the level, told apart from a level it has that lists nothing (R-CUR-007,
	// Q-CUR-165); nil where the level lists something or where the upstream does not tell the two apart. A page with it lists nothing.
	Unknown *UnknownLevel
}

// UnknownLevel is why a level lists nothing: the upstream does not have it. A format sets it where its upstream tells an unknown level
// from an empty one at a constant cost (one or two requests besides the listing), or where the level's name breaks the format's naming
// rule, which is certain, so that no request asks for it.
type UnknownLevel struct {
	// Rule is the naming rule the level's name breaks, "" when it keeps the format's rule; Forbidden holds the characters of the name
	// the rule does not allow, each once, in the order the name has them.
	Rule      NameRule
	Forbidden string
	// Suggestion is a level of the upstream the name may have meant: the name made to keep the rule as the format's rule makes it,
	// asked for once and found; "" when there is none.
	Suggestion string
}

// NameRule names a rule of a format's level names that the screen says in words (R-CUR-018): a closed set across the formats.
type NameRule string

// The naming rules.
const (
	// NameRuleLowercaseDigits: lowercase letters and digits only (a Docker Hub namespace).
	NameRuleLowercaseDigits NameRule = "lowercaseDigits"
	// NameRuleLowercaseDigitsUnderscore: lowercase letters, digits and underscores only (an Ansible Galaxy namespace).
	NameRuleLowercaseDigitsUnderscore NameRule = "lowercaseDigitsUnderscore"
)

// Forbidden returns the characters of name that allowed rejects, each once, in the order name has them; "" when it keeps the rule.
func Forbidden(name string, allowed func(rune) bool) string {
	var out []rune
	for _, r := range name {
		if !allowed(r) && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return string(out)
}

// Exists asks with a HEAD (no body, [Head]) whether a resource of the upstream is there, for a format that tells an unknown level from
// an empty one: exists for a 2xx or a 3xx answer (a redirect names where it is), not for a 404 or a 410; for any other answer known is
// false, and the format makes no claim.
func Exists(ctx context.Context, u Upstream, target *url.URL) (exists, known bool, err error) {
	res, err := Head(ctx, u, target, "application/json")
	if err != nil {
		return false, false, err
	}
	switch {
	case res.Status >= 200 && res.Status < 400:
		return true, true, nil
	case res.Status == http.StatusNotFound || res.Status == http.StatusGone:
		return false, true, nil
	}
	return false, false, nil
}

// EmptyOrUnknown is the page of a level that lists nothing, after asking the upstream's accounts of its name in turn ([Exists]): unknown
// when every account answers that the upstream does not have it, empty as soon as one has it or answers something else (no claim). A
// format whose upstream keeps users and organizations apart names both (Quay).
func EmptyOrUnknown(ctx context.Context, u Upstream, accounts ...*url.URL) (LevelPage, error) {
	empty := LevelPage{Done: true}
	for _, a := range accounts {
		exists, known, err := Exists(ctx, u, a)
		if err != nil {
			return LevelPage{}, err
		}
		if exists || !known {
			return empty, nil
		}
	}
	empty.Unknown = &UnknownLevel{}
	return empty, nil
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
	// Size is the size of the version's download as the upstream announces it, 0 when unknown.
	Size int64
	// UnpackedSize is the size of the unpacked files where the upstream tells only that (npm's dist.unpackedSize), 0 otherwise; it is
	// never counted as a download's size.
	UnpackedSize int64
	Flags        []string
	Platforms    []string
	Digest       string
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

// UnavailableError is a format's answer that the repository's upstream does not answer a catalogue call as the format expects (R-CUR-008):
// an npm registry without the search route (404, 405, 501), a Galaxy without the keyword search (404), or an undocumented endpoint whose
// answer is not a page of the expected shape (Docker Hub's repository search). The engine turns the capability off for its cache
// period, answers the call with 400 catalogue_not_offered and Reason, and says Reason in the capabilities until the period ends.
type UnavailableError struct {
	// Reason is the sentence a person reads, one sentence, no URL and no credential.
	Reason string
	// Shape says that the answer was not of the expected shape (counted as outcome shape), not that the route is absent.
	Shape bool
}

func (e *UnavailableError) Error() string {
	return "discover: the upstream does not answer the call: " + e.Reason
}

// Is makes errors.Is(err, ErrNotOffered) true for an UnavailableError: whoever only asks whether the call is offered reads it so.
func (e *UnavailableError) Is(target error) bool { return target == ErrNotOffered }

// RepositoryIDer is implemented by every [Upstream] the product lends (a pass's, a background run's, the catalogue's): the repository
// the call is for. A format whose catalogue or plan reads rows of its own (Debian's parsed indexes) needs it, and the upstream is all
// it is given.
type RepositoryIDer interface{ RepositoryID() int64 }

// The bounds of a catalogue page (R-CUR-008): what one call answers at most. The engine clamps a request's Limit to them; a format
// honours the Limit it is handed.
const (
	MaxSearchHits     = 50
	MaxLevelEntries   = 200
	MaxVersionsOnPage = 100
	// MaxTextRunes cuts a text of the upstream (a description) before it reaches a person.
	MaxTextRunes = 1000
)
