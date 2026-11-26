package npm

import (
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
)

// TestR_NPM_018_ADeprecationCarriesAMessageOnly: npm deprecates a version with a message of at most 1,024 characters (R-NPM-018,
// R-API-085); NuGet's reasons and alternate package, an empty message (npm's clear, which is the operation's DELETE) and a version left
// out are refused with a sentence that says why.
func TestR_NPM_018_ADeprecationCarriesAMessageOnly(t *testing.T) {
	if msg, err := deprecationMessage(&formats.Deprecation{Message: "  use @acme/ui 2  "}); err != nil || msg != "use @acme/ui 2" {
		t.Errorf("message %q, %v", msg, err)
	}
	if msg, err := deprecationMessage(&formats.Deprecation{Message: strings.Repeat("ü", formats.MaxDeprecationMessage)}); err != nil || msg == "" {
		t.Errorf("1,024 characters of two bytes each: %v", err)
	}
	for name, d := range map[string]*formats.Deprecation{
		"reasons":   {Message: "x", Reasons: []string{"Legacy"}},
		"alternate": {Message: "x", Alternate: &formats.DeprecationAlternate{Package: "other"}},
		"empty":     {Message: "   "},
		"too long":  {Message: strings.Repeat("x", formats.MaxDeprecationMessage+1)},
	} {
		if _, err := deprecationMessage(d); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, _, err := New().Deprecate(t.Context(), nil, formats.DeprecationChange{Package: "@acme/ui", Deprecation: &formats.Deprecation{Message: "x"}}); err == nil ||
		!strings.Contains(err.Error(), "name the version") {
		t.Errorf("a deprecation without a version: %v", err)
	}
}
