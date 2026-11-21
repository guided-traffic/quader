package npm

import (
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
)

// TestR_SIG_068_TheGatesAnswersNameTheArtifactAsTheClientDoes: the declaration names a stored artifact in the client's words for the
// signature gate's answers (Q-SIG-213); only OCI's signing tools read the artifact from the repository (Q-SIG-211). Unit, microseconds.
func TestR_SIG_068_TheGatesAnswersNameTheArtifactAsTheClientDoes(t *testing.T) {
	if got := (signatureAdapter{}).Declaration().ArtifactName("left-pad/-/left-pad-1.3.0.tgz", formats.PathInfo{Package: "left-pad", Version: "1.3.0"}); got != "left-pad@1.3.0" {
		t.Errorf("%q", got)
	}
	if (signatureAdapter{}).Declaration().ReadToSign {
		t.Error("npm signs a local tarball before the publish")
	}
}
