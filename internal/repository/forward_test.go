package repository

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// TestR_BAS_114_APlaceholderThroughABastionIsTheOneNotFound: the 404 of a virtual repository whose member of the project upstream
// is not switched on names that member and the administrator's switch to the installation's own callers (R-REPO-072); a caller of a
// Bastion reads the one sentence of every missing resource, since the member is a repository it did not address (bastion.md
// R-BAS-114, Q-BAS-387).
func TestR_BAS_114_APlaceholderThroughABastionIsTheOneNotFound(t *testing.T) {
	err := PlaceholderNotFound(up("npmjs"), wellknown.StateNotSwitchedOn, 1)
	r := httptest.NewRequest(http.MethodGet, "/api/npm/web/npm-all/lodash", nil)
	own, _ := errs.ProblemFor(r, err, "")
	fwd, _ := errs.ProblemFor(r.WithContext(errs.WithBastionForward(r.Context())), err, "")
	if own.Detail != PlaceholderSentence(up("npmjs"), wellknown.StateNotSwitchedOn, 1) {
		t.Fatalf("an own caller: %+v", own)
	}
	if fwd.Detail != errs.DetailNotFound || fwd.Status != http.StatusNotFound || fwd.Code != errs.CodeNotFound {
		t.Fatalf("a forward: %+v", fwd)
	}
}
