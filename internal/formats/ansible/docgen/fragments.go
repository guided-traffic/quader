package docgen

import (
	"sort"
	"strings"
)

// The documentation fragments of the collection (R-ANS-051, Q-ANS-96): an entry's extends_documentation_fragment names fragments, and a
// fragment of the collection being published is merged into the entry as ansible-doc merges it (ansible/utils/plugin_docs.py,
// add_fragments and merge_fragment of ansible-core): the entry's own option wins key by key over the fragment's option of the same name and
// keeps its place, an option only the fragment has is appended, notes and see-also are appended, requirements become the sorted union,
// attributes merge like options. A fragment is the attribute DOCUMENTATION of the class ModuleDocFragment of plugins/doc_fragments/<name>.py,
// or the attribute an entry names after a last dot, upper-cased ("ns.coll.files.validate" reads VALIDATE of ns.coll.files); the attribute
// is read as the literal it is, never evaluated. A fragment of ansible.builtin, of another collection or one that cannot be read stays in
// extends_documentation_fragment, the list of fragments that were not merged (H-891).

// CodeFragmentNotRead is the warning for a fragment of the collection that an entry names and that could not be merged: its attribute is
// missing or is no literal, its YAML is refused, or it holds neither options nor attributes, which ansible-doc refuses too.
const CodeFragmentNotRead = "docs_fragment_not_read"

// fragment is a shaped fragment: the members merge reads, and the words it adds to an entry's search text.
type fragment struct {
	doc   map[string]any
	words []string
	ok    bool
}

// fragmentOf resolves an entry of extends_documentation_fragment against the collection's fragments; ok is false for a fragment that is
// not the collection's or that cannot be read (a warning says why, once per fragment).
func (g *Generator) fragmentOf(slug string) (map[string]any, []string, bool) {
	rest, own := strings.CutPrefix(slug, g.collection+".")
	if g.collection == "" || !own {
		return nil, nil, false
	}
	name, attr := rest, "DOCUMENTATION"
	it, found := g.items["doc_fragments\x00"+name]
	if !found {
		if i := strings.LastIndexByte(rest, '.'); i > 0 {
			name, attr = rest[:i], strings.ToUpper(rest[i+1:])
			it, found = g.items["doc_fragments\x00"+name]
		}
	}
	if !found {
		return nil, nil, false
	}
	key := name + "\x00" + attr
	if f, done := g.fragments[key]; done {
		return f.doc, f.words, f.ok
	}
	f := g.readFragment(it, attr)
	g.fragments[key] = f
	return f.doc, f.words, f.ok
}

func (g *Generator) readFragment(it *item, attr string) fragment {
	p := it.path + ".py"
	if it.cut {
		return fragment{}
	}
	lit, have := it.lits[attr]
	switch {
	case !have:
		g.warns.add(p, CodeFragmentNotRead, "the fragment has no attribute "+attr+" whose value is a string literal, so it is not merged")
		return fragment{}
	case lit.status == litTooLarge:
		g.warns.add(p, CodeLiteralTooLarge, attr+" is larger than the bound of a literal")
		return fragment{}
	case lit.status == litUnterminated:
		g.warns.add(p, CodeLiteralUnterminate, attr+" does not close")
		return fragment{}
	}
	n, err := parseYAML(lit.text)
	if err != nil {
		g.warns.add(p, CodeYAMLRefused, attr+": "+err.Error())
		return fragment{}
	}
	var w searchWords
	doc := map[string]any{}
	if o := optionList(member(n, "options"), &w, 0, "suboptions"); o != nil {
		doc["options"] = o
	}
	if a := member(n, "attributes"); a != nil && a.Kind == mappingKind {
		doc["attributes"] = generic(a, 0)
	}
	// ansible-doc asks for the keys, not for content: a fragment of notes alone writes "options: {}".
	if member(n, "options") == nil && member(n, "attributes") == nil {
		g.warns.add(p, CodeFragmentNotRead, attr+" holds neither options nor attributes, so it is not merged")
		return fragment{}
	}
	for _, f := range []string{"notes", "requirements"} {
		if v := textList(member(n, f)); v != nil {
			doc[f] = v
		}
	}
	if s := seeAlso(member(n, "seealso")); s != nil {
		doc["seealso"] = s
	}
	return fragment{doc: doc, words: w.parts, ok: true}
}

// mergeFragments merges the collection's fragments an entry names into its shaped documentation, in the order named, and leaves the names
// that were not merged in extends_documentation_fragment.
func (g *Generator) mergeFragments(doc map[string]any, w *searchWords) {
	names, _ := doc["extends_documentation_fragment"].([]string)
	var rest []string
	for _, slug := range names {
		frag, words, ok := g.fragmentOf(slug)
		if !ok {
			rest = append(rest, slug)
			continue
		}
		mergeFragment(doc, frag)
		for _, x := range words {
			w.add(x)
		}
	}
	if rest == nil {
		delete(doc, "extends_documentation_fragment")
	} else {
		doc["extends_documentation_fragment"] = rest
	}
}

// mergeFragment merges one shaped fragment into doc; nothing of frag is changed, since one fragment serves many entries.
func mergeFragment(doc, frag map[string]any) {
	if o, ok := frag["options"].([]any); ok {
		doc["options"] = mergeNamed(asList(doc["options"]), o)
	}
	if a, ok := frag["attributes"].(map[string]any); ok {
		doc["attributes"] = mergeMap(asMap(doc["attributes"]), a)
	}
	if v, ok := frag["notes"].([]string); ok {
		own, _ := doc["notes"].([]string)
		doc["notes"] = append(append([]string(nil), own...), v...)
	}
	if v, ok := frag["seealso"].([]any); ok {
		doc["seealso"] = append(append([]any(nil), asList(doc["seealso"])...), v...)
	}
	if v, ok := frag["requirements"].([]string); ok {
		own, had := doc["requirements"].([]string)
		if !had {
			doc["requirements"] = append([]string(nil), v...)
		} else {
			doc["requirements"] = sortedUnion(own, v)
		}
	}
}

// mergeNamed merges a fragment's list of named options into the entry's: an option of both keeps the entry's place and its members, with
// the fragment's members it lacks; an option of the fragment alone is appended.
func mergeNamed(own, frag []any) []any {
	out := append([]any(nil), own...)
	at := map[string]int{}
	for i, x := range out {
		if o, ok := x.(map[string]any); ok {
			if n, ok := o["name"].(string); ok {
				at[n] = i
			}
		}
	}
	for _, x := range frag {
		f, ok := x.(map[string]any)
		if !ok {
			continue
		}
		n, _ := f["name"].(string)
		if i, both := at[n]; both {
			out[i] = overlay(f, out[i].(map[string]any))
			continue
		}
		at[n] = len(out)
		out = append(out, overlay(f, nil))
	}
	return out
}

// mergeMap merges a fragment's attributes into the entry's: an attribute of both is the fragment's members overlaid by the entry's.
func mergeMap(own, frag map[string]any) map[string]any {
	out := make(map[string]any, len(own)+len(frag))
	for k, v := range frag {
		out[k] = v
	}
	for k, v := range own {
		f, fm := out[k].(map[string]any)
		o, om := v.(map[string]any)
		if fm && om {
			out[k] = overlay(f, o)
		} else {
			out[k] = v
		}
	}
	return out
}

// overlay is a copy of base with the members of top written over it.
func overlay(base, top map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(top))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range top {
		out[k] = v
	}
	return out
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// sortedUnion is the sorted set of both lists: what ansible-doc makes of a list the entry and the fragment both hold.
func sortedUnion(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]string(nil), a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
