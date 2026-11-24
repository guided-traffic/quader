package v1

import (
	"slices"
	"testing"

	_ "github.com/guided-traffic/quader/internal/formats/all" // the formats' presets
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// TestR_CUR_009_AnUnknownLevelNamesTheUpstream: a level page's `unknown` names the upstream by its preset's display name, else by its
// host, and carries the rule, the forbidden characters and the suggestion, null where the format sets none (Q-CUR-165). Microseconds.
func TestR_CUR_009_AnUnknownLevelNamesTheUpstream(t *testing.T) {
	for _, c := range []struct {
		format, upstream, want string
	}{
		{"oci", "https://registry-1.docker.io/", "Docker Hub"},
		{"npm", "https://registry.npmjs.org/", "npm public registry"},
		{"npm", "https://npm.example.org/", "npm.example.org"},
		{"deb", "https://download.docker.com/linux/debian/", "download.docker.com"},
	} {
		if got := upstreamName(c.format, c.upstream); got != c.want {
			t.Errorf("R-CUR-009 upstreamName(%s, %s) = %q, want %q", c.format, c.upstream, got, c.want)
		}
	}
	call := catalogueCall{}
	call.repo.Format, call.t.Upstream = "oci", "https://registry-1.docker.io/"
	if got := unknownLevel(call, nil); got != nil {
		t.Fatalf("R-CUR-009 a known level's unknown is %v, want nil", got)
	}
	got, ok := unknownLevel(call, &discover.UnknownLevel{Rule: discover.NameRuleLowercaseDigits, Forbidden: "-", Suggestion: "guidedtraffic"}).(map[string]any)
	if !ok || got["upstream"] != "Docker Hub" || got["rule"] != "lowercaseDigits" || got["forbidden"] != "-" || got["suggestion"] != "guidedtraffic" {
		t.Fatalf("R-CUR-009 unknown = %v", got)
	}
	bare, ok := unknownLevel(call, &discover.UnknownLevel{}).(map[string]any)
	if !ok || bare["rule"] != nil || bare["forbidden"] != nil || bare["suggestion"] != nil {
		t.Fatalf("R-CUR-009 a bare unknown = %v", bare)
	}
}

// TestR_CUR_007_TheSearchHintsOfTheAPIAreTheCatalogues: the API's enum of `searchHint` lists every code a format may name and no
// other, so that the screen has a sentence for each (Q-CUR-166). Microseconds on the shared parse.
func TestR_CUR_007_TheSearchHintsOfTheAPIAreTheCatalogues(t *testing.T) {
	prop := specDocument(t).Components.Schemas["UpstreamCatalogue"].Value.Properties["searchHint"]
	if prop == nil {
		t.Fatal("R-CUR-007 UpstreamCatalogue has no searchHint")
	}
	var got []string
	for _, v := range prop.Value.Enum {
		if s, ok := v.(string); ok {
			got = append(got, s)
		}
	}
	var want []string
	for _, h := range discover.SearchHints() {
		want = append(want, string(h))
	}
	if !slices.Equal(got, want) {
		t.Fatalf("R-CUR-007 the API's searchHint enum is %v, the catalogue's codes are %v", got, want)
	}
}
