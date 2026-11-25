package observability

import (
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// RedactedValue replaces every secret in logs, spans and error texts (R-OPS-013).
const RedactedValue = "[REDACTED]"

// Secret is a secret held in memory (R-OPS-013 rule 1). Its LogValue, String,
// GoString, MarshalJSON and MarshalText all return the redaction marker; only
// Reveal returns the value, and only the code that needs to send it calls that.
type Secret string

// Reveal returns the secret itself.
func (s Secret) Reveal() string { return string(s) }

// String returns the redaction marker.
func (Secret) String() string { return RedactedValue }

// GoString returns the redaction marker.
func (Secret) GoString() string { return RedactedValue }

// LogValue implements slog.LogValuer.
func (Secret) LogValue() slog.Value { return slog.StringValue(RedactedValue) }

// MarshalJSON returns the redaction marker as a JSON string.
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + RedactedValue + `"`), nil }

// MarshalText returns the redaction marker.
func (Secret) MarshalText() ([]byte, error) { return []byte(RedactedValue), nil }

// sensitiveKeyParts are the substrings of attribute keys whose value is
// replaced (R-OPS-013 rule 2).
func sensitiveKeyParts() []string {
	return []string{"password", "secret", "token", "authorization", "cookie", "api_key", "apikey", "private_key", "credential"}
}

// exemptKey reports the ID fields that contain a sensitive word but carry no secret.
func exemptKey(k string) bool {
	return k == "token_id" || k == "token_type"
}

// sensitiveWholeKeys are keys whose value is replaced although they hold none of
// sensitiveKeyParts, matched whole after lower-casing with "-" read as "_", so
// that a key that merely contains the word stays readable (R-OPS-013 rule 2):
// the header Quader-Bastion-Handover carries a signed link and, for an encrypted
// copy, a data key (R-BAS-073), while a field such as bastion_handover holds an
// outcome.
func sensitiveWholeKeys() []string {
	return []string{"quader_bastion_handover"}
}

// SensitiveKey reports whether the value of an attribute with this key is
// redacted regardless of its content.
func SensitiveKey(key string) bool {
	k := strings.ToLower(key)
	if exemptKey(k) {
		return false
	}
	for _, w := range sensitiveWholeKeys() {
		if strings.ReplaceAll(k, "-", "_") == w {
			return true
		}
	}
	for _, p := range sensitiveKeyParts() {
		if strings.Contains(k, p) {
			return true
		}
	}
	return false
}

// Scrubber removes secrets from free text (R-OPS-013 rule 3).
type Scrubber struct {
	token    *regexp.Regexp
	bearer   *regexp.Regexp
	basic    *regexp.Regexp
	userinfo *regexp.Regexp
	cliPoll  *regexp.Regexp
	callback *regexp.Regexp
	idHint   *regexp.Regexp
	handover *regexp.Regexp
	query    *regexp.Regexp
	// deny is the redaction list in lower case: a name on it loses its value in an address too, even one of ProductQueryParams.
	deny map[string]bool
}

// DefaultQueryParams returns the default of log.request.redact_query_params: the parameters whose value is replaced wherever
// they stand in text, a relative address included. Beside the credentials of a request they name the parameter that carries the
// secret of each well-known signed address (Q-OPS-426): S3's and Google Cloud Storage's signature and credential, the signature of
// Azure's SAS and of a CloudFront link (sig, Signature), the content guard of a Galaxy (validate_token), Docker Hub's blob redirect
// (verify), the token of GitHub's release and package redirects (jwt), a CDN's hmac and Quay's CDN signature (cf_sign). The
// expiry, version and permission parameters of those sets stay readable: without the signature they open nothing.
func DefaultQueryParams() []string {
	return []string{
		"token", "access_token", "password", "secret", "sig", "signature",
		"X-Amz-Signature", "X-Amz-Credential", "X-Amz-Security-Token", "X-Goog-Signature", "X-Goog-Credential",
		"validate_token", "verify", "jwt", "hmac", "cf_sign",
	}
}

// ProductQueryParams returns the query parameters Quader writes into its own requests to other systems and whose value an
// address keeps wherever it is logged, stored, answered or shown (R-OPS-013 rule 7, Q-OPS-426): a search term, a page, a filter,
// the scope of a registry token, the keys of the object store's listing and multipart upload, the scanner's detection. Each names
// what was asked and none carries a credential. Every other value of an address reads [RedactedValue], so a parameter a format
// starts to write is hidden until it is added here: the list fails closed. The names match exactly, case included.
func ProductQueryParams() []string {
	return []string{
		// searches and pages of the upstream catalogues, the mirrors and the clients' searches
		"q", "query", "text", "keywords", "n", "last", "limit", "offset", "page", "page_size", "per_page", "size", "from", "skip",
		"take", "startkey", "endkey", "since",
		// filters
		"artifactType", "prerelease", "semVerLevel", "packageType", "is_highest", "namespace", "name", "owner__username", "public",
		"version",
		// a registry token's request (the realm is the upstream's, these two are Quader's)
		"service", "scope",
		// the requests of a login to an OpenID provider that name Quader (never state, nonce, code or a hint)
		"client_id", "redirect_uri", "post_logout_redirect_uri", "response_type", "code_challenge_method",
		// the object store's API
		"prefix", "delimiter", "max-keys", "list-type", "partNumber", "uploadId",
		// the image scanner's detection
		"database", "analyser", "digest",
	}
}

// productQueryParams is ProductQueryParams as a set.
var productQueryParams = func() map[string]bool {
	m := map[string]bool{}
	for _, p := range ProductQueryParams() {
		m[p] = true
	}
	return m
}()

// NewScrubber builds a scrubber that redacts the values of the given query
// parameters in addition to the fixed patterns.
func NewScrubber(queryParams []string) *Scrubber {
	s := &Scrubber{
		token:  regexp.MustCompile(`qdr_([a-z]{2,8})_[0-9A-Za-z_\-]+`),
		bearer: regexp.MustCompile(`(?i)\bBearer\s+[^\s",;]+`),
		basic:  regexp.MustCompile(`(?i)\bBasic\s+[A-Za-z0-9+/=_\-]{8,}`),
		// The user information of a URL runs from "//" to the last "@" before
		// the authority ends (the first "/", "?", "#", "<", ">" or whitespace).
		// The class is greedy, so a password with an unencoded "@", a "'" or
		// a quote (as in the %q-quoted text of a URL parse error) is redacted
		// whole. Text that merely continues after a URL without a path, up to
		// a later "@", is redacted too: the rule errs towards hiding.
		userinfo: regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.\-]*://)([^/?#\s<>]*)@`),
		cliPoll:  regexp.MustCompile(`(/auth/cli/)[^/?#\s"']+`),
		// The query of an OIDC callback holds the authorization code and the
		// flow's state (and session_state, iss): one-time secrets of the login
		// that the configurable list cannot name without hiding the state filter
		// of the token listing too (Q-OPS-91).
		callback: regexp.MustCompile(`(/auth/oidc/[^/?#\s"']+/callback\?)[^#\s"']*`),
		// An ID token presented as a logout hint is a signed identity statement.
		idHint: regexp.MustCompile(`(?i)([?&;]id_token_hint)=[^&#\s"']*`),
		// The value of Quader-Bastion-Handover on an answer: after the header's
		// name, and bare by its start, the base64url of `{"link":"` that every
		// encoded handover begins with (R-BAS-073). The offer "accept; …" stays.
		handover: regexp.MustCompile(`(?i)(Quader-Bastion-Handover\s*[:=]\s*)([A-Za-z0-9_\-]+)|eyJsaW5rIjoi[A-Za-z0-9_\-]*`),
		deny:     map[string]bool{},
	}
	for _, p := range queryParams {
		s.deny[strings.ToLower(p)] = true
	}
	if len(queryParams) > 0 {
		quoted := make([]string, len(queryParams))
		for i, p := range queryParams {
			quoted[i] = regexp.QuoteMeta(p)
		}
		s.query = regexp.MustCompile(`(?i)([?&;](?:` + strings.Join(quoted, "|") + `))=[^&#\s"']*`)
	}
	return s
}

var defaultScrubber = NewScrubber(DefaultQueryParams())

// Scrub removes secrets from s with the default query parameter list.
func Scrub(s string) string { return defaultScrubber.String(s) }

// String removes secrets from s.
func (s *Scrubber) String(in string) string {
	if in == "" {
		return in
	}
	out := in
	if strings.Contains(out, "qdr_") {
		out = s.token.ReplaceAllString(out, "qdr_${1}_"+RedactedValue)
	}
	if len(out) >= 6 {
		out = s.bearer.ReplaceAllString(out, "Bearer "+RedactedValue)
		out = s.basic.ReplaceAllString(out, "Basic "+RedactedValue)
	}
	if strings.Contains(out, "@") {
		out = s.userinfo.ReplaceAllStringFunc(out, s.redactUserinfo)
	}
	if strings.Contains(out, "/auth/cli/") {
		out = s.cliPoll.ReplaceAllString(out, "${1}"+RedactedValue)
	}
	if strings.Contains(out, "/callback?") {
		out = s.callback.ReplaceAllString(out, "${1}"+RedactedValue)
	}
	if strings.Contains(out, "id_token_hint") {
		out = s.idHint.ReplaceAllString(out, "${1}="+RedactedValue)
	}
	if strings.Contains(out, "eyJsaW5rIjoi") || strings.Contains(out, "andover") || strings.Contains(out, "ANDOVER") {
		out = s.handover.ReplaceAllStringFunc(out, s.redactHandover)
	}
	if strings.Contains(out, "://") && strings.Contains(out, "?") {
		out = absoluteURL.ReplaceAllStringFunc(out, s.textAddress)
	}
	if s.query != nil && strings.ContainsAny(out, "?&;") {
		out = s.query.ReplaceAllString(out, "${1}="+RedactedValue)
	}
	return out
}

// absoluteURL is an http or https address in free text: it ends at whitespace, a quote or an angle bracket, as a URL that Go's
// errors quote with %q does.
var absoluteURL = regexp.MustCompile("(?i)\\bhttps?://[^\\s\"'<>`]+")

// textAddress applies the address rule to an absolute address found in free text: the query keeps its names and the values of
// ProductQueryParams. Whether the address was Quader's own or one an upstream handed back cannot be read from text, so the
// values of the list stay; none of them carries a credential (R-OPS-013 rule 7).
func (s *Scrubber) textAddress(m string) string {
	head, query, ok := strings.Cut(m, "?")
	if !ok || query == "" {
		return m
	}
	return head + "?" + s.redactQuery(query, false)
}

// paramName is what a query parameter's name looks like; a name that does not (a token standing as a name, an encoded blob) is
// redacted whole.
var paramName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.\-]{0,63}$`)

// maxBareName is the longest part without "=" that stays as a name; a longer one is redacted.
const maxBareName = 24

// redactQuery rewrites a raw query to the form of R-OPS-013 rule 7: every name that looks like one stays, its value stays only
// for a parameter of ProductQueryParams that is not on the redaction list and only when the address is not handed (an address an
// upstream handed back: a redirect's target), and every other value reads [RedactedValue].
func (s *Scrubber) redactQuery(raw string, handed bool) string {
	parts := strings.Split(raw, "&")
	for i, p := range parts {
		if p == "" {
			continue
		}
		name, _, hasValue := strings.Cut(p, "=")
		plain, err := url.QueryUnescape(name)
		switch {
		case err != nil || !paramName.MatchString(plain):
			parts[i] = RedactedValue
		case !hasValue && len(plain) > maxBareName:
			parts[i] = RedactedValue // a long word without "=" may be a token itself
		case !hasValue:
			// A bare name (S3's ?uploads) carries no value.
		case !handed && productQueryParams[plain] && !s.deny[strings.ToLower(plain)]:
		default:
			parts[i] = name + "=" + RedactedValue
		}
	}
	return strings.Join(parts, "&")
}

// redactHandover rewrites one match of the handover pattern: the header's name
// stays, its value is redacted unless it is the Bastion's offer ("accept"); a
// bare value is redacted whole.
func (s *Scrubber) redactHandover(m string) string {
	sub := s.handover.FindStringSubmatch(m)
	if len(sub) != 3 || sub[1] == "" {
		return RedactedValue
	}
	if strings.EqualFold(sub[2], "accept") {
		return m
	}
	return sub[1] + RedactedValue
}

// redactUserinfo rewrites one match of the userinfo pattern: "user:secret@"
// keeps the user name, a bare "secret@" (a token in the user field) keeps
// nothing.
func (s *Scrubber) redactUserinfo(m string) string {
	sub := s.userinfo.FindStringSubmatch(m)
	if len(sub) != 3 {
		return m
	}
	if user, _, ok := strings.Cut(sub[2], ":"); ok {
		return sub[1] + user + ":" + RedactedValue + "@"
	}
	return sub[1] + RedactedValue + "@"
}

// URL renders an address Quader built itself the way every log, stored record, answer, event and screen holds it (R-OPS-013
// rule 7, Q-OPS-426): scheme, host and path, no user information and no fragment, and the query's names with the values of
// ProductQueryParams only; every other value reads [RedactedValue]. It is the url field of the upstream log (R-OPS-011).
func (s *Scrubber) URL(u *url.URL) string { return s.address(u, false) }

// HandedURL renders an address an upstream handed back (a redirect's target, the Location of an answer) as URL does, with every
// value of its query redacted: Quader wrote none of them, and a signed address carries its credential there.
func (s *Scrubber) HandedURL(u *url.URL) string { return s.address(u, true) }

// RequestURL renders the address of an outbound request: a request that follows a redirect (r.Response is set) is HandedURL,
// any other URL. Every record of an outbound request takes its address from here.
func (s *Scrubber) RequestURL(r *http.Request) string {
	if r == nil {
		return ""
	}
	return s.address(r.URL, r.Response != nil)
}

// URLPath renders an address with no query at all: scheme, host, port and path, no user information, no fragment. It is the
// stricter form of the records whose screens say that they show no query: the failure of a download (R-CUR-011, Q-CUR-20), the
// address the connection test asked and its token service's realm (Q-REPO-489), the importer's messages (Q-OPS-426).
func (s *Scrubber) URLPath(u *url.URL) string {
	if u == nil {
		return ""
	}
	c := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path, RawPath: u.RawPath}
	return s.String(c.String())
}

func (s *Scrubber) address(u *url.URL, handed bool) string {
	if u == nil {
		return ""
	}
	c := *u
	c.User, c.Fragment, c.RawFragment = nil, "", ""
	if c.RawQuery != "" {
		c.RawQuery = s.redactQuery(c.RawQuery, handed)
	}
	return s.String(c.String())
}

// ScrubURL is [Scrubber.URL] with the default redaction list.
func ScrubURL(u *url.URL) string { return defaultScrubber.URL(u) }

// ScrubHandedURL is [Scrubber.HandedURL] with the default redaction list.
func ScrubHandedURL(u *url.URL) string { return defaultScrubber.HandedURL(u) }

// ScrubRequestURL is [Scrubber.RequestURL] with the default redaction list.
func ScrubRequestURL(r *http.Request) string { return defaultScrubber.RequestURL(r) }

// ScrubURLPath is [Scrubber.URLPath] with the default redaction list.
func ScrubURLPath(u *url.URL) string { return defaultScrubber.URLPath(u) }

// ScrubAddress is [ScrubURL] of a raw address; text that does not parse as one is scrubbed as free text.
func ScrubAddress(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Scrub(raw)
	}
	return ScrubURL(u)
}

// Error returns the scrubbed text of err, for errors that leave the process.
func (s *Scrubber) Error(err error) string {
	if err == nil {
		return ""
	}
	return s.String(err.Error())
}

// ScrubError returns the scrubbed text of err with the default parameter list.
func ScrubError(err error) string { return defaultScrubber.Error(err) }
