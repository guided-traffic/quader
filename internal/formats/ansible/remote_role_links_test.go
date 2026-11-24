package ansible

import (
	"archive/tar"
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/ansible/collection/collectiontest"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
)

// Q-ANS-91, R-ANS-020: a role archive (a tarball of a role's repository, one directory at its root) holds a link whose target stays
// inside the role's directory, which ansible-galaxy role install extracts; one that leaves the role's directory, even if it stays in the
// archive, is refused by its name, as the client refuses it.
func TestR_ANS_010_ARoleArchiveHoldsALinkInsideTheRolesDirectory(t *testing.T) {
	const top = "geerlingguy-ansible-role-docker-7.4.1"
	good := collectiontest.Pack([]collectiontest.Entry{
		{Name: top, Dir: true},
		{Name: top + "/tasks/main.yml", Body: []byte("- name: x\n")},
		{Name: top + "/tasks/alias.yml", Type: tar.TypeSymlink, Link: "main.yml"},
	})
	if err := checkRoleArchive(context.Background(), bytes.NewReader(good), untrusted.DefaultLimits()); err != nil {
		t.Errorf("a link beside its target: %v", err)
	}
	leaving := collectiontest.Pack([]collectiontest.Entry{
		{Name: top, Dir: true},
		{Name: "other/x.yml", Body: []byte("x\n")},
		{Name: top + "/tasks/out.yml", Type: tar.TypeSymlink, Link: "../../other/x.yml"},
	})
	err := checkRoleArchive(context.Background(), bytes.NewReader(leaving), untrusted.DefaultLimits())
	if errs.KindOf(err) != errs.UpstreamFailed || !strings.Contains(err.Error(), "tasks/out.yml") {
		t.Errorf("a link that leaves the role's directory: %v", err)
	}
}
