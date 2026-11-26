package legal

import (
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/config"
)

// imprint is an operator's realistic imprint with everything the sanitizer has to deal with: an address block, a table, links of
// the three kinds, an image of another site, an image of the Bastion's own origin, and what an operator's text must not carry.
const imprint = `# Imprint

Example Operations GmbH\
Musterstraße 1\
12345 Berlin

| Register | Number |
|---|---|
| Amtsgericht Berlin | HRB 12345 |

Mail: <mailto:legal@example.com>, web: [example.com](https://www.example.com), [plain](http://www.example.org)

![Logo](https://cdn.example.net/logo.png)
![Own](https://packages.example.com/files/p/r/logo.png)

<script>alert(1)</script>
<style>body{display:none}</style>
<svg><circle r="5"/></svg>
<iframe src="https://evil.example"></iframe>
<a href="javascript:alert(1)">click</a>
<img src="x" onerror="alert(1)">
<p onclick="alert(1)" style="color:red">styled</p>
`

// TestR_BAS_100_ThePagesAreRenderedAtStartUnderTheReadmeSanitizer: what an operator's Markdown keeps and loses at the Bastion
// (R-UI-225, H-2303): script, style, SVG, frames, handlers and a javascript: link are dropped; an image of another site becomes a
// link; an image of the Bastion's own origin is dropped; links keep http, https and mailto with the external rel; the address
// block and the table stay.
func TestR_BAS_100_ThePagesAreRenderedAtStartUnderTheReadmeSanitizer(t *testing.T) {
	p, err := New(config.UILegal{Imprint: config.UILegalText{En: imprint}}, "https://packages.example.com")
	if err != nil {
		t.Fatal(err)
	}
	s := p.Pages("en")
	if s.Imprint == nil || s.Imprint.Lang != "en" || s.Privacy != nil {
		t.Fatalf("pages = %+v", s)
	}
	h := s.Imprint.HTML
	dropped := []string{
		"<script", "alert(1)", "<style", "display:none", "<svg", "<circle", "<iframe", "evil.example",
		"javascript:", "onerror", "onclick", "style=", "packages.example.com", "<img",
	}
	for _, gone := range dropped {
		if strings.Contains(h, gone) {
			t.Errorf("the page keeps %q:\n%s", gone, h)
		}
	}
	for _, kept := range []string{
		"<h1", "Musterstraße 1<br/>", "<table>", "HRB 12345",
		`<a href="mailto:legal@example.com" rel="nofollow noopener noreferrer">`,
		`<a href="https://www.example.com" rel="nofollow noopener noreferrer">example.com</a>`,
		`<a href="http://www.example.org" rel="nofollow noopener noreferrer">plain</a>`,
		`<a href="https://cdn.example.net/logo.png" rel="nofollow noopener noreferrer">External image: Logo</a>`,
		"styled",
	} {
		if !strings.Contains(h, kept) {
			t.Errorf("the page lacks %q:\n%s", kept, h)
		}
	}
	// German has no text of its own: the English one stands in, marked en.
	if de := p.Pages("de"); de.Imprint == nil || de.Imprint.Lang != "en" || de.Imprint.HTML != h {
		t.Errorf("de = %+v", de.Imprint)
	}
}

// TestR_BAS_100_NoTextsNoPages: a Bastion whose chart sets no text shows no page, and the start does not fail.
func TestR_BAS_100_NoTextsNoPages(t *testing.T) {
	p, err := New(config.UILegal{}, "https://packages.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Pages("en").Empty() || !p.Pages("de").Empty() || p.ETag() == "" {
		t.Errorf("pages = %+v %+v", p.Pages("en"), p.Pages("de"))
	}
}
