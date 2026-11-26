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
	// Configuration is the registration's configuration (R-BAS-093): its switches, the handover per blob among them, and
	// its limits. Always present; the Bastion refuses a handshake whose configuration breaks [Configuration.Validate].
	Configuration Configuration `json:"configuration"`
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
	// RetryAfterMaxSeconds is the longest wait in seconds the Bastion's own refusals
	// on the format's family tell a client, in Retry-After and in the RateLimit
	// field's t (R-BAS-058, Q-BAS-310): 0 tells the true wait, which the detail of
	// the answer names in either case. Always present.
	RetryAfterMaxSeconds int `json:"retryAfterMaxSeconds"`
	// BoundPrefixes are the literal bound-host patterns of the format (R-EP-006),
	// paths from the host root: "/v2/" for the registry root.
	BoundPrefixes []string `json:"boundPrefixes"`
	// ErrorShape is the shape of every answer of the Bastion's own on the format's family (R-BAS-095, Q-BAS-337): its
	// refusal of a write, its limits and the table of R-BAS-029. Always present.
	ErrorShape ErrorShape `json:"errorShape"`
	// ReadWithBody are the format's read routes that take a body (R-BAS-095): a request whose method and escaped path
	// match an entry ([HandshakeFormat.ReadsWithBody]) is forwarded with its body; every other request of a method that
	// is not GET or HEAD is a write. Always present, [] when there are none.
	ReadWithBody []BodyRoute `json:"readWithBody"`
}

// ErrorShape is one of the shapes a Bastion renders its own answers in on a format's family (R-BAS-095, Q-BAS-337). A
// shape is a member of the interface: adding one raises [InterfaceVersion].
type ErrorShape string

// The error shapes. The body of each carries the answer's code and its detail; the status line is the error's.
const (
	// ErrorShapeProblem is problem details (RFC 9457), application/problem+json, the type about:blank.
	ErrorShapeProblem ErrorShape = "problem"
	// ErrorShapeNPM is {"error": "<detail>"}, application/json: npm prints the member error.
	ErrorShapeNPM ErrorShape = "npm"
	// ErrorShapeText is one line "<code>: <detail>", text/plain; charset=utf-8.
	ErrorShapeText ErrorShape = "text"
	// ErrorShapeDistribution is the error list of the OCI distribution specification:
	// {"errors": [{"code": "<registry code>", "message": "<detail>", "detail": {"code": "<code>"}}]}.
	ErrorShapeDistribution ErrorShape = "distribution"
	// ErrorShapeGalaxy is the error list of a Galaxy's API:
	// {"errors": [{"status": "<status>", "code": "<code>", "title": "<status phrase>.", "detail": "<detail>"}]}, which
	// ansible-galaxy reads and problem details are not (measured 2026-11-26, Q-BAS-337).
	ErrorShapeGalaxy ErrorShape = "galaxy"
)

// Valid reports whether s is one of the five shapes.
func (s ErrorShape) Valid() bool {
	switch s {
	case ErrorShapeProblem, ErrorShapeNPM, ErrorShapeText, ErrorShapeDistribution, ErrorShapeGalaxy:
		return true
	}
	return false
}

// BodyRoute is one read route that takes a body (R-BAS-095): a method other than GET and HEAD and a path pattern from the
// host root whose segments are literal or a placeholder "{name}" that stands for one non-empty segment, as the main
// installation's router matches a route: "/api/npm/{project}/{repo}/-/npm/v1/security/advisories/bulk", "/v2/_token".
type BodyRoute struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

// valid reports whether the route has a method of token characters other than GET and HEAD and a pattern from the root
// whose placeholders are whole segments.
func (b BodyRoute) valid() bool {
	if b.Method == "" || b.Method == http.MethodGet || b.Method == http.MethodHead || strings.ToUpper(b.Method) != b.Method ||
		strings.ContainsAny(b.Method, " \t\r\n/:;,") || !strings.HasPrefix(b.Path, "/") {
		return false
	}
	for _, seg := range strings.Split(b.Path[1:], "/") {
		inner, isParam := strings.CutPrefix(seg, "{")
		if isParam {
			inner, isParam = strings.CutSuffix(inner, "}")
			if !isParam || inner == "" || strings.ContainsAny(inner, "{}.") {
				return false
			}
			continue
		}
		if seg == "" || seg == "." || seg == ".." || strings.ContainsAny(seg, "{}") {
			return false
		}
	}
	return true
}

// matches reports whether an escaped path is matched by the route's pattern: the same number of segments, a literal
// segment byte for byte, a placeholder any non-empty segment. A path that spells a literal segment encoded does not match:
// the request is then a write and refused, so a spelling the table does not name fails closed.
func (b BodyRoute) matches(escapedPath string) bool {
	if !strings.HasPrefix(escapedPath, "/") {
		return false
	}
	pat, got := strings.Split(b.Path[1:], "/"), strings.Split(escapedPath[1:], "/")
	if len(pat) != len(got) {
		return false
	}
	for i, seg := range pat {
		if strings.HasPrefix(seg, "{") {
			if got[i] == "" {
				return false
			}
			continue
		}
		if got[i] != seg {
			return false
		}
	}
	return true
}

// ReadsWithBody reports whether a request of the method on the escaped path is one of the format's read routes that take a
// body (R-BAS-095).
func (f HandshakeFormat) ReadsWithBody(method, escapedPath string) bool {
	for _, b := range f.ReadWithBody {
		if b.Method == method && b.matches(escapedPath) {
			return true
		}
	}
	return false
}

// HandshakeFormats is the handshake's format table.
type HandshakeFormats []HandshakeFormat

// CapRetryAfter returns the wait a client of the format is told for a wait of d:
// d, or RetryAfterMaxSeconds when that is set and shorter (R-BAS-058).
func (f HandshakeFormat) CapRetryAfter(d time.Duration) time.Duration {
	if f.RetryAfterMaxSeconds > 0 {
		return min(d, time.Duration(f.RetryAfterMaxSeconds)*time.Second)
	}
	return d
}

// Lookup returns the entry of the format id, or the defaults of R-BAS-057 for a
// format the table does not name: no replay headers, the rate-limit status 429,
// the true wait, the shape problem and no read route with a body.
func (t HandshakeFormats) Lookup(id string) HandshakeFormat {
	for _, f := range t {
		if f.ID == id {
			return f
		}
	}
	return HandshakeFormat{ID: id, RateLimitedStatus: http.StatusTooManyRequests, ErrorShape: ErrorShapeProblem}
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
// at most one declares the registry root; a rate-limit status is 429 or 503; the
// longest wait is not negative; a replay header is a canonical header name without the prefix "Quader-"; a bound
// prefix starts with "/"; the error shape is one of the five; a read route with a body has a method other than GET and
// HEAD and a pattern of whole segments. The main installation's catalog holds the formats to
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
		case f.RetryAfterMaxSeconds < 0:
			return fmt.Errorf("bastion handshake: format %q has the longest wait %d s", f.ID, f.RetryAfterMaxSeconds)
		case !f.ErrorShape.Valid():
			return fmt.Errorf("bastion handshake: format %q has the error shape %q", f.ID, f.ErrorShape)
		}
		for _, b := range f.ReadWithBody {
			if !b.valid() {
				return fmt.Errorf("bastion handshake: format %q has the read route with a body %s %q", f.ID, b.Method, b.Path)
			}
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
	// FeedCatalogue: the public catalogue of a registration changed (R-BAS-101). A Bastion whose web interface is on reads
	// its change list ([PathCatalogue]) at once instead of at its next poll; the entry names no registration, so every such
	// Bastion reads, and one whose catalogue did not change gets an empty list.
	FeedCatalogue FeedKind = "catalogue"
)

// Valid reports whether k is one of the six kinds.
func (k FeedKind) Valid() bool {
	switch k {
	case FeedToken, FeedPrincipal, FeedGrants, FeedSettings, FeedContent, FeedCatalogue:
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

// DownloadsRequest is the body of POST /api/v1/bastion/downloads (R-BAS-016): the
// reporting batch, which carries download events, usage and the replica's status
// (R-LIM-045). A batch without an event is valid: usage and status are reported
// whether or not downloads are recorded.
type DownloadsRequest struct {
	// Events are at most bastions.downloads.max_batch events; empty while
	// recordDownloads is false.
	Events []DownloadEvent `json:"events"`
	// Dropped counts the events the Bastion lost since DroppedSince.
	Dropped int `json:"dropped"`
	// DroppedSince is the start of the period Dropped counts, set with a
	// non-zero Dropped.
	DroppedSince *time.Time `json:"droppedSince,omitempty"`
	// Usage is what the Bastion counted per subject since the last batch (R-LIM-045).
	Usage UsageReport `json:"usage"`
	// Status is the sending replica's state (R-UI-240).
	Status ReplicaStatus `json:"status"`
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
