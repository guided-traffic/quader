package docgen_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/ansible/docgen"
)

// TestR_ANS_039_TheMarkupTableHasACasePerMacro: each macro of the Ansible markup becomes its tag, the text is escaped, and what is not a closed
// macro stays text. Without links, M(), P(), O() and RV() are code.
func TestR_ANS_039_TheMarkupTableHasACasePerMacro(t *testing.T) {
	const rel = ` rel="nofollow noopener noreferrer"`
	for in, want := range map[string]string{
		"plain":                               "plain",
		"I(x) and E(y)":                       "<em>x</em> and <code>y</code>",
		"B(bold)":                             "<strong>bold</strong>",
		"C(a), O(b), V(c), RV(d), M(e), P(f)": "<code>a</code>, <code>b</code>, <code>c</code>, <code>d</code>, <code>e</code>, <code>f</code>",
		"P(ns.coll.env#lookup)":               "<code>ns.coll.env</code>",
		"O(ns.coll.x#module:state=present)":   "<code>state=present</code>",
		"O(ignore:state)":                     "<code>state</code>",
		"R(the page,ref)":                     "<code>the page</code>",
		"U(https://example.com/x)":            `<a href="https://example.com/x"` + rel + `>https://example.com/x</a>`,
		"L(docs,http://example.com)":          `<a href="http://example.com"` + rel + `>docs</a>`,
		"L(x,javascript:alert(1))":            "x",
		"U(javascript:alert(1))":              "javascript:alert(1)",
		"L(x,ftp://example.com)":              "x",
		"HORIZONTALLINE":                      "<hr>",
		"a HORIZONTALLINE b":                  "a <hr> b",
		"C(a\\)b)":                            "<code>a)b</code>",
		"C(a\\\\b)":                           `<code>a\b</code>`,
		"L(a\\, b,https://e.com)":             `<a href="https://e.com"` + rel + `>a, b</a>`,
		"I(unclosed":                          "I(unclosed",
		"XC(x)":                               "XC(x)",
		"B(I(C(x)))":                          "<strong><em><code>x</code></em></strong>",
		"B(I(B(I(B(x)))))":                    "<strong><em><strong><em>B(x)</em></strong></em></strong>",
		"C(<img src=x onerror=1>)":            "<code>&lt;img src=x onerror=1&gt;</code>",
		"<script>alert(1)</script>":           "&lt;script&gt;alert(1)&lt;/script&gt;",
		"a & b":                               "a &amp; b",
	} {
		if got := docgen.RenderMarkup(in, nil); got != want {
			t.Errorf("RenderMarkup(%q) = %q, want %q", in, got, want)
		}
	}
	if got := docgen.PlainText("Connects to C(host) and I(waits). L(x,https://e.com) HORIZONTALLINE"); got != "Connects to host and waits. x  " {
		t.Errorf("PlainText = %q", got)
	}
	if got := docgen.RenderMarkup(strings.Repeat("a", docgen.MaxMarkupInput+100), nil); len(got) != docgen.MaxMarkupInput {
		t.Errorf("the input is cut at the bound, got %d bytes", len(got))
	}
}

// testLinks is a tree of acme.tools with a module mount, a lookup env and a role web, and an entry with the options src, state, opts and
// opts.mode and the alias path, and the return values dev and dev.name.
func testLinks() *docgen.Links {
	tree := map[string]bool{"module/mount": true, "lookup/env": true, "role/web": true}
	return &docgen.Links{
		Collection: "acme.tools",
		Entries:    func(typ, name string) bool { return tree[typ+"/"+name] },
		Options:    map[string]bool{"src": true, "state": true, "opts": true, "opts/mode": true, "path": true},
		Returns:    map[string]bool{"dev": true, "dev/name": true},
	}
}

// TestR_ANS_048_TheMacrosLinkOnlyToWhatTheTreeHolds: M(), P(), O() and RV() link to the entry, the option or the return value the version's
// tree and the entry hold, with an address the server builds; a name the tree does not hold, a name of another collection and a hostile name
// stay code, and E() is code (R-ANS-048).
func TestR_ANS_048_TheMacrosLinkOnlyToWhatTheTreeHolds(t *testing.T) {
	for in, want := range map[string]string{
		"M(acme.tools.mount)":                      `<a href="?doc=module/mount"><code>acme.tools.mount</code></a>`,
		"M(acme.tools.missing)":                    "<code>acme.tools.missing</code>",
		"M(ansible.builtin.copy)":                  "<code>ansible.builtin.copy</code>",
		"M(javascript:alert(1))":                   "<code>javascript:alert(1</code>)",
		`M(acme.tools.mount"><script>)`:            "<code>acme.tools.mount&#34;&gt;&lt;script&gt;</code>",
		"P(acme.tools.env#lookup)":                 `<a href="?doc=lookup/env"><code>acme.tools.env</code></a>`,
		"P(acme.tools.env#filter)":                 "<code>acme.tools.env</code>",
		"P(acme.tools.web#role)":                   `<a href="?doc=role/web"><code>acme.tools.web</code></a>`,
		"O(state)":                                 `<a href="#parameter-state"><code>state</code></a>`,
		"O(state=present)":                         `<a href="#parameter-state"><code>state=present</code></a>`,
		"O(opts.mode)":                             `<a href="#parameter-opts/mode"><code>opts.mode</code></a>`,
		"O(opts[].mode)":                           `<a href="#parameter-opts/mode"><code>opts[].mode</code></a>`,
		"O(path)":                                  `<a href="#parameter-path"><code>path</code></a>`,
		"O(nothing)":                               "<code>nothing</code>",
		`O(it"s)`:                                  "<code>it&#34;s</code>",
		"RV(dev.name)":                             `<a href="#return-dev/name"><code>dev.name</code></a>`,
		"RV(state)":                                "<code>state</code>",
		"O(acme.tools.mount#module:fstab)":         `<a href="?doc=module/mount#parameter-fstab"><code>fstab</code></a>`,
		"RV(acme.tools.mount#module:dev)":          `<a href="?doc=module/mount#return-dev"><code>dev</code></a>`,
		"O(acme.tools.gone#module:fstab)":          "<code>fstab</code>",
		"O(acme.tools.web#role:main:port)":         `<a href="?doc=role/web#parameter-main--port"><code>main:port</code></a>`,
		"O(acme.tools.web#role:main:tls.cert=x)":   `<a href="?doc=role/web#parameter-main--tls/cert"><code>main:tls.cert=x</code></a>`,
		"O(acme.tools.web#role:port)":              "<code>port</code>",
		"O(acme.tools.web#role:a b:port)":          "<code>a b:port</code>",
		"RV(acme.tools.web#role:main:port)":        "<code>main:port</code>",
		"O(acme.tools.gone#role:main:port)":        "<code>main:port</code>",
		"O(acme.tools.mount#module:a b)":           "<code>a b</code>",
		"E(HOME) and V(true)":                      "<code>HOME</code> and <code>true</code>",
		"See M(acme.tools.mount) and O(src).":      `See <a href="?doc=module/mount"><code>acme.tools.mount</code></a> and <a href="#parameter-src"><code>src</code></a>.`,
		"L(x,javascript:alert(1)) M(acme.tools.x)": "x <code>acme.tools.x</code>",
	} {
		if got := docgen.RenderMarkup(in, testLinks()); got != want {
			t.Errorf("RenderMarkup(%q) = %q, want %q", in, got, want)
		}
	}
	for href, want := range map[string]bool{
		"?doc=module/mount": true, "#parameter-opts/mode": true, "?doc=module/mount#return-dev": true, "#return-_list/A": true,
		"": false, "?doc=module/mount&x=1": false, "javascript:alert(1)": false, "#user-content-x": false, "?doc=Module/x": false,
		"#parameter-a b": false, "//evil.example/x": false, "?doc=module/x#parameter-": false,
	} {
		if docgen.PageLink(href) != want {
			t.Errorf("PageLink(%q) = %v, want %v", href, !want, want)
		}
	}
}

// TestR_ANS_052_AnEntryPointsProseLinksItsOwnOptions: in the prose of a role's entry point, O() of the entry point's option links to the
// anchor parameter-<entry point>--<path>, the anchor of the documentation site (H-2102 closed). Unit, microseconds.
func TestR_ANS_052_AnEntryPointsProseLinksItsOwnOptions(t *testing.T) {
	l := testLinks()
	l.EntryPoint = "main"
	for in, want := range map[string]string{
		"O(opts.mode)": `<a href="#parameter-main--opts/mode"><code>opts.mode</code></a>`,
		"O(nothing)":   "<code>nothing</code>",
		"RV(dev)":      `<a href="#return-dev"><code>dev</code></a>`,
	} {
		if got := docgen.RenderMarkup(in, l); got != want {
			t.Errorf("RenderMarkup(%q) = %q, want %q", in, got, want)
		}
	}
	if !docgen.PageLink("?doc=role/web#parameter-main--opts/mode") {
		t.Error("PageLink refuses an entry point's anchor")
	}
}

var (
	tagRE   = regexp.MustCompile(`<(/?)([a-zA-Z][a-zA-Z0-9]*)`)
	hrefRE  = regexp.MustCompile(`href="([^"]*)"`)
	onRE    = regexp.MustCompile(`(?i)<[^>]*\son`)
	allowed = map[string]bool{"em": true, "strong": true, "code": true, "a": true, "hr": true}
)

// FuzzR_ANS_039_RenderMarkup: whatever the input, with and without the links of a tree, the output holds only the converter's tags, no
// script and no on* attribute, and every address is http, https or one of the page-relative addresses of [docgen.PageLink] (R-ANS-039,
// R-ANS-048).
func FuzzR_ANS_039_RenderMarkup(f *testing.F) {
	for _, s := range []string{
		"C(<script>)", "L(a,javascript:x)", "I(B(I(B(I(x)))))", `L(a\,b,https://e.com)`, "HORIZONTALLINE", "U(", `\`,
		"M(acme.tools.mount)", `O(acme.tools.mount#module:x="><b>)`, "P(acme.tools.env#lookup)", "RV(dev.name)", "O(opts[].mode=1)",
		`O(acme.tools.web#role:main:x"><b>)`, "O(acme.tools.web#role:main:opts.mode)",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		ep := testLinks()
		ep.EntryPoint = "main"
		for _, links := range []*docgen.Links{nil, testLinks(), ep} {
			out := docgen.RenderMarkup(in, links)
			for _, m := range tagRE.FindAllStringSubmatch(out, -1) {
				if !allowed[m[2]] {
					t.Fatalf("RenderMarkup(%q) = %q holds the tag %s", in, out, m[2])
				}
			}
			if strings.Contains(out, "<script") || onRE.MatchString(out) {
				t.Fatalf("RenderMarkup(%q) = %q", in, out)
			}
			for _, m := range hrefRE.FindAllStringSubmatch(out, -1) {
				h := m[1]
				if l := strings.ToLower(h); !strings.HasPrefix(l, "http://") && !strings.HasPrefix(l, "https://") && !docgen.PageLink(h) {
					t.Fatalf("RenderMarkup(%q) = %q links to %q", in, out, h)
				}
			}
		}
	})
}
