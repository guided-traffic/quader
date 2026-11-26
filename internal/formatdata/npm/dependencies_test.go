package npm

import (
	"slices"
	"strings"
	"testing"
)

// TestR_API_086_AnNPMVersionDependsOnTheNamesOfItsManifestsFields: the four fields that name packages are four kinds, bundleDependencies
// adds none, an alias depends on the package it names, a scoped name is kept as written, and a name the rule refuses or a value that is
// no string is no dependency. Unit, a manifest of a few hundred bytes, microseconds.
func TestR_API_086_AnNPMVersionDependsOnTheNamesOfItsManifestsFields(t *testing.T) {
	got := Dependencies([]byte(`{"name":"app","version":"1.0.0",
		"dependencies":{"@acme/ui":"^1.2.0","left-pad":"1.x","old":"npm:@acme/new@^2.0.0","a/b/c":"1","x%2f":"1","num":5},
		"peerDependencies":{"react":">=18"},
		"optionalDependencies":{"fsevents":"*"},
		"devDependencies":{"jest":"^29"},
		"bundleDependencies":["left-pad"]}`))
	lines := make([]string, len(got))
	for i, d := range got {
		lines[i] = d.Kind + " " + d.Name + " " + d.Range
	}
	slices.Sort(lines)
	want := []string{
		"dependencies @acme/new ^2.0.0", "dependencies @acme/ui ^1.2.0", "dependencies left-pad 1.x",
		"devDependencies jest ^29", "optionalDependencies fsevents *", "peerDependencies react >=18",
	}
	if !slices.Equal(lines, want) {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if Dependencies([]byte(`not json`)) != nil || len(Dependencies([]byte(`{"name":"x"}`))) != 0 {
		t.Fatal("a manifest without dependencies names none")
	}
}
