package generic

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// TestR_REPO_072_APlaceholdersSentenceIsTheProblemDetail: the problem details of R-ARCH-023, which the generic format and every answer
// below /files/ render, carry a placeholder's sentence as their detail and keep the status and the code of the 404.
func TestR_REPO_072_APlaceholdersSentenceIsTheProblemDetail(t *testing.T) {
	member := qname.Repo{Project: wellknown.ProjectKey, Key: "npmjs"}
	rec := httptest.NewRecorder()
	Format{}.WriteError(rec, httptest.NewRequest(http.MethodGet, "/files/web/npm-all/left-pad/-/left-pad-1.3.0.tgz", nil),
		repository.PlaceholderNotFound(member, wellknown.StateSwitchedOff, 0))
	var body struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusNotFound {
		t.Fatalf("status %d body %q: %v", rec.Code, rec.Body.String(), err)
	}
	if want := repository.PlaceholderSentence(member, wellknown.StateSwitchedOff, 0); body.Detail != want || body.Code != errs.CodeNotFound {
		t.Fatalf("body = %+v, want the detail %q", body, want)
	}
}
