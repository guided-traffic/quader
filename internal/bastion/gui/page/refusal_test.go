package page

import (
	"encoding/xml"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// bastionFavicon is the Bastion's favicon as web/scripts/brand-logo.mjs writes it from MARKS.bastion, the one source of the tower.
const bastionFavicon = "../../../../web/src/bastion/public/favicon.svg"

// TestR_BAS_123_TheRefusalPageDrawsTheBastionsMarkAsTheGeneratorWritesIt: the refusal page's bar draws the Bastion's tower with the
// stones, the coloured merlon and the two colours of each scheme the generator writes into the Bastion's favicon, and names it
// "Quader Bastion" with "Bastion" in Regular (Q-UI-1617). Reads one file of under a kilobyte; milliseconds.
func TestR_BAS_123_TheRefusalPageDrawsTheBastionsMarkAsTheGeneratorWritesIt(t *testing.T) {
	src, err := os.ReadFile(bastionFavicon)
	if err != nil {
		t.Fatal(err)
	}
	var svg struct {
		Style string `xml:"style"`
		Rects []struct {
			Class string `xml:"class,attr"`
			X     string `xml:"x,attr"`
			Y     string `xml:"y,attr"`
			W     string `xml:"width,attr"`
			H     string `xml:"height,attr"`
		} `xml:"rect"`
	}
	if err := xml.Unmarshal(src, &svg); err != nil {
		t.Fatal(err)
	}
	var stones [][4]int
	var colored [][4]int
	for _, r := range svg.Rects {
		var box [4]int
		for i, v := range []string{r.X, r.Y, r.W, r.H} {
			if box[i], err = strconv.Atoi(v); err != nil {
				t.Fatalf("rect %+v: %v", r, err)
			}
		}
		if r.Class == "c" {
			colored = append(colored, box)
		} else {
			stones = append(stones, box)
		}
	}
	if !slices.Equal(stones, markStones()) || len(colored) != 1 || colored[0] != markColored() {
		t.Errorf("the page's tower is %v with %v, the generator's %v with %v", markStones(), markColored(), stones, colored)
	}

	// The favicon's style: the light scheme's stone and coloured fill, then the dark scheme's under prefers-color-scheme.
	fills := regexp.MustCompile(`\.s\{fill:(#[0-9a-f]{6})\}\.c\{fill:(#[0-9a-f]{6})\}`).FindAllStringSubmatch(svg.Style, -1)
	light, dark, ok := strings.Cut(refusalCSS, "@media (prefers-color-scheme:dark)")
	if len(fills) != 2 || !ok {
		t.Fatalf("style %q, css %q", svg.Style, refusalCSS)
	}
	for _, c := range []struct{ css, stone, accent string }{{light, fills[0][1], fills[0][2]}, {dark, fills[1][1], fills[1][2]}} {
		if !strings.Contains(c.css, "--stone:"+c.stone) || !strings.Contains(c.css, "--accent:"+c.accent) {
			t.Errorf("the page's colours differ from the generator's %s and %s:\n%s", c.stone, c.accent, c.css)
		}
	}

	page := string(NewRenderer().RefusalPage(Refusal{Locale: "en", Seconds: 2}, "n0nce"))
	if !strings.Contains(page, `<rect class="accent" x="24" y="4" width="16" height="16" rx="1.5"></rect>`) ||
		!strings.Contains(page, `<span translate="no">Quader <span class="q">Bastion</span></span>`) ||
		!strings.Contains(refusalCSS, ".product .q{font-weight:400}") {
		t.Errorf("the bar names no Quader Bastion or draws no tower:\n%s", page)
	}
}
