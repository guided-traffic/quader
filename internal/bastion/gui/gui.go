// Package gui is the Bastion's read-only web interface (W-35, R-BAS-116 to R-BAS-123): the second, small Angular application of
// web/src/bastion, embedded into the binary from dist/, and the data it reads, all of it from the replica's public catalogue
// (internal/bastion/catalogue) and its rendered legal pages. It is mounted on the client listener before the engine and owns `/`,
// `/ui` and everything below `/ui/`, and `/robots.txt` and the sitemap at the host's root.
//
// Search engines (W-35's B.11, R-BAS-121 to R-BAS-123). A page's address is answered with the application's index.html, its head
// and its text filled from the catalogue (seo.go, package page) for a reader that runs no script; robots.txt and the sitemap name
// the pages on the Bastion's own base URL (crawl.go); a page's address the class gui refuses is a page of its own, not problem
// details (refusal.go).
//
// Read-only by construction (W-35's B.7). Every address answers GET and HEAD only and every other method 405 without reading a
// body. Nothing here reaches the main installation: the package imports neither the client of the main installation, nor the
// handover, nor the engine (the archtest rule of R-BAS-116), and a document the catalogue does not hold is a 404, never a fill.
// No answer sets a cookie or keeps a state per visitor, none carries a header of the product's own (`Quader-…`), and every one
// carries the headers of R-BAS-117. Every request is counted in the guard's class gui (Q-LIM-307): a page view is never a miss.
//
// The state decides first (R-BAS-117, R-BAS-111): while the switch gui is off every address answers 404 not_found; while the
// replica holds no state to show (or knows neither its switches nor a state) the data answers 503 not_ready with Retry-After 30
// and a page is served with that status, so that a person reads the application's sentence and a crawler indexes nothing. The
// legal pages and the bundle's own files are served while the replica is not ready, since they come from the binary and the chart.
package gui

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/catalogue"
	"github.com/guided-traffic/quader/internal/bastion/guard"
	pg "github.com/guided-traffic/quader/internal/bastion/gui/page"
	"github.com/guided-traffic/quader/internal/bastion/legal"
	"github.com/guided-traffic/quader/internal/bastion/settings"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/observability"
	"github.com/guided-traffic/quader/internal/webui"
)

// bundle is the Bastion's web application as the binary embeds it: the committed placeholder dist/index.html, and one directory
// per locale that `npm run build:embed` in web/ adds (dist/en/, dist/de/), which git ignores.
//
//go:embed all:dist
var bundle embed.FS

// Bundle is the embedded application, rooted as webui.Options.Assets takes it.
func Bundle() fs.FS {
	sub, err := fs.Sub(bundle, "dist")
	if err != nil {
		panic(err) // the directory is embedded; a build without it does not compile
	}
	return sub
}

// The locales of the application (W-35's B.3): the main installation's two, English first.
func locales() []string { return []string{"en", "de"} }

const defaultLocale = "en"

// CSP is the Content-Security-Policy of a page (R-BAS-117, W-35's B.7): nothing but the Bastion's own origin, no form, no frame,
// Trusted Types; %s is the nonce of the page's styles.
const CSP = "default-src 'none'; script-src 'self'; style-src 'self' 'nonce-%s'; img-src 'self' data:; font-src 'self'; " +
	"connect-src 'self'; base-uri 'self'; form-action 'none'; frame-ancestors 'none'; " +
	"require-trusted-types-for 'script'; trusted-types angular angular#bundler"

// dataCSP is the policy of every answer that is no page: it runs nothing and is framed nowhere.
const dataCSP = "default-src 'none'; frame-ancestors 'none'"

// notReadyRetry is the Retry-After of not_ready (W-35's B.2b).
const notReadyRetry = 30 * time.Second

// The texts of the web interface's own problems (W-35's B.13, Q-BAS-321): they name no party.
const (
	textNotFound  = "Nothing is offered at this address."
	textNotReady  = "This page is not available right now. Try again in a few minutes."
	textMethod    = "This address only reads: it answers GET and HEAD."
	textUnhandled = "The server failed to process the request."
)

// Guard is what the web interface asks of the Bastion's guard (R-BAS-019): the block list and the class gui (Q-LIM-307).
type Guard interface {
	Blocked(ctx context.Context, addr string) guard.Verdict
	Page(ctx context.Context, addr string) guard.Verdict
}

// Options configure a [Handler].
type Options struct {
	// Catalogue is the replica's public catalogue (Server.Catalogue). Required.
	Catalogue catalogue.Reader
	// Legal is the imprint and the privacy notice rendered at start (Server.Legal). Required.
	Legal *legal.Pages
	// BaseURL is the Bastion's own server.base_url: every absolute address a page's head, robots.txt and the sitemap name is on it
	// (R-BAS-121, W-35's B.13), never on the main installation's. Required.
	BaseURL string
	// Guard counts every request in the class gui; nil admits every request (tests).
	Guard Guard
	// Flood is the class gui's figures of the configuration a request read, for the RateLimit fields of a refusal; nil leaves them
	// out.
	Flood func(ctx context.Context) settings.Rate
	// Quiet marks a request that is not logged one by one (a blocked address, R-BAS-037); nil does nothing.
	Quiet func(ctx context.Context)
	// Assets replaces the embedded application ([Bundle]); tests set it.
	Assets fs.FS
	// Log receives what fails inside; nil discards.
	Log *slog.Logger
}

// Handler serves the web interface. It is safe for concurrent use.
type Handler struct {
	o   Options
	ui  *webui.Handler
	log *slog.Logger
	// sm is the sitemap of the catalogue's current revision (R-BAS-122).
	sm sitemapState
	// pages writes what a crawler reads of a page and the page of a refusal (R-BAS-121, R-BAS-123).
	pages *pg.Renderer
}

// New builds the handler over the application's build.
func New(o Options) (*Handler, error) {
	if o.Catalogue == nil || o.Legal == nil {
		return nil, errors.New("gui: the catalogue and the legal pages are required")
	}
	if u, err := url.Parse(o.BaseURL); err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("gui: the Bastion's base URL is required")
	}
	if o.Assets == nil {
		o.Assets = Bundle()
	}
	h := &Handler{o: o, log: o.Log, pages: pg.NewRenderer()}
	if h.log == nil {
		h.log = slog.New(slog.DiscardHandler)
	}
	ui, err := webui.New(webui.Options{
		Locales: locales(), DefaultLocale: defaultLocale, Assets: o.Assets, CSP: CSP, ReferrerPolicy: "no-referrer",
		Page:    h.fill,
		Problem: func(w http.ResponseWriter, r *http.Request, status int) { h.problem(w, r, statusError(status)) },
	})
	if err != nil {
		return nil, err
	}
	h.ui = ui
	return h, nil
}

// Owns reports whether the web interface serves a path of the client listener: `/`, `/ui` and everything below `/ui/`, and at the
// host's root `/robots.txt`, `/sitemap.xml` and everything below `/sitemap/` (R-BAS-122). Every other path is the engine's
// (R-BAS-018).
func Owns(path string) bool {
	return path == "/" || path == "/ui" || strings.HasPrefix(path, "/ui/") ||
		path == pathRobots || path == pathSitemap || strings.HasPrefix(path, sitemapDir)
}

// Placeholder reports whether the binary holds no build of the application and serves the placeholder page.
func (h *Handler) Placeholder() bool { return h.ui.Placeholder() }

// ServeHTTP implements [http.Handler].
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Every problem of the web interface has the type about:blank and names no party (Q-BAS-321), also without the client
	// listener's chain.
	r = r.WithContext(errs.WithBastionAnswer(r.Context()))
	if ri := observability.RequestInfoFrom(r.Context()); ri != nil {
		ri.SetHandler("bastion-gui")
	}
	secure(w.Header())
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.problem(w, r, errs.E(errs.MethodNotAllowed, errs.CodeMethodNotAllowed, textMethod, errs.WithAllow(http.MethodGet, http.MethodHead)))
		return
	}
	if !h.admit(w, r) {
		return
	}
	path := r.URL.Path
	st := h.o.Catalogue.State()
	if path == pathRobots {
		// Also while the web interface is off: then it keeps every crawler away from the host (R-BAS-122).
		h.robots(w, r, st)
		return
	}
	if st.Decided && !st.Enabled {
		h.problem(w, r, errNotFound())
		return
	}
	switch {
	case path == pathSitemap || strings.HasPrefix(path, sitemapDir):
		h.sitemap(w, r, st.Ready)
	case path == "/" || path == "/ui":
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, webui.MountPath, http.StatusFound)
	case strings.HasPrefix(path, dataRoot):
		// The data are what a page reads, never a page a search engine shows (W-35's B.11).
		w.Header().Set("X-Robots-Tag", "noindex")
		h.data(w, r, st.Ready)
	case strings.HasPrefix(path, "/ui/data/") || path == "/ui/data":
		w.Header().Set("X-Robots-Tag", "noindex")
		h.problem(w, r, errNotFound())
	default:
		// The bundle's files, the locale's redirect and the pages, a page with its head and text from the catalogue and 503 while not
		// ready (fill).
		h.ui.ServeHTTP(w, r)
	}
}

// secure sets the headers every answer of the web interface carries (R-BAS-117, W-35's B.7); a page replaces the policy with its
// own.
func secure(hd http.Header) {
	hd.Set("Content-Security-Policy", dataCSP)
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("X-Frame-Options", "DENY")
	hd.Set("Cross-Origin-Resource-Policy", "same-origin")
	hd.Set("Referrer-Policy", "no-referrer")
}

// admit asks the guard: the block list, then the class gui (R-BAS-018 steps 2 and 4, Q-LIM-307). A refusal is the problem of
// R-BAS-029 with Retry-After and the RateLimit fields of the class.
func (h *Handler) admit(w http.ResponseWriter, r *http.Request) bool {
	if h.o.Guard == nil {
		return true
	}
	addr := addressKey(r)
	v := h.o.Guard.Blocked(r.Context(), addr)
	if v.Allowed {
		v = h.o.Guard.Page(r.Context(), addr)
	}
	if v.Allowed {
		return true
	}
	if v.Quiet && h.o.Quiet != nil {
		h.o.Quiet(r.Context())
	}
	code := v.Code
	if code == "" {
		code = guard.CodeRateLimited
	}
	wait := oneWait(v.RetryAfter)
	var fields [][2]string
	if h.o.Flood != nil {
		rate := h.o.Flood(r.Context())
		window := max(1, (rate.Burst+rate.RequestsPerSecond-1)/max(1, rate.RequestsPerSecond))
		fields = [][2]string{
			{"RateLimit-Policy", `"pages";q=` + strconv.Itoa(rate.Burst) + ";w=" + strconv.Itoa(window)},
			{"RateLimit", `"pages";r=0;t=` + strconv.FormatInt(int64(wait/time.Second), 10)},
		}
	}
	if locale, ok := h.pageLocale(r); ok {
		// A person reads a page in the Bastion's shape, not problem details; the data keep theirs (R-BAS-123).
		h.refusalPage(w, r, locale, wait, code == guard.CodeAddressBlocked, fields)
		return false
	}
	opts := []errs.Option{errs.WithRetryAfter(wait)}
	for _, f := range fields {
		opts = append(opts, errs.WithHeader(f[0], f[1]))
	}
	detail := "Too many requests from this address. Try again in " + strconv.FormatInt(int64(wait/time.Second), 10) + " seconds."
	if code == guard.CodeAddressBlocked {
		detail = "Requests from this address are refused for a while after too many refused requests. Try again in " +
			strconv.FormatInt(int64(wait/time.Second), 10) + " seconds."
	}
	h.problem(w, r, errs.E(errs.RateLimited, code, detail, opts...))
	return false
}

// oneWait rounds a wait up to whole seconds, at least one, as the engine's refusals do.
func oneWait(d time.Duration) time.Duration {
	return max((d+time.Second-1)/time.Second, 1) * time.Second
}

// addressKey is the key the guard counts the request's client under: the address the chain resolved (R-ARCH-006), else the peer.
func addressKey(r *http.Request) string {
	client := httpserver.InfoFrom(r.Context()).Client
	if !client.IsValid() {
		if ap, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
			client = ap.Addr()
		} else if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			client, _ = netip.ParseAddr(host)
		}
	}
	if !client.IsValid() {
		return "invalid"
	}
	return guard.AddressKey(client)
}

// problem writes one of the web interface's own problems: problem details of type about:blank (Q-BAS-321), never cached.
func (h *Handler) problem(w http.ResponseWriter, r *http.Request, err *errs.Error) {
	w.Header().Set("Cache-Control", "no-store")
	httpserver.Problem(w, r, err)
}

func errNotFound() *errs.Error { return errs.E(errs.NotFound, errs.CodeNotFound, textNotFound) }

func errNotReady() *errs.Error {
	return errs.E(errs.Unavailable, errs.CodeNotReady, textNotReady, errs.WithRetryAfter(notReadyRetry))
}

// statusError is the problem of a status the application's file handler answers itself.
func statusError(status int) *errs.Error {
	switch status {
	case http.StatusNotFound:
		return errNotFound()
	case http.StatusMethodNotAllowed:
		return errs.E(errs.MethodNotAllowed, errs.CodeMethodNotAllowed, textMethod, errs.WithAllow(http.MethodGet, http.MethodHead))
	case http.StatusServiceUnavailable:
		return errs.E(errs.Unavailable, errs.CodeUnavailable, "This service is not available right now; try again later.",
			errs.WithRetryAfter(time.Second))
	}
	return errs.E(errs.Internal, errs.CodeInternal, textUnhandled)
}
