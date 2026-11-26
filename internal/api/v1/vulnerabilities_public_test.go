package v1

import (
	"context"
	"net/http"
	"testing"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/vuln/report"
	"github.com/guided-traffic/quader/internal/vuln/store"
)

// vulnServer serves the vulnerability operations to the caller authn produces (nil is anonymous), with public content or without, on a
// replica that has not wired the scanning.
func vulnServer(t *testing.T, authn func(*http.Request) (httpserver.Principal, error), anonymous bool) http.Handler {
	t.Helper()
	router := httpserver.NewClientRouter()
	New(Options{
		Document: testDocument(t), Identity: Identity{Replica: "r", Role: "api"}, Ready: func() bool { return true },
		Access: allowAll{}, Replicas: fakeReplicas{}, Facts: fakeFacts{},
		Repositories: RepositoryServices{PublicContent: func(context.Context) bool { return anonymous }},
	}).Register(router)
	res, err := httpserver.NewResolver(nil, "https://quader.example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	return httpserver.Chain{Router: router, Resolver: res, Authenticate: authn, Replica: "r", ReplicaHeader: httpserver.ReplicaAuthenticated}.Handler()
}

// TestR_VULN_019_TheThreePublicReadsPassTheAnonymousGateFirst: the vulnerabilities of a version and of a package and the advisory page
// are public operations decided by the levels (the owner's decision of 2026-11-26, Q-VULN-280): an anonymous caller is answered 401
// while no repository is public or the endpoint admits no anonymous caller, before anything else, as the package reads beside them;
// with public content it reaches the report (here 503: the replica has not wired it). Every other vulnerability operation stays
// authenticated. Unit, milliseconds.
func TestR_VULN_019_TheThreePublicReadsPassTheAnonymousGateFirst(t *testing.T) {
	public := []string{
		"/api/v1/packages/" + "npm%3Aleft-pad" + "/versions/1.0.0/vulnerabilities",
		"/api/v1/packages/" + "npm%3Aleft-pad" + "/vulnerabilities",
		"/api/v1/vulnerabilities/advisories/npm/GHSA-aaaa-bbbb-cccc",
	}
	for _, path := range public {
		if w := get(vulnServer(t, nil, false), path); w.Code != http.StatusUnauthorized {
			t.Errorf("%s without public content: %d, want 401", path, w.Code)
		}
		if w := get(vulnServer(t, nil, true), path); w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s with public content: %d, want the 503 of a report that is not wired", path, w.Code)
		}
		if w := get(vulnServer(t, authed, false), path); w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s signed in: %d, want 503", path, w.Code)
		}
	}
	for _, path := range []string{"/api/v1/vulnerabilities/findings", "/api/v1/vulnerabilities/summary", "/api/v1/vulnerabilities/trend"} {
		if w := get(vulnServer(t, nil, true), path); w.Code != http.StatusUnauthorized {
			t.Errorf("%s anonymous with public content: %d, want 401", path, w.Code)
		}
	}
	doc := testDocument(t)
	for _, op := range doc.Operations() {
		switch op.ID {
		case "getPackageVersionVulnerabilities", "getPackageVulnerabilities", "getVulnerabilityAdvisory":
			if !op.Requirement.Public {
				t.Errorf("%s is %s, want public", op.ID, op.Requirement)
			}
		case "getVulnerabilityFinding", "listVulnerabilityFindings", "exportVulnerabilityFindings", "getVulnerabilitySummary", "getVulnerabilityTrend":
			if !op.Requirement.Authenticated {
				t.Errorf("%s is %s: the finding's detail and the report stay with who signs in", op.ID, op.Requirement)
			}
		}
	}
}

// TestR_VULN_019_AnAnonymousFindingCarriesNoReason: the finding an anonymous caller is told carries no dismissal or resolution reason
// and no time of a resolution, whatever the row holds; a signed-in caller's carries them. Unit, microseconds.
func TestR_VULN_019_AnAnonymousFindingCarriesNoReason(t *testing.T) {
	row := report.Finding{Row: store.Row{
		ID: "01933b2a-7c1e-7d10-9a4b-3f0e2c1d0a11", Repository: "platform/npm", Package: "left-pad", Version: "1.0.0", Advisory: "GHSA-x",
		Severity: "high", Kind: "vulnerability", Status: "confirmed", DismissalReason: "risk_accepted", ResolvedReason: "no_longer_affected",
	}}
	anon := findingFor(report.Caller{Anonymous: true}, row)
	if anon.DismissalReason != nil || anon.ResolvedReason != nil || anon.ResolvedAt != nil || anon.Status != gen.VulnerabilityStatus("confirmed") {
		t.Errorf("anonymous: %+v", anon)
	}
	signed := findingFor(report.Caller{}, row)
	if signed.DismissalReason == nil || signed.ResolvedReason == nil {
		t.Errorf("signed in: %+v", signed)
	}
}
