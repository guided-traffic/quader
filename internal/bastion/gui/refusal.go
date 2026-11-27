package gui

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	pg "github.com/guided-traffic/quader/internal/bastion/gui/page"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/webui"
)

// pageLocale reports whether a request asks for a page, which a person reads in a browser, and in which language (R-BAS-123): `/`,
// `/ui`, an address the bundle's handler redirects to a locale, and an address below a locale that the handler answers with the
// application's index.html. The data, the bundle's files, robots.txt and the sitemap are no page. The language is the address's,
// else the visitor's (Accept-Language), else English.
func (h *Handler) pageLocale(r *http.Request) (string, bool) {
	path := r.URL.Path
	lang := func() string {
		if l := webui.Negotiate(r.Header.Get("Accept-Language"), pg.Locales()); l != "" {
			return l
		}
		return defaultLocale
	}
	if path == "/" || path == "/ui" || path == webui.MountPath {
		return lang(), true
	}
	rest, ok := strings.CutPrefix(path, webui.MountPath)
	if !ok || rest == "data" || strings.HasPrefix(rest, "data/") {
		return "", false
	}
	first, tail, hasTail := strings.Cut(rest, "/")
	for _, l := range pg.Locales() {
		if first == l {
			return l, !hasTail || webui.IsDocument(tail)
		}
	}
	// No locale: the handler redirects it to the visitor's.
	return lang(), true
}

// unavailable answers every request of the web interface while the replica applies a disable of its registration (R-BAS-133,
// Q-BAS-514): a page's address the page "Not available" in the Bastion's shape, which loads itself again after the wait, anything
// else (the data, robots.txt, the sitemap, the bundle's files) the problem of the Bastion's own 503 unavailable; both with
// Retry-After, so a search engine keeps its index.
func (h *Handler) unavailable(w http.ResponseWriter, r *http.Request) {
	secs := int64(disabledRetry / time.Second)
	locale, page := h.pageLocale(r)
	if !page {
		h.problem(w, r, errs.E(errs.Unavailable, errs.CodeUnavailable, errs.DetailUnavailable, errs.WithRetryAfter(disabledRetry)))
		return
	}
	nonce := rand.Text()
	body := h.pages.RefusalPage(pg.Refusal{Locale: locale, Seconds: secs, Unavailable: true, Legal: h.legalLinks(locale)}, nonce)
	hd := w.Header()
	hd.Set("Retry-After", strconv.FormatInt(secs, 10))
	hd.Set("Content-Type", "text/html; charset=utf-8")
	hd.Set("Content-Length", strconv.Itoa(len(body)))
	hd.Set("Cache-Control", "no-store")
	hd.Set("Content-Security-Policy", fmt.Sprintf(CSP, nonce))
	w.WriteHeader(http.StatusServiceUnavailable)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

// disabledRetry is the Retry-After of a disabled replica's answers (R-BAS-133), the engine's.
const disabledRetry = 300 * time.Second

// refusalPage answers a page's address the class gui refused (R-BAS-123): 429 with Retry-After and the class's RateLimit fields, as
// the problem of the data does, and the page of page.RefusalPage with a policy of its own nonce.
func (h *Handler) refusalPage(w http.ResponseWriter, r *http.Request, locale string, wait time.Duration, blocked bool, fields [][2]string) {
	nonce := rand.Text()
	body := h.pages.RefusalPage(pg.Refusal{
		Locale: locale, Seconds: int64(wait / time.Second), Blocked: blocked, Legal: h.legalLinks(locale),
	}, nonce)
	hd := w.Header()
	for _, f := range fields {
		hd.Set(f[0], f[1])
	}
	hd.Set("Retry-After", strconv.FormatInt(int64(wait/time.Second), 10))
	hd.Set("Content-Type", "text/html; charset=utf-8")
	hd.Set("Content-Length", strconv.Itoa(len(body)))
	hd.Set("Cache-Control", "no-store")
	hd.Set("Content-Security-Policy", fmt.Sprintf(CSP, nonce))
	w.WriteHeader(http.StatusTooManyRequests)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
