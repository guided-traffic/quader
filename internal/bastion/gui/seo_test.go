package gui

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/catalogue"
	"github.com/guided-traffic/quader/internal/bastion/settings"
)

// What a crawler reads (W-35's B.11): the pages' heads and texts, robots.txt, the sitemap and the page of a refusal, against the
// catalogue in memory of gui_test.go and a build whose index.html holds the two slots. Every case runs in microseconds; the
// sitemap's split writes 50,002 addresses of tiny entries in about a tenth of a second.

const base = "https://bastion.example.com"

// sentinel stands in a format's metadata of a version; no page may carry it (W-35's B.11: no format knowledge).
const sentinel = "SENTINEL-OF-A-FORMATS-METADATA"

// slotted is a build whose index.html holds the slots of web/src/bastion/index.html.
func slotted() fstest.MapFS {
	m := fstest.MapFS{}
	for _, l := range []string{"en", "de"} {
		m[l+"/index.html"] = &fstest.MapFile{Data: []byte(`<!doctype html><html lang="` + l + `"><head><meta charset="utf-8"><!--QUADER_HEAD-->` +
			`<base href="/ui/` + l + `/"></head><body><quader-root ngCspNonce="QUADER_CSP_NONCE"><div class="quader-boot"></div>` +
			`<!--QUADER_PAGE--></quader-root></body></html>`)}
		m[l+"/main-ABCD1234.js"] = &fstest.MapFile{Data: []byte("console.log(1)")}
	}
	return m
}

// seoReader is a catalogue of one npm package in the project acme, two versions, a README and a documentation tree.
func seoReader() *reader {
	r := newReader()
	r.pkgs[0].Description = `A UI kit </script><script>alert(1)</script>`
	r.pkgs[0].LatestVersion = "1.1.0"
	r.pkgs[0].Keywords = []string{"ui", "kit"}
	r.pkgs[0].UpdatedAt = time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	r.pkgs[1].LatestVersion = "2.0.0"
	r.pkgs[1].UpdatedAt = time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)
	v := func(version string) []byte {
		return []byte(`{"version":"` + version + `","publishedAt":"2026-11-20T08:00:00Z","licenses":["MIT"],"keywords":["ui"],` +
			`"metadata":{"description":"` + sentinel + `","homepage":"https://` + sentinel + `.example"}}`)
	}
	r.docs["packages/"+pkgID+"/versions/1.1.0"] = v("1.1.0")
	r.docs["packages/"+pkgID+"/versions/1.0.0"] = v("1.0.0")
	r.docs["packages/"+pkgID+"/versions/1.1.0/readme"] = []byte(`{"contentType":"text/markdown","html":"<h1>UI kit</h1><p>Buttons.</p>"}`)
	r.docs["packages/"+pkgID+"/versions/1.1.0/docs"] = []byte(`{"state":"available","repository":"acme/npm","groups":[{"category":"module",` +
		`"label":"Modules","count":1,"types":[{"type":"module","count":1,"entries":[{"type":"module","name":"button","shortDescription":"Draws a button","hasDocs":true}]}]}]}`)
	return r
}

func seoGUI(t *testing.T, r *reader, mod func(*Options)) *Handler {
	t.Helper()
	return newGUI(t, r, func(o *Options) {
		o.Assets = slotted()
		if mod != nil {
			mod(o)
		}
	})
}

// ldOf reads the JSON-LD of a page.
func ldOf(t *testing.T, body string) map[string]any {
	t.Helper()
	m := regexp.MustCompile(`<script type="application/ld\+json">(.*?)</script>`).FindStringSubmatch(body)
	if m == nil {
		return nil
	}
	var ld map[string]any
	if err := json.Unmarshal([]byte(m[1]), &ld); err != nil {
		t.Fatalf("JSON-LD %q: %v", m[1], err)
	}
	return ld
}

// TestR_BAS_121_APagesHeadAndTextComeFromTheCatalogue: a package's, a version's, the versions', a list's, the start's, a legal and a
// documentation entry's page answer with their title, description, canonical address and languages on the Bastion's base URL, JSON-LD
// and their text from the catalogue; an address that opens the findings and a list of results are noindex and canonical without their
// query; an address the catalogue does not show is 404 and noindex; nothing of a format's metadata reaches a page, and a description
// cannot close the script element (W-35's B.11).
func TestR_BAS_121_APagesHeadAndTextComeFromTheCatalogue(t *testing.T) {
	r := seoReader()
	h := seoGUI(t, r, nil)
	pkg := "/projects/acme/packages/npm/@acme/ui"

	rec := do(h, http.MethodGet, "/ui/de"+pkg)
	body := rec.Body.String()
	for _, want := range []string{
		`<title>@acme/ui – Quader</title>`,
		`<meta name="description" content="A UI kit &lt;/script&gt;&lt;script&gt;alert(1)&lt;/script&gt;">`,
		`<link rel="canonical" href="` + base + `/ui/de` + pkg + `">`,
		`<link rel="alternate" hreflang="en" href="` + base + `/ui/en` + pkg + `">`,
		`<link rel="alternate" hreflang="de" href="` + base + `/ui/de` + pkg + `">`,
		`<link rel="alternate" hreflang="x-default" href="` + base + `/ui` + pkg + `">`,
		`<div class="quader-page" lang="de">`, `<h1>@acme/ui</h1>`, `<p>A UI kit &lt;/script&gt;`,
		`<dt>Neueste Version</dt><dd><a href="/ui/de` + pkg + `@1.1.0">1.1.0</a></dd>`, `<dt>Lizenzen</dt><dd>MIT</dd>`,
		`<article><h1>UI kit</h1><p>Buttons.</p></article>`, `<h2>Versionen</h2>`, `<a href="/ui/de` + pkg + `@1.0.0">1.0.0</a>`,
		`<footer><ul><li><a href="/ui/de/legal/imprint">Impressum</a></li></ul></footer>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the package's page lacks %q:\n%s", want, body)
		}
	}
	if rec.Code != 200 || strings.Contains(body, "noindex") || strings.Contains(body, "<!--QUADER_") || strings.Contains(body, sentinel) {
		t.Errorf("the package's page = %d %s", rec.Code, body)
	}
	if strings.Count(body, "</script>") != 1 || !strings.Contains(body, `\u003c/script\u003e`) {
		t.Errorf("a description closes the JSON-LD's script element:\n%s", body)
	}
	// The text is for a reader without script: a browser that runs the application shows its spinner (R-UI-217).
	nonce := regexp.MustCompile(`'nonce-([A-Za-z0-9]+)'`).FindStringSubmatch(rec.Header().Get("Content-Security-Policy"))
	if nonce == nil || !strings.Contains(body, `<style nonce="`+nonce[1]+`">.quader-page{display:none}</style>`) ||
		!strings.Contains(body, `<noscript><style nonce="`+nonce[1]+`">.quader-boot{display:none}`) {
		t.Errorf("the text is not hidden from the application with the page's nonce:\n%s", body)
	}
	ld := ldOf(t, body)
	if ld["@type"] != "SoftwareApplication" || ld["name"] != "@acme/ui" || ld["softwareVersion"] != "1.1.0" || ld["license"] != "MIT" ||
		ld["keywords"] != "ui" || ld["datePublished"] != "2026-11-20T08:00:00Z" || ld["url"] != base+"/ui/de"+pkg {
		t.Errorf("the package's JSON-LD = %v", ld)
	}

	cases := []struct {
		target, title, canonical, ldType string
		noindex                          bool
	}{
		{"/ui/en" + pkg + "@1.0.0", "@acme/ui 1.0.0 – Quader", base + "/ui/en" + pkg + "@1.0.0", "SoftwareApplication", false},
		{"/ui/en" + pkg + "@1.0.0?tab=vulnerabilities", "@acme/ui 1.0.0 – Quader", base + "/ui/en" + pkg + "@1.0.0", "SoftwareApplication", true},
		{"/ui/en" + pkg + "?severity=critical", "@acme/ui – Quader", base + "/ui/en" + pkg, "SoftwareApplication", true},
		{"/ui/en" + pkg + "?tab=files", "@acme/ui – Quader", base + "/ui/en" + pkg, "SoftwareApplication", false},
		{"/ui/en" + pkg + "/-/versions", "Versions of @acme/ui – Quader", base + "/ui/en" + pkg + "/-/versions", "WebPage", false},
		{
			"/ui/en" + pkg + "?tab=documentation&doc=module/button", "button – @acme/ui 1.1.0 – Quader",
			base + "/ui/en" + pkg + "?doc=module%2Fbutton&tab=documentation", "TechArticle", false,
		},
		{"/ui/en" + pkg + "?tab=documentation&doc=module/none", "@acme/ui – Quader", base + "/ui/en" + pkg, "SoftwareApplication", false},
		{"/ui/de/projects/acme/packages", "Pakete in acme – Quader", base + "/ui/de/projects/acme/packages", "WebPage", false},
		{"/ui/de/projects/acme", "Pakete in acme – Quader", base + "/ui/de/projects/acme/packages", "WebPage", false},
		{"/ui/en/projects/acme/packages?q=ui", "Packages in acme – Quader", base + "/ui/en/projects/acme/packages", "WebPage", true},
		{"/ui/en/", "Projects – Quader", base + "/ui/en/", "WebPage", false},
		{"/ui/en/legal/imprint", "Imprint – Quader", base + "/ui/en/legal/imprint", "WebPage", false},
	}
	for _, c := range cases {
		rec := do(h, http.MethodGet, c.target)
		body := rec.Body.String()
		if rec.Code != 200 || !strings.Contains(body, "<title>"+c.title+"</title>") ||
			!strings.Contains(body, `<link rel="canonical" href="`+strings.ReplaceAll(c.canonical, "&", "&amp;")+`">`) ||
			strings.Contains(body, `content="noindex"`) != c.noindex || ldOf(t, body)["@type"] != c.ldType || strings.Contains(body, sentinel) {
			t.Errorf("%s = %d, want %q, %q, %s, noindex %t:\n%s", c.target, rec.Code, c.title, c.canonical, c.ldType, c.noindex, body)
		}
	}
	if body := do(h, http.MethodGet, "/ui/en/").Body.String(); !strings.Contains(body, `<a href="/ui/en/projects/acme/packages">acme</a>`) ||
		!strings.Contains(body, `<a href="/ui/en/projects/web/packages">web</a>`) || strings.Contains(body, ">upstream<") {
		t.Errorf("the start's text:\n%s", body)
	}
	if body := do(h, http.MethodGet, "/ui/de/legal/imprint").Body.String(); !strings.Contains(body, "Beispiel GmbH") {
		t.Errorf("the imprint's text:\n%s", body)
	}

	// What the catalogue does not show is not found: the application says so, and a crawler drops the address (D15).
	for _, target := range []string{
		"/ui/en/nothing", "/ui/en/index.html", "/ui/en/projects/web/packages/npm/@acme/ui", "/ui/en/projects/acme/packages/npm/@acme/gone",
		"/ui/en" + pkg + "@9.9.9", "/ui/en/projects/upstream/packages", "/ui/en/projects/nobody/packages", "/ui/en/legal/privacy",
		"/ui/en/projects/acme/packages/npm/@acme/ui@1.0.0/-/versions",
	} {
		rec := do(h, http.MethodGet, target)
		body := rec.Body.String()
		if rec.Code != 404 || !strings.Contains(body, `<meta name="robots" content="noindex">`) || strings.Contains(body, "canonical") ||
			!strings.Contains(body, "<title>Page not found – Quader</title>") || !strings.Contains(body, "<h1>Page not found</h1>") {
			t.Errorf("%s = %d:\n%s", target, rec.Code, body)
		}
	}

	// Without the documentation (docs off, a wiki that is not public) the catalogue does not show the README, and the page has none.
	delete(r.docs, "packages/"+pkgID+"/versions/1.1.0/readme")
	if body := do(h, http.MethodGet, "/ui/en"+pkg).Body.String(); strings.Contains(body, "<article>") {
		t.Errorf("a README the catalogue does not show:\n%s", body)
	}

	// While the replica holds no state a page is 503, noindex, without a text.
	r.state = catalogue.State{Enabled: true, Decided: true}
	rec = do(h, http.MethodGet, "/ui/en"+pkg)
	if body := rec.Body.String(); rec.Code != 503 || rec.Header().Get("Retry-After") != "30" || !strings.Contains(body, `content="noindex"`) ||
		!strings.Contains(body, "<title>Quader</title>") || strings.Contains(body, "quader-page\"") {
		t.Errorf("not ready = %d:\n%s", rec.Code, body)
	}
}

// TestR_BAS_122_RobotsAndTheSitemapAtTheHostsRoot: robots.txt keeps a crawler off the package API's families and the findings'
// documents and names the sitemap, or keeps it off the host while the web interface is off; the sitemap lists the start, the
// projects, every package in every project that shows it and the legal pages in both languages with their alternates, splits at
// 50,000 addresses, is kept per revision of the catalogue, and is gzip to a crawler that takes it; the data are noindex.
func TestR_BAS_122_RobotsAndTheSitemapAtTheHostsRoot(t *testing.T) {
	r := seoReader()
	r.revision = "rev-1"
	h := seoGUI(t, r, nil)
	if !Owns("/robots.txt") || !Owns("/sitemap.xml") || !Owns("/sitemap/0.xml") || Owns("/robots.txt.bak") || Owns("/sitemaps") {
		t.Error("Owns claims the wrong paths at the root")
	}

	rec := do(h, http.MethodGet, "/robots.txt")
	if body := rec.Body.String(); rec.Code != 200 || rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" ||
		!strings.Contains(body, "Allow: /ui/\nDisallow: /ui/data/*vulnerabilit\nDisallow: /api/\nDisallow: /files/\nDisallow: /v2/\n") ||
		!strings.Contains(body, "Sitemap: "+base+"/sitemap.xml\n") {
		t.Errorf("robots.txt = %d %s", rec.Code, body)
	}
	r.state = catalogue.State{Decided: true}
	if rec := do(h, http.MethodGet, "/robots.txt"); rec.Code != 200 || rec.Body.String() != "User-agent: *\nDisallow: /\n" {
		t.Errorf("robots.txt while off = %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodGet, "/sitemap.xml"); rec.Code != 404 {
		t.Errorf("the sitemap while off = %d", rec.Code)
	}
	r.state = catalogue.State{}
	if rec := do(h, http.MethodGet, "/robots.txt"); rec.Code != 503 || problemCode(t, rec) != "not_ready" {
		t.Errorf("robots.txt while undecided = %d %s", rec.Code, rec.Body.String())
	}
	r.state = catalogue.State{Enabled: true, Decided: true}
	if rec := do(h, http.MethodGet, "/sitemap.xml"); rec.Code != 503 || problemCode(t, rec) != "not_ready" {
		t.Errorf("the sitemap while not ready = %d", rec.Code)
	}
	r.state = onState()

	if rec := do(h, http.MethodGet, "/ui/data/v1/state"); rec.Header().Get("X-Robots-Tag") != "noindex" {
		t.Errorf("the data's X-Robots-Tag = %q", rec.Header().Get("X-Robots-Tag"))
	}

	var index struct {
		Sitemaps []struct {
			Loc string `xml:"loc"`
		} `xml:"sitemap"`
	}
	rec = do(h, http.MethodGet, "/sitemap.xml")
	if err := xml.Unmarshal(rec.Body.Bytes(), &index); err != nil || rec.Code != 200 || len(index.Sitemaps) != 1 ||
		index.Sitemaps[0].Loc != base+"/sitemap/0.xml" {
		t.Fatalf("the sitemap's index = %d %v %s", rec.Code, err, rec.Body.String())
	}
	type url struct {
		Loc     string `xml:"loc"`
		Lastmod string `xml:"lastmod"`
		Links   []struct {
			Hreflang string `xml:"hreflang,attr"`
			Href     string `xml:"href,attr"`
		} `xml:"link"`
	}
	var set struct {
		URLs []url `xml:"url"`
	}
	rec = do(h, http.MethodGet, "/sitemap/0.xml")
	if err := xml.Unmarshal(rec.Body.Bytes(), &set); err != nil || rec.Code != 200 {
		t.Fatalf("the sitemap = %d %v %s", rec.Code, err, rec.Body.String())
	}
	var locs []string
	for _, u := range set.URLs {
		locs = append(locs, u.Loc)
	}
	want := []string{
		base + "/ui/en/", base + "/ui/de/",
		base + "/ui/en/projects/acme/packages", base + "/ui/de/projects/acme/packages",
		base + "/ui/en/projects/web/packages", base + "/ui/de/projects/web/packages",
		base + "/ui/en/projects/acme/packages/npm/@acme/ui", base + "/ui/de/projects/acme/packages/npm/@acme/ui",
		base + "/ui/en/projects/web/packages/ansible/acme.web", base + "/ui/de/projects/web/packages/ansible/acme.web",
		base + "/ui/en/legal/imprint", base + "/ui/de/legal/imprint",
	}
	if strings.Join(locs, "\n") != strings.Join(want, "\n") {
		t.Errorf("the sitemap lists\n%s\nwant\n%s", strings.Join(locs, "\n"), strings.Join(want, "\n"))
	}
	if u := set.URLs[6]; u.Lastmod != "2026-11-20T08:00:00Z" || len(u.Links) != 3 || u.Links[0].Hreflang != "en" ||
		u.Links[1].Href != base+"/ui/de/projects/acme/packages/npm/@acme/ui" || u.Links[2].Hreflang != "x-default" ||
		u.Links[2].Href != base+"/ui/projects/acme/packages/npm/@acme/ui" {
		t.Errorf("a package's address = %+v", u)
	}
	if set.URLs[0].Lastmod != "2026-11-21T08:00:00Z" || set.URLs[10].Lastmod != "" {
		t.Errorf("the start's and the imprint's lastmod = %q %q", set.URLs[0].Lastmod, set.URLs[10].Lastmod)
	}
	for _, target := range []string{"/sitemap/1.xml", "/sitemap/00.xml", "/sitemap/-1.xml", "/sitemap/x.xml", "/sitemap/0"} {
		if rec := do(h, http.MethodGet, target); rec.Code != 404 {
			t.Errorf("%s = %d", target, rec.Code)
		}
	}

	// gzip to a crawler that takes it, the same bytes, and kept while the revision is the same.
	rec = do(h, http.MethodGet, "/sitemap/0.xml", "Accept-Encoding", "gzip")
	zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	unzipped, _ := io.ReadAll(zr)
	plain := do(h, http.MethodGet, "/sitemap/0.xml").Body.Bytes()
	if rec.Header().Get("Content-Encoding") != "gzip" || !bytes.Equal(unzipped, plain) {
		t.Errorf("gzip = %q, %d bytes against %d", rec.Header().Get("Content-Encoding"), len(unzipped), len(plain))
	}
	if again := do(h, http.MethodGet, "/sitemap/0.xml", "If-None-Match", rec.Header().Get("ETag"), "Accept-Encoding", "gzip"); again.Code != 304 {
		t.Errorf("If-None-Match = %d", again.Code)
	}

	// 25,001 tiny packages are 50,002 addresses, two files; the new revision replaces the set.
	r.mu.Lock()
	r.pkgs = make([]catalogue.Entry, 25001)
	for i := range r.pkgs {
		r.pkgs[i] = catalogue.Entry{ID: fmt.Sprint(i), Format: "npm", Name: fmt.Sprintf("p%05d", i), Repositories: []string{"acme/npm"}}
	}
	r.revision = "rev-2"
	r.mu.Unlock()
	rec = do(h, http.MethodGet, "/sitemap.xml")
	index.Sitemaps = nil
	if err := xml.Unmarshal(rec.Body.Bytes(), &index); err != nil || len(index.Sitemaps) != 2 || index.Sitemaps[1].Loc != base+"/sitemap/1.xml" {
		t.Fatalf("the split index = %v %s", err, rec.Body.String())
	}
	for n, count := range []int{50000, 2 + 2 + 50002 + 2 - 50000} {
		if got := bytes.Count(do(h, http.MethodGet, fmt.Sprintf("/sitemap/%d.xml", n)).Body.Bytes(), []byte("<url><loc>")); got != count {
			t.Errorf("file %d holds %d addresses, want %d", n, got, count)
		}
	}
}

// TestR_BAS_123_APageOverTheLimitIsAPageInTheBastionsShape: a page's address the class gui refuses answers 429 with Retry-After and
// the class's RateLimit fields, and a page of its own in the address's language (the visitor's for an address without one): the
// sentence, a refresh after the wait, noindex, styles under the page's nonce and nothing to load; HEAD carries no body; the data, the
// bundle's files and robots.txt keep problem details (P9b's open point 3).
func TestR_BAS_123_APageOverTheLimitIsAPageInTheBastionsShape(t *testing.T) {
	g := &fakeGuard{limited: true}
	h := seoGUI(t, seoReader(), func(o *Options) {
		o.Guard = g
		o.Flood = func(context.Context) settings.Rate { return settings.Rate{RequestsPerSecond: 20, Burst: 100} }
	})
	for _, c := range []struct{ target, lang, locale, text string }{
		{"/ui/de/projects/acme/packages/npm/@acme/ui", "", "de", "Von Ihrer Adresse kamen zu viele Anfragen. Versuchen Sie es in 2\u00a0Sekunden erneut."},
		{"/ui/en/", "de", "en", "Too many requests came from your address. Try again in 2\u00a0seconds."},
		{"/", "de-DE,de;q=0.9", "de", "Versuchen Sie es in 2\u00a0Sekunden erneut."},
		{"/ui/projects/acme", "", "en", "Try again in 2\u00a0seconds."},
	} {
		rec := do(h, http.MethodGet, c.target, "Accept-Language", c.lang)
		body := rec.Body.String()
		csp := rec.Header().Get("Content-Security-Policy")
		nonce := regexp.MustCompile(`'nonce-([A-Za-z0-9]+)'`).FindStringSubmatch(csp)
		if rec.Code != 429 || rec.Header().Get("Retry-After") != "2" || rec.Header().Get("RateLimit-Policy") != `"pages";q=100;w=5` ||
			rec.Header().Get("Content-Type") != "text/html; charset=utf-8" || rec.Header().Get("Cache-Control") != "no-store" ||
			nonce == nil || !strings.HasPrefix(csp, "default-src 'none'; script-src 'self';") || !strings.Contains(body, `<style nonce="`+nonce[1]+`">`) ||
			!strings.Contains(body, `<html lang="`+c.locale+`">`) || !strings.Contains(body, c.text) ||
			!strings.Contains(body, `<meta http-equiv="refresh" content="2">`) || !strings.Contains(body, `<meta name="robots" content="noindex">`) ||
			strings.Contains(body, "<script") || strings.Contains(body, "<link") {
			t.Errorf("%s = %d %v\n%s", c.target, rec.Code, rec.Header(), body)
		}
	}
	if rec := do(h, http.MethodHead, "/ui/en/"); rec.Code != 429 || rec.Body.Len() != 0 {
		t.Errorf("HEAD = %d %q", rec.Code, rec.Body.String())
	}
	for _, target := range []string{"/ui/data/v1/state", "/ui/en/main-ABCD1234.js", "/robots.txt", "/sitemap.xml"} {
		if rec := do(h, http.MethodGet, target); rec.Code != 429 || problemCode(t, rec) != "rate_limited" {
			t.Errorf("%s = %d %s", target, rec.Code, rec.Body.String())
		}
	}
	g.limited, g.blocked = false, true
	if body := do(h, http.MethodGet, "/ui/en/").Body.String(); !strings.Contains(body, "Requests from your address are refused for a while") ||
		!strings.Contains(body, "Try again in 90\u00a0seconds.") || !strings.Contains(body, `<a href="/ui/en/legal/imprint">Imprint</a>`) {
		t.Errorf("blocked:\n%s", body)
	}
}
