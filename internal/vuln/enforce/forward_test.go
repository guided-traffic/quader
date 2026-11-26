package enforce

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/vuln"
)

// TestR_BAS_114_ABlockThroughABastionNamesNoPageAndNoFindingOfTheInstallation: the refusal of a blocked version (R-VULN-016)
// names the version, the addressed repository, the reason and the advisory to every caller; a caller of a Bastion reads it without
// the link into the installation's web interface and without the finding's ID (bastion.md R-BAS-114, Q-BAS-387).
func TestR_BAS_114_ABlockThroughABastionNamesNoPageAndNoFindingOfTheInstallation(t *testing.T) {
	d := Download{Addressed: "web/npm-all", Block: vuln.Block{
		Reason: vuln.BlockTriage, FindingID: "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10", AdvisoryID: "GHSA-xxxx-yyyy-zzzz", Enforced: true,
	}}
	err := refusal("https://quader.test", d, "lodash", "4.17.20")
	r := httptest.NewRequest(http.MethodGet, "/api/npm/web/npm-all/lodash/-/lodash-4.17.20.tgz", nil)
	own, _ := errs.ProblemFor(r, err, "")
	fwd, _ := errs.ProblemFor(r.WithContext(errs.WithBastionForward(r.Context())), err, "")
	if !strings.Contains(own.Detail, "https://quader.test/ui/vulnerabilities/findings/") || own.Extensions["findingId"] == nil {
		t.Fatalf("an own caller: %+v", own)
	}
	if fwd.Detail != "Quader blocked lodash@4.17.20 in web/npm-all: blocked in triage (GHSA-xxxx-yyyy-zzzz)." ||
		fwd.Extensions["findingId"] != nil || fwd.Extensions["advisory"] != "GHSA-xxxx-yyyy-zzzz" || fwd.Code != own.Code {
		t.Fatalf("a forward: %+v", fwd)
	}
}
