package formathost

import "github.com/guided-traffic/quader/internal/formats"

// Markup implements [formats.Host]: the renderer the composition root wired (Options.Markup), or [formats.NoMarkup] for a replica that
// has not wired it.
func (h *Host) Markup() formats.Markup {
	if h.o.Markup == nil {
		return formats.NoMarkup()
	}
	return h.o.Markup
}

// Signatures implements [formats.Host]: the verdicts the composition root wired (Options.Verdicts; nothing is vouched for without
// them, signatures.md R-SIG-028), and the signature gate of a stored document a format serves without the item store (R-SIG-064).
func (h *Host) Signatures() formats.SignatureVerdicts { return hostSignatures{h: h} }
