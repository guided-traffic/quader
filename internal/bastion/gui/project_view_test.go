package gui

import (
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/catalogue"
)

// What a crawler reads of the project view (R-BAS-132), against the catalogue in memory of gui_test.go with an image in the project
// acme's image repository: a project's repositories, a repository's page, the way to each page, and all of it while the repositories
// are hidden (R-BAS-131). Microseconds.

// lacks is the wants a body does not hold.
func lacks(body string, wants ...string) []string {
	var out []string
	for _, w := range wants {
		if !strings.Contains(body, w) {
			out = append(out, w)
		}
	}
	return out
}

func projectViewReader() *reader {
	r := seoReader()
	r.pkgs = append(r.pkgs, catalogue.Entry{ID: "img", Format: "oci", Name: "tools/agent", Description: "The agent", Repositories: []string{"acme/images"}})
	return r
}

// TestR_BAS_132_ACrawlerReadsTheProjectView: a project's repositories and a repository's page have their title, canonical address,
// heading, description and links in both languages; an image repository lists its images, a package repository its packages, each
// linked to its page in the project; every page's way leads Projects › project › Repositories or › Packages, a file's through the
// repository's key, never `<project>/<key>`.
func TestR_BAS_132_ACrawlerReadsTheProjectView(t *testing.T) {
	h := seoGUI(t, projectViewReader(), nil)
	trail := `<a href="/ui/en/">Projects</a></li><li><a href="/ui/en/projects/acme/repositories">Acme platform</a>`
	cases := []struct {
		target string
		wants  []string
	}{
		{"/ui/en/projects/acme/repositories", []string{
			`<title>Repositories in Acme platform – Quader Bastion</title>`, `<link rel="canonical" href="` + base + `/ui/en/projects/acme/repositories">`,
			`<h1>Acme platform</h1>`, `<p>The packages Acme publishes.</p>`, `<h2>Repositories</h2>`,
			`<a href="/ui/en/projects/acme/repositories/images">images</a> – Base images`,
			`<a href="/ui/en/projects/acme/repositories/npm">npm</a> – Our npm packages`, trail,
		}},
		{"/ui/de/projects/acme/repositories", []string{`<title>Repositories in Acme platform – Quader Bastion</title>`, `<h2>Repositories</h2>`}},
		{"/ui/en/projects/acme/repositories/images", []string{
			`<title>images – Quader Bastion</title>`, `<link rel="canonical" href="` + base + `/ui/en/projects/acme/repositories/images">`,
			`<h1>images</h1>`, `<p>Base images</p>`, `<h2>Images</h2>`,
			`<a href="/ui/en/projects/acme/packages/oci/tools/agent">tools/agent</a> – The agent`,
			trail + `</li><li><a href="/ui/en/projects/acme/repositories">Repositories</a>`,
		}},
		{"/ui/de/projects/acme/repositories/npm", []string{
			`<title>npm – Quader Bastion</title>`, `<h2>Pakete</h2>`, `<a href="/ui/de/projects/acme/packages/npm/@acme/ui">@acme/ui</a>`,
			`<a href="/ui/de/projects/acme/repositories">Repositories</a>`,
		}},
		{"/ui/en/projects/acme/packages/npm/@acme/ui", []string{trail + `</li><li><a href="/ui/en/projects/acme/packages">Packages</a>`}},
		{"/ui/en/projects/acme/repositories/npm/setup", []string{
			trail + `</li><li><a href="/ui/en/projects/acme/repositories">Repositories</a></li><li><a href="/ui/en/projects/acme/repositories/npm">npm</a>`,
		}},
		{"/ui/en/projects/tools/repositories/downloads/files/tools", []string{
			`<a href="/ui/en/projects/tools/repositories">Repositories</a></li><li><a href="/ui/en/projects/tools/repositories/downloads">downloads</a>`,
		}},
	}
	for _, c := range cases {
		rec := do(h, http.MethodGet, c.target)
		body := rec.Body.String()
		// A page Set up client is never indexed (Q-BAS-401).
		noindex := strings.HasSuffix(c.target, "/setup")
		if missing := lacks(body, c.wants...); rec.Code != 200 || len(missing) > 0 || strings.Contains(body, "noindex") != noindex {
			t.Errorf("%s = %d lacks %q:\n%s", c.target, rec.Code, missing, body)
		}
	}
	// A repository without a package has its page without a list.
	if body := do(h, http.MethodGet, "/ui/en/projects/upstream/repositories/npmjs").Body.String(); strings.Contains(body, "<h2>") ||
		!strings.Contains(body, "<h1>npmjs</h1>") {
		t.Errorf("a repository without a package:\n%s", body)
	}
}

// TestR_BAS_132_HiddenRepositoriesHaveNoPage: while the repositories are hidden a project's address is its packages, a project's
// repositories and a repository's page that is no root of files are 404 and noindex; the root of a generic repository's files stays,
// without its description, and every way leads Projects › project to the packages, with no step Repositories; the start links the
// projects' packages and has no section of files.
func TestR_BAS_132_HiddenRepositoriesHaveNoPage(t *testing.T) {
	r := projectViewReader()
	r.state.HideRepositories = true
	h := seoGUI(t, r, nil)
	for _, target := range []string{"/ui/en/projects/acme/repositories", "/ui/en/projects/acme/repositories/npm", "/ui/en/projects/acme/repositories/images/files"} {
		rec := do(h, http.MethodGet, target)
		if rec.Code != 404 || !strings.Contains(rec.Body.String(), `<meta name="robots" content="noindex">`) {
			t.Errorf("hidden, %s = %d", target, rec.Code)
		}
	}
	trail := `<a href="/ui/en/">Projects</a></li><li><a href="/ui/en/projects/acme/packages">Acme platform</a>`
	cases := []struct {
		target string
		wants  []string
	}{
		{"/ui/en/projects/acme", []string{
			`<title>Packages in Acme platform – Quader Bastion</title>`, `<link rel="canonical" href="` + base + `/ui/en/projects/acme/packages">`,
		}},
		{"/ui/en/projects/acme/packages/npm/@acme/ui", []string{trail + `</li><li><a href="/ui/en/projects/acme/packages">Packages</a>`}},
		{"/ui/en/projects/acme/repositories/npm/setup", []string{trail + `</li></ol>`, "<h1>Set up client</h1></main>"}},
		{"/ui/en/projects/tools/repositories/downloads", []string{`<h1>downloads</h1>`, "<p>The files published in this repository.</p>"}},
		{"/ui/en/projects/tools/repositories/downloads/files/tools", []string{
			`<a href="/ui/en/projects/tools/packages">tools</a></li><li><a href="/ui/en/projects/tools/repositories/downloads">downloads</a>`,
		}},
		{"/ui/en/", []string{`<a href="/ui/en/projects/acme/packages">Acme platform</a>`}},
	}
	for _, c := range cases {
		rec := do(h, http.MethodGet, c.target)
		body := rec.Body.String()
		if missing := lacks(body, c.wants...); rec.Code != 200 || len(missing) > 0 || strings.Contains(body, ">Repositories<") ||
			strings.Contains(body, "Agents to download") {
			t.Errorf("hidden, %s = %d lacks %q:\n%s", c.target, rec.Code, missing, body)
		}
	}
	if body := do(h, http.MethodGet, "/ui/en/").Body.String(); strings.Contains(body, "<h2>Files</h2>") {
		t.Errorf("hidden, the start lists the repositories with files:\n%s", body)
	}
}
