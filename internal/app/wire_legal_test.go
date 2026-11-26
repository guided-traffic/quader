package app

import (
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/config"
)

// TestR_UI_225_TheMainInstallationRendersTheLegalPagesUnderItsOwnPolicy: the main installation's renderer instance of the legal
// pages admits no external image origin, whatever ui.readme admits: an image of another site is a link, an image of one of the
// installation's own origins is dropped, a script is gone (H-2303). The full policy is tested once, at the Bastion's package.
func TestR_UI_225_TheMainInstallationRendersTheLegalPagesUnderItsOwnPolicy(t *testing.T) {
	md := "# Impressum\n\n![Logo](https://img.shields.io/badge.svg)\n![Own](https://partners.example.com/files/p/r/x.png)\n\n<script>alert(1)</script>\n"
	p, err := legalPages(config.UILegal{Imprint: config.UILegalText{De: md}}, []string{"https://quader.example.com", "https://partners.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	en := p.For("en")
	if en.Imprint == nil || en.Imprint.Lang != "de" || en.Privacy != nil {
		t.Fatalf("en = %+v", en)
	}
	h := en.Imprint.HTML
	if !strings.Contains(h, `<a href="https://img.shields.io/badge.svg" rel="nofollow noopener noreferrer"><span class="external-image">External image: <span class="external-image-alt">Logo</span></span></a>`) ||
		strings.Contains(h, "<img") || strings.Contains(h, "partners.example.com") || strings.Contains(h, "alert") {
		t.Fatalf("html = %s", h)
	}
}
