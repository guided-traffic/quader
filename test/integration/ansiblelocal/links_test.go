//go:build integration

package ansiblelocal

import (
	"bytes"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/ansible/collection/collectiontest"
)

// TestQ_ANS_91_ACollectionWithAContainedLinkPublishesAndIsServedUnchanged: a collection whose archive holds a symbolic link inside it,
// as ansible.posix's does, is accepted by a publish, and its archive is served byte for byte as it was published (R-ANS-010).
func TestQ_ANS_91_ACollectionWithAContainedLinkPublishesAndIsServedUnchanged(t *testing.T) {
	e := newEnv(t)
	s := spec("1.0.0")
	s.Files["tests/utils/shippable/remote.sh"] = "#!/bin/sh\necho remote\n"
	s.Links = map[string]string{"tests/utils/shippable/aix.sh": "remote.sh"}
	archive := collectiontest.Build(s)
	a := e.publish(e.tokA, archive, true)
	if a.Status != 202 {
		t.Fatalf("publish: %d %s", a.Status, a.Body)
	}
	if task := e.task(e.tokA, a); task["state"] != "completed" {
		t.Fatalf("task: %v", task)
	}
	got := e.get(e.tokA, "v3/artifacts/collections/acme-tools-1.0.0.tar.gz")
	if got.Status != 200 || !bytes.Equal(got.Body, archive) {
		t.Errorf("the archive served: %d, %d bytes, equal %v", got.Status, len(got.Body), bytes.Equal(got.Body, archive))
	}
}
