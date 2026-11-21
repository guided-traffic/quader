package oci

import (
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
)

// TestR_SIG_068_TheGatesAnswersNameTheArtifactAsTheClientDoes: the declaration names a stored artifact in the client's words for the
// signature gate's answers (Q-SIG-213); only OCI's signing tools read the artifact from the repository (Q-SIG-211). Unit, microseconds.
func TestR_SIG_068_TheGatesAnswersNameTheArtifactAsTheClientDoes(t *testing.T) {
	f := &Format{}
	d := "sha256:" + strings.Repeat("ab", 32)
	for path, want := range map[string]string{"unsigned/_tags/1.0": "unsigned:1.0", "team/app/_manifests/" + d: "team/app@" + d, "app/_blobs/" + d: ""} {
		if got := (signatureAdapter{}).Declaration().ArtifactName(path, f.Classify(formats.Path{Path: path})); got != want {
			t.Errorf("%s: %q, want %q", path, got, want)
		}
	}
	if !(signatureAdapter{}).Declaration().ReadToSign {
		t.Error("cosign and notation read the manifest before they sign: ReadToSign")
	}
}
