package formats

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
)

// TestR_BAS_114_AFormatsOwnWalkThroughABastionReadsItsOwnNotFound: a format that walks the members of a virtual repository itself
// answers a miss of every member with the plan's sentence about a member of the project upstream that is not switched on
// (R-REPO-072); a caller of a Bastion reads the format's own not-found sentence instead, which names no member (bastion.md R-BAS-114,
// Q-BAS-387).
func TestR_BAS_114_AFormatsOwnWalkThroughABastionReadsItsOwnNotFound(t *testing.T) {
	plan := &VirtualPlan{Admitted: true, NotFound: "upstream/nuget-org is a member of this virtual repository, but an administrator of Quader has not switched it on."}
	own := errs.E(errs.NotFound, errs.CodeNotFound, "The package is not in the repository.")
	err := plan.Unknown(own)
	r := httptest.NewRequest(http.MethodGet, "/api/nuget/web/nuget-all/v3/flatcontainer/x/index.json", nil)
	p, _ := errs.ProblemFor(r, err, "")
	f, _ := errs.ProblemFor(r.WithContext(errs.WithBastionForward(r.Context())), err, "")
	if p.Detail != plan.NotFound {
		t.Fatalf("an own caller: %+v", p)
	}
	if f.Detail != own.Detail || f.Status != http.StatusNotFound {
		t.Fatalf("a forward: %+v", f)
	}
	if other := errs.E(errs.Forbidden, errs.CodeForbidden, "no"); !errors.Is(plan.Unknown(other), other) {
		t.Fatal("an error of another kind is not returned as it is")
	}
}
