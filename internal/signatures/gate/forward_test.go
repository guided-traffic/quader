package gate

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/qname"
)

// TestR_BAS_114_ASignatureRefusalThroughABastionNamesNoPageOfTheInstallation: the refusal of an unverified signature (R-SIG-068)
// links the file's page of the installation's web interface for its own callers; a caller of a Bastion reads the same sentence
// without it. The pending answer names only the artifact, and a caller of a Bastion reads it too instead of the neutral text of a
// 503 (bastion.md R-BAS-114, Q-BAS-386, Q-BAS-387).
func TestR_BAS_114_ASignatureRefusalThroughABastionNamesNoPageOfTheInstallation(t *testing.T) {
	g := &Gate{o: Options{BaseURL: "https://quader.test", RetryAfter: time.Minute}}
	d := Download{Addressed: Repo{Name: qname.Repo{Project: "web", Key: "docker"}}, Path: "app/manifests/sha256:ab", Name: "app:1.0"}
	r := httptest.NewRequest(http.MethodGet, "/v2/web/docker/app/manifests/1.0", nil)
	fr := r.WithContext(errs.WithBastionForward(r.Context()))

	own, _ := errs.ProblemFor(r, g.refusal(d, ReasonUntrusted), "")
	fwd, _ := errs.ProblemFor(fr, g.refusal(d, ReasonUntrusted), "")
	if !strings.Contains(own.Detail, "https://quader.test/ui/projects/web/repositories/docker/files/") {
		t.Fatalf("an own caller: %+v", own)
	}
	if fwd.Detail != RefusalText("app:1.0", d.Addressed.Name, ReasonUntrusted) || strings.Contains(fwd.Detail, "/ui/") || fwd.Extensions["reason"] == nil {
		t.Fatalf("a forward: %+v", fwd)
	}
	if p, _ := errs.ProblemFor(fr, g.pendingError(d), ""); p.Detail != PendingText("app:1.0") || p.Status != http.StatusServiceUnavailable {
		t.Fatalf("a forward of the pending answer: %+v", p)
	}
}
