package ansible

import (
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
)

// TestR_SIG_068_TheGatesAnswersNameTheArtifactAsTheClientDoes: the declaration names a stored artifact in the client's words for the
// signature gate's answers (Q-SIG-213); only OCI's signing tools read the artifact from the repository (Q-SIG-211). Unit, microseconds.
func TestR_SIG_068_TheGatesAnswersNameTheArtifactAsTheClientDoes(t *testing.T) {
	if got := (signatureAdapter{}).Declaration().ArtifactName("x", formats.PathInfo{Package: "sigm.unsigned", Version: "1.0.0"}); got != "sigm.unsigned:1.0.0" {
		t.Errorf("%q", got)
	}
}
