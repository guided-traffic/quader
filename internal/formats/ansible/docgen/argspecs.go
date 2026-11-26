package docgen

import "encoding/json"

// The argument specs of a role (R-ANS-052, Q-ANS-97): ansible-doc documents a role by the entry points of its argument specs, read from the
// first of meta/argument_specs.yml, .yaml and .json and meta/main.yml, .yaml and .json that exists (ansible-core's RoleMixin, the order of
// C.YAML_FILENAME_EXTENSIONS), under the key argument_specs. Each entry point has a short description, a description and options, whose
// nested options are written as options (not suboptions); the generator shapes an entry point like a plugin's documentation, so the screens
// draw its options with the same tables. The file is YAML read under the refusals of every other YAML the generator reads; nothing in it is
// evaluated, and a role whose specs cannot be read keeps its README and has no entry points, with a warning.

// argSpecFiles are the files below a role's meta/ that can hold its argument specs, in the order ansible-doc tries them.
var argSpecFiles = [...]string{"argument_specs.yml", "argument_specs.yaml", "argument_specs.json", "main.yml", "main.yaml", "main.json"}

// argSpecFile reports whether a role's member below meta/ is one of argSpecFiles.
func argSpecFile(name string) bool {
	for _, f := range argSpecFiles {
		if name == f {
			return true
		}
	}
	return false
}

// entryPointKeys are the text members of an entry point that are kept as they are.
var entryPointKeys = [...]string{"version_added", "version_added_collection"}

// entryPoints reads the argument specs of a role: the shaped entry points, in the order of the file, and the short description of the role,
// which is main's or else the first entry point's. ok is false when the role has no argument specs.
func (g *Generator) entryPoints(role string, w *searchWords) (eps []any, short string, ok bool) {
	files := g.roleSpecs[role]
	for _, f := range argSpecFiles {
		body, have := files[f]
		if !have {
			continue
		}
		p := "roles/" + role + "/meta/" + f
		if int64(len(body)) > g.lim.File {
			g.warns.add(p, CodeFileTooLarge, "the argument specs of the role are larger than the bound of a file")
			return nil, "", false
		}
		n, err := parseYAML(string(body))
		if err != nil {
			g.warns.add(p, CodeYAMLRefused, err.Error())
			return nil, "", false
		}
		names, specs := pairs(member(n, "argument_specs"))
		main := false
		for i, name := range names {
			ep, s := entryPoint(name, specs[i], w)
			if ep == nil {
				continue
			}
			eps = append(eps, ep)
			if name == "main" || !main && len(eps) == 1 {
				short, main = s, name == "main"
			}
		}
		return eps, short, len(eps) > 0
	}
	return nil, "", false
}

// entryPoint shapes one entry point; nil when its spec is not a mapping.
func entryPoint(name string, n *yamlNode, w *searchWords) (map[string]any, string) {
	if n == nil || n.Kind != mappingKind {
		return nil, ""
	}
	ep := map[string]any{"name": name}
	w.add(name)
	short := plainShort(text(member(n, "short_description")))
	if short != "" {
		ep["short_description"] = short
		w.add(PlainText(short))
	}
	for _, f := range entryPointKeys {
		if v := text(member(n, f)); v != "" {
			ep[f] = v
		}
	}
	for _, f := range []string{"description", "notes", "requirements", "author"} {
		if v := textList(member(n, f)); v != nil {
			ep[f] = v
		}
	}
	for _, d := range textList(member(n, "description")) {
		w.add(PlainText(d))
	}
	if o := optionList(member(n, "options"), w, 0, "options"); o != nil {
		ep["options"] = o
	}
	if s := seeAlso(member(n, "seealso")); s != nil {
		ep["seealso"] = s
	}
	if a := member(n, "attributes"); a != nil && a.Kind == mappingKind {
		ep["attributes"] = generic(a, 0)
	}
	return ep, short
}

// roleDoc is the stored documentation of a role with argument specs: doc_strings with the entry points in doc.entry_points.
func roleDoc(eps []any) ([]byte, error) {
	return json.Marshal(map[string]any{"doc": map[string]any{"entry_points": eps}, "return": nil, "examples": nil, "metadata": nil})
}

// roleMember returns the role and the file of a member roles/<role>/meta/<file>; ok is false for any other member.
func roleMember(segs []string) (role, file string, ok bool) {
	if len(segs) != 4 || segs[0] != "roles" || segs[2] != "meta" || !argSpecFile(segs[3]) {
		return "", "", false
	}
	return segs[1], segs[3], true
}
