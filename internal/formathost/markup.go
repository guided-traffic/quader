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

// Signatures implements [formats.Host]: the verdicts the composition root wired (Options.Verdicts), or [formats.NoSignatureVerdicts] for
// a replica without the trust step (signatures.md R-SIG-028).
func (h *Host) Signatures() formats.SignatureVerdicts {
	if h.o.Verdicts == nil {
		return formats.NoSignatureVerdicts
	}
	return h.o.Verdicts
}
