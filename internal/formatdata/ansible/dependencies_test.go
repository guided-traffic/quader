package ansible

import (
	"slices"
	"testing"
)

// TestR_API_086_ACollectionDependsOnTheCollectionsOfItsManifest: the names of the stored map in lower case with their ranges, one list;
// a key that is no collection name and a range that is no string name nothing. Unit, a few bytes, microseconds.
func TestR_API_086_ACollectionDependsOnTheCollectionsOfItsManifest(t *testing.T) {
	got := dependenciesOf([]byte(`{"Community.General":">=7.0.0","ansible.posix":"*","notacollection":"1","acme.tools":5}`))
	var lines []string
	for _, d := range got {
		lines = append(lines, d.Kind+"|"+d.Name+"|"+d.Range)
	}
	slices.Sort(lines)
	if want := []string{"|ansible.posix|*", "|community.general|>=7.0.0"}; !slices.Equal(lines, want) {
		t.Fatalf("got %q, want %q", lines, want)
	}
	if dependenciesOf([]byte(`[]`)) != nil {
		t.Fatal("a column that is no object names nothing")
	}
}
