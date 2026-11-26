package catalog

import (
	"slices"
	"testing"
)

// TestR_UI_220_NamespacesOfAName: each rule finds the namespaces of a name, outermost first, and the prefix of a namespace.
func TestR_UI_220_NamespacesOfAName(t *testing.T) {
	ansible := &NamespaceRule{Kind: NamespaceNamespace, Separator: "."}
	npm := &NamespaceRule{Kind: NamespaceScope, Separator: "/", Marker: "@"}
	oci := &NamespaceRule{Kind: NamespacePath, Separator: "/", Nested: true}
	var none *NamespaceRule
	for _, c := range []struct {
		rule *NamespaceRule
		name string
		want []string
	}{
		{ansible, "acme.tools", []string{"acme"}},
		{ansible, "acme", nil},
		{npm, "@acme/ui", []string{"@acme"}},
		{npm, "lodash", nil},
		{npm, "@/ui", nil},
		{oci, "team/app/image", []string{"team", "team/app"}},
		{oci, "image", nil},
		{none, "acme.tools", nil},
	} {
		if got := c.rule.Namespaces(c.name); !slices.Equal(got, c.want) {
			t.Errorf("%v of %q: %q, want %q", c.rule, c.name, got, c.want)
		}
	}
	for _, c := range []struct {
		rule *NamespaceRule
		ns   string
		want string
		ok   bool
	}{
		{ansible, "acme", "acme.", true},
		{ansible, "acme.tools", "", false},
		{ansible, "", "", false},
		{npm, "@acme", "@acme/", true},
		{npm, "acme", "", false},
		{npm, "@", "", false},
		{npm, "@acme/ui", "", false},
		{oci, "team/app", "team/app/", true},
		{oci, "team//app", "", false},
		{oci, "/team", "", false},
		{oci, "team/", "", false},
		{none, "acme", "", false},
	} {
		got, ok := c.rule.Prefix(c.ns)
		if got != c.want || ok != c.ok {
			t.Errorf("%v prefix of %q: %q %v, want %q %v", c.rule, c.ns, got, ok, c.want, c.ok)
		}
	}
}
