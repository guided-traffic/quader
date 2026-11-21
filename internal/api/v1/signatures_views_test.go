package v1

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/packages/view"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/signatures"
)

// Unit tests of the reads of what an artifact carries (R-SIG-034, R-SIG-004, Q-SIG-26): the version's answer per repository, a
// virtual repository naming its members, the scope of the trust rows, the member signatures and the origin.

func sigTopology() view.Topology {
	r := sigRepos()
	return view.NewTopology([]repository.Repository{r.repos["npm-local"], r.repos["npmjs"], r.repos["npm-all"]})
}

// sigFixture is a version ui@1.0.0 held by web/npm-local and upstream/npmjs (the same tarball), shown through web/npm-all; the
// tarball carries a provenance bundle attached in npm-local, a publisher signature attached in npmjs, an attestation and the
// registry signature of npmjs.
func sigFixture() (*fakeSigStore, sigVersion, [32]byte) {
	tgz := [32]byte{0xaa}
	st := newFakeSigStore()
	internal := &signatures.Trust{State: signatures.TrustInternal, Anchors: []signatures.AnchorRef{{ID: 4, Title: "Team key", Scope: "project"}}, ComputedAt: sigT0()}
	public := &signatures.Trust{State: signatures.TrustPublic, Anchors: []signatures.AnchorRef{{ID: 1, Title: "Public Sigstore", Scope: "shipped"}}, ComputedAt: sigT0(), Stale: true}
	att := sigViewFor(3, tgz, signatures.RoleAttestation, signatures.StateValid, nil, 1)
	att.Signature.PredicateType = "https://spdx.dev/Document"
	reg := sigViewFor(4, [32]byte{0xbb}, signatures.RoleRegistry, signatures.StateValid, nil, 2)
	reg.Signature.SubjectName = "ui@1.0.0"
	st.views = []signatures.View{
		sigViewFor(1, tgz, signatures.RoleProvenance, signatures.StateValid, internal, 1),
		sigViewFor(2, tgz, signatures.RolePublisher, signatures.StateValid, public, 2),
		att, reg,
	}
	in := sigVersion{
		detail: view.VersionDetail{
			PackageID: view.ID("npm", "ui"), Format: "npm", Name: "ui", Version: "1.0.0", Repo: qname.Repo{Project: "web", Key: "npm-local"},
			Repositories: []view.RepositoryEntry{
				{Repo: qname.Repo{Project: "web", Key: "npm-local"}, Class: "local"},
				{Repo: qname.Repo{Project: "upstream", Key: "npmjs"}, Class: "remote"},
				{Repo: qname.Repo{Project: "web", Key: "npm-all"}, Class: "virtual"},
			},
			Files: []view.File{{Path: "ui/-/ui-1.0.0.tgz", SHA256: hex.EncodeToString(tgz[:]), Repo: qname.Repo{Project: "web", Key: "npm-local"}}},
		},
		topo: sigTopology(),
	}
	return st, in, tgz
}

func TestR_SIG_034_VersionSignatures(t *testing.T) {
	sv := func(st *fakeSigStore) SignatureServices {
		return SignatureServices{Store: st, Enabled: true, Repos: sigRepos()}
	}
	t.Run("R-SIG-034 each repository that holds the version answers its own attachments, judged in its project's scope", func(t *testing.T) {
		st, in, _ := sigFixture()
		out, err := (&Server{}).versionSignatureReport(context.Background(), sv(st), in, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Repositories) != 3 {
			t.Fatalf("%+v", out)
		}
		local, remote, virtual := out.Repositories[0], out.Repositories[1], out.Repositories[2]
		if local.Repository != "web/npm-local" || local.State != "verified_internal" || local.Stale || len(local.Signatures) != 1 || len(local.Attestations) != 1 {
			t.Fatalf("local %+v", local)
		}
		if local.Attestations[0].Type != "https://spdx.dev/Document" || local.Signatures[0].Path == nil || *local.Signatures[0].Path != "ui/-/ui-1.0.0.tgz" {
			t.Fatalf("local %+v", local.Attestations[0])
		}
		if remote.State != "verified_public" || !remote.Stale || len(remote.Signatures) != 2 || remote.Signatures[1].Role != "registry" {
			t.Fatalf("remote %+v", remote)
		}
		if q := st.viewQueries[0]; q.ProjectID != "p-web" || len(q.RepoIDs) != 1 || q.RepoIDs[0] != 1 || len(q.SubjectNames) != 1 || q.SubjectNames[0] != "ui@1.0.0" {
			t.Fatalf("local query %+v", q)
		}
		if q := st.viewQueries[1]; q.ProjectID != "" || q.RepoIDs[0] != 2 {
			t.Fatalf("a repository of upstream addressed directly is judged in the installation's scope: %+v", q)
		}
		if virtual.Class != "virtual" || len(virtual.Members) != 2 || virtual.Members[0] != "web/npm-local" || virtual.Members[1] != "upstream/npmjs" {
			t.Fatalf("virtual %+v", virtual)
		}
		if q := st.viewQueries[2]; q.ProjectID != "p-web" || len(q.RepoIDs) != 2 {
			t.Fatalf("through a virtual repository its project's scope judges every member's content (Q-SIG-26): %+v", q)
		}
		named := map[string]string{}
		for _, e := range virtual.Signatures {
			named[string(e.Role)] = e.Repository
		}
		if named["provenance"] != "web/npm-local" || named["publisher"] != "upstream/npmjs" || virtual.State != "verified_internal" {
			t.Fatalf("each signature names the member that holds it: %v %+v", named, virtual)
		}
	})
	t.Run("R-SIG-034 repository= answers only that repository, 404 for one that does not hold the version", func(t *testing.T) {
		st, in, _ := sigFixture()
		only := qname.Repo{Project: "web", Key: "npm-all"}
		out, err := (&Server{}).versionSignatureReport(context.Background(), sv(st), in, &only, nil)
		if err != nil || len(out.Repositories) != 1 || out.Repositories[0].Repository != "web/npm-all" {
			t.Fatalf("%v %+v", err, out)
		}
		other := qname.Repo{Project: "api", Key: "npm-local"}
		if _, err := (&Server{}).versionSignatureReport(context.Background(), sv(st), in, &other, nil); err == nil {
			t.Fatal("a repository that does not hold the version was answered")
		}
	})
	t.Run("R-SIG-004 a holder whose file has other bytes is asked for its own subject, and its origin is its item's", func(t *testing.T) {
		st, in, tgz := sigFixture()
		other := [32]byte{0xcc}
		st.origins[71] = signatures.StoredOrigin{
			PrincipalKind: "ci", PrincipalName: "web-release", UploadedAt: sigT0(),
			CI: json.RawMessage(`{"issuer":"gitlab","project_path":"web/ui","pipeline_id":8812,"ref":"main","ref_protected":"true","user_email":"x@example.com"}`),
		}
		lookup := func(_ context.Context, repo qname.Repo, path string) (items.Item, bool, error) {
			if repo.Key == "npmjs" {
				return items.Item{ID: 72, Kind: items.KindFile, Digest: other, Path: path, UpstreamURL: "https://registry.npmjs.org/ui/-/ui-1.0.0.tgz", CreatedAt: sigT0()}, true, nil
			}
			return items.Item{ID: 71, Kind: items.KindFile, Digest: tgz, Path: path}, true, nil
		}
		out, err := (&Server{}).versionSignatureReport(context.Background(), sv(st), in, nil, lookup)
		if err != nil {
			t.Fatal(err)
		}
		if q := st.viewQueries[1]; len(q.Subjects) != 1 || q.Subjects[0] != other {
			t.Fatalf("%+v", q)
		}
		if out.Repositories[1].State != "none" || out.Repositories[1].Origin == nil || out.Repositories[1].Origin.Kind != "fetched" {
			t.Fatalf("%+v", out.Repositories[1])
		}
		o := out.Repositories[0].Origin
		if o == nil || o.Kind != "uploaded" || *o.PrincipalKind != "ci" || o.Ci == nil || *o.Ci.ProjectPath != "web/ui" || *o.Ci.PipelineId != "8812" ||
			o.Ci.RefProtected == nil || !*o.Ci.RefProtected {
			t.Fatalf("%+v %+v", o, o.Ci)
		}
		b, _ := json.Marshal(o)
		if strings.Contains(string(b), "x@example.com") {
			t.Fatalf("an e-mail address left the snapshot: %s", b)
		}
	})
	t.Run("R-SIG-024 an OCI tag asks for its manifest and each platform's manifest", func(t *testing.T) {
		st := newFakeSigStore()
		idx, amd := [32]byte{1}, [32]byte{2}
		in := sigVersion{
			detail: view.VersionDetail{
				Format: "oci", Name: "app", Version: "1.0",
				Repositories: []view.RepositoryEntry{{Repo: qname.Repo{Project: "web", Key: "npm-local"}, Class: "local"}},
				Metadata:     map[string]any{"digest": "sha256:" + hex.EncodeToString(idx[:]), "platforms": []map[string]any{{"platform": "linux/amd64", "digest": "sha256:" + hex.EncodeToString(amd[:])}}},
			},
			topo: sigTopology(),
		}
		st.views = []signatures.View{sigViewFor(1, amd, signatures.RolePublisher, signatures.StateInvalid, nil, 1)}
		out, err := (&Server{}).versionSignatureReport(context.Background(), sv(st), in, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		e := out.Repositories[0]
		if e.State != "invalid" || len(e.Signatures) != 1 || e.Signatures[0].Platform == nil || *e.Signatures[0].Platform != "linux/amd64" || len(st.viewQueries[0].Subjects) != 2 {
			t.Fatalf("%+v %+v", e, st.viewQueries)
		}
	})
	t.Run("R-SIG-034 a signature's texts are returned as they were read, without markup or escaping", func(t *testing.T) {
		st, in, _ := sigFixture()
		out, _ := (&Server{}).versionSignatureReport(context.Background(), sv(st), in, nil, nil)
		e := out.Repositories[0].Signatures[0]
		if e.Signer == nil || *e.Signer != "CN=<b>Release</b>" || e.Ci == nil || len(e.Log) != 1 || e.TimeProof == nil || e.Original == "" || e.Trust == nil || e.Trust.Anchors[0].Title != "Team key" {
			t.Fatalf("%+v", e)
		}
	})
}

func TestR_SIG_034_TheMemberSignatures(t *testing.T) {
	st, in, tgz := sigFixture()
	on := &Server{o: Options{Signatures: SignatureServices{Store: st, Enabled: true, Repos: sigRepos()}}}
	local := qname.Repo{Project: "web", Key: "npm-local"}
	t.Run("R-SIG-034 a stored file carries the badge of its attachments in its repository", func(t *testing.T) {
		m, err := on.signatureMember(context.Background(), local, items.Item{Kind: items.KindFile, Digest: tgz})
		if err != nil || m == nil || m.State != "verified_internal" || m.Stale {
			t.Fatalf("%v %+v", err, m)
		}
	})
	t.Run("R-SIG-034 a folder carries no member, nor any file while signatures are off or the store is missing", func(t *testing.T) {
		if m, _ := on.signatureMember(context.Background(), local, items.Item{Kind: items.KindFolder}); m != nil {
			t.Fatalf("%+v", m)
		}
		off := &Server{o: Options{Signatures: SignatureServices{Store: st, Enabled: false, Repos: sigRepos()}}}
		if m, _ := off.signatureMember(context.Background(), local, items.Item{Kind: items.KindFile, Digest: tgz}); m != nil {
			t.Fatalf("%+v", m)
		}
		none := &Server{}
		if m, _ := none.signatureMember(context.Background(), local, items.Item{Kind: items.KindFile, Digest: tgz}); m != nil {
			t.Fatalf("%+v", m)
		}
	})
	t.Run("R-SIG-034 a version and each of its files carry the badge, read in one call", func(t *testing.T) {
		before := len(st.viewQueries)
		out := versionOf("https://quader.example.com", in.detail)
		if err := on.addSignatureStatus(context.Background(), in.detail, &out); err != nil {
			t.Fatal(err)
		}
		if out.Signatures == nil || out.Signatures.State != "verified_internal" || out.Files[0].Signatures == nil || out.Files[0].Signatures.State != "verified_internal" {
			t.Fatalf("%+v %+v", out.Signatures, out.Files)
		}
		if len(st.viewQueries) != before+1 {
			t.Fatalf("%d calls", len(st.viewQueries)-before)
		}
		b, _ := json.Marshal(out)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		if s, ok := m["signatures"].(map[string]any); !ok || s["state"] != "verified_internal" || s["stale"] != false {
			t.Fatalf("%s", b)
		}
	})
	t.Run("R-SIG-034 the file's answer reads the file's subject and the registry signature of its npm version", func(t *testing.T) {
		out, err := on.fileSignatureReport(context.Background(), on.o.Signatures, local, items.Item{ID: 71, Kind: items.KindFile, Digest: tgz, Path: "ui/-/ui-1.0.0.tgz", CreatedBy: "user:alice", CreatedAt: sigT0()})
		if err != nil {
			t.Fatal(err)
		}
		if out.State != "verified_internal" || out.Sha256 != hex.EncodeToString(tgz[:]) || len(out.Signatures) != 1 || len(out.Attestations) != 1 {
			t.Fatalf("%+v", out)
		}
		if o := out.Origin; o == nil || o.Kind != "uploaded" || *o.PrincipalKind != "user" || *o.PrincipalName != "alice" || o.Ci != nil {
			t.Fatalf("%+v", o)
		}
	})
}
