package engine

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/qname"
)

// family is the path family of a request (INV-6, R-BAS-018 step 3). The Bastion knows families, not formats: what is
// particular to a format reaches it as the main installation's marking or as the format's facts in the handshake
// (R-BAS-050, R-BAS-057).
type family int

const (
	// familyAPI is /api/<format>/<projectKey>/<repoKey>/….
	familyAPI family = iota
	// familyFiles is /files/<projectKey>/<repoKey>/….
	familyFiles
	// familyRegistry is the registry root /v2/… at the host root of server.base_url (R-BAS-051, Q-BAS-165): its core
	// routes, /v2/<projectKey>/<repoKey>/…, and on a bound host every other path below /v2/ (R-BAS-057).
	familyRegistry
	// familyBound is a bound-host prefix of a format outside /v2/ on a host the main installation binds to one
	// repository (R-BAS-057). No format of today declares one.
	familyBound
)

// The core routes of the registry root (R-BAS-051): they name no repository.
const (
	coreVersion = "version" // /v2 and /v2/, the version check
	coreToken   = "token"   // /v2/_token, the main installation's token service
	coreCatalog = "catalog" // /v2/_catalog
)

// registryPrefix is the registry root's family below the host root (R-OCI-001).
const registryPrefix = "/v2/"

// belowRegistryRoot reports whether an escaped path lies below the registry root: /v2 or below /v2/.
func belowRegistryRoot(p string) bool { return p == "/v2" || strings.HasPrefix(p, registryPrefix) }

// coreRoute names the core route of a path below the registry root, "" for every other path.
func coreRoute(p string) string {
	switch p {
	case "/v2", "/v2/":
		return coreVersion
	case "/v2/_token":
		return coreToken
	case "/v2/_catalog":
		return coreCatalog
	}
	return ""
}

// pathClass is what the classification read of a path.
type pathClass struct {
	fam          family
	core         string
	format, repo string
	facts        iface.HandshakeFormat
}

// classify reads the family, the format, the repository and the format's facts of an escaped path (R-BAS-018 step 3).
// ok is false for a path the Bastion does not serve: below /v2/ that is a path that names no repository, and the whole
// family while the handshake's table names no format with the registry root.
func (e *engine) classify(p string) (pathClass, bool) {
	st := e.d.Link.State()
	if belowRegistryRoot(p) {
		return classifyRegistry(p, st.Handshaken, st.Formats, st.BoundRepository)
	}
	if st.BoundRepository != "" {
		if f, ok := boundFormat(st.Formats, p); ok {
			return pathClass{fam: familyBound, format: f.ID, repo: st.BoundRepository, facts: f}, true
		}
	}
	format, repo, ok := classifyPath(p)
	if !ok {
		return pathClass{}, false
	}
	c := pathClass{fam: familyAPI, format: format, repo: repo, facts: st.Formats.Lookup(format)}
	if strings.HasPrefix(p, iface.FilesPrefix) {
		// /files/ is a family of the core, no format: the defaults of R-BAS-057.
		c.fam, c.facts = familyFiles, iface.HandshakeFormats(nil).Lookup(format)
	}
	return c, true
}

// classifyRegistry reads a path below the registry root (R-BAS-051, R-BAS-057). The format is the one whose entry of the
// handshake's table says registryRoot; before the first handshake the table is not known, the path is classified by
// its form and served once the main installation answered, which a read needs anyway. On a host the main installation
// binds to a repository every path below /v2/ but the core routes is that repository, the whole path after /v2/ the
// image name, as the main installation's dispatcher reads it (formats/oci.md R-OCI-029, Q-BAS-195). Otherwise
// /v2/<projectKey>/<repoKey>/<rest> is read with qname.ParseRegistryAddress (INV-5's grammar, a "%" in the project or
// the key is no name), and a dot segment is not classified (Q-BAS-121).
func classifyRegistry(p string, handshaken bool, table iface.HandshakeFormats, bound string) (pathClass, bool) {
	root, ok := table.RegistryRoot()
	if !ok && handshaken {
		return pathClass{fam: familyRegistry}, false
	}
	c := pathClass{fam: familyRegistry, format: root.ID, facts: root, core: coreRoute(p)}
	if !ok {
		c.facts = table.Lookup("")
	}
	if c.core != "" {
		return c, true
	}
	if bound != "" && hasBoundPrefix(root, registryPrefix) {
		rest := strings.TrimPrefix(p, registryPrefix)
		if rest == "" || strings.HasPrefix(rest, "/") || dotSegments(rest) {
			return c, false
		}
		c.repo = bound
		return c, true
	}
	a, ok := qname.ParseRegistryAddress(p)
	if !ok || a.DotSegments() {
		return c, false
	}
	c.repo = a.Repo.String()
	return c, true
}

// hasBoundPrefix reports whether a format's entry names the bound-host prefix p.
func hasBoundPrefix(f iface.HandshakeFormat, p string) bool {
	for _, b := range f.BoundPrefixes {
		if b == p {
			return true
		}
	}
	return false
}

// boundFormat returns the format whose bound-host prefix outside the registry root the path matches (R-BAS-057).
func boundFormat(table iface.HandshakeFormats, p string) (iface.HandshakeFormat, bool) {
	for _, f := range table {
		for _, b := range f.BoundPrefixes {
			if b != registryPrefix && !strings.HasPrefix(b, registryPrefix) && strings.HasPrefix(p, b) && !dotSegments(p) {
				return f, true
			}
		}
	}
	return iface.HandshakeFormat{}, false
}

// dotSegments reports whether an escaped path has a "." or ".." segment.
func dotSegments(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." {
			return true
		}
	}
	return false
}

// registryChallenge is the WWW-Authenticate value of a 401 below the registry root (R-OCI-004 step 1, R-BAS-051): the
// realm of the token service and the service on the Bastion's own base URL, as the main installation renders them for
// the host it answers for (R-BAS-017), and error="invalid_token" for a credential the Bastion refused. A scope names an
// image of the path, which only the format's grammar reads; the Bastion names none, and a client asks for the scope of
// the operation it runs (Q-BAS-196).
func (e *engine) registryChallenge(rejected bool) string {
	var b strings.Builder
	b.WriteString(`Bearer realm="`)
	b.WriteString(e.realm)
	b.WriteString(`",service="`)
	b.WriteString(e.service)
	b.WriteString(`"`)
	if rejected {
		b.WriteString(`,error="invalid_token"`)
	}
	return b.String()
}

// The answers of the distribution specification (formats/oci.md R-OCI-003, R-OCI-008) that a Bastion gives itself below
// the registry root (Q-BAS-169).
const (
	distributionVersionHeader = "Docker-Distribution-Api-Version"
	distributionVersion       = "registry/2.0"
)

// registryCode maps a refusal onto the code of the distribution specification, as the format of the main installation
// maps its own (the table of R-OCI-008).
func registryCode(e *errs.Error) string {
	switch e.Kind {
	case errs.Unauthenticated:
		return "UNAUTHORIZED"
	case errs.Forbidden:
		return "DENIED"
	case errs.NotFound:
		return "NAME_UNKNOWN"
	case errs.MethodNotAllowed, errs.Misdirected:
		return "UNSUPPORTED"
	case errs.TooLarge:
		return "SIZE_INVALID"
	case errs.RangeNotSatisfiable:
		return "BLOB_UPLOAD_INVALID"
	case errs.RateLimited:
		return "TOOMANYREQUESTS"
	case errs.Unavailable:
		switch e.Code {
		case iface.CodeRateLimited, iface.CodeAddressBlocked, iface.CodeAuthTimeout:
			return "TOOMANYREQUESTS"
		}
		return "UNAVAILABLE"
	case errs.UpstreamFailed, errs.UpstreamTimeout:
		return "UNAVAILABLE"
	}
	return "UNKNOWN"
}

// registryErrorBody is one entry of the distribution's error list; detail carries the Bastion's own code (R-BAS-029,
// Q-BAS-169).
type registryErrorBody struct {
	Code    string        `json:"code"`
	Message string        `json:"message"`
	Detail  quaderProblem `json:"detail"`
}

// quaderProblem is the detail of an entry: the code of R-BAS-029.
type quaderProblem struct {
	Code string `json:"code"`
}

// writeRegistryError renders one of the Bastion's own answers below the registry root in the distribution's shape
// (R-BAS-029, R-BAS-051, Q-BAS-169): the status of the error, the API version header, the challenge, Retry-After and the
// other headers the error carries, and {"errors":[{"code":…,"message":…,"detail":{"code":"<Quader code>"}}]}.
func writeRegistryError(w http.ResponseWriter, r *http.Request, err *errs.Error) {
	p, e := errs.ProblemFor(r, err, "")
	h := w.Header()
	switch e.Kind {
	case errs.Unauthenticated:
		if e.Challenge != "" {
			h.Set("WWW-Authenticate", e.Challenge)
		}
	case errs.MethodNotAllowed:
		if len(e.Allow) > 0 {
			h.Set("Allow", strings.Join(e.Allow, ", "))
		}
	case errs.RateLimited, errs.Unavailable:
		h.Set("Retry-After", retrySeconds(e.RetryAfter))
	}
	for k, v := range e.Headers {
		h.Set(k, v)
	}
	text := p.Detail
	if text == "" {
		text = p.Title
	}
	body, _ := json.Marshal(struct {
		Errors []registryErrorBody `json:"errors"`
	}{[]registryErrorBody{{Code: registryCode(e), Message: text, Detail: quaderProblem{Code: p.Code}}}})
	h.Set("Content-Type", "application/json")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set(distributionVersionHeader, distributionVersion)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(e.Status())
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

// retrySeconds renders a Retry-After value in whole seconds, at least 1.
func retrySeconds(d time.Duration) string {
	s := int64((d + time.Second - 1) / time.Second)
	return strconv.FormatInt(max(s, 1), 10)
}
