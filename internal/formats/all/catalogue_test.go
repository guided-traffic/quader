package all

import (
	"fmt"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
)

// TestR_CUR_007_EveryRemoteFormatDeclaresItsCatalogue pins, per format, what its remote adapter answers of the upstream catalogue and
// whether it has a catalogue built (remote-curation.md R-CUR-007, Q-CUR-12, Q-CUR-109): the lookup by exact name is every remote
// format's floor, and search and levels are declared or given a reason. A new format with the class remote, and a catalogue the
// catalogue's package builds, fail here until the table says so; a format without the class declares none.
func TestR_CUR_007_EveryRemoteFormatDeclaresItsCatalogue(t *testing.T) {
	type decl struct {
		search, levels string // "" without; the levels joined by " > "
		built          bool   // the remote adapter implements formats.Catalogue
	}
	want := map[formats.ID]*decl{
		"npm":     {},
		"nuget":   {},
		"oci":     {},
		"deb":     {},
		"ansible": {},
		"generic": nil, // no class remote (R-GENERIC-001)
		"clamav":  nil, // no class remote (R-CLAMAV-001)
	}
	seen := map[formats.ID]bool{}
	for _, f := range formats.List() {
		d := f.Descriptor()
		seen[d.ID] = true
		rule, ok := want[d.ID]
		if !ok {
			t.Errorf("%s is not in this list: declare its upstream catalogue (R-CUR-007)", d.ID)
			continue
		}
		c := d.Catalogue
		if rule == nil {
			if d.Supports(formats.Remote) || c.Lookup || d.VersionCanon != nil {
				t.Errorf("%s is listed without the class remote and declares a catalogue or a canon", d.ID)
			}
			continue
		}
		got := decl{built: formats.CapabilitiesOf(f).Remote.Catalogue != nil}
		if c.Search {
			got.search = "search"
		}
		for i, l := range c.Levels {
			if i > 0 {
				got.levels += " > "
			}
			got.levels += l
		}
		switch {
		case !c.Lookup:
			t.Errorf("%s does not declare the lookup by exact name, the floor of every remote format", d.ID)
		case d.VersionCanon == nil:
			t.Errorf("%s declares no version canon (R-CUR-006)", d.ID)
		case fmt.Sprint(got) != fmt.Sprint(*rule):
			t.Errorf("%s declares %+v, want %+v", d.ID, got, *rule)
		}
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("%s is listed and not registered", id)
		}
	}
}

// TestR_CUR_006_EveryRemoteFormatHasItsVersionCanon: the canonical form of a version per format, the form a delivery rule stores and
// the decision folds into (R-CUR-006): npm and Ansible Galaxy the SemVer text as given, NuGet its URL form (Q-CUR-3), Debian the
// version as the index row has it, OCI the tag as given; a range, a tag where a version is meant, or an empty string is none.
func TestR_CUR_006_EveryRemoteFormatHasItsVersionCanon(t *testing.T) {
	for id, cases := range map[formats.ID]map[string]string{ // input -> canonical, "" for refused
		"npm":     {"4.17.21": "4.17.21", "1.0.0-rc.1+build.5": "1.0.0-rc.1+build.5", "v1.0.0": "", "^1.0.0": "", "latest": "", "": ""},
		"nuget":   {"1.0": "1.0.0", "1.0.0.0": "1.0.0", "01.2.0.0-RC.1+sha.9f2c": "1.2.0-rc.1", "1.0.0-Beta": "1.0.0-beta", "[1.0,2.0)": "", "": ""},
		"oci":     {"latest": "latest", "v1.2.3": "v1.2.3", "sha256:" + fmt.Sprintf("%064d", 0): "", "-bad": "", "": ""},
		"deb":     {"3.5.1-1+deb13u1": "3.5.1-1+deb13u1", "1:2.10-3": "1:2.10-3", "abc": "", "": ""},
		"ansible": {"9.1.0": "9.1.0", "1.0.0-beta.1": "1.0.0-beta.1", "1.0": "", "": ""},
	} {
		f, ok := formats.Lookup(string(id))
		if !ok {
			t.Fatalf("%s is not registered", id)
		}
		canon := f.Descriptor().VersionCanon
		for in, want := range cases {
			got, ok := canon.Canonical(in)
			if want == "" {
				if ok {
					t.Errorf("%s: %q is accepted as %q, want it refused", id, in, got)
				}
				continue
			}
			if !ok || got != want {
				t.Errorf("%s: %q = %q, %v; want %q", id, in, got, ok, want)
			}
			if again, ok := canon.Canonical(got); !ok || again != got {
				t.Errorf("%s: the canon is not idempotent on %q (%q, %v)", id, got, again, ok)
			}
		}
	}
}
