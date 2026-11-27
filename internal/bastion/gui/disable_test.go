package gui

import (
	"net/http"
	"strings"
	"testing"
)

// TestR_BAS_133_ADisabledReplicasWebInterfaceAnswers503: while the replica applies a disable (R-BAS-133, Q-BAS-514), a page's address
// is the page "Not available" in the Bastion's shape and the visitor's language, 503 with Retry-After 300; the data, robots.txt and
// the sitemap are the problem of the Bastion's own 503 unavailable with the same wait; nothing names a disable, and nothing reads the
// catalogue. Enabled again, the web interface serves. Microseconds, no data.
func TestR_BAS_133_ADisabledReplicasWebInterfaceAnswers503(t *testing.T) {
	r := newReader()
	disabled := true
	h := newGUI(t, r, func(o *Options) { o.Disabled = func() bool { return disabled } })
	for _, c := range []struct{ target, lang, heading, text string }{
		{"/ui/en/", "", "Not available", "Not available at the moment. Try again later."},
		{"/ui/de/projects/acme/packages/npm/@acme/ui", "", "Nicht verfügbar", "Zurzeit nicht verfügbar. Versuchen Sie es später noch einmal."},
		{"/", "de", "Nicht verfügbar", "Zurzeit nicht verfügbar."},
	} {
		rec := do(h, http.MethodGet, c.target, "Accept-Language", c.lang)
		body := rec.Body.String()
		if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "300" ||
			!strings.Contains(body, "<h1>"+c.heading+"</h1>") || !strings.Contains(body, c.text) || !strings.Contains(body, `content="300"`) {
			t.Errorf("%s: %d, Retry-After %q, body %s", c.target, rec.Code, rec.Header().Get("Retry-After"), body)
		}
		if strings.Contains(strings.ToLower(body), "disable") || strings.Contains(strings.ToLower(body), "deaktiv") {
			t.Errorf("%s names the disable", c.target)
		}
	}
	for _, target := range []string{"/ui/data/v1/state", "/robots.txt", "/sitemap.xml"} {
		rec := do(h, http.MethodGet, target)
		if rec.Code != http.StatusServiceUnavailable || problemCode(t, rec) != "unavailable" || rec.Header().Get("Retry-After") != "300" {
			t.Errorf("%s: %d %q %s", target, rec.Code, rec.Header().Get("Retry-After"), rec.Body)
		}
	}
	r.mu.Lock()
	reads := r.reads
	r.mu.Unlock()
	if reads != 0 {
		t.Errorf("the catalogue was read %d times while disabled", reads)
	}
	disabled = false
	if rec := do(h, http.MethodGet, "/ui/data/v1/state"); rec.Code != http.StatusOK {
		t.Errorf("enabled again: %d %s", rec.Code, rec.Body)
	}
}
