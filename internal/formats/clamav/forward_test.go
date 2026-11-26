package clamav

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
)

// TestR_BAS_114_ALimitRefusalOfTheDefinitionsThroughABastionKeepsItsWait: the endpoint answers a rate limit 503 (R-CLAMAV-005), a
// status whose own text a caller of a Bastion does not read; the refusal names only the caller's own wait, so a caller of a Bastion
// reads it as written (bastion.md R-BAS-114, Q-BAS-386).
func TestR_BAS_114_ALimitRefusalOfTheDefinitionsThroughABastionKeepsItsWait(t *testing.T) {
	text := "The download limit of 100 requests per hour is used up. Try again in 15 minutes."
	err := Rendered(errs.E(errs.RateLimited, errs.CodeRateLimited, text, errs.WithRetryAfter(15*time.Minute)))
	r := httptest.NewRequest(http.MethodGet, "/api/clamav/upstream/clamav/daily.cvd", nil)
	p, _ := errs.ProblemFor(r.WithContext(errs.WithBastionForward(r.Context())), err, "")
	if p.Status != http.StatusServiceUnavailable || p.Detail != text {
		t.Fatalf("a forward: %+v", p)
	}
	if p, _ := errs.ProblemFor(r.WithContext(errs.WithBastionForward(r.Context())), Rendered(errs.E(errs.Unavailable, errs.CodeUnavailable, "The files service is not available on this replica.")), ""); p.Detail != errs.DetailUnavailable {
		t.Fatalf("a forward of another 503: %+v", p)
	}
}
