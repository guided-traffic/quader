package iface

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DecodeStrict decodes one JSON value into v and refuses an unknown member and
// trailing data. Equality of the interface versions is the rule (R-BAS-009), so
// a body with a member the reader does not know is a version problem, never
// something to skip. Both sides decode every body of the interface with it.
func DecodeStrict(r io.Reader, v any) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("bastion interface body: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("bastion interface body: trailing data after the JSON value")
	}
	return nil
}

// Marshal encodes a body of the interface.
func Marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

// MediaType is the content type of every body of the interface.
const MediaType = "application/json"

// Handshake is the answer of GET /api/v1/bastion/handshake (R-BAS-010).
type Handshake struct {
	// InterfaceVersion is the main installation's interface version.
	InterfaceVersion int `json:"interfaceVersion"`
	// Bastion is the registration the token belongs to.
	Bastion HandshakeBastion `json:"bastion"`
	// Installation describes the main installation.
	Installation HandshakeInstallation `json:"installation"`
	// ResponseBaseURL is the response base URL the main installation derived for
	// this request (R-OPS-005, R-EP-003). The Bastion requires it to equal its own
	// server.base_url (R-BAS-010).
	ResponseBaseURL string `json:"responseBaseUrl"`
	// AnonymousReads says an anonymous request reads something through the default endpoint: some repository's artifacts are public (R-AUTH-037, R-BAS-019).
	AnonymousReads bool `json:"anonymousReads"`
	// RecordDownloads is audit.record_downloads (R-BAS-016, R-BAS-039).
	RecordDownloads bool `json:"recordDownloads"`
	// Feed is the feed position at the time of the answer.
	Feed HandshakeFeed `json:"feed"`
	// ServerTime is the main installation's clock.
	ServerTime time.Time `json:"serverTime"`
	// Formats are the facts of every registered format with routes that matter to
	// the Bastion before or without a response of the main installation (R-BAS-057,
	// Q-BAS-170). The Bastion keeps the table until the next handshake.
	Formats HandshakeFormats `json:"formats"`
	// BoundRepository is the qualified name "<projectKey>/<repoKey>" the default
	// endpoint's host_roots binds the host of [Handshake.ResponseBaseURL] to
	// (R-EP-006, R-BAS-057, Q-BAS-178), absent when it binds none.
	BoundRepository string `json:"boundRepository,omitempty"`
	// Handover is the registration's switch of the handover per blob (R-BAS-071): while it is true the Bastion offers
	// to take a handover on a miss ([HandoverOffer], R-BAS-072). Always present.
	Handover bool `json:"handover"`
}

// HandshakeFormat is one entry of the handshake's format table (R-BAS-057): the
// facts the format declares in catalog.Facts, which reach every Bastion as data,
// so that the Bastion's code holds no knowledge of a format (Q-BAS-179).
type HandshakeFormat struct {
	// ID is the format's name, the format segment of /api/<format>/.
	ID string `json:"id"`
	// RegistryRoot is true for the one format whose routes the registry root /v2/
	// serves (R-BAS-051); the Bastion knows the format of a request below /v2/ by it.
	RegistryRoot bool `json:"registryRoot"`
	// ReplayHeaders are the headers a cached answer of the format replays besides
	// the core's representation headers (Q-BAS-171), in canonical form.
	ReplayHeaders []string `json:"replayHeaders"`
	// RateLimitedStatus is the status of the Bastion's own rate-limit refusals on
	// the format's family: 429 or 503 (R-BAS-056, R-BAS-058).
	RateLimitedStatus int `json:"rateLimitedStatus"`
	// BoundPrefixes are the literal bound-host patterns of the format (R-EP-006),
	// paths from the host root: "/v2/" for the registry root.
	BoundPrefixes []string `json:"boundPrefixes"`
}

// HandshakeFormats is the handshake's format table.
type HandshakeFormats []HandshakeFormat

// Lookup returns the entry of the format id, or the defaults of R-BAS-057 for a
// format the table does not name: no replay headers and the rate-limit status 429.
func (t HandshakeFormats) Lookup(id string) HandshakeFormat {
	for _, f := range t {
		if f.ID == id {
			return f
		}
	}
	return HandshakeFormat{ID: id, RateLimitedStatus: http.StatusTooManyRequests}
}

// RegistryRoot returns the entry of the format that declares the registry root;
// ok is false when the table names none, and the Bastion then serves nothing
// below /v2/.
func (t HandshakeFormats) RegistryRoot() (HandshakeFormat, bool) {
	for _, f := range t {
		if f.RegistryRoot {
			return f, true
		}
	}
	return HandshakeFormat{}, false
}

// Validate checks the table as the Bastion reads it: every entry has an ID, once;
// at most one declares the registry root; a rate-limit status is 429 or 503; a
// replay header is a canonical header name without the prefix "Quader-"; a bound
// prefix starts with "/". The main installation's catalog holds the formats to
// stricter rules (catalog.BastionFacts); this is what the Bastion relies on.
func (t HandshakeFormats) Validate() error {
	seen := map[string]bool{}
	roots := 0
	for _, f := range t {
		switch {
		case f.ID == "" || seen[f.ID]:
			return fmt.Errorf("bastion handshake: the format table names %q twice or without an ID", f.ID)
		case f.RateLimitedStatus != http.StatusTooManyRequests && f.RateLimitedStatus != http.StatusServiceUnavailable:
			return fmt.Errorf("bastion handshake: format %q has the rate-limit status %d", f.ID, f.RateLimitedStatus)
		}
		seen[f.ID] = true
		if f.RegistryRoot {
			roots++
		}
		for _, h := range f.ReplayHeaders {
			if h == "" || http.CanonicalHeaderKey(h) != h || strings.ContainsAny(h, " \t\r\n:") || strings.HasPrefix(h, "Quader-") {
				return fmt.Errorf("bastion handshake: format %q replays the header %q", f.ID, h)
			}
		}
		for _, p := range f.BoundPrefixes {
			if !strings.HasPrefix(p, "/") {
				return fmt.Errorf("bastion handshake: format %q has the bound prefix %q", f.ID, p)
			}
		}
	}
	if roots > 1 {
		return errors.New("bastion handshake: more than one format declares the registry root")
	}
	return nil
}

// HandshakeBastion identifies the registration and the token in use.
type HandshakeBastion struct {
	Name           string    `json:"name"`
	TokenID        string    `json:"tokenId"`
	TokenExpiresAt time.Time `json:"tokenExpiresAt"`
}

// HandshakeInstallation describes the main installation.
type HandshakeInstallation struct {
	BaseURL string `json:"baseUrl"`
	Release string `json:"release"`
}

// HandshakeFeed carries the feed position of the handshake.
type HandshakeFeed struct {
	// Cursor is a [Cursor] in wire form.
	Cursor string `json:"cursor"`
}

// CheckRequest is the body of POST /api/v1/bastion/check (R-BAS-011).
type CheckRequest struct {
	// Credential is the token string the caller presented, or null for an
	// anonymous caller. The field is always present.
	Credential *string `json:"credential"`
	// ClientAddress is the caller's address as the Bastion determined it
	// (R-BAS-019); the failed-credential throttle and token.rejected count
	// against it.
	ClientAddress string `json:"clientAddress"`
	// Repositories are the qualified repository names "<projectKey>/<repoKey>" to
	// answer for, at most bastions.check.max_repositories. A name without a
	// project is refused with 400 malformed_request (R-BAS-011).
	Repositories []string `json:"repositories"`
	// Packages are the (repository, package) pairs to answer for (R-BAS-083),
	// each repository named in Repositories; always present, [] when none.
	// [CheckRequest.PackagesProblem] holds the rules.
	Packages CheckPackages `json:"packages"`
}

// CheckPackage is one pair of a check request's packages (R-BAS-083).
type CheckPackage struct {
	// Repository is a qualified name of the request's repositories.
	Repository string `json:"repository"`
	// Package is the package name, decoded, within the bounds of [ValidPackage].
	Package string `json:"package"`
}

// CheckPackages is the member packages of a check request. It is written as []
// when it holds nothing: the member is always present (R-BAS-083).
type CheckPackages []CheckPackage

// MarshalJSON writes the pairs, and [] for none.
func (p CheckPackages) MarshalJSON() ([]byte, error) {
	if p == nil {
		return []byte("[]"), nil
	}
	return Marshal([]CheckPackage(p))
}

// PackageProblem is the first rule of R-BAS-083 the packages of a check request
// break: the index of the element, a code (count, invalid_package,
// unknown_repository or duplicate) and a sentence for the field error at
// /packages/<Index>.
type PackageProblem struct {
	Index  int
	Code   string
	Detail string
}

// PackagesProblem checks the packages of the request (R-BAS-083) and returns
// the first rule an element breaks, or nil: at most maxPairs elements (the main
// installation's bastions.check.max_repositories); a package name within the
// bounds of [ValidPackage]; a repository the request's repositories name; no pair
// twice. The repositories themselves are the caller's to validate.
func (r CheckRequest) PackagesProblem(maxPairs int) *PackageProblem {
	named := make(map[string]bool, len(r.Repositories))
	for _, repo := range r.Repositories {
		named[repo] = true
	}
	seen := make(map[CheckPackage]bool, len(r.Packages))
	for i, p := range r.Packages {
		switch {
		case i >= maxPairs:
			return &PackageProblem{Index: i, Code: "count", Detail: fmt.Sprintf("must name at most %d packages", maxPairs)}
		case !ValidPackage(p.Package):
			return &PackageProblem{Index: i, Code: "invalid_package", Detail: fmt.Sprintf("must be a package name of 1 to %d bytes of UTF-8 without a control character", MaxPackageBytes)}
		case !named[p.Repository]:
			return &PackageProblem{Index: i, Code: "unknown_repository", Detail: "must name a repository of repositories"}
		case seen[p]:
			return &PackageProblem{Index: i, Code: "duplicate", Detail: "names a pair twice"}
		}
		seen[p] = true
	}
	return nil
}

// CheckAnswer is the answer of the check (R-BAS-011).
type CheckAnswer struct {
	Subject Subject `json:"subject"`
	// Repositories maps every requested qualified name to the caller's shape on it.
	Repositories map[string]RepositoryRead `json:"repositories"`
	// Packages holds one element per requested pair, in request order, and none
	// for a subject that is not usable (R-BAS-083); always present.
	Packages PackageReads `json:"packages"`
}

// PackageRead is the shape of a subject on one package of a repository
// (R-BAS-080, R-BAS-083): [ReadFull] or [ReadPartial], never [ReadNone]; a pair
// whose repository reads none reads partial.
type PackageRead struct {
	Repository string `json:"repository"`
	Package    string `json:"package"`
	Read       Read   `json:"read"`
}

// PackageReads is the member packages of a check answer. It is written as []
// when it holds nothing: the member is always present (R-BAS-083).
type PackageReads []PackageRead

// MarshalJSON writes the shapes, and [] for none.
func (p PackageReads) MarshalJSON() ([]byte, error) {
	if p == nil {
		return []byte("[]"), nil
	}
	return Marshal([]PackageRead(p))
}

// RepositoryRead is the shape of a subject on one repository and, for none, the
// status the caller would receive for a read of it (R-BAS-011, R-BAS-012).
type RepositoryRead struct {
	Read Read `json:"read"`
	// Status is 401, 403 or 404, set with [ReadNone] only.
	Status int `json:"status,omitempty"`
}

// FeedKind is the kind of a feed entry (R-BAS-015).
type FeedKind string

// The feed entry kinds. A kind is a member of the interface: adding one raises
// [InterfaceVersion].
const (
	// FeedToken: the tokens listed in Tokens are revoked.
	FeedToken FeedKind = "token"
	// FeedPrincipal: the principals listed in Principals can no longer be used
	// as before (disabled, deleted, a member removed, a binding removed).
	FeedPrincipal FeedKind = "principal"
	// FeedGrants: every permission answer of every caller is stale.
	FeedGrants FeedKind = "grants"
	// FeedSettings: a setting the handshake reports changed; the Bastion
	// handshakes again. When anonymous access was switched off, the same audit
	// event also yields a grants entry.
	FeedSettings FeedKind = "settings"
	// FeedContent: content was withdrawn from the repositories in Repositories,
	// the repository itself and every virtual repository that includes it,
	// recursively.
	FeedContent FeedKind = "content"
)

// Valid reports whether k is one of the five kinds.
func (k FeedKind) Valid() bool {
	switch k {
	case FeedToken, FeedPrincipal, FeedGrants, FeedSettings, FeedContent:
		return true
	}
	return false
}

// FeedEntry is one entry of the feed. ID is the audit event's ID, so the Bastion
// skips the IDs it applied (the lookback re-reads entries, R-BAS-015). An event
// that yields a second entry (settings and grants) gives it the ID
// "<event ID>:1", a third "<event ID>:2", so every entry has an ID of its own
// and the same event always yields the same IDs. Entries
// carry IDs and qualified repository names, never names or secrets. Exactly one of the
// lists is set, by Kind: Tokens for token, Principals for principal,
// Repositories for content, none for grants and settings.
type FeedEntry struct {
	ID           string    `json:"id"`
	At           time.Time `json:"at"`
	Kind         FeedKind  `json:"kind"`
	Tokens       []string  `json:"tokens,omitempty"`
	Principals   []string  `json:"principals,omitempty"`
	Repositories []string  `json:"repositories,omitempty"`
}

// FeedAnswer is the answer of GET /api/v1/bastion/feed (R-BAS-015).
type FeedAnswer struct {
	Entries []FeedEntry `json:"entries"`
	// Cursor names the last entry returned, or the request's cursor when there is
	// none; it is a [Cursor] in wire form.
	Cursor string `json:"cursor"`
	// More is true when the limit cut the answer.
	More bool `json:"more"`
	// Reset is true when the cursor is no longer valid: the Bastion drops every
	// answer and takes Cursor.
	Reset bool `json:"reset"`
}

// DownloadsRequest is the body of POST /api/v1/bastion/downloads (R-BAS-016).
type DownloadsRequest struct {
	// Events are at most bastions.downloads.max_batch events.
	Events []DownloadEvent `json:"events"`
	// Dropped counts the events the Bastion lost since DroppedSince.
	Dropped int `json:"dropped"`
	// DroppedSince is the start of the period Dropped counts, set with a
	// non-zero Dropped.
	DroppedSince *time.Time `json:"droppedSince,omitempty"`
}

// DownloadEvent is one artifact the Bastion served from its cache (R-BAS-016,
// R-BAS-039). The Bastion knows no item path; Path is the request path.
type DownloadEvent struct {
	At time.Time `json:"at"`
	// TokenID and PrincipalID are empty for an anonymous caller.
	TokenID       string `json:"tokenId,omitempty"`
	PrincipalID   string `json:"principalId,omitempty"`
	ClientAddress string `json:"clientAddress"`
	// Repository is the qualified name "<projectKey>/<repoKey>".
	Repository string `json:"repository"`
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	UserAgent  string `json:"userAgent,omitempty"`
	Replica    string `json:"replica"`
}
