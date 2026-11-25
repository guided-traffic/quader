package v1

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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
