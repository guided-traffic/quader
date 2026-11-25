package view

import (
	"context"
	"errors"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/qname"
)

// TestR_OCI_040_ContentGoesOnlyWhereItsDownloadWouldBeServed: the members a format read from a file's content (an image's configuration
// and its labels) go to a caller who may read every such file and to whom a download of each would be served now; when the serve point
// would refuse one or make it wait, the members are dropped and contentHeld names the code the download would answer; a question that
// fails holds them as unavailable. The rest of the metadata stays. Unit, microseconds.
func TestR_OCI_040_ContentGoesOnlyWhereItsDownloadWouldBeServed(t *testing.T) {
	repo := qname.Repo{Project: "web", Key: "images"}
	data := Data{
		Metadata:        map[string]any{"kind": "image", "configs": map[string]any{"sha256:a": "env"}, "labeled": map[string]string{"x": "y"}},
		ContentMetadata: []string{"configs", "labeled"},
		ContentPaths:    []string{"app/_blobs/sha256:c1", "app/_blobs/sha256:c2"},
	}
	readAll := func(qname.Repo, string, string, string) bool { return true }
	for name, c := range map[string]struct {
		hold     Hold
		wantHeld string
	}{
		"served":            {func(context.Context, qname.Repo, string) (string, error) { return "", nil }, ""},
		"no question asked": {nil, ""},
		"the second refused": {func(_ context.Context, _ qname.Repo, p string) (string, error) {
			if p == "app/_blobs/sha256:c2" {
				return errs.CodeMalwareFound, nil
			}
			return "", nil
		}, errs.CodeMalwareFound},
		"a check pending": {func(context.Context, qname.Repo, string) (string, error) { return errs.CodeSignatureCheckPending, nil }, errs.CodeSignatureCheckPending},
		"the question fails": {func(context.Context, qname.Repo, string) (string, error) {
			return "", errors.New("the store did not answer")
		}, errs.CodeUnavailable},
	} {
		sc := Scope{path: readAll}.WithHold(c.hold)
		md := readableMetadata(context.Background(), sc, repo, "app", "1.0", data, true)
		_, configs := md["configs"]
		held, _ := md[MemberContentHeld].(map[string]any)
		if md["kind"] != "image" {
			t.Errorf("%s: the rest of the metadata: %v", name, md)
		}
		switch {
		case c.wantHeld == "" && (!configs || held != nil):
			t.Errorf("%s: the configuration is held: %v", name, md)
		case c.wantHeld != "" && (configs || md["labeled"] != nil || held["code"] != c.wantHeld):
			t.Errorf("%s: %v, want the content held with %s", name, md, c.wantHeld)
		}
	}
}
