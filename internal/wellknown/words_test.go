package wellknown_test

import (
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/gui/page"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// TestR_UI_272_EveryDeclaredRepositoryHasItsWords: every repository the product declares in the project upstream (every format's
// presets and the system repositories) has its words in both languages of the Bastion's crawler text, and the English ones are the
// product's stored text, so a preset added later fails here instead of showing English on a German page. The test sits here, where
// the formats are registered: the Bastion's code imports nothing of the main installation (R-BAS-001). The application's table
// (web/src/app/core/upstream-project.ts) is held to the same keys by upstream-project.spec.ts, and to these words by the German
// file (TestR_UI_272_TheCrawlerTextsMatchTheApplicationsGermanWords).
func TestR_UI_272_EveryDeclaredRepositoryHasItsWords(t *testing.T) {
	en, de := page.TextsOf("en"), page.TextsOf("de")
	decls := wellknown.Declared(nil)
	for _, d := range decls {
		if got := en.WellKnown[d.Key]; got != d.DisplayName {
			t.Errorf("%s: the English words are %q, the product stores %q", d.Key, got, d.DisplayName)
		}
		if de.WellKnown[d.Key] == "" {
			t.Errorf("%s has no German words", d.Key)
		}
	}
	if len(en.WellKnown) != len(decls) || len(de.WellKnown) != len(decls) {
		t.Errorf("the tables name %d and %d repositories, the product declares %d: a key it no longer declares stays in a table",
			len(en.WellKnown), len(de.WellKnown), len(decls))
	}
	if en.UpstreamDescription != wellknown.ProjectDescription {
		t.Errorf("the project's English description is %q, the product stores %q", en.UpstreamDescription, wellknown.ProjectDescription)
	}
	if page.UpstreamProject != wellknown.ProjectKey {
		t.Errorf("the project's key is %q, the product's %q", page.UpstreamProject, wellknown.ProjectKey)
	}
}
