package ansible

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
	"github.com/guided-traffic/quader/internal/repository"
)

// deliveryHost is a remote repository over the upstream of check 23 (acme.tools 1.0.0 and 2.0.0) with the rules.
func deliveryHost(d *formattest.DeliveryRules) *formattest.Host {
	o := hostOptions()
	o.Class, o.Remote, o.Delivery = formats.Remote, deliveryFixture().Remote(), d
	return formattest.NewHost(New(), o)
}

// TestR_ANS_047_VersionListCountsAndPagesTheDeliveredVersions: meta.count counts the delivered versions and the links are computed over
// them, so a page of one version has no next page when one version is delivered (R-ANS-047).
func TestR_ANS_047_VersionListCountsAndPagesTheDeliveredVersions(t *testing.T) {
	h := deliveryHost(&formattest.DeliveryRules{Mode: rc.AllExceptBlocked, Rules: []rules.Rule{
		{Kind: rc.KindVersion, Name: "acme.tools", Version: "2.0.0", Effect: rc.Block},
	}})
	resp, err := h.Serve(t.Context(), http.MethodGet, "v3/collections/acme/tools/versions/", nil, false,
		func(r *http.Request) { r.URL.RawQuery = "limit=1&offset=0" })
	if err != nil || resp.Code != http.StatusOK {
		t.Fatalf("version list: %v %d %s", err, resp.Code, resp.Body.String())
	}
	var l struct {
		Meta  struct{ Count int } `json:"meta"`
		Links struct{ Next *string }
		Data  []struct{ Version string }
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &l); err != nil {
		t.Fatal(err)
	}
	if l.Meta.Count != 1 || l.Links.Next != nil || len(l.Data) != 1 || l.Data[0].Version != "1.0.0" {
		t.Fatalf("got count %d, next %v, data %+v; want 1, none, [1.0.0]", l.Meta.Count, l.Links.Next, l.Data)
	}
}

// TestR_ANS_047_ACollectionWithoutADeliveredVersionIsRefused: in mode onlyAllowed a rule that allows a version the upstream does not
// list delivers the package by the package's decision, and none of its listed versions: its documents answer the refusal.
func TestR_ANS_047_ACollectionWithoutADeliveredVersionIsRefused(t *testing.T) {
	h := deliveryHost(&formattest.DeliveryRules{Mode: rc.OnlyAllowed, Rules: []rules.Rule{
		{Kind: rc.KindVersion, Name: "acme.tools", Version: "3.0.0", Effect: rc.Allow},
	}})
	for _, p := range []string{"v3/collections/acme/tools/", "v3/collections/acme/tools/versions/"} {
		resp, err := h.Serve(t.Context(), http.MethodGet, p, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Errors []struct{ Status, Code string }
		}
		_ = json.Unmarshal(resp.Body.Bytes(), &body)
		if resp.Code != http.StatusForbidden || len(body.Errors) != 1 || body.Errors[0].Code != "not_allowed_in_repository" ||
			resp.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %q %s, want 403 not_allowed_in_repository with no-store", p, resp.Code, resp.Header().Get("Cache-Control"), resp.Body.String())
		}
	}
}

// TestR_ANS_047_AVirtualMergeLeavesOutWhatARemoteMembersRulesRefuse: a remote member's part of the merged version list lists only what
// that member's rules deliver, and a second member that delivers the version still supplies it (R-CUR-003 point 3).
func TestR_ANS_047_AVirtualMergeLeavesOutWhatARemoteMembersRulesRefuse(t *testing.T) {
	curated, plain := remote("curated", 2, 0), remote("plain", 3, 1)
	block := []rules.Rule{{Kind: rc.KindVersion, Name: "acme.tools", Version: "2.0.0", Effect: rc.Block}}
	withRules, err := repository.WithRules(curated.repo(), rc.AllExceptBlocked, block, rules.Canon{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		members []testMember
		want    string
	}{
		{[]testMember{curated}, "1.0.0"},
		{[]testMember{curated, plain}, "2.0.0,1.0.0"},
	} {
		w := newWorld(t, tc.members...)
		repos := []repository.Repository{withRules}
		if len(tc.members) == 2 {
			repos = append(repos, plain.repo())
			w.doc("collections/acme/tools/versions.json", "plain", listPage(1, "2.0.0"))
		}
		*w.h.Repo() = repository.Assemble(*w.h.Repo(), repos, nil, 60)
		w.doc("collections/acme/tools/versions.json", "curated", listPage(2, "2.0.0", "1.0.0"))
		r := w.get(versionsPath)
		if r.Code != http.StatusOK {
			t.Fatalf("%d %s", r.Code, r.Body.String())
		}
		if v := decodeVersions(t, r); strings.Join(versionsOf(v), ",") != tc.want || v.Meta.Count != strings.Count(tc.want, ",")+1 {
			t.Errorf("members %d: list %+v, want %s", len(tc.members), v, tc.want)
		}
	}
}
