// Package legal renders the Bastion's imprint and privacy notice at start (R-BAS-100, ADR-302): the texts of its own chart's
// config.ui.legal, under the sanitizer of the README renderer without external image origins and with the Bastion's own origin as
// the one whose images are dropped. Nothing here comes from or names the main installation (R-BAS-001's rule that a Bastion tells
// nothing of it). The pages are held in memory; the Bastion's web interface (a later package) serves them through [Pages.Pages].
// This package serves no route.
package legal

import (
	"fmt"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/legaltext"
	"github.com/guided-traffic/quader/internal/packages/readme"
)

// Pages are the Bastion's rendered legal pages.
type Pages struct{ p *legaltext.Pages }

// New renders the texts of the role's ui.legal. baseURL is the Bastion's server.base_url: an image of that origin is dropped, every
// other image becomes a link (R-UI-225). A renderer the options refuse is an error that stops the start.
func New(cfg config.UILegal, baseURL string) (*Pages, error) {
	rd, err := readme.New(readme.Options{MaxSourceBytes: config.LegalMaxBytes, OwnOrigins: []string{baseURL}})
	if err != nil {
		return nil, fmt.Errorf("the renderer of the legal pages: %w", err)
	}
	texts := legaltext.Texts{
		Imprint: legaltext.Source{En: cfg.Imprint.En, De: cfg.Imprint.De},
		Privacy: legaltext.Source{En: cfg.Privacy.En, De: cfg.Privacy.De},
	}
	return &Pages{p: legaltext.Build(texts, func(md string) string { return rd.Render(md).HTML })}, nil
}

// Pages is what a locale shows: each page's HTML and language, or nil where neither language has a text (R-UI-225).
func (p *Pages) Pages(locale string) legaltext.Set { return p.p.For(locale) }

// ETag names the rendered pages; it changes only with a rollout that changed the texts.
func (p *Pages) ETag() string { return p.p.ETag() }
