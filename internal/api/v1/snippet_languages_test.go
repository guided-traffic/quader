package v1

import (
	"testing"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/formats"
	_ "github.com/guided-traffic/quader/internal/formats/all" // registers the formats whose templates are checked
)

// TestR_API_030_EverySetupTemplateLanguageIsAValueOfTheEnum: the language of a setup template reaches the client as the
// language of the snippet; a format that declares a language the enum of api/openapi.yaml lacks would answer a body that
// breaks its own contract.
func TestR_API_030_EverySetupTemplateLanguageIsAValueOfTheEnum(t *testing.T) {
	for _, f := range formats.List() {
		for _, tpl := range f.SetupTemplates() {
			if !gen.ClientSnippetLanguage(tpl.Lang).Valid() {
				t.Errorf("%s: template %s has language %q, which the ClientSnippet enum of api/openapi.yaml lacks", f.Descriptor().ID, tpl.ID, tpl.Lang)
			}
		}
	}
}

// TestR_API_030_TheCommandLinesKeepTheirLanguage: PowerShell and cmd reach the client as themselves, so a screen shows a
// command with the prompt and the syntax of its command line (Q-UI-801).
func TestR_API_030_TheCommandLinesKeepTheirLanguage(t *testing.T) {
	for _, l := range []string{"powershell", "cmd", "shell", "xml"} {
		if got := clientSnippet(snippet{ID: "x.install", Tab: "install", Language: l}).Language; string(got) != l {
			t.Errorf("%s reported as %q", l, got)
		}
	}
}
