// Package legaltext holds the imprint and the privacy notice an installation serves itself (R-UI-225, ADR-302): the operator's
// Markdown per page and language, rendered once at start by a renderer the caller hands in, and the rule that chooses the text a
// locale shows. The main installation and the Bastion use it alike (R-BAS-100), each with a renderer instance of its own; the
// package imports the standard library only, so it brings nothing of the main installation into the Bastion's role.
package legaltext

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// The languages of the interface, in the order a missing text is looked for in: a locale's own text first, then the others in
// this order (R-UI-225).
var locales = [...]string{"en", "de"}

// Source is one page in the languages of the interface: Markdown, empty where the operator wrote none.
type Source struct {
	En string
	De string
}

func (s Source) of(locale string) string {
	switch locale {
	case "en":
		return s.En
	case "de":
		return s.De
	}
	return ""
}

// Texts are the two pages as the configuration holds them (ui.legal).
type Texts struct {
	Imprint Source
	Privacy Source
}

// Page is a page as a locale shows it: sanitized HTML and the language it is written in, which differs from the locale when the
// locale has no text of its own and another language's stands in.
type Page struct {
	HTML string
	Lang string
}

// Set is what one locale shows: a page that no language has a text for is nil.
type Set struct {
	Imprint *Page
	Privacy *Page
}

// Empty reports whether the locale shows neither page: then no link is shown at all (R-UI-225).
func (s Set) Empty() bool { return s.Imprint == nil && s.Privacy == nil }

// Render turns the operator's Markdown into sanitized HTML. The caller's renderer decides the policy; the one of R-UI-225 is the
// README renderer's sanitizer without external image origins.
type Render func(markdown string) string

// Pages are the rendered pages, held in memory for the life of the process. A change of the texts is a rollout.
type Pages struct {
	byLocale map[string]Set
	etag     string
}

// Build renders every text that is not blank once and decides per locale which one it shows. A text of white space only counts
// as none.
func Build(t Texts, render Render) *Pages {
	p := &Pages{byLocale: make(map[string]Set, len(locales))}
	imprint, privacy := renderAll(t.Imprint, render), renderAll(t.Privacy, render)
	sum := sha256.New()
	for _, l := range locales {
		s := Set{Imprint: choose(imprint, l), Privacy: choose(privacy, l)}
		p.byLocale[l] = s
		for _, pg := range []*Page{s.Imprint, s.Privacy} {
			if pg == nil {
				sum.Write([]byte{0})
				continue
			}
			sum.Write([]byte(pg.Lang + "\x00" + pg.HTML + "\x00"))
		}
	}
	p.etag = `"legal-` + hex.EncodeToString(sum.Sum(nil)[:12]) + `"`
	return p
}

func renderAll(s Source, render Render) map[string]string {
	out := map[string]string{}
	for _, l := range locales {
		if src := s.of(l); strings.TrimSpace(src) != "" {
			out[l] = render(src)
		}
	}
	return out
}

// choose is the text of the locale, else the first other language that has one, else none.
func choose(rendered map[string]string, locale string) *Page {
	if h, ok := rendered[locale]; ok {
		return &Page{HTML: h, Lang: locale}
	}
	for _, l := range locales {
		if h, ok := rendered[l]; ok {
			return &Page{HTML: h, Lang: l}
		}
	}
	return nil
}

// For is what the locale shows. A locale the interface does not have shows what its first language shows.
func (p *Pages) For(locale string) Set {
	if p == nil {
		return Set{}
	}
	if s, ok := p.byLocale[locale]; ok {
		return s
	}
	return p.byLocale[locales[0]]
}

// ETag names the rendered pages of every locale: it changes only with the texts or the renderer, so a reader that polls gets a
// 304 until a rollout changed them.
func (p *Pages) ETag() string {
	if p == nil {
		return `"legal-none"`
	}
	return p.etag
}
