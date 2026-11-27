package all

import (
	"html"
	"os"
	"regexp"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
)

// TestR_UI_049_EveryGuideHeadingHasAUnitInBothLanguages: a guide's heading is the page's frame and is shown in the reader's language,
// while the text under it stays the format's English (api-and-ui.md R-UI-049, Q-UI-86 as amended on 2026-11-27). The screen words
// each heading by its section's stable ID in `guideTitle` of web/src/app/features/guides/guide-titles.ts, whose English is the
// section's title word for word, and the German file holds its German. A section a format adds or retitles, and an entry left for a
// section no format serves, fail here until the table and the German file say so. Milliseconds, the two files only.
func TestR_UI_049_EveryGuideHeadingHasAUnitInBothLanguages(t *testing.T) {
	table, err := os.ReadFile("../../../web/src/app/features/guides/guide-titles.ts")
	if err != nil {
		t.Fatal(err)
	}
	entry := regexp.MustCompile("(?m)^\\s*'([^']+)': \\$localize`:@@guide\\.title\\.([^:]+):([^`]*)`,$")
	english := map[string]string{}
	for _, m := range entry.FindAllStringSubmatch(string(table), -1) {
		if m[1] != m[2] {
			t.Errorf("the entry %s names the unit guide.title.%s", m[1], m[2])
		}
		english[m[1]] = m[3]
	}
	german := germanUnits(t)
	served := map[string]bool{}
	for _, f := range formats.List() {
		for _, sec := range f.Guide() {
			served[sec.ID] = true
			got, ok := english[sec.ID]
			switch {
			case !ok:
				t.Errorf("%s: the section %s (%q) has no heading in guide-titles.ts", f.Descriptor().ID, sec.ID, sec.Title)
				continue
			case got != sec.Title:
				t.Errorf("%s: the heading of %s says %q in guide-titles.ts, the format %q", f.Descriptor().ID, sec.ID, got, sec.Title)
			}
			if de := german["guide.title."+sec.ID]; de == "" {
				t.Errorf("%s: messages.de.xlf has no German heading for %s (%q)", f.Descriptor().ID, sec.ID, sec.Title)
			}
		}
	}
	for id := range english {
		if !served[id] {
			t.Errorf("guide-titles.ts words %s, which no registered format's guide has", id)
		}
	}
}

// germanUnits reads the German targets of the application's translation file by their unit's id.
func germanUnits(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile("../../../web/src/locale/messages.de.xlf")
	if err != nil {
		t.Fatal(err)
	}
	unit := regexp.MustCompile(`<unit id="([^"]+)">[\s\S]*?<target>([\s\S]*?)</target>`)
	out := map[string]string{}
	for _, m := range unit.FindAllStringSubmatch(string(raw), -1) {
		out[m[1]] = html.UnescapeString(m[2])
	}
	return out
}
