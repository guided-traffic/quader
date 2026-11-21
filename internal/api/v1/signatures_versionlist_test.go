package v1

import (
	"context"
	"testing"

	"github.com/guided-traffic/quader/internal/packages/view"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/signatures"
)

// TestR_SIG_034_TheVersionListCarriesEachVersionsBadge: a page of the version list carries the member signatures per row from one read of
// the page's files and one count per project of the repositories the rows name, not one per row; a version's badge is the best of its
// files' signatures in every repository that holds it (a repository of upstream judged in the installation's scope); a version without a
// counted signature reads none; with the signatures off no row carries the member (R-SIG-009, R-SIG-034). Unit, milliseconds.
func TestR_SIG_034_TheVersionListCarriesEachVersionsBadge(t *testing.T) {
	tarball1, tarball2, tarball3 := [32]byte{1}, [32]byte{2}, [32]byte{3}
	st := newFakeSigStore()
	st.versionBlobs = []signatures.VersionBlob{
		{Version: "1.0.0", RepoID: 1, Blob: tarball1}, {Version: "2.0.0", RepoID: 2, Blob: tarball2}, {Version: "3.0.0", RepoID: 1, Blob: tarball3},
	}
	st.views = []signatures.View{
		{
			Signature:   signatures.Signature{ID: 1, Subject: tarball1, Role: signatures.RoleProvenance, State: signatures.StateValid},
			Attachments: []signatures.Attachment{{RepoID: 1}}, Trust: &signatures.Trust{State: signatures.TrustInternal},
		},
		{
			Signature:   signatures.Signature{ID: 2, Subject: tarball2, Role: signatures.RolePublisher, State: signatures.StateValid},
			Attachments: []signatures.Attachment{{RepoID: 2}}, Trust: &signatures.Trust{State: signatures.TrustPublic, Stale: true},
		},
	}
	s := &Server{o: Options{Signatures: SignatureServices{Store: st, Enabled: true, Repos: sigRepos()}}}
	web, npmjs := qname.Repo{Project: "web", Key: "npm-local"}, qname.Repo{Project: "upstream", Key: "npmjs"}
	rows := []view.VersionSummary{
		{Version: "1.0.0", Repositories: []qname.Repo{web}},
		{Version: "2.0.0", Repositories: []qname.Repo{npmjs}},
		{Version: "3.0.0", Repositories: []qname.Repo{web, npmjs}},
	}
	got, err := s.versionListSignatures(context.Background(), "ui", rows)
	if err != nil {
		t.Fatal(err)
	}
	if g := got["1.0.0"]; g == nil || g.State != "verified_internal" || g.Stale {
		t.Errorf("1.0.0: %+v", g)
	}
	if g := got["2.0.0"]; g == nil || g.State != "verified_public" || !g.Stale {
		t.Errorf("2.0.0: %+v", g)
	}
	if g := got["3.0.0"]; g == nil || g.State != "none" {
		t.Errorf("3.0.0: %+v", g)
	}
	if st.versionBlobCalls != 1 || len(st.viewQueries) != 2 {
		t.Errorf("%d reads of the files and %d counts for three rows, want 1 and one per project (2)", st.versionBlobCalls, len(st.viewQueries))
	}
	for _, q := range st.viewQueries {
		if (q.ProjectID == "") != (len(q.RepoIDs) == 1 && q.RepoIDs[0] == 2) {
			t.Errorf("a count's scope: %+v", q)
		}
	}
	s.o.Signatures.Enabled = false
	if got, err := s.versionListSignatures(context.Background(), "ui", rows); err != nil || got != nil {
		t.Errorf("with the signatures off: %+v %v", got, err)
	}
}
