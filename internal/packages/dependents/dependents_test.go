package dependents

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestR_API_086_ARowIsKeptOnceWithinTheBoundsOfTheTable: a repeated row is one, an empty or overlong name and an overlong kind are no
// row, a range is cut at a character, and a version keeps at most maxPerItem rows. Unit, microseconds.
func TestR_API_086_ARowIsKeptOnceWithinTheBoundsOfTheTable(t *testing.T) {
	long := strings.Repeat("ü", maxRange+10)
	got := clean([]Dependency{
		{Name: "a", Range: "1"},
		{Name: " a ", Range: "1"},
		{Name: "a", Range: "1", Provides: true},
		{Name: ""},
		{Name: strings.Repeat("n", maxName+1)},
		{Name: "b", Kind: strings.Repeat("k", maxKind+1)},
		{Name: "c", Range: long},
	})
	if len(got) != 3 || got[0].Name != "a" || !got[1].Provides || got[2].Name != "c" || utf8.RuneCountInString(got[2].Range) != maxRange {
		t.Fatalf("%+v", got)
	}
	many := make([]Dependency, maxPerItem+5)
	for i := range many {
		many[i] = Dependency{Name: "p" + strconv.Itoa(i)}
	}
	if n := len(clean(many)); n != maxPerItem {
		t.Fatalf("%d rows, want %d", n, maxPerItem)
	}
	if !Has("deb") || Has("oci") || Has("generic") {
		t.Fatal("the formats with dependencies are npm, NuGet, Debian and Ansible Galaxy")
	}
}
