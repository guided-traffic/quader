package iface

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Class is the kind of a response, which says what the Bastion may keep of it
// (R-BAS-013, R-BAS-021).
type Class string

// The values of [HeaderClass].
const (
	// ClassArtifact is a 200 or 304 of a Download route or of a file under /files/:
	// immutable bytes, kept by content address (R-BAS-022).
	ClassArtifact Class = "artifact"
	// ClassShared is a 200 or 304 of a Metadata or Computed route (or a /files/
	// folder listing) when the caller's shape on the addressed repository is full:
	// the same bytes for every full reader (R-BAS-023).
	ClassShared Class = "shared"
	// ClassPrivate is the same routes for any other caller: a document filtered
	// for that caller, never shared.
	ClassPrivate Class = "private"
	// ClassAbsent is a 404 of a read route when the item does not exist in the
	// repository for any subject (R-BAS-024). A refusal, a hidden item and a
	// serve-time refusal are never absent.
	ClassAbsent Class = "absent"
	// ClassPass is every other answer: Volatile and other routes, errors,
	// redirects. The Bastion keeps nothing of it.
	ClassPass Class = "pass"
	// ClassHandover is the 200 of a Download route or of a file under /files/ that the main installation answers with a
	// [Handover] in [HeaderHandover] instead of the bytes: the file's own headers, Content-Length 0 and no body (R-BAS-073).
	// It answers only a recognized forward that carried a [HandoverOffer]; the Bastion reads the bytes from the object store
	// and keeps them as an artifact (R-BAS-074). The engine never passes such an answer to a caller.
	ClassHandover Class = "handover"
)

// Valid reports whether c is one of the six classes.
func (c Class) Valid() bool {
	switch c {
	case ClassArtifact, ClassShared, ClassPrivate, ClassAbsent, ClassPass, ClassHandover:
		return true
	}
	return false
}

// Read is the read shape of a subject on a repository (R-BAS-012, Q-BAS-28).
type Read string

// The values of [HeaderRead] and of the shape members of the check answer.
const (
	// ReadFull: the subject may read every package and version reachable through
	// the repository, and every response of a read route is the same for it as for
	// every other full subject.
	ReadFull Read = "full"
	// ReadPartial is every other case in which the subject may read something.
	ReadPartial Read = "partial"
	// ReadNone: the subject may read nothing through the repository.
	ReadNone Read = "none"
)

// Valid reports whether r is one of the three shapes.
func (r Read) Valid() bool { return r == ReadFull || r == ReadPartial || r == ReadNone }

// Marking is the set of response headers the main installation adds to the
// response of a recognized Bastion request (R-BAS-013).
type Marking struct {
	Class Class
	// Repository is the qualified name "<projectKey>/<repoKey>".
	Repository string
	Read       Read
	Subject    Subject
	// Interface is the main installation's interface version.
	Interface int
	// Counted says that serving the response counted a download at the main
	// installation ([HeaderCounted], R-BAS-059): what a download is stays the main
	// installation's rule for every format, and the Bastion reports every serve of
	// such an entry from its cache.
	Counted bool
	// Package is the package the response belongs to, decoded ([HeaderPackage],
	// R-BAS-081, R-BAS-082); empty for a response that belongs to none.
	Package string
	// PackageRead is the caller's shape on Package ([HeaderPackageRead]):
	// [ReadFull] or [ReadPartial], set exactly with Package.
	PackageRead Read
}

// Set writes the marking headers. A marking without a repository (a response
// that addresses none, class pass) leaves [HeaderRepository] and [HeaderRead]
// out; one that counted no download leaves [HeaderCounted] out. A package is
// written percent-encoded with its shape, which is [ReadPartial] unless
// PackageRead is [ReadFull]; a marking without a package, or with a name
// outside the bounds of [ValidPackage], leaves both package headers out, so the
// response belongs to no package and claims less, never more (R-BAS-082).
func (m Marking) Set(h http.Header) {
	h.Set(HeaderClass, string(m.Class))
	if m.Repository != "" {
		h.Set(HeaderRepository, m.Repository)
	}
	if m.Read != "" {
		h.Set(HeaderRead, string(m.Read))
	}
	h.Set(HeaderSubject, m.Subject.Header())
	if m.Subject.Kind == SubjectToken && m.Subject.Limits != nil && m.Subject.Limits.Valid() {
		h.Set(HeaderLimits, m.Subject.Limits.Header())
	}
	h.Set(HeaderInterface, strconv.Itoa(m.Interface))
	if m.Counted {
		h.Set(HeaderCounted, CountedDownload)
	}
	if ValidPackage(m.Package) {
		read := ReadPartial
		if m.PackageRead == ReadFull {
			read = ReadFull
		}
		h.Set(HeaderPackage, encodePackage(m.Package))
		h.Set(HeaderPackageRead, string(read))
	}
}

// StripMarking removes every header with [HeaderPrefix] from a response header
// map. The main installation strips them from the response of every request that
// is not a recognized Bastion request (R-BAS-013), and the Bastion strips them
// before it replays a response to its caller (R-BAS-021).
func StripMarking(h http.Header) {
	for k := range h {
		if strings.HasPrefix(http.CanonicalHeaderKey(k), HeaderPrefix) {
			delete(h, k)
		}
	}
}

// ErrMarking is the error of a response whose marking the Bastion cannot read.
var ErrMarking = errors.New("bastion marking: malformed")

// ParseMarking reads the marking of a response. A response without [HeaderClass]
// is unmarked: ok is false and the Bastion treats the response as the answer of a
// proxy in front of the main installation (R-BAS-021). A marked response with a
// malformed header is an error. Repository and read may be absent, and so may
// the package; parsePackage says what makes the package headers malformed.
func ParseMarking(h http.Header) (m Marking, ok bool, err error) {
	class := h.Get(HeaderClass)
	if class == "" {
		return Marking{}, false, nil
	}
	m.Class = Class(class)
	if !m.Class.Valid() {
		return Marking{}, true, fmt.Errorf("%w: %s %q", ErrMarking, HeaderClass, class)
	}
	m.Repository = h.Get(HeaderRepository)
	if r := h.Get(HeaderRead); r != "" {
		m.Read = Read(r)
		if !m.Read.Valid() {
			return Marking{}, true, fmt.Errorf("%w: %s %q", ErrMarking, HeaderRead, r)
		}
	}
	if s := h.Get(HeaderSubject); s != "" {
		sub, err := ParseSubjectHeader(s)
		if err != nil {
			return Marking{}, true, err
		}
		m.Subject = sub
	}
	if v := h.Values(HeaderLimits); len(v) > 0 {
		if len(v) != 1 || m.Subject.Kind != SubjectToken {
			return Marking{}, true, fmt.Errorf("%w: %s %q without one token subject", ErrMarking, HeaderLimits, v)
		}
		l, err := ParseSubjectLimits(v[0])
		if err != nil {
			return Marking{}, true, err
		}
		m.Subject.Limits = &l
	}
	if v := h.Get(HeaderInterface); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return Marking{}, true, fmt.Errorf("%w: %s %q", ErrMarking, HeaderInterface, v)
		}
		m.Interface = n
	}
	if v := h.Values(HeaderCounted); len(v) > 0 {
		if len(v) != 1 || v[0] != CountedDownload {
			return Marking{}, true, fmt.Errorf("%w: %s %q", ErrMarking, HeaderCounted, v)
		}
		m.Counted = true
	}
	if m.Package, m.PackageRead, err = parsePackage(h); err != nil {
		return Marking{}, true, err
	}
	return m, true, nil
}

// parsePackage reads [HeaderPackage] and [HeaderPackageRead] (R-BAS-082). Both
// absent is a response that belongs to no package. It refuses one header without
// the other, more than one value of either, a shape other than full or partial,
// a name that does not decode strictly (decodePackage) and a decoded name
// outside the bounds of [ValidPackage].
func parsePackage(h http.Header) (string, Read, error) {
	names, reads := h.Values(HeaderPackage), h.Values(HeaderPackageRead)
	switch {
	case len(names) == 0 && len(reads) == 0:
		return "", "", nil
	case len(names) == 0 || len(reads) == 0:
		return "", "", fmt.Errorf("%w: %s and %s come together", ErrMarking, HeaderPackage, HeaderPackageRead)
	case len(names) > 1 || len(reads) > 1:
		return "", "", fmt.Errorf("%w: more than one %s or %s", ErrMarking, HeaderPackage, HeaderPackageRead)
	}
	read := Read(reads[0])
	if read != ReadFull && read != ReadPartial {
		return "", "", fmt.Errorf("%w: %s %q", ErrMarking, HeaderPackageRead, reads[0])
	}
	name, ok := decodePackage(names[0])
	if !ok {
		return "", "", fmt.Errorf("%w: %s %q does not decode", ErrMarking, HeaderPackage, names[0])
	}
	if !ValidPackage(name) {
		return "", "", fmt.Errorf("%w: %s %q is outside the bounds of a package name", ErrMarking, HeaderPackage, names[0])
	}
	return name, read, nil
}

// SubjectKind says who a credential turned out to be (R-BAS-011, R-BAS-013).
type SubjectKind string

// The subject kinds. The header [HeaderSubject] carries token, anonymous and
// invalid only; the check answer carries all five.
const (
	// SubjectToken: a valid token. ExpiresAt is its expiry, if it has one.
	SubjectToken SubjectKind = "token"
	// SubjectAnonymous: no credential and anonymous access on.
	SubjectAnonymous SubjectKind = "anonymous"
	// SubjectAnonymousRefused: no credential and anonymous access off.
	SubjectAnonymousRefused SubjectKind = "anonymous_refused"
	// SubjectInvalid: malformed, unknown, expired, revoked or owner disabled; the
	// main installation tells which to nobody (R-AUTH-002).
	SubjectInvalid SubjectKind = "invalid"
	// SubjectThrottled: the address is in the failed-credential throttle's
	// throttled mode and the credential is not exempt (R-AUTH-038). Check only.
	SubjectThrottled SubjectKind = "throttled"
)

// Subject is the main installation's statement about a credential. It is the
// subject member of the check answer (JSON) and, in the form of [Subject.Header],
// the value of [HeaderSubject].
type Subject struct {
	Kind        SubjectKind `json:"kind"`
	TokenID     string      `json:"tokenId,omitempty"`
	PrincipalID string      `json:"principalId,omitempty"`
	// ExpiresAt is the token's expiry; absent for a token that does not expire.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	// RetryAfterSeconds is set with [SubjectThrottled] only.
	RetryAfterSeconds *int `json:"retryAfterSeconds,omitempty"`
	// Limits is the subject's key and rule at the Bastion's place (R-BAS-096), set with [SubjectToken] only and only once
	// the main installation resolves a rule there; absent for every other kind, since the Bastion decides an anonymous
	// caller's key and rule itself. In the marking it travels in [HeaderLimits].
	Limits *SubjectLimits `json:"limits,omitempty"`
}

// SubjectLimits names what a Bastion charges a signed-in caller against (R-BAS-096, Q-BAS-328): the subject key of the
// download limits (limits_subject of R-LIM-030: user:<id>, technical_token:<id>, ci:<issuer>:<project>) and the ID of the
// rule of the Bastion's place the main installation resolved for the caller, which names an entry of the handshake's
// [PlaceTable].
type SubjectLimits struct {
	Key  string `json:"key"`
	Rule string `json:"rule"`
}

// Valid reports whether the key is within the bounds of [ValidPackage] and the rule an ID of [ValidRuleID].
func (l SubjectLimits) Valid() bool { return ValidPackage(l.Key) && ValidRuleID(l.Rule) }

// MaxRuleIDBytes bounds a rule ID of a [PlaceTable].
const MaxRuleIDBytes = 64

// ValidRuleID reports whether s can name a rule of a [PlaceTable]: 1 to [MaxRuleIDBytes] bytes of letters, digits, ".",
// ":", "_" and "-" (a UUID is one), so that it needs no encoding in [HeaderLimits].
func ValidRuleID(s string) bool {
	if s == "" || len(s) > MaxRuleIDBytes {
		return false
	}
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == ':', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// Header formats the value of [HeaderLimits]: "key=<key>; rule=<rule>", the key percent-encoded as [HeaderPackage] is.
func (l SubjectLimits) Header() string { return "key=" + encodePackage(l.Key) + "; rule=" + l.Rule }

// ParseSubjectLimits reads a value written by [SubjectLimits.Header]: both members once, in either order, the key
// decoding strictly to a name of [ValidPackage], the rule a [ValidRuleID].
func ParseSubjectLimits(v string) (SubjectLimits, error) {
	var l SubjectLimits
	var haveKey, haveRule bool
	for _, part := range strings.Split(v, ";") {
		k, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		switch {
		case ok && k == "key" && !haveKey:
			key, good := decodePackage(val)
			if !good {
				return SubjectLimits{}, fmt.Errorf("%w: %s %q", ErrMarking, HeaderLimits, v)
			}
			l.Key, haveKey = key, true
		case ok && k == "rule" && !haveRule:
			l.Rule, haveRule = val, true
		default:
			return SubjectLimits{}, fmt.Errorf("%w: %s %q", ErrMarking, HeaderLimits, v)
		}
	}
	if !haveKey || !haveRule || !l.Valid() {
		return SubjectLimits{}, fmt.Errorf("%w: %s %q", ErrMarking, HeaderLimits, v)
	}
	return l, nil
}

// Header formats the subject for [HeaderSubject] (R-BAS-013):
//
//	token=<token ID>; principal=<principal ID>; expires=<unix seconds>
//	anonymous
//	invalid
//
// The expires member is left out for a token that does not expire (a service
// account token may). The refused and throttled kinds are never a header value;
// they format as invalid, which is what the response to the caller is.
func (s Subject) Header() string {
	switch s.Kind {
	case SubjectToken:
		out := "token=" + s.TokenID + "; principal=" + s.PrincipalID
		if s.ExpiresAt != nil {
			out += "; expires=" + strconv.FormatInt(s.ExpiresAt.Unix(), 10)
		}
		return out
	case SubjectAnonymous:
		return string(SubjectAnonymous)
	}
	return string(SubjectInvalid)
}

// ParseSubjectHeader parses a [HeaderSubject] value written by [Subject.Header].
func ParseSubjectHeader(v string) (Subject, error) {
	switch v {
	case string(SubjectAnonymous):
		return Subject{Kind: SubjectAnonymous}, nil
	case string(SubjectInvalid):
		return Subject{Kind: SubjectInvalid}, nil
	}
	s := Subject{Kind: SubjectToken}
	seen := map[string]bool{}
	for _, part := range strings.Split(v, ";") {
		k, val, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found || val == "" || seen[k] {
			return Subject{}, fmt.Errorf("%w: %s %q", ErrMarking, HeaderSubject, v)
		}
		seen[k] = true
		switch k {
		case "token":
			s.TokenID = val
		case "principal":
			s.PrincipalID = val
		case "expires":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil || n < 0 {
				return Subject{}, fmt.Errorf("%w: %s %q", ErrMarking, HeaderSubject, v)
			}
			t := time.Unix(n, 0).UTC()
			s.ExpiresAt = &t
		default:
			return Subject{}, fmt.Errorf("%w: %s %q", ErrMarking, HeaderSubject, v)
		}
	}
	if s.TokenID == "" || s.PrincipalID == "" {
		return Subject{}, fmt.Errorf("%w: %s %q", ErrMarking, HeaderSubject, v)
	}
	return s, nil
}
