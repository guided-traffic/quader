package deb

import (
	"slices"
	"testing"
)

// TestR_API_086_ADebianVersionUsesTheNamesOfItsRelationFields: the five fields of use are five kinds named by the field, every
// alternative of a group is a row, an architecture qualifier is dropped, the version constraint is the range, Provides is kept as
// provided, Conflicts and Breaks name nothing, and the files of two architectures add their names. Unit, two paragraphs, microseconds.
func TestR_API_086_ADebianVersionUsesTheNamesOfItsRelationFields(t *testing.T) {
	got := Dependencies([]string{
		"Package: hello\nVersion: 1.0\nArchitecture: amd64\nPre-Depends: dpkg (>= 1.19)\nDepends: libc6 (>= 2.34), python3:any | python3-minimal\n" +
			"Recommends: mail-transport-agent\nConflicts: hello-old\nBreaks: hello-tools (<< 1.0)\nProvides: hello-world (= 1.0)\nDescription: hi\n",
		"Package: hello\nVersion: 1.0\nArchitecture: arm64\nDepends: libarm\nDescription: hi\n",
		"no paragraph",
	})
	var lines []string
	for _, d := range got {
		mark := ""
		if d.Provides {
			mark = " (provided)"
		}
		lines = append(lines, d.Kind+"|"+d.Name+"|"+d.Range+mark)
	}
	want := []string{
		"Pre-Depends|dpkg|>= 1.19", "Depends|libc6|>= 2.34", "Depends|python3|", "Depends|python3-minimal|",
		"Recommends|mail-transport-agent|", "Provides|hello-world|= 1.0 (provided)", "Depends|libarm|",
	}
	if !slices.Equal(lines, want) {
		t.Fatalf("got %q\nwant %q", lines, want)
	}
}
