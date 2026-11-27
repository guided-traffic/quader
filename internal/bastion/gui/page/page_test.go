package page

import (
	"net/url"
	"strings"
	"testing"
)

// TestR_BAS_121_TheAddressGrammarIsTheApplicationsRoutes: Parse reads an address as web/src/bastion/app.routes.ts and the package
// matchers do (an npm scope's slash, the version after the last "@", `-/versions`, the legal pages), and Route writes it back in one
// spelling. Microseconds.
func TestR_BAS_121_TheAddressGrammarIsTheApplicationsRoutes(t *testing.T) {
	cases := []struct {
		tail, query string
		want        Address
		route       string
	}{
		{"", "", Address{Kind: Start}, ""},
		{"projects/web", "", Address{Kind: Project, Project: "web"}, "projects/web"},
		{"projects/web/repositories/", "q=npm", Address{Kind: Repositories, Project: "web"}, "projects/web/repositories"},
		{"projects/web/packages/", "q=ui", Address{Kind: List, Project: "web", Filtered: true}, "projects/web/packages"},
		{
			"projects/web/packages/npm/@team-a/ui", "",
			Address{Kind: Package, Project: "web", Format: "npm", Name: "@team-a/ui"},
			"projects/web/packages/npm/@team-a/ui",
		},
		{
			"projects/web/packages/npm/%40team-a/ui@1.4.0+build.1", "tab=vulnerabilities",
			Address{Kind: Version, Project: "web", Format: "npm", Name: "@team-a/ui", Version: "1.4.0+build.1", Tab: "vulnerabilities", Findings: true},
			"projects/web/packages/npm/@team-a/ui@1.4.0+build.1",
		},
		{
			"projects/web/packages/ansible/acme.web/-/versions", "",
			Address{Kind: Versions, Project: "web", Format: "ansible", Name: "acme.web"},
			"projects/web/packages/ansible/acme.web/-/versions",
		},
		{
			"projects/web/packages/oci/portal/web@sha256:abc", "doc=module/x&tab=documentation",
			Address{Kind: Version, Project: "web", Format: "oci", Name: "portal/web", Version: "sha256:abc", Tab: "documentation", Doc: "module/x"},
			"projects/web/packages/oci/portal/web@sha256:abc",
		},
		{"legal/imprint", "", Address{Kind: Legal, LegalPage: "imprint"}, "legal/imprint"},
		{
			"projects/web/repositories/raw%20files/setup", "client=wget",
			Address{Kind: Setup, Project: "web", Repository: "raw files"},
			"projects/web/repositories/raw%20files/setup",
		},
		{"projects/web/repositories/raw/setup/x", "", Address{Project: "web"}, ""},
		// A repository's page (R-BAS-132), and the root of a public generic repository's files (R-UI-229, Q-BAS-483): the repository's
		// address and `files` alone are the root, whose one spelling is the repository's; a folder named setup is a folder.
		{"projects/web/repositories/raw%20files", "", Address{Kind: Repository, Project: "web", Repository: "raw files"}, "projects/web/repositories/raw%20files"},
		{"projects/web/repositories/raw/files/", "", Address{Kind: Repository, Project: "web", Repository: "raw"}, "projects/web/repositories/raw"},
		{
			"projects/web/repositories/raw/files/tools/setup/agent%20v1.tar.gz", "sort=name",
			Address{Kind: Files, Project: "web", Repository: "raw", Path: "tools/setup/agent v1.tar.gz"},
			"projects/web/repositories/raw/files/tools/setup/agent%20v1.tar.gz",
		},
		{"projects/web/repositories/raw/other", "", Address{Project: "web"}, ""},
		{"projects/web/repositories/raw/files/a/../b", "", Address{}, ""},
		{"legal/other", "", Address{}, ""},
		{"projects/web/packages/npm", "", Address{Project: "web"}, ""},
		{"projects/web/packages/npm/ui@1/-/versions", "", Address{Project: "web", Format: "npm"}, ""},
		{"projects/../x", "", Address{}, ""},
		{"projects/a%2Fb/packages", "", Address{}, ""},
		{"index.html", "", Address{}, ""},
	}
	for _, c := range cases {
		q, _ := url.ParseQuery(c.query)
		got := Parse("en", c.tail, q)
		c.want.Locale = "en"
		if got != c.want || got.Route() != c.route {
			t.Errorf("Parse(%q, %q) = %+v route %q, want %+v route %q", c.tail, c.query, got, got.Route(), c.want, c.route)
		}
	}
	if u := URL("https://b.example.com/", "", "projects/web/packages", url.Values{"tab": {"documentation"}, "doc": {"module/x"}}); u !=
		"https://b.example.com/ui/projects/web/packages?doc=module%2Fx&tab=documentation" {
		t.Errorf("URL = %q", u)
	}
	if c := Cut("one two  three four", 12); c != "one two…" || Cut("short", 12) != "short" || len([]rune(Cut(strings.Repeat("x", 400), MaxDescription))) != MaxDescription {
		t.Errorf("Cut = %q", c)
	}
}
