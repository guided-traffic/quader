package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
)

// putPresets sends a PUT of the registry presets to the handler, with If-Match, as the binder hands it on.
func putPresets(s *Server, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPut, "/api/v1/settings/registry-presets", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("If-Match", `"1"`)
	w := httptest.NewRecorder()
	s.SetRegistryPresets(w, r, gen.SetRegistryPresetsParams{IfMatch: `"1"`})
	return w
}

// TestR_REPO_076_APresetCarriesNoCredential: the body is read against a preset's four members, so a credential member is a 400
// before anything is checked or stored, and a well-known registry's URL is the 422 of the validation at its pointer (R-API-084).
func TestR_REPO_076_APresetCarriesNoCredential(t *testing.T) {
	s := upstreamServer(t, &recordingProber{})
	for name, body := range map[string]string{
		"a password":          `{"presets":[{"format":"npmlike","displayName":"Mine","url":"https://npm.example.com/","extraHosts":[],"password":{"value":"s3cret"}}]}`,
		"an authMode":         `{"presets":[{"format":"npmlike","displayName":"Mine","url":"https://npm.example.com/","extraHosts":[],"authMode":"bearer"}]}`,
		"a username":          `{"presets":[{"format":"npmlike","displayName":"Mine","url":"https://npm.example.com/","extraHosts":[],"username":"ci"}]}`,
		"a member at the top": `{"presets":[],"token":"x"}`,
	} {
		t.Run("R-REPO-076 "+name+" is malformed", func(t *testing.T) {
			w := putPresets(s, body)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"malformed_request"`) {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
		})
	}
	t.Run("R-REPO-076 a well-known registry's URL is refused at its pointer", func(t *testing.T) {
		w := putPresets(s, `{"presets":[{"format":"npmlike","displayName":"Again","url":"https://registry.example.org","extraHosts":[]}]}`)
		var problem struct {
			Code   string                           `json:"code"`
			Errors []struct{ Pointer, Code string } `json:"errors"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusUnprocessableEntity || problem.Code != "validation_failed" || len(problem.Errors) != 1 ||
			problem.Errors[0].Pointer != "/presets/0/url" || problem.Errors[0].Code != "invalid_value" {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	})
	t.Run("R-REPO-076 a body without the list is refused", func(t *testing.T) {
		if w := putPresets(s, `{}`); w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	})
}
