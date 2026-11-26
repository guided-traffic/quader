package catalog

import "strings"

// NamespaceKind names what a format calls the part of a package name that groups packages (api-and-ui.md R-UI-220, Q-UI-1290): the
// namespace of an Ansible collection, the scope of an npm package, a path prefix of an OCI image. The screens say the word.
type NamespaceKind string

// The kinds.
const (
	// NamespaceNamespace: the part before the one dot of the name (Ansible Galaxy, "acme" of "acme.tools").
	NamespaceNamespace NamespaceKind = "namespace"
	// NamespaceScope: the part before the one slash of a name that starts with "@" (npm, "@acme" of "@acme/ui"); an unscoped name has
	// none.
	NamespaceScope NamespaceKind = "scope"
	// NamespacePath: every path before a slash of the name (OCI, "team" and "team/app" of "team/app/image").
	NamespacePath NamespaceKind = "path"
)

// NamespaceRule is how a format's package names group into namespaces (R-UI-220): nil in [Facts] for a format whose names have no
// such part (NuGet, Debian, generic files, Q-UI-1290). A namespace is a name's prefix that ends before Separator; Marker is what a
// namespace starts with ("@" of an npm scope, "" for none); Nested says that every level of a name is a namespace (an image path), not
// only the first.
type NamespaceRule struct {
	Kind      NamespaceKind
	Separator string
	Marker    string
	Nested    bool
}

// Namespaces returns the namespaces of a normalized package name, outermost first: ["acme"] of "acme.tools", ["@acme"] of
// "@acme/ui", ["team", "team/app"] of "team/app/image", none of a name without one or of a format without a rule.
func (r *NamespaceRule) Namespaces(name string) []string {
	if r == nil || r.Separator == "" || !strings.HasPrefix(name, r.Marker) {
		return nil
	}
	var out []string
	for i := len(r.Marker); i < len(name); {
		j := strings.Index(name[i:], r.Separator)
		if j <= 0 {
			break
		}
		end := i + j
		out = append(out, name[:end])
		if !r.Nested {
			break
		}
		i = end + len(r.Separator)
	}
	return out
}

// Prefix is the prefix every package name in the namespace starts with, the namespace and its separator ("acme." of "acme",
// "@acme/" of "@acme", "team/app/" of "team/app"); ok is false for what is no namespace of the rule: an empty text, a text without
// the marker, a text that holds the separator where the rule is not nested, or one that starts or ends with it.
func (r *NamespaceRule) Prefix(namespace string) (string, bool) {
	if r == nil || r.Separator == "" || !strings.HasPrefix(namespace, r.Marker) || len(namespace) <= len(r.Marker) {
		return "", false
	}
	rest := namespace[len(r.Marker):]
	if strings.HasPrefix(rest, r.Separator) || strings.HasSuffix(rest, r.Separator) {
		return "", false
	}
	if strings.Contains(rest, r.Separator) && (!r.Nested || strings.Contains(rest, r.Separator+r.Separator)) {
		return "", false
	}
	return namespace + r.Separator, true
}
