package v1

import (
	"context"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/signatures"
)

// TestR_SIG_030_TheCoveringReleasesOfAPackageAreListedWithTheirSignatures: the format's coverage names the releases whose indexes name
// a package; each is listed once with the signatures attached to it in the repository, its state read as a publisher's would be (a
// release's registry signature under an anchor reads verified), its suite and date; a format without coverage lists none (R-SIG-030,
// R-SIG-034). Unit, milliseconds.
func TestR_SIG_030_TheCoveringReleasesOfAPackageAreListedWithTheirSignatures(t *testing.T) {
	release := [32]byte{7}
	deb := [32]byte{9}
	st := &fakeSigStore{views: []signatures.View{{
		Signature: signatures.Signature{ID: 41, Subject: release, Role: signatures.RoleRegistry, State: signatures.StateValid},
		Scheme:    signatures.SchemeOpenPGP, Attachments: []signatures.Attachment{{RepoID: 3, Location: "dists/bookworm/InRelease", Source: "release"}},
		Trust: &signatures.Trust{State: signatures.TrustPublic},
	}}}
	date := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	asked := 0
	sv := SignatureServices{Store: st, Coverage: func(_ context.Context, repoID int64, _ qname.Repo, format, _, path string, sha [32]byte) ([]SignatureCoveringDocument, error) {
		asked++
		if repoID != 3 || format != "deb" || path != "pool/main/h/hello/hello_1.0_amd64.deb" || sha != deb {
			t.Errorf("asked %d %s %s %x", repoID, format, path, sha)
		}
		doc := SignatureCoveringDocument{Path: "dists/bookworm/InRelease", SHA256: release, Name: "bookworm", Date: date}
		return []SignatureCoveringDocument{doc, doc}, nil
	}}
	var ss sigSubjects
	ss.add(deb, "pool/main/h/hello/hello_1.0_amd64.deb", "")
	ss.add([32]byte{1}, "", "") // a subject without a path (an OCI platform) asks nothing
	repo := qname.Repo{Project: "upstream", Key: "debian"}
	got, err := (&Server{}).covering(context.Background(), sv, 3, repo, "deb", "remote", "", ss)
	if err != nil {
		t.Fatal(err)
	}
	if asked != 1 || len(got) != 1 {
		t.Fatalf("asked %d times, %d releases: %+v", asked, len(got), got)
	}
	c := got[0]
	if c.Release != "dists/bookworm/InRelease" || c.Suite != "bookworm" || c.Date == nil || !c.Date.Equal(date) || c.State != "verified_public" || len(c.Signatures) != 1 {
		t.Fatalf("covering %+v", c)
	}
	sv.Coverage = nil
	if got, err := (&Server{}).covering(context.Background(), sv, 3, repo, "deb", "remote", "", ss); err != nil || len(got) != 0 {
		t.Fatalf("without coverage: %+v %v", got, err)
	}
}
