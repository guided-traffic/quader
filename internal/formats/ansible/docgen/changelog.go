package docgen

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/formats/ansible/rows"
	"github.com/guided-traffic/quader/internal/formats/ansible/version"
)

// The changelog of a collection version (R-ANS-053, Q-ANS-98). Its source is the first of: changelogs/changelog.yaml, the changelog of
// antsibull-changelog, which the generator renders as Markdown by version and category as the documentation site's changelog page shows
// it; CHANGELOG.md at the root, Markdown as it is; CHANGELOG.rst at the root, reStructuredText, which is kept as text and not rendered (the
// root's names are matched without regard to case). The Markdown reaches a browser only through the README renderer and its sanitizer
// (R-ANS-039); the rendered changelog.yaml escapes every character of an entry that Markdown would read, so an entry is text, with its
// inline literals as code and its http and https links as links. A changelog is cut, never refused: at most [ChangelogSource] bytes of
// the member are read, and the stored body is at most [ChangelogBody] bytes, cut after the last whole version (or line) that fits, with
// Truncated set.

const (
	// ChangelogSource is the most bytes of a changelog member read.
	ChangelogSource = 4 << 20
	// ChangelogBody is the most bytes of a stored changelog: the README renderer's source bound of R-UI-008.
	ChangelogBody = 512 << 10
	// maxChangelogNodes bounds the nodes of changelog.yaml; the largest changelog of the community package of 2026 has about 8,000.
	maxChangelogNodes = 100_000
)

// changelogSource and changelogBody are the bounds in force: the constants, lowered by a test.
var changelogSource, changelogBody = ChangelogSource, ChangelogBody

// The warning codes of the changelog.
const (
	CodeChangelogTooLarge = "docs_changelog_too_large"
	CodeChangelogUnread   = "docs_changelog_not_read"
)

const (
	changelogYAML = "changelogs/changelog.yaml"
	changelogYML  = "changelogs/changelog.yml"
)

// ChangelogReader is the tap that keeps the changelog members of an archive (collection.Tap). The generator uses one for a local publish;
// the fill of a remote's archive uses one alone (R-ANS-053), since a remote's other documentation is the upstream's.
type ChangelogReader struct {
	bodies map[string][]byte
	warns  []rows.DocWarning
}

// NewChangelogReader returns an empty reader.
func NewChangelogReader() *ChangelogReader { return &ChangelogReader{bodies: map[string][]byte{}} }

// changelogKind is the kind of a changelog member: "yaml", "md", "rst", or "" for any other member.
func changelogKind(p string) string {
	switch p {
	case changelogYAML, changelogYML:
		return "yaml"
	}
	if strings.Contains(p, "/") {
		return ""
	}
	switch strings.ToLower(p) {
	case "changelog.md":
		return "md"
	case "changelog.rst":
		return "rst"
	}
	return ""
}

// Wants implements the tap: a changelog member, at most ChangelogSource bytes and one more to tell a longer one.
func (c *ChangelogReader) Wants(p string) (int64, bool) {
	if changelogKind(p) == "" {
		return 0, false
	}
	return int64(changelogSource) + 1, true
}

// Take implements the tap.
func (c *ChangelogReader) Take(p string, body []byte) {
	if changelogKind(p) != "" {
		c.bodies[p] = body
	}
}

// Warnings are what the last Changelog left out.
func (c *ChangelogReader) Warnings() []rows.DocWarning { return c.warns }

// Changelog builds the changelog of the collection named collection ("<namespace>.<name>"); nil when the archive holds none that can be
// read.
func (c *ChangelogReader) Changelog(collection string) *rows.DocChangelog {
	c.warns = nil
	for _, kind := range []string{"yaml", "md", "rst"} {
		names := make([]string, 0, 1)
		for p := range c.bodies {
			if changelogKind(p) == kind {
				names = append(names, p)
			}
		}
		sort.Strings(names)
		for _, p := range names {
			if out := c.one(p, kind, collection); out != nil {
				return out
			}
		}
	}
	return nil
}

func (c *ChangelogReader) warn(p, code, msg string) {
	c.warns = append(c.warns, rows.DocWarning{Path: p, Code: code, Message: msg})
}

func (c *ChangelogReader) one(p, kind, collection string) *rows.DocChangelog {
	body := c.bodies[p]
	over := int64(len(body)) > int64(changelogSource)
	if over {
		body = body[:changelogSource]
	}
	if !utf8.Valid(body) && !over {
		c.warn(p, CodeFileNotUTF8, "the changelog is not UTF-8")
		return nil
	}
	if kind != "yaml" {
		format := rows.DocFormatMarkdown
		if kind == "rst" {
			format = rows.DocFormatText
		}
		text, cut := cutLines(validPrefix(body), changelogBody)
		return &rows.DocChangelog{Source: p, Format: format, Body: text, Truncated: over || cut}
	}
	if over {
		c.warn(p, CodeChangelogTooLarge, "the changelog is larger than 4 MiB and is not read")
		return nil
	}
	n, err := parseYAMLWithin(string(body), maxChangelogNodes)
	if err != nil {
		c.warn(p, CodeYAMLRefused, err.Error())
		return nil
	}
	md, cut, ok := renderChangelog(n, collection)
	if !ok {
		c.warn(p, CodeChangelogUnread, "the changelog has no releases")
		return nil
	}
	return &rows.DocChangelog{Source: p, Format: rows.DocFormatMarkdown, Body: md, Truncated: cut}
}

// validPrefix is the longest prefix of b that is valid UTF-8 (a cut member may end inside a character).
func validPrefix(b []byte) string {
	for !utf8.Valid(b) && len(b) > 0 {
		b = b[:len(b)-1]
	}
	return string(b)
}

// cutLines cuts s after the last whole line within max bytes.
func cutLines(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	s = s[:max]
	if i := strings.LastIndexByte(s, '\n'); i > 0 {
		s = s[:i+1]
	}
	return validPrefix([]byte(s)), true
}

// The categories of a release, in the order and with the titles of antsibull-changelog's default configuration.
var changelogSections = [...][2]string{
	{"major_changes", "Major Changes"},
	{"minor_changes", "Minor Changes"},
	{"breaking_changes", "Breaking Changes / Porting Guide"},
	{"deprecated_features", "Deprecated Features"},
	{"removed_features", "Removed Features (previously deprecated)"},
	{"security_fixes", "Security Fixes"},
	{"bugfixes", "Bugfixes"},
	{"known_issues", "Known Issues"},
}

// renderChangelog renders the releases of changelog.yaml, the newest first; ok is false when there are none.
func renderChangelog(n *yamlNode, collection string) (md string, truncated, ok bool) {
	versions, rels := pairs(member(n, "releases"))
	if len(versions) == 0 {
		return "", false, false
	}
	order := make([]int, len(versions))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return newer(versions[order[a]], versions[order[b]]) })
	var b strings.Builder
	for _, i := range order {
		sec := release(versions[i], rels[i], collection)
		if b.Len()+len(sec) > changelogBody {
			if b.Len() == 0 {
				sec, _ = cutLines(sec, changelogBody)
				b.WriteString(sec)
			}
			return b.String(), true, true
		}
		b.WriteString(sec)
	}
	return b.String(), false, true
}

// newer orders two versions of the changelog, the higher first: semantic versions by precedence, before anything that is none, which is
// ordered by its text.
func newer(a, b string) bool {
	va, ea := version.Parse(a)
	vb, eb := version.Parse(b)
	switch {
	case ea == nil && eb == nil:
		return version.Compare(va, vb) > 0
	case ea == nil:
		return true
	case eb == nil:
		return false
	}
	return a > b
}

// release renders one release: its heading, the summary, the categories, and the plugins, modules and other objects it added.
func release(ver string, rel *yamlNode, collection string) string {
	var b strings.Builder
	b.WriteString("## v" + mdText(ver) + "\n\n")
	changes := member(rel, "changes")
	if s := text(member(changes, "release_summary")); s != "" {
		b.WriteString("### Release Summary\n\n" + mdInline(s) + "\n\n")
	}
	known := map[string]bool{"release_summary": true, "trivial": true}
	for _, s := range changelogSections {
		known[s[0]] = true
		list(&b, "### "+s[1], textList(member(changes, s[0])))
	}
	keys, _ := pairs(changes)
	sort.Strings(keys)
	for _, k := range keys {
		if !known[k] {
			list(&b, "### "+mdText(title(k)), textList(member(changes, k)))
		}
	}
	types, plugins := pairs(member(rel, "plugins"))
	if len(types) > 0 {
		var p strings.Builder
		for _, i := range sortedIndex(types) {
			list(&p, "#### "+mdText(title(types[i])), added(plugins[i], collection))
		}
		if p.Len() > 0 {
			b.WriteString("### New Plugins\n\n" + p.String())
		}
	}
	list(&b, "### New Modules", added(member(rel, "modules"), collection))
	kinds, objects := pairs(member(rel, "objects"))
	for _, i := range sortedIndex(kinds) {
		list(&b, "### New "+mdText(title(kinds[i]))+"s", added(objects[i], collection))
	}
	return b.String()
}

func sortedIndex(names []string) []int {
	out := make([]int, len(names))
	for i := range out {
		out[i] = i
	}
	sort.SliceStable(out, func(a, b int) bool { return names[out[a]] < names[out[b]] })
	return out
}

// list writes a heading and a list of Markdown lines, nothing when the list is empty.
func list(b *strings.Builder, heading string, items []string) {
	if len(items) == 0 {
		return
	}
	b.WriteString(heading + "\n\n")
	for _, it := range items {
		b.WriteString("- " + mdInline(it) + "\n")
	}
	b.WriteString("\n")
}

// added is the list of the plugins, modules or objects a release added, as "<collection>.<name> - <description>".
func added(n *yamlNode, collection string) []string {
	if n == nil || n.Kind != sequenceKind {
		return nil
	}
	var out []string
	for _, e := range n.Content {
		name := text(member(e, "name"))
		if name == "" {
			continue
		}
		line := name
		if collection != "" {
			line = collection + "." + name
		}
		if d := text(member(e, "description")); d != "" {
			line += " - " + d
		}
		out = append(out, line)
	}
	return out
}

// title is a key as a heading: underscores as spaces, the first letter upper case.
func title(k string) string {
	k = strings.ReplaceAll(k, "_", " ")
	if k == "" {
		return k
	}
	return strings.ToUpper(k[:1]) + k[1:]
}

// mdPunct are the characters an entry's text escapes: every ASCII punctuation Markdown or HTML reads, so the text stays text.
const mdPunct = "\\`*_{}[]<>()#+-.!|~&\"'=:;$%^?@,/"

// mdText escapes every ASCII punctuation character of s.
func mdText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x80 && strings.ContainsRune(mdPunct, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// mdInline is the text of a changelog entry as Markdown (Q-ANS-98): its lines joined, a reStructuredText literal (“x“ or `x`) a code
// span, a link `text <url>`_ to an http or https URL a link, a role such as :ref:`text <target>` its text, a bare http or https URL an
// autolink, and every other character escaped.
func mdInline(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	var b strings.Builder
	for i := 0; i < len(s); {
		switch {
		case s[i] == '`':
			end, out := backquoted(s, i)
			b.WriteString(out)
			i = end
		case s[i] == ':' && roleAt(s, i) > 0:
			i = roleAt(s, i)
		case strings.HasPrefix(s[i:], "http://") || strings.HasPrefix(s[i:], "https://"):
			u := bareURL(s[i:])
			b.WriteString("<" + u + ">")
			i += len(u)
		default:
			_, size := utf8.DecodeRuneInString(s[i:])
			b.WriteString(mdText(s[i : i+size]))
			i += size
		}
	}
	return b.String()
}

// roleAt returns the index of the backquote after a role name that starts at i (":ref:`"), or 0 when none starts there.
func roleAt(s string, i int) int {
	j := i + 1
	for j < len(s) && (s[j] >= 'a' && s[j] <= 'z' || s[j] == '-' || s[j] == '_') {
		j++
	}
	if j == i+1 || j+1 >= len(s) || s[j] != ':' || s[j+1] != '`' {
		return 0
	}
	return j + 1
}

// backquoted reads the reStructuredText construct that starts with the backquote at i and returns the index after it and its Markdown.
func backquoted(s string, i int) (int, string) {
	if strings.HasPrefix(s[i:], "``") {
		if end := strings.Index(s[i+2:], "``"); end >= 0 {
			return i + 2 + end + 2, codeSpan(s[i+2 : i+2+end])
		}
		return i + 2, mdText("``")
	}
	end := strings.IndexByte(s[i+1:], '`')
	if end < 0 {
		return i + 1, mdText("`")
	}
	inner := s[i+1 : i+1+end]
	next := i + 1 + end + 1
	for next < len(s) && s[next] == '_' {
		next++
	}
	if lt := strings.LastIndex(inner, "<"); lt >= 0 && strings.HasSuffix(inner, ">") {
		txt, target := strings.TrimSpace(inner[:lt]), inner[lt+1:len(inner)-1]
		if httpURL(target) && !strings.ContainsAny(target, "<>") {
			if txt == "" {
				txt = target
			}
			return next, "[" + mdText(txt) + "](<" + target + ">)"
		}
		return next, mdText(txt)
	}
	return next, codeSpan(inner)
}

// codeSpan is s as a Markdown code span, fenced by one more backquote than the longest run in it.
func codeSpan(s string) string {
	longest, run := 0, 0
	for i := 0; i < len(s); i++ {
		if s[i] == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	return fence + " " + s + " " + fence
}

// bareURL is the URL at the start of s: to the first space or angle bracket, without trailing punctuation and without a closing
// parenthesis it does not open.
func bareURL(s string) string {
	end := strings.IndexAny(s, " <>`")
	if end < 0 {
		end = len(s)
	}
	u := s[:end]
	for len(u) > 0 {
		last := u[len(u)-1]
		if strings.IndexByte(".,;:!?'\"", last) >= 0 || last == ')' && strings.Count(u, "(") < strings.Count(u, ")") {
			u = u[:len(u)-1]
			continue
		}
		break
	}
	return u
}
