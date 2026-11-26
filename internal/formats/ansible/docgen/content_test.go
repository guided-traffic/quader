package docgen_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/ansible/docgen"
	"github.com/guided-traffic/quader/internal/formats/ansible/rows"
)

// The fixture testdata/acme_tools is a collection written for these tests; ansible-doc of ansible-core 2.15.8 made the two JSON files from
// it (`ansible-doc --json -t module acme.tools.ping_host`, `-t role acme.tools.web`; the role's local path removed). They are the measure
// of what the generator makes of a fragment and of argument specs.

func readFixture(t *testing.T, names ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join("testdata", "acme_tools", n))
		if err != nil {
			t.Fatal(err)
		}
		out[n] = string(b)
	}
	return out
}

func docOf(t *testing.T, e rows.DocEntry) map[string]any {
	t.Helper()
	var ds struct {
		Doc map[string]any `json:"doc"`
	}
	if err := json.Unmarshal(e.Doc, &ds); err != nil || ds.Doc == nil {
		t.Fatalf("doc of %s: %v %s", e.Name, err, e.Doc)
	}
	return ds.Doc
}

// named turns a list of named objects into a map by name.
func named(list any) map[string]map[string]any {
	out := map[string]map[string]any{}
	l, _ := list.([]any)
	for _, x := range l {
		o, _ := x.(map[string]any)
		n, _ := o["name"].(string)
		out[n] = o
	}
	return out
}

// strs is a string or a list of strings as a list.
func strs(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, x := range t {
			s, _ := x.(string)
			out = append(out, s)
		}
		return out
	}
	return nil
}

// sameOption compares the members of an option both sides know: type, elements, default, required, choices and description.
func sameOption(t *testing.T, where string, ours, theirs map[string]any) {
	t.Helper()
	for _, k := range []string{"type", "elements", "default", "required", "choices"} {
		if !reflect.DeepEqual(ours[k], theirs[k]) {
			t.Errorf("%s.%s = %v, ansible-doc %v", where, k, ours[k], theirs[k])
		}
	}
	if !reflect.DeepEqual(strs(ours["description"]), strs(theirs["description"])) {
		t.Errorf("%s.description = %v, ansible-doc %v", where, ours["description"], theirs["description"])
	}
}

// TestR_ANS_051_AFragmentOfTheCollectionIsMergedAsAnsibleDocMergesIt: the module extends two fragments of its collection (one by an
// attribute after a dot) and one of ansible.builtin. Its options, notes, requirements and attributes are those ansible-doc made, without the
// options of ansible.builtin.files, which stays listed as not merged; the module's own option wins member by member and keeps its place,
// the fragment's options follow; the search finds a merged option. Unit, about a millisecond.
func TestR_ANS_051_AFragmentOfTheCollectionIsMergedAsAnsibleDocMergesIt(t *testing.T) {
	src := readFixture(t, "plugins/modules/ping_host.py", "plugins/doc_fragments/common.py", "ansible-doc-module.json")
	set := build(t, docgen.DefaultLimits(), "", map[string]string{
		"plugins/modules/ping_host.py": src["plugins/modules/ping_host.py"], "plugins/doc_fragments/common.py": src["plugins/doc_fragments/common.py"],
	})
	e := entry(t, set, "module", "ping_host")
	doc := docOf(t, e)
	var ad map[string]any
	if err := json.Unmarshal([]byte(src["ansible-doc-module.json"]), &ad); err != nil {
		t.Fatal(err)
	}
	builtinFiles := []string{"attributes", "group", "mode", "owner", "selevel", "serole", "setype", "seuser", "unsafe_writes"}
	ours := named(doc["options"])
	theirs, _ := ad["options"].(map[string]any)
	for name, spec := range theirs {
		if slices.Contains(builtinFiles, name) {
			if _, ok := ours[name]; ok {
				t.Errorf("an option of ansible.builtin.files was merged: %s", name)
			}
			continue
		}
		o, ok := ours[name]
		if !ok {
			t.Errorf("option %s of ansible-doc is missing", name)
			continue
		}
		sameOption(t, name, o, spec.(map[string]any))
	}
	if len(ours) != len(theirs)-len(builtinFiles) {
		t.Errorf("options = %v", reflect.ValueOf(ours).MapKeys())
	}
	var order []string
	for _, x := range doc["options"].([]any) {
		order = append(order, x.(map[string]any)["name"].(string))
	}
	if !reflect.DeepEqual(order, []string{"host", "timeout", "api_url", "validate"}) {
		t.Errorf("order = %v", order)
	}
	for _, k := range []string{"notes", "requirements", "attributes"} {
		if !reflect.DeepEqual(doc[k], ad[k]) {
			t.Errorf("%s = %v, ansible-doc %v", k, doc[k], ad[k])
		}
	}
	if !reflect.DeepEqual(doc["seealso"], ad["seealso"]) {
		t.Errorf("seealso = %v, ansible-doc %v", doc["seealso"], ad["seealso"])
	}
	if !reflect.DeepEqual(strs(doc["extends_documentation_fragment"]), []string{"ansible.builtin.files"}) {
		t.Errorf("not merged = %v", doc["extends_documentation_fragment"])
	}
	if !strings.Contains(e.SearchText, "api_url") || len(set.Warnings) != 0 {
		t.Errorf("search %q, warnings %v", e.SearchText, set.Warnings)
	}
	if f := entry(t, set, "doc_fragments", "common"); f.HasDocs {
		t.Errorf("a fragment is an entry with documentation: %+v", f)
	}
}

// TestR_ANS_051_AFragmentThatCannotBeReadStaysListed: a fragment of another collection, an attribute the fragment lacks, an attribute that
// is no literal, a DOCUMENTATION outside the class or inside a method, refused YAML and a fragment without options each leave the name in
// the list of fragments not merged; the collection's own ones warn. Unit, about a millisecond.
func TestR_ANS_051_AFragmentThatCannotBeReadStaysListed(t *testing.T) {
	mod := func(frag string) string {
		return "DOCUMENTATION = '''\nmodule: m\nextends_documentation_fragment: [" + frag + "]\noptions:\n  a: {type: str}\n'''\n"
	}
	for name, tc := range map[string]struct {
		frag, fragment, code string
	}{
		"another collection":      {"other.coll.common", "class ModuleDocFragment:\n    DOCUMENTATION = '''\noptions: {x: {type: str}}\n'''\n", ""},
		"a missing attribute":     {"acme.tools.common.nope", "class ModuleDocFragment:\n    DOCUMENTATION = '''\noptions: {x: {type: str}}\n'''\n", docgen.CodeFragmentNotRead},
		"no literal":              {"acme.tools.common", "class ModuleDocFragment:\n    DOCUMENTATION = PREFIX + '''\noptions: {x: {}}\n'''\n", docgen.CodeFragmentNotRead},
		"outside the class":       {"acme.tools.common", "DOCUMENTATION = '''\noptions: {x: {type: str}}\n'''\nclass ModuleDocFragment:\n    pass\n", docgen.CodeFragmentNotRead},
		"inside a method":         {"acme.tools.common", "class ModuleDocFragment:\n    def f(self):\n        DOCUMENTATION = '''\noptions: {x: {}}\n'''\n", docgen.CodeFragmentNotRead},
		"another class":           {"acme.tools.common", "class Other:\n    DOCUMENTATION = '''\noptions: {x: {}}\n'''\n", docgen.CodeFragmentNotRead},
		"refused YAML":            {"acme.tools.common", "class ModuleDocFragment:\n    DOCUMENTATION = '''\noptions: &a {x: {}}\nb: *a\n'''\n", docgen.CodeYAMLRefused},
		"neither options nor att": {"acme.tools.common", "class ModuleDocFragment:\n    DOCUMENTATION = '''\nnotes: [n]\n'''\n", docgen.CodeFragmentNotRead},
		"a short builtin name":    {"files", "class ModuleDocFragment:\n    DOCUMENTATION = '''\noptions: {x: {}}\n'''\n", ""},
	} {
		set := build(t, docgen.DefaultLimits(), "", map[string]string{"plugins/modules/m.py": mod(tc.frag), "plugins/doc_fragments/common.py": tc.fragment})
		doc := docOf(t, entry(t, set, "module", "m"))
		if !reflect.DeepEqual(strs(doc["extends_documentation_fragment"]), []string{tc.frag}) || len(named(doc["options"])) != 1 {
			t.Errorf("%s: not merged %v, options %v", name, doc["extends_documentation_fragment"], doc["options"])
		}
		if got := codes(set); tc.code == "" && len(got) != 0 || tc.code != "" && !reflect.DeepEqual(got, []string{tc.code}) {
			t.Errorf("%s: warnings %v, want %q", name, set.Warnings, tc.code)
		}
	}
}

// TestR_ANS_051_OneFragmentServesManyEntries: two modules extend one fragment; each gets the fragment's option, and the second module's own
// option of the same name does not change the first's. Unit, under a millisecond.
func TestR_ANS_051_OneFragmentServesManyEntries(t *testing.T) {
	set := build(t, docgen.DefaultLimits(), "", map[string]string{
		"plugins/doc_fragments/conn.py": "class ModuleDocFragment(object):\n\n    # shared\n    DOCUMENTATION = r'''\noptions:\n  url: {type: str, description: The URL.}\n'''\n",
		"plugins/modules/a.py":          "DOCUMENTATION = '''\nmodule: a\nextends_documentation_fragment: acme.tools.conn\n'''\n",
		"plugins/modules/b.py":          "DOCUMENTATION = '''\nmodule: b\nextends_documentation_fragment: [acme.tools.conn]\noptions:\n  url: {description: B's URL.}\n'''\n",
	})
	a, b := named(docOf(t, entry(t, set, "module", "a"))["options"]), named(docOf(t, entry(t, set, "module", "b"))["options"])
	if !reflect.DeepEqual(strs(a["url"]["description"]), []string{"The URL."}) || a["url"]["type"] != "str" {
		t.Errorf("a = %v", a)
	}
	if !reflect.DeepEqual(strs(b["url"]["description"]), []string{"B's URL."}) || b["url"]["type"] != "str" {
		t.Errorf("b = %v", b)
	}
}

// TestR_ANS_052_ARolesArgumentSpecsAreItsEntryPoints: the role's entry points are those ansible-doc reads, with their short description,
// description and options, nested options included; the role's short description is main's; the search finds an entry point's option.
// Unit, about a millisecond.
func TestR_ANS_052_ARolesArgumentSpecsAreItsEntryPoints(t *testing.T) {
	src := readFixture(t, "roles/web/meta/argument_specs.yml", "ansible-doc-role.json")
	set := build(t, docgen.DefaultLimits(), "", map[string]string{
		"roles/web/meta/argument_specs.yml": src["roles/web/meta/argument_specs.yml"], "roles/web/README.md": "# web\n",
	})
	e := entry(t, set, "role", "web")
	if !e.HasDocs || e.ShortDescription != "Installs the web server" || e.ReadmeMD != "# web\n" || !strings.Contains(e.SearchText, "web_graceful") {
		t.Fatalf("role = %+v", e)
	}
	var ad struct {
		EntryPoints map[string]map[string]any `json:"entry_points"`
	}
	if err := json.Unmarshal([]byte(src["ansible-doc-role.json"]), &ad); err != nil {
		t.Fatal(err)
	}
	eps, _ := docOf(t, e)["entry_points"].([]any)
	if len(eps) != len(ad.EntryPoints) {
		t.Fatalf("entry points = %v", eps)
	}
	var compare func(where string, ours, theirs any)
	compare = func(where string, ours, theirs any) {
		o := named(ours)
		th, _ := theirs.(map[string]any)
		if len(o) != len(th) {
			t.Errorf("%s: options %v, ansible-doc %v", where, ours, theirs)
		}
		for name, spec := range th {
			sameOption(t, where+"."+name, o[name], spec.(map[string]any))
			compare(where+"."+name, o[name]["suboptions"], spec.(map[string]any)["options"])
		}
	}
	for _, x := range eps {
		ep := x.(map[string]any)
		name := ep["name"].(string)
		want := ad.EntryPoints[name]
		for _, k := range []string{"short_description", "version_added"} {
			if ep[k] != want[k] {
				t.Errorf("%s.%s = %v, ansible-doc %v", name, k, ep[k], want[k])
			}
		}
		if !reflect.DeepEqual(strs(ep["description"]), strs(want["description"])) || !reflect.DeepEqual(strs(ep["author"]), strs(want["author"])) {
			t.Errorf("%s: %v", name, ep)
		}
		compare(name, ep["options"], want["options"])
	}
}

// TestR_ANS_052_WhereARoleKeepsItsArgumentSpecs: argument_specs.yml wins over meta/main.yml, meta/main.yml's key argument_specs is read
// without it, a main.yml without the key gives no entry points, and refused YAML keeps the README with a warning. Unit, under a millisecond.
func TestR_ANS_052_WhereARoleKeepsItsArgumentSpecs(t *testing.T) {
	spec := func(short string) string {
		return "argument_specs:\n  main:\n    short_description: " + short + "\n    options:\n      x: {type: str}\n"
	}
	set := build(t, docgen.DefaultLimits(), "", map[string]string{
		"roles/both/meta/argument_specs.yml": spec("from the specs"), "roles/both/meta/main.yml": "galaxy_info: {}\n" + spec("from main"),
		"roles/main/meta/main.yml":           "galaxy_info: {author: x}\n" + spec("from main"),
		"roles/none/meta/main.yml":           "galaxy_info: {author: x}\n",
		"roles/bad/meta/argument_specs.yaml": "argument_specs: &a {main: {}}\nx: *a\n", "roles/bad/README.md": "# bad\n",
	})
	if e := entry(t, set, "role", "both"); e.ShortDescription != "from the specs" {
		t.Errorf("both = %+v", e)
	}
	if e := entry(t, set, "role", "main"); !e.HasDocs || e.ShortDescription != "from main" {
		t.Errorf("main = %+v", e)
	}
	if e := entry(t, set, "role", "none"); e.HasDocs {
		t.Errorf("none = %+v", e)
	}
	if e := entry(t, set, "role", "bad"); e.HasDocs || e.ReadmeMD != "# bad\n" || !reflect.DeepEqual(codes(set), []string{docgen.CodeYAMLRefused}) {
		t.Errorf("bad = %+v, warnings %v", e, set.Warnings)
	}
}

const changelogYAML = `ancestor: null
releases:
  1.0.0:
    changes:
      release_summary: First release.
    modules:
    - description: Pings a host
      name: ping_host
      namespace: ''
    release_date: '2026-01-10'
  1.10.0:
    changes:
      breaking_changes:
      - ping_host - the option ` + "``timeout``" + ` counts seconds (https://example.com/pr/9).
      bugfixes:
      - ping_host - a <script>alert(1)</script> in an entry *stays* text.
      major_changes:
      - The collection needs ansible-core 2.15.
      minor_changes:
      - Many small things, see ` + "`the guide <https://example.com/guide>`_" + ` and :ref:` + "`porting <porting_guide>`" + `.
      release_summary: Feature release.
      trivial:
      - not shown
      custom_section:
      - A section of the collection's own configuration.
    objects:
      role:
      - description: Installs the web server
        name: web
        namespace: null
    plugins:
      lookup:
      - description: Reads a value
        name: value
        namespace: null
  1.2.0:
    changes:
      deprecated_features:
      - ping_host - the option ` + "``wait``" + ` goes.
      known_issues:
      - None known.
      removed_features:
      - The module ` + "``old``" + `.
      security_fixes:
      - ping_host - no_log on the token.
`

// TestR_ANS_053_TheChangelogIsRenderedByVersionAndCategory: changelog.yaml becomes Markdown, the newest version first (1.10.0 before 1.2.0),
// with the categories in antsibull-changelog's order and titles, the collection's own section after them, trivial left out, the plugins,
// modules and roles a release added by their full names, an entry's literal as code, its links as links, and every other character
// escaped, so a tag is text. Unit, about a millisecond.
func TestR_ANS_053_TheChangelogIsRenderedByVersionAndCategory(t *testing.T) {
	set := build(t, docgen.DefaultLimits(), "", map[string]string{"changelogs/changelog.yaml": changelogYAML, "CHANGELOG.rst": "ignored\n"})
	c := set.Changelog
	if c == nil || c.Source != "changelogs/changelog.yaml" || c.Format != rows.DocFormatMarkdown || c.Truncated {
		t.Fatalf("changelog = %+v", c)
	}
	var heads []string
	for _, l := range strings.Split(c.Body, "\n") {
		if strings.HasPrefix(l, "#") {
			heads = append(heads, l)
		}
	}
	want := []string{
		`## v1\.10\.0`, "### Release Summary", "### Major Changes", "### Minor Changes", "### Breaking Changes / Porting Guide", "### Bugfixes",
		"### Custom section", "### New Plugins", "#### Lookup", "### New Roles",
		`## v1\.2\.0`, "### Deprecated Features", "### Removed Features (previously deprecated)", "### Security Fixes", "### Known Issues",
		`## v1\.0\.0`, "### Release Summary", "### New Modules",
	}
	if !reflect.DeepEqual(heads, want) {
		t.Errorf("headings =\n%s", strings.Join(heads, "\n"))
	}
	for _, s := range []string{
		"\\<script\\>alert\\(1\\)\\<\\/script\\>", "\\*stays\\*", "` timeout `", "<https://example.com/pr/9>",
		"[the guide](<https://example.com/guide>)", " porting\\.", "- acme\\.tools\\.value \\- Reads a value", "- acme\\.tools\\.web \\- Installs",
		"- acme\\.tools\\.ping\\_host \\- Pings a host",
	} {
		if !strings.Contains(c.Body, s) {
			t.Errorf("the body lacks %q:\n%s", s, c.Body)
		}
	}
	if strings.Contains(c.Body, "not shown") || strings.Contains(c.Body, "<script>") {
		t.Errorf("body:\n%s", c.Body)
	}
}

// TestR_ANS_053_TheChangelogsSourcesAndBounds: without changelog.yaml a CHANGELOG.md is Markdown as it is, a changelog.rst (any case) text;
// refused YAML falls through to the next source with a warning; a body past its bound is cut after a whole version or line with Truncated,
// and a member past the read bound is cut, never refused. Unit, about a millisecond, with the bounds lowered to kilobytes.
func TestR_ANS_053_TheChangelogsSourcesAndBounds(t *testing.T) {
	if c := build(t, docgen.DefaultLimits(), "", map[string]string{"CHANGELOG.md": "# Changes\n\n- one\n", "Changelog.rst": "x\n"}).Changelog; c == nil ||
		c.Source != "CHANGELOG.md" || c.Format != rows.DocFormatMarkdown || c.Body != "# Changes\n\n- one\n" {
		t.Errorf("md = %+v", c)
	}
	if c := build(t, docgen.DefaultLimits(), "", map[string]string{"Changelog.rst": "v1\n==\n"}).Changelog; c == nil || c.Format != rows.DocFormatText || c.Body != "v1\n==\n" {
		t.Errorf("rst = %+v", c)
	}
	set := build(t, docgen.DefaultLimits(), "", map[string]string{"changelogs/changelog.yaml": "releases: &a {}\nx: *a\n", "CHANGELOG.md": "# md\n"})
	if set.Changelog == nil || set.Changelog.Source != "CHANGELOG.md" || !reflect.DeepEqual(codes(set), []string{docgen.CodeYAMLRefused}) {
		t.Errorf("refused yaml: %+v %v", set.Changelog, set.Warnings)
	}
	if c := build(t, docgen.DefaultLimits(), "", map[string]string{"docs/x.md": "# x\n"}).Changelog; c != nil {
		t.Errorf("none = %+v", c)
	}

	defer docgen.SetChangelogBounds(2048, 600)()
	c := build(t, docgen.DefaultLimits(), "", map[string]string{"changelogs/changelog.yaml": changelogYAML}).Changelog
	if c == nil || !c.Truncated || !strings.HasPrefix(c.Body, `## v1\.10\.0`) || strings.Contains(c.Body, "v1\\.2\\.0") || len(c.Body) > 600 {
		t.Errorf("cut yaml = %+v", c)
	}
	c = build(t, docgen.DefaultLimits(), "", map[string]string{"CHANGELOG.md": strings.Repeat("- a line of the changelog\n", 200)}).Changelog
	if c == nil || !c.Truncated || len(c.Body) > 600 || !strings.HasSuffix(c.Body, "changelog\n") {
		t.Errorf("cut md: %d bytes, truncated %v", len(c.Body), c.Truncated)
	}
	set = build(t, docgen.DefaultLimits(), "", map[string]string{"changelogs/changelog.yaml": changelogYAML + strings.Repeat("# pad\n", 400), "CHANGELOG.rst": "rst\n"})
	if set.Changelog == nil || set.Changelog.Source != "CHANGELOG.rst" || !reflect.DeepEqual(codes(set), []string{docgen.CodeChangelogTooLarge}) {
		t.Errorf("yaml past the read bound: %+v %v", set.Changelog, set.Warnings)
	}
}

// TestR_ANS_053_AChangelogReaderAloneReadsTheChangelogOfAnArchive: the tap a remote's fill uses wants only the changelog members.
// Unit, microseconds.
func TestR_ANS_053_AChangelogReaderAloneReadsTheChangelogOfAnArchive(t *testing.T) {
	r := docgen.NewChangelogReader()
	for _, p := range []string{"plugins/modules/m.py", "README.md", "docs/changelog.md", "roles/r/CHANGELOG.md"} {
		if _, ok := r.Wants(p); ok {
			t.Errorf("wants %s", p)
		}
	}
	if _, ok := r.Wants("changelogs/changelog.yaml"); !ok {
		t.Fatal("does not want changelog.yaml")
	}
	r.Take("changelogs/changelog.yaml", []byte(changelogYAML))
	if c := r.Changelog("acme.tools"); c == nil || !strings.Contains(c.Body, "acme\\.tools\\.web") {
		t.Errorf("changelog = %+v", c)
	}
}
