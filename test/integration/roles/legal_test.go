//go:build integration

package roles

import (
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/auth/session"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_UI_225_TheLegalPagesAnswerEveryCallerOnApi: on a replica of role api, through the production composition root, with no level
// public (the format list answers an anonymous caller 401), the imprint and the privacy notice of ui.legal answer a caller without
// a credential and one with an ended session alike (R-AUTH-037's always-open list, R-UI-225): rendered at start under the README
// sanitizer, the German text standing in for English marked de, the page without a text null, the ETag answered 304. A write stays
// under the hard limit of /api/v1. Every answer is validated against the management API's document.
func TestR_UI_225_TheLegalPagesAnswerEveryCallerOnApi(t *testing.T) {
	in := newInstallation(t)
	api := in.st.Start(t, testenv.ReplicaOptions{Name: "api-a", Role: "api", Env: []string{
		"QUADER_AUTH_BREAK_GLASS_ENABLED=true", "QUADER_AUTH_BREAK_GLASS_PASSWORD_FILE=" + in.pw,
		"QUADER_UI_LEGAL_IMPRINT_DE=# Impressum\n\nBeispiel GmbH\\\nMusterstraße 1\n\n<script>alert(1)</script>\n![Logo](https://cdn.example.net/logo.png)",
	}})
	if got := in.get(api, "/api/v1/formats", ""); got.Status != http.StatusUnauthorized {
		t.Fatalf("the format list without a credential: %d %s, want 401 while no level is public", got.Status, got.Body)
	}
	ended, endedCSRF := in.signIn(api)
	if out := in.st.Do(api, http.MethodPost, "/auth/logout", "", nil, "Cookie", ended, "Origin", origin, session.HeaderCSRF, endedCSRF); out.Status != http.StatusOK && out.Status != http.StatusNoContent {
		t.Fatalf("logout: %d %s", out.Status, out.Body)
	}
	var etag string
	for name, hdr := range map[string][]string{"no credential": nil, "an ended session": {"Cookie", ended}} {
		got := in.get(api, "/api/v1/legal?locale=en", "", hdr...)
		if got.Status != http.StatusOK {
			t.Fatalf("%s: %d %s", name, got.Status, got.Body)
		}
		body := got.JSON(t)
		imprint, _ := body["imprint"].(map[string]any)
		html, _ := imprint["html"].(string)
		if imprint["lang"] != "de" || !strings.Contains(html, "Beispiel GmbH<br/>") || strings.Contains(html, "<script") || strings.Contains(html, "alert") ||
			!strings.Contains(html, `<a href="https://cdn.example.net/logo.png" rel="nofollow noopener noreferrer"><span class="external-image">External image: <span class="external-image-alt">Logo</span></span></a>`) {
			t.Errorf("%s: imprint %v", name, imprint)
		}
		if body["privacy"] != nil {
			t.Errorf("%s: privacy %v, want null", name, body["privacy"])
		}
		etag = got.Header.Get("ETag")
		if etag == "" || got.Header.Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: headers %v", name, got.Header)
		}
	}
	if got := in.get(api, "/api/v1/legal?locale=de", "", "If-None-Match", etag); got.Status != http.StatusNotModified {
		t.Errorf("If-None-Match: %d %s", got.Status, got.Body)
	}
	// A probe: the document has no write of the address, so its answer is not validated against it.
	if got := in.probe(api, http.MethodPost, "/api/v1/legal?locale=de", "", "Origin", origin); got.Status != http.StatusUnauthorized {
		t.Errorf("POST without a credential: %d %s, want the 401 of the hard limit", got.Status, got.Body)
	}
}
