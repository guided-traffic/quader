package catalog

import (
	"fmt"
	"net/http"
	"net/textproto"
	"slices"
	"strings"
)

// BastionFacts are what a Quader Bastion needs to know of a format before, or without, a response of the main installation
// (bastion.md R-BAS-057, R-BAS-058, ADR-238): the headers a cached answer of the format replays besides the core's, and the
// status the format answers a rate limit with. They reach every Bastion in the handshake's format table, so the Bastion's code
// holds no knowledge of a format. The zero value is a format with no protocol header of its own that answers a rate limit 429,
// which is what every format of today but two declares.
type BastionFacts struct {
	// ReplayHeaders are the response headers of the format's content routes that a cached answer replays besides the core's
	// [RepresentationHeaders]: the protocol headers every reader of the answer may receive (OCI's Docker-Content-Digest and the
	// Link of a page). Each is a valid header name in net/http's canonical form (http.CanonicalHeaderKey), at most once, and
	// none is a representation or per-request header of the core ([RepresentationHeaders], [PerRequestHeaders]), a hop-by-hop
	// header, Set-Cookie, or a header with the prefix "Quader-". Kit check 19 fails a content route that answers with a header
	// none of the three lists names: a Bastion's cached answer would drop it (R-BAS-058).
	ReplayHeaders []string
	// RateLimitedStatus is the status the format's WriteError answers a rate limit (an error of the kind RateLimited) with, and so
	// the status of a Bastion's own rate-limit refusals on the format's family: 0 stands for 429; otherwise 429 or 503. The ClamAV
	// definitions declare 503, because freshclam puts a 429 on a cool-down of hours (formats/clamav.md R-CLAMAV-005). Kit check 19
	// holds WriteError to it.
	RateLimitedStatus int
}

// RateLimited returns the status of a rate limit of the format: RateLimitedStatus, or 429 when it is 0.
func (b BastionFacts) RateLimited() int {
	if b.RateLimitedStatus == 0 {
		return http.StatusTooManyRequests
	}
	return b.RateLimitedStatus
}

// RepresentationHeaders are the response headers of the core that describe the representation of a content route's answer
// (bastion.md R-BAS-022, R-BAS-058, Q-BAS-181): the representation metadata and validators of RFC 9110 that the core or a
// format sets for every reader alike, and the content headers of R-ARCH-007 that formats.ServeRoute puts on every Download,
// Metadata, Computed and Volatile answer. A Bastion's cached answer replays them for every format, with the format's
// [BastionFacts.ReplayHeaders]; a format declares none of them. The names are canonical (http.CanonicalHeaderKey: "Etag").
func RepresentationHeaders() []string {
	return []string{
		"Content-Type", "Content-Encoding", "Content-Language", "Content-Disposition", "Etag", "Last-Modified", "Cache-Control",
		"Expires", "Vary", "Repr-Digest",
		"X-Content-Type-Options", "Content-Security-Policy", "Cross-Origin-Resource-Policy",
	}
}

// PerRequestHeaders are the response headers of the core that belong to one answer and are never replayed from a cache
// (bastion.md R-BAS-021, R-BAS-058, Q-BAS-181): the framing a server computes for each answer it writes (Content-Length,
// Content-Range, Accept-Ranges, Date, Age), the request ID and the replica that answered (R-OPS-002), the cache state of
// the remote engine (Cache-Status), the rate-limit field Retry-After, and the member of a virtual repository that answered
// (Quader-Resolved-From, R-REPO-019). A Bastion computes the framing of its own answers and drops the others from a hit; a
// format declares none of them. The names are canonical.
func PerRequestHeaders() []string {
	return []string{
		"Content-Length", "Content-Range", "Accept-Ranges", "Date", "Age",
		"X-Request-Id", "X-Quader-Replica", "Cache-Status", "Retry-After", "Quader-Resolved-From",
	}
}

// neverReplayed are the response headers no cache replays and no format may declare: the hop-by-hop headers of RFC 9110
// section 7.6.1 and cookies.
func neverReplayed() []string {
	return []string{
		"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Proxy-Connection", "Te", "Trailer",
		"Transfer-Encoding", "Upgrade", "Set-Cookie",
	}
}

// reservedHeaderPrefix starts every header Quader names itself: the Bastion interface's (Quader-Bastion-) and the core's own
// (Quader-Resolved-From). No format declares one.
const reservedHeaderPrefix = "Quader-"

// BoundPrefixes are the literal bound-host patterns of the format (endpoints.md R-EP-006): the paths relative to the root of a
// host that the main installation's host_roots binds to one repository, which a request on such a host reaches with the
// repository fixed. The format that declares [Facts.RegistryRoot] has "/v2/" (formats/oci.md R-OCI-029); every other format of
// today has none. They are derived, not declared (Q-BAS-183): the dispatcher reads the registry root's binding from
// RegistryRoot, and a format to come with a bound host of its own (Terraform's discovery) adds its declaration then.
func (f Facts) BoundPrefixes() []string {
	if f.RegistryRoot {
		return []string{"/v2/"}
	}
	return nil
}

// checkBastion applies the rules of [BastionFacts] to a format's facts.
func checkBastion(f Facts) error {
	switch f.Bastion.RateLimitedStatus {
	case 0, http.StatusTooManyRequests, http.StatusServiceUnavailable:
	default:
		return fmt.Errorf("%q declares the rate-limit status %d; a rate limit answers 429 or 503 (R-BAS-058)", f.ID, f.Bastion.RateLimitedStatus)
	}
	core := slices.Concat(RepresentationHeaders(), PerRequestHeaders(), neverReplayed())
	seen := map[string]bool{}
	for _, h := range f.Bastion.ReplayHeaders {
		switch {
		case !validHeaderName(h):
			return fmt.Errorf("%q declares the replay header %q, which is no header name (R-BAS-058)", f.ID, h)
		case textproto.CanonicalMIMEHeaderKey(h) != h:
			return fmt.Errorf("%q declares the replay header %q; write it in canonical form, %q (R-BAS-058)", f.ID, h, textproto.CanonicalMIMEHeaderKey(h))
		case seen[h]:
			return fmt.Errorf("%q declares the replay header %q twice", f.ID, h)
		case strings.HasPrefix(h, reservedHeaderPrefix):
			return fmt.Errorf("%q declares the replay header %q; a header with the prefix %q is Quader's own, never a format's (R-BAS-058)", f.ID, h, reservedHeaderPrefix)
		case slices.Contains(core, h):
			return fmt.Errorf("%q declares the replay header %q, which is the core's: a cache replays or drops it whatever the format says (R-BAS-058)", f.ID, h)
		}
		seen[h] = true
	}
	return nil
}

// validHeaderName reports whether s is a field name of RFC 9110 section 5.1: one or more token characters.
func validHeaderName(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}
