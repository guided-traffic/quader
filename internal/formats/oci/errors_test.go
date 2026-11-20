package oci

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// TestR_REPO_072_APlaceholdersSentenceIsTheMessageOfTheRegistrysCode: OCI walks the members of a plan itself, so the plan words its
// not-found answer (formats.VirtualPlan.Unknown); the code stays the registry's own (MANIFEST_UNKNOWN for a manifest, NAME_UNKNOWN for
// the engine's 404) and the sentence is the message docker prints (R-OCI-008).
func TestR_REPO_072_APlaceholdersSentenceIsTheMessageOfTheRegistrysCode(t *testing.T) {
	member := qname.Repo{Project: wellknown.ProjectKey, Key: "docker-hub"}
	sentence := repository.PlaceholderSentence(member, wellknown.StateNotSwitchedOn, 0)
	plan := &formats.VirtualPlan{Admitted: true, NotFound: sentence}
	for _, c := range []struct {
		name string
		err  error
		code string
	}{
		{"a manifest of a walk over the members", plan.Unknown(errManifestUnknown()), codeManifestUnknown},
		{"a blob of a walk over the members", plan.Unknown(errBlobUnknown()), codeBlobUnknown},
		{"the engine's 404", repository.PlaceholderNotFound(member, wellknown.StateNotSwitchedOn, 0), codeNameUnknown},
	} {
		t.Run("R-REPO-072 "+c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			New().WriteError(w, httptest.NewRequest(http.MethodGet, "/v2/web/oci-all/library/alpine/manifests/3", nil), c.err)
			var b struct {
				Errors []registryErrorBody `json:"errors"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil || w.Code != http.StatusNotFound || len(b.Errors) != 1 {
				t.Fatalf("status %d body %s: %v", w.Code, w.Body.String(), err)
			}
			if b.Errors[0].Code != c.code || b.Errors[0].Message != sentence {
				t.Fatalf("error = %+v, want %s with %q", b.Errors[0], c.code, sentence)
			}
		})
	}
	if got := (&formats.VirtualPlan{}).Unknown(errManifestUnknown()); got.Error() != errManifestUnknown().Error() {
		t.Fatalf("a plan without a placeholder changed the error: %v", got)
	}
}
