package v1

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/formats/ansible/rows"
)

// treeStore answers the tree of a version and counts the reads; every other method of the port is not called.
type treeStore struct {
	AnsibleStore
	entries []rows.DocEntryHead
	reads   int
}

func (s *treeStore) DocTree(context.Context, int64, string, string, *rows.DocQuery) (rows.DocTree, error) {
	s.reads++
	return rows.DocTree{Entries: s.entries}, nil
}

// TestR_ANS_048_AnEntryLinksToTheTreeAndToItsOwnOptions: the prose of an entry links M() to an entry of the version's tree, O() to an option
// of the entry or one of another entry, RV() to a return value; a see-also module of the tree carries its entry; the tree is read once, and
// not at all for an entry without a reference to another entry. A hostile M() and an option name with quotes stay code (R-ANS-048).
func TestR_ANS_048_AnEntryLinksToTheTreeAndToItsOwnOptions(t *testing.T) {
	st := &treeStore{entries: []rows.DocEntryHead{{Type: "module", Name: "mount"}, {Type: "lookup", Name: "env"}}}
	d := &docsRead{
		ps: PackageServices{Readme: testReadme(t)}, st: st, ctx: context.Background(), name: "acme.tools",
		state: rows.DocState{Source: rows.DocSourceUpstream},
	}
	doc := `{"doc":{"description":["Use M(acme.tools.mount) with O(src), O(opts.mode=0644) and O(acme.tools.mount#module:fstab); see RV(dev.name) and P(acme.tools.env#lookup).",
	    "M(javascript:alert(1)) O(it\"s) M(acme.tools.gone)"],
	  "options":[{"name":"src","aliases":["source"]},{"name":"opts","suboptions":[{"name":"mode"}]},{"name":"it\"s"}],
	  "seealso":[{"module":"acme.tools.mount"},{"plugin":"acme.tools.env","plugin_type":"lookup"},{"module":"ansible.builtin.copy"}]},
	  "return":[{"name":"dev","contains":[{"name":"name"}]}]}`
	e := entryOf(d, rows.DocEntry{Type: "module", Name: "m", HasDocs: true, Doc: json.RawMessage(doc)})
	desc := strings.Join(*e.Description, "\n")
	for _, want := range []string{
		`<a href="?doc=module/mount"><code>acme.tools.mount</code></a>`,
		`<a href="#parameter-src"><code>src</code></a>`,
		`<a href="#parameter-opts/mode"><code>opts.mode=0644</code></a>`,
		`<a href="?doc=module/mount#parameter-fstab"><code>fstab</code></a>`,
		`<a href="#return-dev/name"><code>dev.name</code></a>`,
		`<a href="?doc=lookup/env"><code>acme.tools.env</code></a>`,
		`<code>acme.tools.gone</code>`,
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("the description lacks %s:\n%s", want, desc)
		}
	}
	if strings.Contains(desc, "javascript") && strings.Contains(desc, `href="javascript`) || strings.Count(desc, "<a ") != 6 {
		t.Errorf("a link that the tree does not hold:\n%s", desc)
	}
	sa := *e.Seealso
	if len(sa) != 3 || sa[0].Entry == nil || *sa[0].Entry != "module/mount" || sa[1].Entry == nil || *sa[1].Entry != "lookup/env" || sa[2].Entry != nil {
		t.Errorf("seealso = %+v", sa)
	}
	if st.reads != 1 {
		t.Errorf("the tree was read %d times, want once", st.reads)
	}
	st.reads = 0
	entryOf(d, rows.DocEntry{Type: "module", Name: "plain", HasDocs: true, Doc: json.RawMessage(`{"doc":{"description":["Only O(src)."],"options":[{"name":"src"}]}}`)})
	if st.reads != 0 {
		t.Error("the tree was read for an entry that names no other entry")
	}
}

// TestR_ANS_049_AnOptionCarriesItsConfigurationAndTheCollectionOfItsVersion: an option's ini, env, vars, keyword and cli become its
// configuration; version_added_collection is passed on, and a generated entry that names none counts in its own collection (R-ANS-049). A
// positional list written as one string is split (R-ANS-050).
func TestR_ANS_049_AnOptionCarriesItsConfigurationAndTheCollectionOfItsVersion(t *testing.T) {
	doc := `{"doc":{"version_added":"1.0.0","positional":"_input, unit","options":[
	  {"name":"sort_order","version_added":"1.2.0","version_added_collection":"other.coll",
	   "ini":[{"section":"callback_timer","key":"sort_order"},"junk"],"env":[{"name":"TIMER_SORT"}],"vars":[{"name":"timer_sort"}],
	   "keyword":[{"name":"timer"}],"cli":[{"name":"sort","option":"--sort"}]},
	  {"name":"plain","version_added":"1.1.0"}]}}`
	gen := &docsRead{ps: PackageServices{Readme: testReadme(t)}, name: "acme.tools", state: rows.DocState{Source: rows.DocSourceGenerated}}
	e := entryOf(gen, rows.DocEntry{Type: "callback", Name: "timer", HasDocs: true, Doc: json.RawMessage(doc)})
	if e.VersionAddedCollection == nil || *e.VersionAddedCollection != "acme.tools" {
		t.Errorf("the entry's collection = %v", e.VersionAddedCollection)
	}
	if e.Positional == nil || strings.Join(*e.Positional, "|") != "_input|unit" {
		t.Errorf("positional = %v", e.Positional)
	}
	o := (*e.Options)[0]
	c := o.Configuration
	if c == nil || c.Ini == nil || len(*c.Ini) != 1 || *(*c.Ini)[0].Section != "callback_timer" || *(*c.Ini)[0].Key != "sort_order" ||
		c.Env == nil || (*c.Env)[0].Name != "TIMER_SORT" || c.Vars == nil || (*c.Vars)[0].Name != "timer_sort" ||
		c.Keyword == nil || (*c.Keyword)[0].Name != "timer" || c.Cli == nil || *(*c.Cli)[0].Option != "--sort" {
		t.Fatalf("configuration = %+v", c)
	}
	if o.VersionAddedCollection == nil || *o.VersionAddedCollection != "other.coll" {
		t.Errorf("the option's collection = %v", o.VersionAddedCollection)
	}
	if p := (*e.Options)[1]; p.Configuration != nil || p.VersionAddedCollection == nil || *p.VersionAddedCollection != "acme.tools" {
		t.Errorf("plain option = %+v", p)
	}
	up := &docsRead{ps: gen.ps, name: "acme.tools", state: rows.DocState{Source: rows.DocSourceUpstream}}
	if u := entryOf(up, rows.DocEntry{Type: "callback", Name: "timer", HasDocs: true, Doc: json.RawMessage(doc)}); u.VersionAddedCollection != nil {
		t.Errorf("an upstream entry that names no collection got %q", *u.VersionAddedCollection)
	}
}

// TestR_ANS_052_ARolesEntryPointsAreTablesWithAnchorsOfTheirOwn: a role's entry points come in the order of the file with their prose as
// sanitized HTML; O() of an entry point's own option links to parameter-<entry point>--<path>, and O() of another role's option to its
// page; an option of another entry point is code (R-ANS-052, H-2102). Unit, about a millisecond.
func TestR_ANS_052_ARolesEntryPointsAreTablesWithAnchorsOfTheirOwn(t *testing.T) {
	st := &treeStore{entries: []rows.DocEntryHead{{Type: "role", Name: "web"}, {Type: "role", Name: "db"}}}
	d := &docsRead{
		ps: PackageServices{Readme: testReadme(t)}, st: st, ctx: context.Background(), name: "acme.tools",
		state: rows.DocState{Source: rows.DocSourceGenerated},
	}
	doc := `{"doc":{"entry_points":[
	  {"name":"main","short_description":"Installs","version_added":"1.2.0","author":["Hans"],
	   "description":["Set O(web_tls.cert) or O(acme.tools.db#role:main:db_port); O(reload_only) is reload's. <script>x</script>"],
	   "options":[{"name":"web_tls","type":"dict","suboptions":[{"name":"cert","type":"path","required":true}]}]},
	  {"name":"reload","options":[{"name":"reload_only","type":"bool","default":true}]}]}}`
	e := entryOf(d, rows.DocEntry{Type: "role", Name: "web", HasDocs: true, Doc: json.RawMessage(doc), ReadmeMD: "# web"})
	if e.EntryPoints == nil || len(*e.EntryPoints) != 2 || e.ReadmeHtml == nil {
		t.Fatalf("entry = %+v", e)
	}
	main, reload := (*e.EntryPoints)[0], (*e.EntryPoints)[1]
	desc := strings.Join(*main.Description, "")
	for _, want := range []string{
		`<a href="#parameter-main--web_tls/cert"><code>web_tls.cert</code></a>`,
		`<a href="?doc=role/db#parameter-main--db_port"><code>main:db_port</code></a>`,
		`<code>reload_only</code>`,
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("main's description lacks %s:\n%s", want, desc)
		}
	}
	if strings.Contains(desc, "<script") || main.Name != "main" || *main.ShortDescription != "Installs" || *main.VersionAddedCollection != "acme.tools" ||
		(*main.Author)[0] != "Hans" {
		t.Errorf("main = %+v, %s", main, desc)
	}
	opts := *main.Options
	if len(opts) != 1 || opts[0].Suboptions == nil || (*opts[0].Suboptions)[0].Name != "cert" || !*(*opts[0].Suboptions)[0].Required {
		t.Errorf("options = %+v", opts)
	}
	if reload.Name != "reload" || reload.Options == nil || (*reload.Options)[0].Default != true {
		t.Errorf("reload = %+v", reload)
	}
}

// TestR_ANS_053_AChangelogInReStructuredTextIsTextInAPreElement: the text is escaped, and a text whose escaped form passes the bound of a
// rendered document is cut on a character. Unit, about a millisecond.
func TestR_ANS_053_AChangelogInReStructuredTextIsTextInAPreElement(t *testing.T) {
	if out, cut := preText("v1\n==\n<b>x</b> & é"); out != "<pre>v1\n==\n&lt;b&gt;x&lt;/b&gt; &amp; é</pre>" || cut {
		t.Errorf("pre = %q, %v", out, cut)
	}
	out, cut := preText(strings.Repeat("é<", 200_000))
	if !cut || len(out) > 1<<20 || !strings.HasSuffix(out, "</pre>") || !utf8.ValidString(out) {
		t.Errorf("%d bytes, cut %v", len(out), cut)
	}
	if !matchesChangelog(&rows.DocQuery{Terms: []string{"Change", "log"}}) || matchesChangelog(&rows.DocQuery{Terms: []string{"news"}}) {
		t.Error("the changelog's title rule")
	}
}
