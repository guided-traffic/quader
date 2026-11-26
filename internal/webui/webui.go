// Package webui serves the embedded Angular UI under /ui/{locale}/ (R-ARCH-004, R-UI-001).
//
// Mount the handler at the pattern "/ui/" of the client listener without stripping the prefix:
//
//	h, err := webui.New(webui.Options{Locales: []string{"en", "de"}, DefaultLocale: "en"})
//	mux.Handle("/ui/", h)
//
// Behavior:
//   - /ui/ and /ui/<route> without a locale redirect (302) to the same route under the preferred
//     locale: Options.Preference, then Accept-Language, then Options.DefaultLocale.
//   - /ui/<locale> redirects to /ui/<locale>/.
//   - /ui/<locale>/<file> serves the file; hashed build output is cached as immutable, other
//     files are revalidated. A hashed file the bundle lacks is served from Options.Shared, the
//     store every release's replicas publish their hashed files to (R-HA-020, Q-HA-120). Any
//     other missing path whose last segment has an extension is a 404, except below `packages/`, where a dot belongs to a package name.
//   - every other path under a locale is a client-side route and answers with the locale's
//     index.html (SPA fallback), with a fresh CSP nonce and the document headers of R-UI-001.
package webui

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// NoncePlaceholder is the literal in index.html that is replaced with the per-response nonce. The
// Angular build copies it into the nonce attribute of every script and style tag it writes.
const NoncePlaceholder = "QUADER_CSP_NONCE"

// MountPath is the path prefix the handler serves. Register it as "/ui/".
const MountPath = "/ui/"

// Options configures New.
type Options struct {
	// Locales are the served locale codes (ui.locales), for example ["en", "de"]. Required.
	Locales []string
	// DefaultLocale (ui.default_locale) must be one of Locales. Required.
	DefaultLocale string
	// ImageOrigins are exact https origins added to the CSP img-src
	// (ui.readme.external_image_origins and server.content_base_url). Optional.
	ImageOrigins []string
	// Preference returns the signed-in user's locale preference (R-API-021) or "". Optional;
	// a value outside Locales is ignored.
	Preference func(r *http.Request) string
	// Assets is the build the handler serves, rooted like a dist/ directory: <locale>/index.html per locale, or a single
	// index.html as placeholder. Required: the main installation passes its build (internal/webui/dist), the Bastion its own
	// (internal/bastion/gui, R-BAS-116), and tests a tree of their own.
	Assets fs.FS
	// CSP replaces the Content-Security-Policy of the documents (R-UI-001): a policy with one %s slot for the per-response nonce.
	// ImageOrigins is then not used. Optional; the Bastion's web interface sets its stricter policy (R-BAS-117).
	CSP string
	// ReferrerPolicy replaces the Referrer-Policy of the documents, same-origin by default. Optional.
	ReferrerPolicy string
	// OnIndex is called before a document (a locale's index.html) is written: it may set headers and returns the status, 0 for
	// 200. Optional; the Bastion answers a page 503 while it holds no state to show (R-BAS-117).
	OnIndex func(w http.ResponseWriter, r *http.Request) int
	// Problem writes an error answer of the handler (a method it does not serve, a file it does not hold, the shared store that
	// could not be asked) in place of its minimal problem details. Optional; the Bastion renders its own (Q-BAS-321).
	Problem func(w http.ResponseWriter, r *http.Request, status int)
	// Shared is the shared asset store (R-HA-020, Q-HA-120): a hashed file the bundle lacks is
	// looked up there. Optional; nil answers such a file 404, as a replica without the store did.
	Shared Shared
	// OnSharedError is told about a store that could not be asked, which the request answers 503.
	// Optional; the app logs it.
	OnSharedError func(locale, path string, err error)
}

// Handler serves the UI. It is safe for concurrent use.
type Handler struct {
	locales       []string
	def           string
	preference    func(*http.Request) string
	cspFormat     string
	bundles       map[string]*bundle
	placeholder   bool
	shared        *sharedCache // nil without Options.Shared
	onSharedError func(locale, path string, err error)
	referrer      string
	onIndex       func(http.ResponseWriter, *http.Request) int
	problem       func(http.ResponseWriter, *http.Request, int)
}

// bundle is one locale: the index.html template and, for a real build, the asset tree.
type bundle struct {
	locale string
	index  []byte
	assets fs.FS // nil in placeholder mode
}

var localeCode = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

// New validates the options and the UI tree and returns the handler.
//
// If no locale directory exists in the tree, New serves the committed placeholder index.html for
// every locale (a binary built without the web build). If some locales are built and a configured
// one is missing, New fails: a half-built UI must not start.
func New(opts Options) (*Handler, error) {
	if len(opts.Locales) == 0 {
		return nil, errors.New("webui: no locales configured")
	}
	seen := map[string]bool{}
	for _, l := range opts.Locales {
		if !localeCode.MatchString(l) {
			return nil, fmt.Errorf("webui: locale %q is not a valid locale code", l)
		}
		if seen[l] {
			return nil, fmt.Errorf("webui: locale %q is listed twice", l)
		}
		seen[l] = true
	}
	if !seen[opts.DefaultLocale] {
		return nil, fmt.Errorf("webui: default locale %q is not in the locale list", opts.DefaultLocale)
	}
	csp := opts.CSP
	if csp == "" {
		var err error
		if csp, err = buildCSP(opts.ImageOrigins); err != nil {
			return nil, err
		}
	} else if strings.Count(csp, "%s") != 1 || strings.ContainsAny(csp, "\r\n") {
		return nil, errors.New("webui: the CSP holds exactly one %s slot for the nonce and no line break")
	}
	root := opts.Assets
	if root == nil {
		return nil, errors.New("webui: no build to serve (Options.Assets)")
	}

	h := &Handler{
		locales:       append([]string(nil), opts.Locales...),
		def:           opts.DefaultLocale,
		preference:    opts.Preference,
		cspFormat:     csp,
		bundles:       make(map[string]*bundle, len(opts.Locales)),
		onSharedError: opts.OnSharedError,
		referrer:      opts.ReferrerPolicy,
		onIndex:       opts.OnIndex,
		problem:       opts.Problem,
	}
	if h.referrer == "" {
		h.referrer = "same-origin"
	}
	if h.problem == nil {
		h.problem = problem
	}
	if opts.Shared != nil {
		h.shared = newSharedCache(opts.Shared)
	}

	var missing []string
	for _, l := range h.locales {
		index, err := fs.ReadFile(root, l+"/index.html")
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				missing = append(missing, l)
				continue
			}
			return nil, fmt.Errorf("webui: read %s/index.html: %w", l, err)
		}
		sub, err := fs.Sub(root, l)
		if err != nil {
			return nil, fmt.Errorf("webui: locale %s: %w", l, err)
		}
		if err := checkIndex(l, index); err != nil {
			return nil, err
		}
		h.bundles[l] = &bundle{locale: l, index: index, assets: sub}
	}

	switch {
	case len(missing) == len(h.locales):
		index, err := fs.ReadFile(root, "index.html")
		if err != nil {
			return nil, fmt.Errorf("webui: neither locale directories nor a placeholder index.html: %w", err)
		}
		if err := checkIndex("placeholder", index); err != nil {
			return nil, err
		}
		for _, l := range h.locales {
			h.bundles[l] = &bundle{locale: l, index: index}
		}
		h.placeholder = true
	case len(missing) > 0:
		return nil, fmt.Errorf("webui: built UI lacks configured locale(s) %s", strings.Join(missing, ", "))
	}
	return h, nil
}

func checkIndex(name string, index []byte) error {
	if !bytes.Contains(index, []byte(NoncePlaceholder)) {
		return fmt.Errorf("webui: %s index.html lacks the %s placeholder", name, NoncePlaceholder)
	}
	return nil
}

// Placeholder reports whether the handler serves the placeholder page because the binary holds
// no web build.
func (h *Handler) Placeholder() bool { return h.placeholder }

// Locales returns the served locale codes.
func (h *Handler) Locales() []string { return append([]string(nil), h.locales...) }

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		h.problem(w, r, http.StatusMethodNotAllowed)
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, MountPath)
	if !ok {
		h.problem(w, r, http.StatusNotFound)
		return
	}

	first, tail, hasTail := strings.Cut(rest, "/")
	if b := h.bundles[first]; b != nil {
		if !hasTail {
			h.redirect(w, r, MountPath+first+"/")
			return
		}
		h.serveLocale(w, r, b, tail)
		return
	}
	h.redirect(w, r, MountPath+h.preferred(r)+"/"+rest)
}

// preferred returns the locale for a request without one in its path.
func (h *Handler) preferred(r *http.Request) string {
	if h.preference != nil {
		if l := h.preference(r); h.bundles[l] != nil {
			return l
		}
	}
	if l := negotiate(r.Header.Get("Accept-Language"), h.locales); l != "" {
		return l
	}
	return h.def
}

func (h *Handler) redirect(w http.ResponseWriter, r *http.Request, target string) {
	u := url.URL{Path: target, RawQuery: r.URL.RawQuery}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Add("Vary", "Accept-Language")
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (h *Handler) serveLocale(w http.ResponseWriter, r *http.Request, b *bundle, tail string) {
	last := tail[strings.LastIndexByte(tail, '/')+1:]
	if tail == "" || tail == "index.html" || !strings.Contains(last, ".") || isPackageRoute(tail) || isFilesRoute(tail) {
		h.serveIndex(w, r, b)
		return
	}
	// A path whose last segment has an extension names a file. It never falls back to the
	// index, so a missing script or stylesheet is a 404 and not an HTML page.
	if !fs.ValidPath(tail) {
		h.problem(w, r, http.StatusNotFound)
		return
	}
	if b.assets != nil {
		if served := h.serveFrom(w, r, b.assets, tail); served {
			return
		}
	}
	// A hashed file of another release: during a rolling upgrade the page that asks for it got its
	// index.html from a replica of that release (R-HA-020, Q-HA-120). Nothing else is ever looked up.
	if h.shared != nil && IsHashed(tail) {
		h.serveShared(w, r, b.locale, tail)
		return
	}
	h.problem(w, r, http.StatusNotFound)
}

// isPackageRoute reports whether tail is a package page of the UI (`projects/<key>/packages/<format>/<name>`): the name of a package can
// have a dot (an Ansible collection is `namespace.name`), which is no file extension, so a reload or a link to the page must reach the
// index and not a 404 (R-UI-007).
func isPackageRoute(tail string) bool {
	return strings.HasPrefix(tail, "packages/") || strings.Contains(tail, "/packages/")
}

// isFilesRoute reports whether tail is the page of a folder or a file of a repository (`projects/<key>/repositories/<key>/files/<path>`,
// R-UI-010): the page of a file ends in the file's name, `…/files/docs/readme.txt`, which is no asset of the UI, so a reload or a link
// to it must reach the index and not a 404. The UI's own assets are never below a `repositories` segment.
func isFilesRoute(tail string) bool {
	_, rest, found := strings.Cut(tail, "/repositories/")
	if !found {
		return false
	}
	_, _, found = strings.Cut(rest, "/files/")
	return found
}

// serveFrom serves the file at tail of the bundle and reports whether it did: false for a path
// that is missing or a directory, which the caller answers.
func (h *Handler) serveFrom(w http.ResponseWriter, r *http.Request, assets fs.FS, tail string) bool {
	f, err := assets.Open(tail)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		data, err := io.ReadAll(f)
		if err != nil {
			h.problem(w, r, http.StatusInternalServerError)
			return true
		}
		rs = bytes.NewReader(data)
	}
	serveFile(w, r, tail, rs)
	return true
}

// serveFile writes a file of a bundle or of the shared store with the headers of R-UI-001: its
// content type, nosniff, and immutable caching for a hashed name, revalidation for any other.
func serveFile(w http.ResponseWriter, r *http.Request, tail string, rs io.ReadSeeker) {
	last := tail[strings.LastIndexByte(tail, '/')+1:]
	hd := w.Header()
	hd.Set("Content-Type", contentType(tail))
	hd.Set("X-Content-Type-Options", "nosniff")
	if IsHashed(tail) {
		hd.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		hd.Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, last, time.Time{}, rs)
}

func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request, b *bundle) {
	nonce := rand.Text()
	body := bytes.ReplaceAll(b.index, []byte(NoncePlaceholder), []byte(nonce))

	hd := w.Header()
	status := http.StatusOK
	if h.onIndex != nil {
		if s := h.onIndex(w, r); s != 0 {
			status = s
		}
	}
	hd.Set("Content-Type", "text/html; charset=utf-8")
	hd.Set("Content-Length", strconv.Itoa(len(body)))
	hd.Set("Cache-Control", "no-cache")
	hd.Set("Content-Security-Policy", fmt.Sprintf(h.cspFormat, nonce))
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Referrer-Policy", h.referrer)
	hd.Set("Cross-Origin-Opener-Policy", "same-origin")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func contentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".woff2":
		return "font/woff2"
	case ".woff":
		return "font/woff"
	case ".json", ".map":
		return "application/json"
	case ".ico":
		return "image/x-icon"
	case ".svg":
		return "image/svg+xml"
	case ".txt":
		return "text/plain; charset=utf-8"
	}
	if t := mime.TypeByExtension(path.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

// problem writes a minimal problem+json answer whose title is the status's phrase. The edge's error model (R-ARCH-023) is not
// imported on purpose: webui stays free of other internal packages.
func problem(w http.ResponseWriter, _ *http.Request, status int) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"type":"about:blank","title":%q,"status":%d}`+"\n", http.StatusText(status), status)
}
