package rules

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/qname"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
)

// TestR_BAS_114_ADeliveryRefusalThroughABastionNamesNoMemberAndNoPage: the refusal of a remote's delivery rules (R-CUR-004) names
// the remote that holds the rules and the page to ask on to the installation's own callers; through a virtual repository that remote
// is a member the caller did not address. A caller of a Bastion reads the package, the version and the reason in "this repository"
// (bastion.md R-BAS-114, Q-BAS-387).
func TestR_BAS_114_ADeliveryRefusalThroughABastionNamesNoMemberAndNoPage(t *testing.T) {
	ref := &Refusal{Repo: qname.Repo{Project: "web", Key: "npm-remote"}, Name: "left-pad", Version: "1.3.0", Reason: rc.ReasonNotAllowed}
	err := ref.Err("https://packages.example.com")
	r := httptest.NewRequest(http.MethodGet, "/api/npm/web/npm-all/left-pad", nil)
	own, _ := errs.ProblemFor(r, err, "")
	fwd, _ := errs.ProblemFor(r.WithContext(errs.WithBastionForward(r.Context())), err, "")
	if !strings.Contains(own.Detail, "web/npm-remote") || !strings.Contains(own.Detail, "/ui/projects/web/repositories/npm-remote/upstream") {
		t.Fatalf("an own caller: %+v", own)
	}
	if fwd.Detail != "left-pad 1.3.0 is not allowed in this repository: this repository delivers only what is allowed." ||
		fwd.Code != errs.CodeNotAllowedInRepository || fwd.Status != http.StatusForbidden {
		t.Fatalf("a forward: %+v", fwd)
	}
	if _, ok := RefusalOf(err); !ok {
		t.Fatal("the refusal is no longer the error's cause")
	}
}
