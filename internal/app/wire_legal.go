package app

import (
	"fmt"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/legaltext"
	"github.com/guided-traffic/quader/internal/packages/readme"
)

// legalPages renders the imprint and the privacy notice of ui.legal once, before the listeners open (R-UI-225, ADR-302): a renderer
// instance of its own with the README sanitizer of R-UI-008 and no external image origins, whatever ui.readme admits, so an image
// in an operator's text becomes a link and one of the installation's own origins is dropped. Every endpoint serves the one result.
func legalPages(cfg config.UILegal, own []string) (*legaltext.Pages, error) {
	rd, err := readme.New(readme.Options{MaxSourceBytes: config.LegalMaxBytes, OwnOrigins: own})
	if err != nil {
		return nil, fmt.Errorf("ui.legal: the renderer: %w", err)
	}
	texts := legaltext.Texts{
		Imprint: legaltext.Source{En: cfg.Imprint.En, De: cfg.Imprint.De},
		Privacy: legaltext.Source{En: cfg.Privacy.En, De: cfg.Privacy.De},
	}
	return legaltext.Build(texts, func(md string) string { return rd.Render(md).HTML }), nil
}
