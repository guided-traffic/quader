package dispatch

import (
	"testing"

	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// TestR_REPO_070_ASystemRepositoryOfUpstreamAnswersNoClient: a protocol request to a system repository of the project upstream
// (scanner-trivy) is answered 404 repository_not_found as for a repository that does not exist, also when the decision point allowed
// it (an administrator), and its handler never runs (Q-REPO-380). The decision for a caller who may not read it is the one for a
// repository that does not exist (R-AUTHZ-016), so the answer is the same for every caller.
func TestR_REPO_070_ASystemRepositoryOfUpstreamAnswersNoClient(t *testing.T) {
	r := newRig(t)
	r.repos.byKey[qr("scanner")] = repository.Repository{
		ID: "9", Name: qr("scanner"), Class: repository.Remote, Format: "probe",
		Upstream: &repository.UpstreamFacts{Kind: wellknown.KindSystem},
	}
	r.repos.byKey[qr("preset")] = repository.Repository{
		ID: "10", Name: qr("preset"), Class: repository.Remote, Format: "probe",
		Upstream: &repository.UpstreamFacts{Kind: wellknown.KindPreset, State: wellknown.StateActive},
	}
	system := r.do("GET", "/api/probe/it/scanner/left-pad")
	if len(r.reached) != 0 {
		t.Fatalf("the handler ran for the system repository: %v", r.reached)
	}
	missing := r.do("GET", "/api/probe/it/ghost/left-pad")
	if system.Code != 404 || system.Header().Get("X-Probe-Error") != "repository_not_found" || system.Header().Get("X-Probe-Error") != missing.Header().Get("X-Probe-Error") {
		t.Fatalf("system %d %v, missing %d %v", system.Code, system.Header(), missing.Code, missing.Header())
	}
	if rec := r.do("GET", "/api/probe/it/preset/left-pad"); rec.Code != 204 || len(r.reached) != 1 {
		t.Fatalf("an active preset repository is served: %d %v %v", rec.Code, rec.Header(), r.reached)
	}
	// R-REPO-075: a system repository whose clients an administrator switched on (upstream/clamav) is served at its level.
	r.repos.byKey[qr("definitions")] = repository.Repository{
		ID: "11", Name: qr("definitions"), Class: repository.Local, Format: "probe",
		Upstream: &repository.UpstreamFacts{Kind: wellknown.KindSystem, ClientsEnabled: true},
	}
	if rec := r.do("GET", "/api/probe/it/definitions/left-pad"); rec.Code != 204 || len(r.reached) != 2 {
		t.Fatalf("R-REPO-075 a system repository with its clients on is not served: %d %v %v", rec.Code, rec.Header(), r.reached)
	}
}
