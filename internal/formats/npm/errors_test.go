package npm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// TestR_REPO_072_APlaceholdersSentenceIsTheErrorTheNpmClientPrints: the 404 of a name a placeholder would have been asked for keeps
// npm's status and code, and its sentence is the error and the message npm prints (R-NPM-005).
func TestR_REPO_072_APlaceholdersSentenceIsTheErrorTheNpmClientPrints(t *testing.T) {
	sentence := repository.PlaceholderSentence(qname.Repo{Project: wellknown.ProjectKey, Key: "npmjs"}, wellknown.StateNotSwitchedOn, 0)
	w := httptest.NewRecorder()
	New().WriteError(w, httptest.NewRequest(http.MethodGet, "/api/npm/web/npm-all/left-pad", nil),
		repository.PlaceholderNotFound(qname.Repo{Project: wellknown.ProjectKey, Key: "npmjs"}, wellknown.StateNotSwitchedOn, 0))
	var b errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil || w.Code != http.StatusNotFound {
		t.Fatalf("status %d body %s: %v", w.Code, w.Body.String(), err)
	}
	if b.Error != sentence || b.Message != sentence || b.Code != "not_found" {
		t.Fatalf("body = %+v, want the sentence %q", b, sentence)
	}
}
