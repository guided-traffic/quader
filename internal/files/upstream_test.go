package files

import (
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// TestR_REPO_070_TheFilesOfASystemRepositoryServeNoClient: a request below /files/ (strict) to a system repository of the project
// upstream answers as for a repository that does not exist; the management API's item reads (not strict) read it (Q-REPO-380).
func TestR_REPO_070_TheFilesOfASystemRepositoryServeNoClient(t *testing.T) {
	tg := target{repo: repository.Repository{Lifecycle: repository.Active, Upstream: &repository.UpstreamFacts{Kind: wellknown.KindSystem}}}
	if _, err := tg.resolved(true); err == nil {
		t.Fatal("a client reads the system repository")
	} else if e, _ := errs.As(err); e.Code != errs.CodeRepositoryNotFound {
		t.Fatalf("err = %v", err)
	}
	if _, err := tg.resolved(false); err != nil {
		t.Fatalf("the management API: %v", err)
	}
	// R-REPO-075: upstream/clamav serves clients while they are switched on.
	open := target{repo: repository.Repository{Lifecycle: repository.Active, Upstream: &repository.UpstreamFacts{Kind: wellknown.KindSystem, ClientsEnabled: true}}}
	if _, err := open.resolved(true); err != nil {
		t.Fatalf("R-REPO-075 a system repository with its clients on: %v", err)
	}
}

// TestR_REPO_075_NoOneDeletesADefinitionFileByHand: a local system repository of the project upstream refuses a hand delete with 409
// managed_by_product, whether its clients are on or off; the remote scanner-trivy does not (R-REPO-075). Milliseconds.
func TestR_REPO_075_NoOneDeletesADefinitionFileByHand(t *testing.T) {
	for _, on := range []bool{false, true} {
		r := repository.Repository{Class: repository.Local, Upstream: &repository.UpstreamFacts{Kind: wellknown.KindSystem, ClientsEnabled: on}}
		if !r.RefusesHandDelete() {
			t.Errorf("clients on %v: a hand delete is taken", on)
		}
	}
	if (repository.Repository{Class: repository.Remote, Upstream: &repository.UpstreamFacts{Kind: wellknown.KindSystem}}).RefusesHandDelete() ||
		(repository.Repository{Class: repository.Local}).RefusesHandDelete() {
		t.Error("scanner-trivy or a repository of another project refuses a hand delete")
	}
}
