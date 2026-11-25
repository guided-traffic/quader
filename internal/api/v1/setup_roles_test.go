package v1

import (
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/ansible"
)

// TestR_ANS_056_ARolesInstallCommandIsTheRolesOwnByItsDisplayName: for a role's package "role/<id>" the install snippets are the role's,
// with the package written by its display name and the version after a comma; for a collection, and with no package, they are the
// collection's (R-API-030, formats.SetupTemplate.Packages).
func TestR_ANS_056_ARolesInstallCommandIsTheRolesOwnByItsDisplayName(t *testing.T) {
	in := setupInput{
		Templates: ansible.New().SetupTemplates(), Client: ansible.ClientGalaxy, Credentials: credNone,
		Data: formats.SetupData{RegistryURL: "https://quader.example.com/api/ansible/p/galaxy/"},
	}
	installs := func(in setupInput) []string {
		t.Helper()
		ss, err := renderSnippets(in)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, s := range ss {
			if s.Tab == "install" {
				out = append(out, s.Content)
			}
		}
		return out
	}

	in.Package, in.Version, in.DisplayName = "role/16", "2.0.0", "geerlingguy.pip"
	got := installs(in)
	if len(got) != 2 || got[0] != "ansible-galaxy role install geerlingguy.pip,2.0.0\n" ||
		!strings.Contains(got[1], "roles:\n  - name: geerlingguy.pip\n    version: \"2.0.0\"") {
		t.Errorf("a role's install snippets: %q", got)
	}
	in.Package, in.Version, in.DisplayName = "acme.tools", "1.0.0", ""
	if got := installs(in); len(got) != 2 || got[0] != "ansible-galaxy collection install acme.tools:1.0.0\n" {
		t.Errorf("a collection's install snippets: %q", got)
	}
	in.Package, in.Version = "", ""
	if got := installs(in); len(got) != 2 || !strings.HasPrefix(got[0], "ansible-galaxy collection install ") {
		t.Errorf("the install snippets without a package: %q", got)
	}
}
