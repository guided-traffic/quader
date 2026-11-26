//go:build chart

package chart_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The imprint and the privacy notice (R-UI-225, R-BAS-100): config.ui.legal of both charts is rendered into the configuration file
// as it was given, a change of a text rolls the pods, and a text above 64 KiB is refused by the values schema.

// legalImprint is an operator's imprint as a values file holds it: a block scalar with a hard line break, a table and a link.
const legalImprint = `# Impressum

Example Operations GmbH\
Musterstraße 1

| Register | Nummer |
|---|---|
| Amtsgericht Berlin | HRB 12345 |

Kontakt: <mailto:legal@example.com>
`

// legalValues writes a values file that sets the German imprint and returns its path.
func legalValues(t *testing.T) string {
	t.Helper()
	body := "config:\n  ui:\n    legal:\n      imprint:\n        de: |\n"
	for _, l := range strings.Split(strings.TrimSuffix(legalImprint, "\n"), "\n") {
		body += "          " + l + "\n"
	}
	f := filepath.Join(t.TempDir(), "legal.yaml")
	if err := os.WriteFile(f, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestR_UI_225_TheLegalTextsReachTheConfigurationFile(t *testing.T) {
	long := "config.ui.legal.privacy.en=" + strings.Repeat("x", 65537)
	t.Run("R-UI-225 quader renders config.ui.legal as given, empty by default, and a change rolls the pods", func(t *testing.T) {
		_, docs := render(t, append(required(), legalValues(t)))
		cfg := configOf(t, docs)
		if got := str(cfg, "ui", "legal", "imprint", "de"); got != legalImprint {
			t.Errorf("ui.legal.imprint.de = %q", got)
		}
		if str(cfg, "ui", "legal", "imprint", "en") != "" || str(cfg, "ui", "legal", "privacy", "de") != "" {
			t.Errorf("ui.legal = %v", at(cfg, "ui", "legal"))
		}
		sum := func(files []string) string {
			_, docs := render(t, files)
			return str(find(docs, "Deployment", "quader-api"), "spec", "template", "metadata", "annotations", "checksum/config")
		}
		if a, b := sum(required()), sum(append(required(), legalValues(t))); a == "" || a == b {
			t.Errorf("checksum/config %q and %q: a new text must roll the pods", a, b)
		}
	})
	t.Run("R-UI-225 quader refuses a text above 64 KiB", func(t *testing.T) {
		renderFails(t, required(), []string{long}, "privacy", "maxLength")
	})
	t.Run("R-BAS-100 quader-bastion renders config.ui.legal as given, and a change rolls the pods", func(t *testing.T) {
		_, docs := bstRender(t, append(bstRequired(), legalValues(t)))
		if got := str(bstConfig(t, docs), "ui", "legal", "imprint", "de"); got != legalImprint {
			t.Errorf("ui.legal.imprint.de = %q", got)
		}
		if bstChecksum(t, bstRequired()) == bstChecksum(t, append(bstRequired(), legalValues(t))) {
			t.Error("a new text must roll the Bastion's pods")
		}
	})
	t.Run("R-BAS-100 quader-bastion refuses a text above 64 KiB and a key of the main installation's ui section", func(t *testing.T) {
		bstFails(t, bstRequired(), []string{long}, "privacy", "maxLength")
		bstFails(t, bstRequired(), []string{"config.ui.readme.max_source_bytes=1KiB"}, "readme")
	})
}
