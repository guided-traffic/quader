package all

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
)

// specRows reads the table of R-SIG-002 in docs/spec/signatures.md: the roles each format's row names, by the format's display name.
func specRows(t *testing.T) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile("../../../docs/spec/signatures.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	from := strings.Index(text, "### R-SIG-002 ·")
	through := strings.Index(text, "### R-SIG-003 ·")
	if from < 0 || through < from {
		t.Fatal("the specification has no section R-SIG-002")
	}
	rows := map[string][]string{}
	for _, line := range strings.Split(text[from:through], "\n") {
		cells := strings.Split(line, "|")
		if len(cells) < 8 || strings.TrimSpace(cells[1]) == "Format" || strings.HasPrefix(strings.TrimSpace(cells[1]), "---") {
			continue
		}
		var roles []string
		for _, r := range strings.Split(cells[4], ",") {
			// a role is the code span of the cell's item: "`publisher` (author)" names publisher
			if _, rest, ok := strings.Cut(r, "`"); ok {
				if role, _, ok := strings.Cut(rest, "`"); ok {
					roles = append(roles, role)
				}
			}
		}
		slices.Sort(roles)
		rows[strings.TrimSpace(cells[1])] = roles
	}
	return rows
}

// TestR_SIG_002_EveryFormatDeclaresItsRowOfTheSpecification: every registered format has a row in the table of R-SIG-002, and the
// roles its declaration names are that row's (R-SIG-022: "the declaration of each registered format equals the table's row"). A format
// added to Quader without a row, or a declaration that drifts from it, fails here. Unit, milliseconds.
func TestR_SIG_002_EveryFormatDeclaresItsRowOfTheSpecification(t *testing.T) {
	rows := specRows(t)
	row := map[formats.ID]string{"oci": "OCI", "npm": "npm", "nuget": "NuGet", "deb": "Debian", "ansible": "Ansible Galaxy", "generic": "generic"}
	for _, f := range formats.List() {
		d := f.Descriptor()
		name, ok := row[d.ID]
		if !ok {
			name = d.DisplayName
		}
		want, ok := rows[name]
		if !ok {
			t.Errorf("%s has no row %q in the table of R-SIG-002", d.ID, name)
			continue
		}
		decl := f.Signatures().Declaration()
		var got []string
		for _, k := range decl.Kinds {
			if !slices.Contains(got, k.Role) {
				got = append(got, k.Role)
			}
		}
		slices.Sort(got)
		if decl.None != "" {
			got = nil
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s declares the roles %v, the specification's row %v", d.ID, got, want)
		}
	}
}
