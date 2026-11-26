package npm

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
)

// TestR_BAS_114_AnAuditBodyTooLargeThroughABastionNamesNoMember: an audit body above the bound of the repository that forwards it
// names that repository to the installation's own callers, which for an audit of a virtual repository is a member the caller did not
// address; a caller of a Bastion reads "this repository" (bastion.md R-BAS-114, Q-BAS-387).
func TestR_BAS_114_AnAuditBodyTooLargeThroughABastionNamesNoMember(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/npm/web/npm-all/-/npm/v1/security/advisories/bulk", strings.NewReader(`{"a":["1.0.0"]}`))
	_, err := readAuditBody(r, "web/npm-remote", 4)
	if err == nil {
		t.Fatal("a body above the bound was read")
	}
	own, _ := errs.ProblemFor(r, err, "")
	fwd, _ := errs.ProblemFor(r.WithContext(errs.WithBastionForward(r.Context())), err, "")
	if !strings.Contains(own.Detail, "web/npm-remote") {
		t.Fatalf("an own caller: %+v", own)
	}
	if strings.Contains(fwd.Detail, "npm-remote") || !strings.Contains(fwd.Detail, "this repository accepts") || fwd.Code != errs.CodeNPMTooLarge {
		t.Fatalf("a forward: %+v", fwd)
	}
}
