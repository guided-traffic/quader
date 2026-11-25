package docgen

import (
	"html"
	"regexp"
	"strings"
	"unicode/utf8"
)

// The converter of the Ansible markup of a documentation string to HTML (R-ANS-039, Q-ANS-59). Every character of the text is escaped before it
// is written; the macros only decide which fixed tags surround the escaped text. The caller still runs the result through the Host's
// sanitizer, so the converter is not the one weak point.
//
// The links to the documentation itself (R-ANS-048, Q-ANS-94): M(), P(), O() and RV() become links only to what the version's own tree
// holds, and the address is built here from the names that were found, never taken from the text. An address is page-relative:
// "?doc=<type>/<name>" opens an entry, "#parameter-<path>" and "#return-<path>" an option or a return value of the entry that is open, and
// the two together one of another entry. [PageLink] is the grammar of exactly these addresses; the sanitizer of the operations keeps a
// relative link only when it matches.

// MaxMarkupInput is the longest text one conversion reads; more is cut (R-ANS-039).
const MaxMarkupInput = 64 << 10

// maxMarkupDepth is how deeply emphasis macros nest before the rest is text.
const maxMarkupDepth = 4

// linkRel is the rel of every link the converter makes.
const linkRel = "nofollow noopener noreferrer"

// Links is what the converter knows of the page a text is shown on (R-ANS-048): the collection, the entry the text belongs to, the names the
// version's tree holds and the option and return value paths of the entry. A nil *Links makes no link to the documentation.
type Links struct {
	// Collection is the full name of the collection, "<namespace>.<name>".
	Collection string
	// Entries reports whether the tree of the version holds the entry; nil holds none.
	Entries func(typ, name string) bool
	// Options and Returns are the paths of the entry's own options (with their aliases) and return values, as [OptionPath] spells them.
	Options, Returns map[string]bool
}

// Entry reports whether a full name such as ns.coll.mount names an entry of the type in this collection's tree, and returns its name.
func (l *Links) Entry(full, typ string) (string, bool) {
	if l == nil || l.Entries == nil || l.Collection == "" || !strings.HasPrefix(full, l.Collection+".") {
		return "", false
	}
	name := strings.TrimPrefix(full, l.Collection+".")
	if !entryName.MatchString(name) || !entryType.MatchString(typ) || !l.Entries(typ, name) {
		return "", false
	}
	return name, true
}

// own is the set of paths of the entry's options (O) or return values (RV).
func (l *Links) own(macro string) map[string]bool {
	switch {
	case l == nil:
		return nil
	case macro == "RV":
		return l.Returns
	}
	return l.Options
}

var (
	entryType = regexp.MustCompile(`^[a-z_]{1,32}$`)
	entryName = regexp.MustCompile(`^[a-z_][a-z0-9_]*(\.[a-z_][a-z0-9_]*)*$`)
	pathSeg   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	pageLink  = regexp.MustCompile(`^(\?doc=[a-z_]{1,32}/[a-z_][a-z0-9_.]{0,255})?(#(parameter|return)-[A-Za-z0-9_-]+(/[A-Za-z0-9_-]+)*)?$`)
)

// maxPageLink is the longest page-relative address: an entry and a path of 12 names of 128 characters.
const maxPageLink = 2048

// EntryDoc is the page-relative address of an entry: "?doc=<type>/<name>".
func EntryDoc(typ, name string) string { return "?doc=" + typ + "/" + name }

// PageLink reports whether href is one of the page-relative addresses the converter makes (Q-ANS-94): an entry, an option or a return
// value of the open entry, or both. The sanitizer of the documentation operations keeps a relative link only when it is one.
func PageLink(href string) bool {
	return href != "" && len(href) <= maxPageLink && pageLink.MatchString(href)
}

// OptionPath is the path of an option or a return value in the markup's spelling, "a.b[].c", as the anchor spells it, "a/b/c"; "" when a
// part is no plain name. The UI builds the same path from the names of the nested lists (R-ANS-048).
func OptionPath(spelled string) string {
	parts := strings.Split(strings.ReplaceAll(spelled, "[]", ""), ".")
	if len(parts) > 12 {
		return ""
	}
	for _, p := range parts {
		if !pathSeg.MatchString(p) {
			return ""
		}
	}
	return strings.Join(parts, "/")
}

// RenderMarkup converts the Ansible markup of a documentation string to HTML: I(x) emphasis, B(x) strong, C(x), V(x), E(x) and R(text,ref)
// code, U(url) and L(text,url) a link when the URL is http or https and the text otherwise, HORIZONTALLINE a rule, and the backslash
// escapes of the markup. M(x), P(x), O(x) and RV(x) are code, inside a link to the entry, the option or the return value when links finds
// it (R-ANS-048). A macro that does not close, or nests deeper than 4, is text. The input is cut at [MaxMarkupInput] bytes. The caller
// sanitizes the result with the policy of R-UI-008.
func RenderMarkup(text string, links *Links) string {
	var b strings.Builder
	r := renderer{out: &b, links: links}
	r.run(cut(text), 0)
	return b.String()
}

// PlainText is the text of a documentation string without its markup: the content of every macro, no tags. It builds the search text of an
// entry.
func PlainText(text string) string {
	var b strings.Builder
	r := renderer{out: &b, plain: true}
	r.run(cut(text), 0)
	return b.String()
}

func cut(s string) string {
	if len(s) <= MaxMarkupInput {
		return s
	}
	s = s[:MaxMarkupInput]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
}

type renderer struct {
	out   *strings.Builder
	plain bool
	links *Links
}

// macroTag returns the tag of a macro ("" for the macros that write a link or text only) and whether its argument is literal text.
func macroTag(name string) (tag string, code, ok bool) {
	switch name {
	case "I":
		return "em", false, true
	case "B":
		return "strong", false, true
	case "C", "O", "V", "RV", "M", "P", "R", "E":
		return "code", true, true
	case "U", "L":
		return "", true, true
	}
	return "", false, false
}

func (r *renderer) text(s string) {
	if r.plain {
		r.out.WriteString(s)
		return
	}
	r.out.WriteString(html.EscapeString(s))
}

// run writes s, finding the macros in it. depth is the nesting of emphasis macros.
func (r *renderer) run(s string, depth int) {
	i, last := 0, 0
	flush := func(to int) {
		if to > last {
			r.text(unescapeMarkup(s[last:to]))
		}
	}
	for i < len(s) {
		c := s[i]
		if c == '\\' {
			i += 2
			continue
		}
		if !isUpper(c) || i > 0 && isWordByte(s[i-1]) {
			i++
			continue
		}
		j := i
		for j < len(s) && isUpper(s[j]) {
			j++
		}
		name := s[i:j]
		if name == "HORIZONTALLINE" && (j >= len(s) || !isWordByte(s[j])) {
			flush(i)
			if r.plain {
				r.out.WriteByte(' ')
			} else {
				r.out.WriteString("<hr>")
			}
			i, last = j, j
			continue
		}
		tag, code, ok := macroTag(name)
		if !ok || j >= len(s) || s[j] != '(' {
			i = j
			continue
		}
		arg, end, closed := macroArgument(s, j+1, !code || name == "U" || name == "L" || name == "R")
		if !closed || !code && depth >= maxMarkupDepth {
			i = j
			continue
		}
		flush(i)
		r.macro(name, tag, code, arg, depth)
		i, last = end, end
	}
	flush(len(s))
}

// macroArgument returns the text between the parenthesis that opens at start-1 and the first closing one that is not escaped, and the
// index after it. With nest the parentheses inside count, so an emphasis macro can hold another macro and a URL can hold parentheses.
func macroArgument(s string, start int, nest bool) (arg string, end int, closed bool) {
	level := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '(':
			if nest {
				level++
			}
		case ')':
			if level == 0 {
				return s[start:i], i + 1, true
			}
			level--
		}
	}
	return "", 0, false
}

func (r *renderer) macro(name, tag string, code bool, arg string, depth int) {
	switch name {
	case "R", "L":
		text, ref := splitArgument(arg)
		if name == "R" {
			r.wrap("code", unescapeMarkup(text))
			return
		}
		url := strings.TrimSpace(unescapeMarkup(ref))
		if r.plain || !httpURL(url) {
			r.text(unescapeMarkup(text))
			return
		}
		r.out.WriteString(`<a href="` + html.EscapeString(url) + `" rel="` + linkRel + `">`)
		r.text(unescapeMarkup(text))
		r.out.WriteString("</a>")
	case "U":
		url := strings.TrimSpace(unescapeMarkup(arg))
		if r.plain || !httpURL(url) {
			r.text(url)
			return
		}
		r.out.WriteString(`<a href="` + html.EscapeString(url) + `" rel="` + linkRel + `">`)
		r.text(url)
		r.out.WriteString("</a>")
	case "M", "P", "O", "RV":
		href, shown := r.reference(name, unescapeMarkup(arg))
		if r.plain || href == "" {
			r.wrap("code", shown)
			return
		}
		r.out.WriteString(`<a href="` + html.EscapeString(href) + `">`)
		r.wrap("code", shown)
		r.out.WriteString("</a>")
	default:
		if code {
			r.wrap(tag, unescapeMarkup(arg))
			return
		}
		if !r.plain {
			r.out.WriteString("<" + tag + ">")
		}
		r.run(arg, depth+1)
		if !r.plain {
			r.out.WriteString("</" + tag + ">")
		}
	}
}

// reference resolves the target of M(), P(), O() or RV() against the links (R-ANS-048). It returns the address, "" for none, and the text
// that is shown: the full name for M(ns.coll.mount) and P(ns.coll.env#lookup), what follows the entry for O(ns.coll.x#module:opt=value),
// and the argument for O(opt=value) and RV(key).
func (r *renderer) reference(macro, text string) (href, shown string) {
	switch macro {
	case "M":
		if name, ok := r.links.Entry(text, "module"); ok {
			return EntryDoc("module", name), text
		}
		return "", text
	case "P":
		full, typ, found := strings.Cut(text, "#")
		if !found {
			return "", text
		}
		if name, ok := r.links.Entry(full, typ); ok {
			return EntryDoc(typ, name), full
		}
		return "", full
	}
	anchor := "parameter-"
	if macro == "RV" {
		anchor = "return-"
	}
	if rest, ok := strings.CutPrefix(text, "ignore:"); ok {
		return "", rest
	}
	full, rest, other := strings.Cut(text, "#")
	if !other {
		key, _, _ := strings.Cut(text, "=")
		if p := OptionPath(key); p != "" && r.links.own(macro)[p] {
			return "#" + anchor + p, text
		}
		return "", text
	}
	typ, opt, ok := strings.Cut(rest, ":")
	if !ok {
		return "", text
	}
	key, _, _ := strings.Cut(opt, "=")
	name, found := r.links.Entry(full, typ)
	if p := OptionPath(key); found && p != "" && typ != "role" {
		return EntryDoc(typ, name) + "#" + anchor + p, opt
	}
	return "", opt
}

func (r *renderer) wrap(tag, text string) {
	if r.plain {
		r.text(text)
		return
	}
	r.out.WriteString("<" + tag + ">")
	r.text(text)
	r.out.WriteString("</" + tag + ">")
}

// splitArgument splits "text,ref" at the first comma that is not escaped.
func splitArgument(arg string) (text, ref string) {
	for i := 0; i < len(arg); i++ {
		switch arg[i] {
		case '\\':
			i++
		case ',':
			return arg[:i], arg[i+1:]
		}
	}
	return arg, ""
}

// unescapeMarkup removes the backslash of the escapes of the markup: \\, \(, \), \, and a backslash before any other character stays.
func unescapeMarkup(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && strings.IndexByte(`\(),`, s[i+1]) >= 0 {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// httpURL reports whether u is an absolute http or https URL without white space or control characters.
func httpURL(u string) bool {
	l := strings.ToLower(u)
	if !strings.HasPrefix(l, "http://") && !strings.HasPrefix(l, "https://") || len(u) <= len("http://") {
		return false
	}
	for _, c := range u {
		if c <= ' ' || c == 0x7f {
			return false
		}
	}
	return true
}

func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }
func isWordByte(c byte) bool {
	return isUpper(c) || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_'
}
