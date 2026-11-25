package upstream

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/repository"
)

// TestR_REPO_024_AnUpstreamsErrorAnswerIsShownWithoutTheCredential: the result names the address the probe asked without its query,
// and carries the upstream's own error answer as text, cut at 4,000 bytes, with every value of the credential replaced, also when
// the upstream echoes the request's Authorization header; a binary answer gives its type and size only.
func TestR_REPO_024_AnUpstreamsErrorAnswerIsShownWithoutTheCredential(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/echo/"):
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("upstream failed; you sent Authorization: " + r.Header.Get("Authorization")))
		case strings.HasPrefix(r.URL.Path, "/long/"):
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(strings.Repeat("é", 3000)))
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte{0xff, 0xfe, 0x00, 0x01})
		}
	}))
	t.Cleanup(srv.Close)
	f := newFixture(t, fixtureOpts{servers: []*httptest.Server{srv}})

	f.seal(181, "s3cr3t-token")
	res := f.clients.TestSaved(t.Context(), repo(181, "echo", 1), settings(srv.URL+"/echo/?sig=abc", repository.AuthBearer))
	if res.Verdict != VerdictUpstreamError || res.Body == nil || res.Body.ContentType != "text/plain; charset=utf-8" ||
		!strings.Contains(res.Body.Text, "upstream failed") || !strings.Contains(res.Body.Text, redacted) {
		t.Fatalf("result %+v, body %+v", res, res.Body)
	}
	if res.ProbeURL == nil || *res.ProbeURL != srv.URL+"/echo/-/ping" {
		t.Fatalf("probe address %v: the query is not named", res.ProbeURL)
	}
	if b, _ := json.Marshal(res); strings.Contains(string(b), "s3cr3t") || strings.Contains(string(b), "sig=abc") {
		t.Fatalf("the result carries the credential or the query: %s", b)
	}

	res = f.clients.TestSaved(t.Context(), repo(182, "long", 1), settings(srv.URL+"/long/", repository.AuthNone))
	if res.Body == nil || !res.Body.Cut || len(res.Body.Text) > maxBodyText || res.Body.Bytes != 6000 || !strings.HasSuffix(res.Body.Text, "é") {
		t.Fatalf("a long answer: cut %v, %d bytes of text, %d read", res.Body.Cut, len(res.Body.Text), res.Body.Bytes)
	}

	res = f.clients.TestSaved(t.Context(), repo(183, "bin", 1), settings(srv.URL+"/bin/", repository.AuthNone))
	if res.Body == nil || res.Body.Text != "" || res.Body.Bytes != 4 || res.Body.ContentType != "application/octet-stream" {
		t.Fatalf("a binary answer: %+v", res.Body)
	}
}

func TestAnswerBodyReplacesABasicPassword(t *testing.T) {
	b := answerBody("application/json", []byte(`{"error":"bad password hunter2 for ci"}`), 40, []string{"Basic Y2k6aHVudGVyMg==", "Y2k6aHVudGVyMg==", "hunter2"})
	if b.Text != `{"error":"bad password [redacted] for ci"}` {
		t.Fatalf("text %q", b.Text)
	}
}
