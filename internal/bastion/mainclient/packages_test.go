package mainclient_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
)

// TestR_BAS_087_TheClientAndTheFakeSpeakTheVersionWithPackages: at the interface version of this build the client reads a
// forwarded response's package from its marking and strips both headers (R-BAS-082); a check sends its pairs, which the fake
// main installation hands its check function, and the answer's member packages arrives; a pair the main installation refuses
// is refused by the fake as well, so the client gets no answer (R-BAS-083).
func TestR_BAS_087_TheClientAndTheFakeSpeakTheVersionWithPackages(t *testing.T) {
	r := newRig(t, nil)
	r.main.SetForward(func(w http.ResponseWriter, _ *http.Request) {
		r.main.Mark(w, iface.Marking{
			Class: iface.ClassArtifact, Repository: "web/deb", Read: iface.ReadPartial, Subject: iface.Subject{Kind: iface.SubjectAnonymous},
			Package: "g++", PackageRead: iface.ReadFull,
		})
		_, _ = w.Write([]byte("deb"))
	})
	resp, err := r.c.Do(context.Background(), forward(http.MethodGet, "/api/deb/web/deb/pool/main/g/g++/g++_1_all.deb"))
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Marked || resp.Marking.Interface != iface.InterfaceVersion || resp.Marking.Package != "g++" || resp.Marking.PackageRead != iface.ReadFull ||
		resp.Header.Get(iface.HeaderPackage) != "" || resp.Header.Get(iface.HeaderPackageRead) != "" || closeBody(t, resp) != "deb" {
		t.Fatalf("marking = %+v marked %v, header %v", resp.Marking, resp.Marked, resp.Header)
	}

	r.main.SetCheck(func(req iface.CheckRequest) iface.CheckAnswer {
		out := iface.CheckAnswer{Subject: iface.Subject{Kind: iface.SubjectAnonymous}, Repositories: map[string]iface.RepositoryRead{"web/deb": {Read: iface.ReadPartial}}}
		for _, p := range req.Packages {
			out.Packages = append(out.Packages, iface.PackageRead{Repository: p.Repository, Package: p.Package, Read: iface.ReadFull})
		}
		return out
	})
	pair := iface.CheckPackage{Repository: "web/deb", Package: "g++"}
	ans, err := r.c.Check(context.Background(), mainclient.Anonymous, iface.CheckRequest{ClientAddress: "203.0.113.7", Repositories: []string{"web/deb"}, Packages: iface.CheckPackages{pair}})
	if err != nil || len(ans.Packages) != 1 || ans.Packages[0] != (iface.PackageRead{Repository: "web/deb", Package: "g++", Read: iface.ReadFull}) {
		t.Fatalf("check: %+v %v", ans, err)
	}
	if checks := r.main.Checks(); len(checks) != 1 || len(checks[0].Packages) != 1 || checks[0].Packages[0] != pair {
		t.Fatalf("the fake was asked %+v", checks)
	}
	stray := iface.CheckPackage{Repository: "web/npm", Package: "g++"}
	if _, err := r.c.Check(context.Background(), mainclient.Anonymous, iface.CheckRequest{ClientAddress: "203.0.113.7", Repositories: []string{"web/deb"}, Packages: iface.CheckPackages{stray}}); err == nil {
		t.Fatal("a pair of a repository the check does not name was answered")
	}
	if n := len(r.main.Checks()); n != 1 || r.main.Calls(iface.OperationCheck) != 2 {
		t.Fatalf("the refused check was recorded: %d recorded, %d calls", n, r.main.Calls(iface.OperationCheck))
	}
}
