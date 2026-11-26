package nuget

import (
	"slices"
	"testing"
)

// TestR_API_086_ANuGetVersionDependsOnTheIDsOfItsGroups: the IDs of every framework's group are one list, in lower case, with the range
// as written; an ID without a range has none, and an ID no client asks for in its lower-case form is left out (the table keeps a row
// that two groups repeat once). Unit, a few hundred bytes, microseconds.
func TestR_API_086_ANuGetVersionDependsOnTheIDsOfItsGroups(t *testing.T) {
	got := Dependencies([]byte(`{"dependencyGroups":[
		{"targetFramework":"net8.0","dependencies":[{"id":"Acme.Core","version":"[2.0.0, )"},{"id":"Unranged"},{"id":"Ünicode.Dep","version":"1.0.0"}]},
		{"targetFramework":"netstandard2.0","dependencies":[{"id":"acme.core","version":"[2.0.0, )"}]},
		{"dependencies":[{"id":"Flat.Dep","version":"1.0.0"}]}]}`))
	var lines []string
	for _, d := range got {
		lines = append(lines, d.Kind+"|"+d.Name+"|"+d.Range)
	}
	want := []string{"|acme.core|[2.0.0, )", "|unranged|", "|acme.core|[2.0.0, )", "|flat.dep|1.0.0"}
	if !slices.Equal(lines, want) {
		t.Fatalf("got %q, want %q", lines, want)
	}
	if Dependencies([]byte(`{}`)) != nil {
		t.Fatal("a version without groups depends on nothing")
	}
}
