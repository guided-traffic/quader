//go:build integration

package ansibleremote

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"
)

// TestR_ANS_010_ACollectionWithAContainedLinkIsFetchedThroughARemote: the upstream serves a collection whose archive holds a symbolic
// link inside it, as the public Galaxy serves ansible.posix 2.2.2; the remote stores it and serves the archive the upstream announced, byte
// for byte (R-ANS-013, R-ANS-010).
func TestR_ANS_010_ACollectionWithAContainedLinkIsFetchedThroughARemote(t *testing.T) {
	e := newEnv(t, options{})
	if err := e.stub.AddLinkedVersion("acme.linked", "1.0.0", map[string]string{"plugins/README-link.md": "../README.md"}); err != nil {
		t.Fatal(err)
	}
	r := e.start(1)[0]
	repo := e.fresh()
	v := e.get(r, repo, "v3/collections/acme/linked/versions/1.0.0/")
	if v.Status != http.StatusOK {
		t.Fatalf("version document: %d %s", v.Status, v.Body)
	}
	var doc struct {
		Artifact struct{ SHA256 string } `json:"artifact"`
	}
	if err := json.Unmarshal(v.Body, &doc); err != nil {
		t.Fatal(err)
	}
	a := e.get(r, repo, "v3/artifacts/collections/acme-linked-1.0.0.tar.gz")
	sum := sha256.Sum256(a.Body)
	if a.Status != http.StatusOK || hex.EncodeToString(sum[:]) != doc.Artifact.SHA256 {
		t.Errorf("the archive through the remote: %d, sha256 %x, announced %s: %s", a.Status, sum, doc.Artifact.SHA256, a.Body[:min(len(a.Body), 200)])
	}
}
