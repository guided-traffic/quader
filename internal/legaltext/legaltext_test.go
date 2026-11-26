package legaltext

import (
	"strings"
	"testing"
)

// upper stands for a renderer: it marks what it rendered, so the test sees which source a page shows.
func upper(md string) string { return "<p>" + strings.ToUpper(md) + "</p>" }

func TestR_UI_225_ALocaleShowsItsOwnTextElseTheOtherLanguageMarkedWithItsLang(t *testing.T) {
	p := Build(Texts{
		Imprint: Source{En: "imprint", De: "impressum"},
		Privacy: Source{De: "datenschutz"},
	}, upper)
	en, de := p.For("en"), p.For("de")
	if en.Imprint == nil || *en.Imprint != (Page{HTML: "<p>IMPRINT</p>", Lang: "en"}) {
		t.Errorf("en imprint = %+v", en.Imprint)
	}
	if de.Imprint == nil || *de.Imprint != (Page{HTML: "<p>IMPRESSUM</p>", Lang: "de"}) {
		t.Errorf("de imprint = %+v", de.Imprint)
	}
	// English has no privacy notice of its own: the German one stands in, marked de.
	if en.Privacy == nil || *en.Privacy != (Page{HTML: "<p>DATENSCHUTZ</p>", Lang: "de"}) {
		t.Errorf("en privacy = %+v", en.Privacy)
	}
	if de.Privacy == nil || de.Privacy.Lang != "de" {
		t.Errorf("de privacy = %+v", de.Privacy)
	}
	// A locale the interface does not have reads what the first language shows.
	if fr := p.For("fr"); fr.Imprint == nil || fr.Imprint.Lang != "en" {
		t.Errorf("fr = %+v", fr)
	}
}

func TestR_UI_225_APageWithoutAnyTextIsNoneAndBlankIsNoText(t *testing.T) {
	renders := 0
	count := func(md string) string { renders++; return md }
	p := Build(Texts{Imprint: Source{En: " \n\t", De: ""}, Privacy: Source{En: "x"}}, count)
	for _, l := range []string{"en", "de"} {
		s := p.For(l)
		if s.Imprint != nil || s.Privacy == nil || s.Empty() {
			t.Errorf("%s: %+v", l, s)
		}
	}
	if renders != 1 {
		t.Errorf("%d renderings, want each text that is not blank once", renders)
	}
	none := Build(Texts{}, count)
	if !none.For("en").Empty() || !none.For("de").Empty() {
		t.Errorf("no texts: %+v %+v", none.For("en"), none.For("de"))
	}
	var nilPages *Pages
	if !nilPages.For("en").Empty() || nilPages.ETag() == "" {
		t.Error("nil pages must show nothing and still have an ETag")
	}
}

func TestR_UI_225_TheETagFollowsTheTexts(t *testing.T) {
	a := Build(Texts{Imprint: Source{En: "a"}}, upper)
	b := Build(Texts{Imprint: Source{En: "a"}}, upper)
	c := Build(Texts{Imprint: Source{En: "b"}}, upper)
	d := Build(Texts{Imprint: Source{De: "a"}}, upper)
	if a.ETag() != b.ETag() {
		t.Errorf("the same texts: %s and %s", a.ETag(), b.ETag())
	}
	if a.ETag() == c.ETag() || a.ETag() == d.ETag() || !strings.HasPrefix(a.ETag(), `"`) {
		t.Errorf("etags %s %s %s", a.ETag(), c.ETag(), d.ETag())
	}
}
