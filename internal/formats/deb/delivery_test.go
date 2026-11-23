package deb

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/internal/qname"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
	"github.com/guided-traffic/quader/internal/repository"
)

// TestR_DEB_036_ARefusedPackageFileOfARemoteAnswers403: the .deb of a version the remote's rules refuse answers 403
// not_allowed_in_repository with Cache-Control: no-store as a problem (apt shows the status line), and another version's file is served
// (R-DEB-036, R-CUR-004). The remote's indexes are its upstream's signed bytes, which no rule changes (Q-CUR-9).
func TestR_DEB_036_ARefusedPackageFileOfARemoteAnswers403(t *testing.T) {
	kept, refused := "pool/main/h/hello/hello_1.0_amd64.deb", "pool/main/h/hello/hello_2.0_amd64.deb"
	m := &formattest.MemRemote{Artifacts: map[string][]byte{kept: []byte("deb 1"), refused: []byte("deb 2")}}
	h := formattest.NewHost(New(), formattest.HostOptions{
		Class: formats.Remote, Data: twinPort{twinStore: newTwin()}, Resolver: noShadow{}, Remote: m,
		Delivery: &formattest.DeliveryRules{Mode: rc.AllExceptBlocked, Rules: []rules.Rule{{Kind: rc.KindVersion, Name: "hello", Version: "2.0", Effect: rc.Block}}},
	})
	resp, err := h.Serve(context.Background(), http.MethodGet, kept, nil, false)
	if err != nil || resp.Code != http.StatusOK {
		t.Fatalf("the delivered version: %v %d %s", err, resp.Code, resp.Body.String())
	}
	resp, err = h.Serve(context.Background(), http.MethodGet, refused, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	var body struct{ Code string }
	_ = json.Unmarshal(resp.Body.Bytes(), &body)
	if resp.Code != http.StatusForbidden || body.Code != "not_allowed_in_repository" || resp.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("the refused version: %d %q %s, want 403 not_allowed_in_repository with no-store", resp.Code, resp.Header().Get("Cache-Control"), resp.Body.String())
	}
}

// TestR_DEB_036_TheMergeLeavesOutWhatARemoteMembersRulesRefuse: a paragraph of a remote member whose binary package and version its
// rules refuse is not in the merged Packages, and a later member that holds the same version still supplies it (R-DEB-036, Q-CUR-9).
func TestR_DEB_036_TheMergeLeavesOutWhatARemoteMembersRulesRefuse(t *testing.T) {
	para := func(ver string) string {
		return "Package: hello\nVersion: " + ver + "\nArchitecture: amd64\nFilename: pool/main/h/hello/hello_" + ver + "_amd64.deb\nSize: 1\nSHA256: " +
			strings.Repeat("a", 64) + "\n"
	}
	curated := repository.Repository{ID: "2", Name: qname.Repo{Project: "it", Key: "curated"}, Class: repository.Remote, Format: string(ID)}
	curated, err := repository.WithRules(curated, rc.AllExceptBlocked,
		[]rules.Rule{{Kind: rc.KindVersion, Name: "hello", Version: "2.0", Effect: rc.Block}}, rules.Canon{})
	if err != nil {
		t.Fatal(err)
	}
	plain := repository.Repository{ID: "3", Name: qname.Repo{Project: "it", Key: "plain"}, Class: repository.Remote, Format: string(ID)}
	virt := repository.Assemble(repository.Repository{ID: "1", Name: qname.Repo{Project: "it", Key: "virt"}, Class: repository.Virtual, Format: string(ID)},
		[]repository.Repository{curated, plain}, nil, 60)
	rq := &formats.Request{Repo: &virt}
	part := func(r repository.Repository, pos int, doc string) formats.MemberDocument {
		return formats.MemberDocument{Member: formats.RepoRef{Name: r.Name, Class: formats.Remote}, Tier: formats.TierRemote, Position: pos, Doc: []byte(doc)}
	}

	out, err := virtualAdapter{}.Merge(context.Background(), rq, formats.Path{}, []formats.MemberDocument{part(curated, 0, para("1.0")+"\n"+para("2.0"))})
	if err != nil {
		t.Fatal(err)
	}
	if s := string(out); !strings.Contains(s, "Version: 1.0") || strings.Contains(s, "Version: 2.0") {
		t.Fatalf("the merge of the curated member alone:\n%s", s)
	}
	out, err = virtualAdapter{}.Merge(context.Background(), rq, formats.Path{}, []formats.MemberDocument{
		part(curated, 0, para("1.0")+"\n"+para("2.0")), part(plain, 1, para("2.0")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if s := string(out); !strings.Contains(s, "members/plain/pool/main/h/hello/hello_2.0_amd64.deb") {
		t.Fatalf("2.0 is not supplied by the member that delivers it:\n%s", s)
	}
}
