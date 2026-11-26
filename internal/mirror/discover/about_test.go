package discover

import (
	"strings"
	"testing"
)

// TestR_CUR_028_AnAddressIsALinkOnlyForHTTPAndHTTPS: the facts of an upstream keep an address only when it is an absolute http or https
// address with a host and no credential; npm's git forms are read as the https address of the same host and path. Microseconds.
func TestR_CUR_028_AnAddressIsALinkOnlyForHTTPAndHTTPS(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/acme/a#readme":                        "https://github.com/acme/a#readme",
		"git+https://github.com/acme/a.git":                       "https://github.com/acme/a",
		"git+ssh://git@github.com/acme/a.git":                     "https://github.com/acme/a",
		"git://github.com/acme/a.git":                             "https://github.com/acme/a",
		"git@github.com:acme/a.git":                               "https://github.com/acme/a",
		"http://example.org/":                                     "http://example.org/",
		"javascript:alert(1)":                                     "",
		"data:text/html,<b>x</b>":                                 "",
		"https://user:pw@example.org/":                            "",
		"//example.org/a":                                         "",
		"/relative":                                               "",
		"https://exa mple.org/":                                   "",
		"https://example.org/" + strings.Repeat("a", MaxURLBytes): "",
	} {
		got, ok := WebAddress(in)
		if got != want || ok != (want != "") {
			t.Errorf("R-CUR-028 WebAddress(%.60q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	a := CutAbout(About{Version: "1.0.0", Links: []Link{
		{Kind: LinkRepository, URL: "https://a.example/"}, {Kind: LinkHomepage, URL: "ftp://h.example/"},
		{Kind: LinkHomepage, URL: "https://h.example/"}, {Kind: LinkRepository, URL: "https://b.example/"},
	}, Dependencies: []Dependency{{Name: ""}, {Name: "x", Range: "^1"}}})
	if len(a.Links) != 2 || a.Links[0] != (Link{LinkHomepage, "https://h.example/"}) || a.Links[1] != (Link{LinkRepository, "https://a.example/"}) ||
		len(a.Dependencies) != 1 {
		t.Fatalf("R-CUR-028 CutAbout: %+v", a)
	}
	if !CutAbout(About{License: "MIT"}).Empty() {
		t.Fatal("R-CUR-028 facts without a version are kept")
	}
}
