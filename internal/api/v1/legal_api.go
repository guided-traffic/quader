package v1

import (
	"net/http"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/legaltext"
)

// GetLegal implements GET /api/v1/legal (R-UI-225): the imprint and the privacy notice as the locale shows them, rendered at start
// (Options.Legal). Always open without a credential (R-AUTH-037); no gate of the public content, because the sign-in screen links to
// the pages. A replica without the pages (a test server) answers both null, as an installation without texts does.
func (s *Server) GetLegal(w http.ResponseWriter, r *http.Request, params gen.GetLegalParams) {
	if !params.Locale.Valid() {
		httpserver.Problem(w, r, errs.E(errs.Invalid, errs.CodeInvalid, "The locale is en or de."))
		return
	}
	etag := s.o.Legal.ETag()
	// The texts change only with a rollout: the browser keeps the answer and asks again with its ETag, which a poll turns into a 304.
	w.Header().Set("Cache-Control", "no-cache")
	if IfNoneMatchHits(r, etag) {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	set := s.o.Legal.For(string(params.Locale))
	w.Header().Set("ETag", etag)
	writeJSONKeepCache(w, r, http.StatusOK, gen.LegalPages{Imprint: legalPage(set.Imprint), Privacy: legalPage(set.Privacy)})
}

func legalPage(p *legaltext.Page) *gen.LegalPage {
	if p == nil {
		return nil
	}
	return &gen.LegalPage{Html: p.HTML, Lang: gen.LegalPageLang(p.Lang)}
}
