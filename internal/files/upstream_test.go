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
}
