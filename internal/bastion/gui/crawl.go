package gui

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/catalogue"
	pg "github.com/guided-traffic/quader/internal/bastion/gui/page"
)

// robots.txt and the sitemap at the host's root (R-BAS-122, W-35's B.11): what a crawler may fetch and which pages there are, from the
// Bastion's own base URL and the catalogue's index alone.

const (
	pathRobots  = "/robots.txt"
	pathSitemap = "/sitemap.xml"
	sitemapDir  = "/sitemap/"
	// maxSitemapURLs is the sitemaps protocol's bound of the addresses of one file.
	maxSitemapURLs = 50000
)

// robotsOn keeps a crawler on the pages and off everything that costs the main installation or names findings: the package API's
// families (a crawler that heeds the file costs the main installation nothing), and the documents of the open findings, which a
// rendering crawler would otherwise fetch (W-35's B.11). The data of the pages stay allowed, so that a rendering crawler can render
// them; they answer X-Robots-Tag: noindex.
const robotsOn = "User-agent: *\n" +
	"Allow: /ui/\n" +
	"Disallow: /ui/data/*vulnerabilit\n" +
	"Disallow: /api/\n" +
	"Disallow: /files/\n" +
	"Disallow: /v2/\n"

// robotsOff keeps every crawler away from a host whose web interface is off.
const robotsOff = "User-agent: *\nDisallow: /\n"

// robots answers /robots.txt: the rules of a web interface that is on, with the sitemap's address, or the host disallowed while it is
// off; while the replica knows neither, not ready, which a crawler takes as "try again later" rather than as a rule.
func (h *Handler) robots(w http.ResponseWriter, r *http.Request, st catalogue.State) {
	var body string
	switch {
	case st.Decided && !st.Enabled:
		body = robotsOff
	case !st.Decided:
		h.problem(w, r, errNotReady())
		return
	default:
		body = robotsOn + "\nSitemap: " + strings.TrimSuffix(h.o.BaseURL, "/") + pathSitemap + "\n"
	}
	sum := sha256.Sum256([]byte(body))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	h.write(w, r, `"`+hex.EncodeToString(sum[:])+`"`, []byte(body))
}

// sitemapURL is one address of the sitemap: a page in one language, its other languages' addresses and when it last changed.
type sitemapURL struct {
	route   string
	locale  string
	lastmod time.Time
}

// sitemapSet is the sitemap of one revision of the catalogue's packages: its files, compressed, built on first request.
type sitemapSet struct {
	revision string
	urls     []sitemapURL
	mu       sync.Mutex
	files    map[int][]byte
}

func (s *sitemapSet) count() int { return max(1, (len(s.urls)+maxSitemapURLs-1)/maxSitemapURLs) }

// sitemapState holds the one set the handler keeps: a new revision of the catalogue replaces it.
type sitemapState struct {
	mu  sync.Mutex
	cur *sitemapSet
}

// sitemaps is the set of the catalogue's current revision, built from the index when the revision changed.
func (h *Handler) sitemaps(ctx context.Context) (*sitemapSet, error) {
	entries, rev, err := h.o.Catalogue.Packages()
	if err != nil {
		return nil, err
	}
	h.sm.mu.Lock()
	defer h.sm.mu.Unlock()
	if h.sm.cur != nil && h.sm.cur.revision == rev {
		return h.sm.cur, nil
	}
	set := &sitemapSet{revision: rev, urls: h.sitemapURLs(ctx, entries), files: map[int][]byte{}}
	h.sm.cur = set
	return set, nil
}

// sitemapURLs are the pages the sitemap lists (W-35's B.11), each in every language: the start, every project's list, every package's
// page in every project that shows it, its documentation's pages (docRoutes), and the imprint and the privacy notice that have a text.
// A version's own page is not listed: a package's page links its versions, and each is canonical to itself.
func (h *Handler) sitemapURLs(ctx context.Context, entries []catalogue.Entry) []sitemapURL {
	type page struct {
		route   string
		lastmod time.Time
	}
	var pages []page
	projects := map[string]time.Time{}
	var newest time.Time
	var packages []page
	for _, e := range entries {
		seen := map[string]bool{}
		for _, repo := range e.Repositories {
			project, _, _ := strings.Cut(repo, "/")
			if seen[project] {
				continue
			}
			seen[project] = true
			if e.UpdatedAt.After(projects[project]) || projects[project].IsZero() {
				projects[project] = e.UpdatedAt
			}
			packages = append(packages, page{pg.PackageRoute(project, e.Format, e.Name), e.UpdatedAt})
			for _, route := range h.docRoutes(ctx, e, project) {
				packages = append(packages, page{route, e.UpdatedAt})
			}
		}
		if e.UpdatedAt.After(newest) {
			newest = e.UpdatedAt
		}
	}
	if len(projects) > 0 {
		pages = append(pages, page{"", newest})
	}
	names := make([]string, 0, len(projects))
	for p := range projects {
		names = append(names, p)
	}
	slices.Sort(names)
	for _, p := range names {
		pages = append(pages, page{pg.Address{Kind: pg.List, Project: p}.Route(), projects[p]})
	}
	pages = append(pages, packages...)
	set := h.o.Legal.Pages(defaultLocale)
	if set.Imprint != nil {
		pages = append(pages, page{"legal/imprint", time.Time{}})
	}
	if set.Privacy != nil {
		pages = append(pages, page{"legal/privacy", time.Time{}})
	}
	out := make([]sitemapURL, 0, len(pages)*len(pg.Locales()))
	for _, p := range pages {
		for _, l := range pg.Locales() {
			out = append(out, sitemapURL{route: p.route, locale: l, lastmod: p.lastmod})
		}
	}
	return out
}

// sitemap answers /sitemap.xml, the index of the files, and /sitemap/<n>.xml, a file of at most 50,000 addresses.
func (h *Handler) sitemap(w http.ResponseWriter, r *http.Request, ready bool) {
	if !ready {
		h.problem(w, r, errNotReady())
		return
	}
	n := -1
	if r.URL.Path != pathSitemap {
		name, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, sitemapDir), ".xml")
		v, err := strconv.Atoi(name)
		if !ok || err != nil || v < 0 || strconv.Itoa(v) != name {
			h.problem(w, r, errNotFound())
			return
		}
		n = v
	}
	set, err := h.sitemaps(r.Context())
	if err != nil {
		h.readerError(w, r, err)
		return
	}
	if n >= set.count() {
		h.problem(w, r, errNotFound())
		return
	}
	gz, tag := set.file(h, n)
	hd := w.Header()
	hd.Set("Content-Type", "application/xml; charset=utf-8")
	hd.Set("Cache-Control", "no-cache")
	hd.Add("Vary", "Accept-Encoding")
	if acceptsGzip(r) {
		hd.Set("Content-Encoding", "gzip")
		h.write(w, r, `"`+tag+`-gzip"`, gz)
		return
	}
	raw, err := gunzip(gz)
	if err != nil {
		h.readerError(w, r, err)
		return
	}
	h.write(w, r, `"`+tag+`"`, raw)
}

// file is the compressed bytes of file n, -1 the index, and their tag: written once per revision.
func (s *sitemapSet) file(h *Handler, n int) ([]byte, string) {
	tag := s.revision[:min(len(s.revision), 32)] + "-" + strconv.Itoa(n)
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.files[n]; ok {
		return b, tag
	}
	var raw bytes.Buffer
	if n < 0 {
		h.writeIndex(&raw, s.count())
	} else {
		h.writeURLs(&raw, s.urls[n*maxSitemapURLs:min(len(s.urls), (n+1)*maxSitemapURLs)])
	}
	var gz bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&gz, gzip.BestSpeed)
	_, _ = zw.Write(raw.Bytes())
	_ = zw.Close()
	s.files[n] = gz.Bytes()
	return s.files[n], tag
}

func (h *Handler) writeIndex(w io.Writer, count int) {
	base := strings.TrimSuffix(h.o.BaseURL, "/")
	_, _ = io.WriteString(w, xml.Header+`<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`+"\n")
	for i := range count {
		_, _ = io.WriteString(w, "<sitemap><loc>"+escapeXML(base+sitemapDir+strconv.Itoa(i)+".xml")+"</loc></sitemap>\n")
	}
	_, _ = io.WriteString(w, "</sitemapindex>\n")
}

func (h *Handler) writeURLs(w io.Writer, urls []sitemapURL) {
	_, _ = io.WriteString(w, xml.Header+`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" `+
		`xmlns:xhtml="http://www.w3.org/1999/xhtml">`+"\n")
	for _, u := range urls {
		_, _ = io.WriteString(w, "<url><loc>"+escapeXML(pg.URL(h.o.BaseURL, u.locale, u.route, nil))+"</loc>")
		if !u.lastmod.IsZero() {
			_, _ = io.WriteString(w, "<lastmod>"+u.lastmod.UTC().Format(time.RFC3339)+"</lastmod>")
		}
		for _, l := range pg.Locales() {
			_, _ = io.WriteString(w, `<xhtml:link rel="alternate" hreflang="`+l+`" href="`+escapeXML(pg.URL(h.o.BaseURL, l, u.route, nil))+`"/>`)
		}
		_, _ = io.WriteString(w, `<xhtml:link rel="alternate" hreflang="x-default" href="`+escapeXML(pg.URL(h.o.BaseURL, "", u.route, nil))+`"/>`)
		_, _ = io.WriteString(w, "</url>\n")
	}
	_, _ = io.WriteString(w, "</urlset>\n")
}

func escapeXML(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func gunzip(gz []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(zr)
}
