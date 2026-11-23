package proxy

import (
	"net/http"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
)

// TestR_OCI_034_TheTokenRequestsOfAHandshakeAreCountedByOutcome: every request the handshake sends to an upstream's token service
// counts once in quader_oci_upstream_token_requests_total under the repository and its outcome: ok for a token, denied for the
// service's 401 or 403, failed for any other answer. A token the replica holds costs no request and counts nothing.
func TestR_OCI_034_TheTokenRequestsOfAHandshakeAreCountedByOutcome(t *testing.T) {
	w := newHSWorld(t)
	count := func(outcome string) float64 {
		var m dto.Metric
		if err := w.e.m.tokens.WithLabelValues(w.rem.Repo.Name.String(), outcome).Write(&m); err != nil {
			t.Fatal(err)
		}
		return m.GetCounter().GetValue()
	}
	a := &hsAuth{key: "repository:a:pull", ttl: 5 * time.Minute}
	mustGet(t, w, "/v2/a/blobs/1", a)
	mustGet(t, w, "/v2/a/blobs/2", a) // the token the replica holds
	if ok := count(tokenOK); ok != 1 {
		t.Fatalf("ok = %v after one token request and one held token, want 1", ok)
	}
	for _, c := range []struct {
		status  int
		outcome string
	}{
		{http.StatusUnauthorized, tokenDenied},
		{http.StatusForbidden, tokenDenied},
		{http.StatusInternalServerError, tokenFailed},
		{http.StatusTooManyRequests, tokenFailed},
	} {
		before, hits := count(c.outcome), w.realmHits.Load()
		w.realmStatus = c.status
		if _, err := w.get("/v2/b/blobs/1", &hsAuth{key: "repository:b:pull"}); err == nil {
			t.Fatalf("the token service answered %d and the fetch succeeded", c.status)
		}
		sent := w.realmHits.Load() - hits
		if got := count(c.outcome); sent == 0 || got != before+float64(sent) {
			t.Errorf("a token service answering %d to %d requests: %s = %v, want %v", c.status, sent, c.outcome, got, before+float64(sent))
		}
	}
	if ok := count(tokenOK); ok != 1 {
		t.Errorf("ok = %v after refused token requests, want it unchanged at 1", ok)
	}
}
