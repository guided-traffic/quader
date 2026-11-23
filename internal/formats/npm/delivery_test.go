package npm

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/internal/formats/npm/npmtest"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
	"github.com/guided-traffic/quader/internal/repository"
)

// deliveryUpstream is an upstream document of left-pad whose latest and next point to 2.0.0 and whose old points to 1.0.0.
func deliveryUpstream() []byte {
	var d map[string]any
	_ = json.Unmarshal(upstreamDocument("left-pad", "https://registry.example.org/left-pad/-/", "1.0.0", "1.1.0", "2.0.0"), &d)
	d["dist-tags"] = map[string]string{"latest": "2.0.0", "next": "2.0.0", "old": "1.0.0"}
	b, _ := json.Marshal(d)
	return b
}

func deliveryHost(d *formattest.DeliveryRules) *formattest.Host {
	return formattest.NewHost(New(), formattest.HostOptions{
		Class: formats.Remote, RepoKey: "npm-remote", Delivery: d, Data: npmtest.New(),
		Remote: &formattest.MemRemote{Docs: map[string][]byte{"left-pad": deliveryUpstream()}},
	})
}

func blockVersion(v string) *formattest.DeliveryRules {
	return &formattest.DeliveryRules{Mode: rc.AllExceptBlocked, Rules: []rules.Rule{{Kind: rc.KindVersion, Name: "left-pad", Version: v, Effect: rc.Block}}}
}

func getOn(t *testing.T, h *formattest.Host, path string) *formattest.Response {
	t.Helper()
	resp, err := h.Serve(context.Background(), http.MethodGet, path, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// wantRefusal holds the answer of R-CUR-004 in npm's shape: 403, Cache-Control: no-store, and the three members of R-NPM-005 with the
// code not_allowed_in_repository.
func wantRefusal(t *testing.T, resp *formattest.Response, what string) {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: %d %s", what, resp.Code, resp.Body)
	}
	if resp.Code != http.StatusForbidden || body.Code != errs.CodeNotAllowedInRepository || !strings.Contains(resp.Header().Get("Cache-Control"), "no-store") ||
		body.Error == "" || body.Error != body.Message || !strings.Contains(body.Error, "is not allowed in it/npm-remote") {
		t.Fatalf("%s: %d %q %+v", what, resp.Code, resp.Header().Get("Cache-Control"), body)
	}
}

// TestR_NPM_049_ARemoteDocumentListsTheDeliveredVersions: a version rule that refuses 2.0.0 leaves it out of the full document, moves
// latest to the highest delivered release, drops a tag that points to it and keeps the others; the dist-tags say the same, the version
// document of 2.0.0 answers the refusal and the one of the dropped tag a 404. The rules join the rendition's key and validator, so the
// document of the same remote without rules is another one (R-NPM-049, R-CUR-003). Unit, milliseconds, bytes.
func TestR_NPM_049_ARemoteDocumentListsTheDeliveredVersions(t *testing.T) {
	h := deliveryHost(blockVersion("2.0.0"))
	resp := getOn(t, h, "left-pad")
	var doc struct {
		Versions map[string]json.RawMessage `json:"versions"`
		Tags     map[string]string          `json:"dist-tags"`
		Time     map[string]string          `json:"time"`
		Desc     string                     `json:"description"`
	}
	if resp.Code != 200 || json.Unmarshal(resp.Body.Bytes(), &doc) != nil {
		t.Fatalf("%d %s", resp.Code, resp.Body)
	}
	var versions []string
	for v := range doc.Versions {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	if !slices.Equal(versions, []string{"1.0.0", "1.1.0"}) || doc.Tags["latest"] != "1.1.0" || doc.Tags["old"] != "1.0.0" || len(doc.Tags) != 2 {
		t.Fatalf("versions %v, tags %v", versions, doc.Tags)
	}
	if _, ok := doc.Time["2.0.0"]; ok || doc.Desc != "upstream 1.1.0" {
		t.Fatalf("time %v, description %q", doc.Time, doc.Desc)
	}
	if tags := getOn(t, h, "-/package/left-pad/dist-tags"); tags.Code != 200 || strings.Contains(tags.Body.String(), "2.0.0") {
		t.Fatalf("dist-tags: %d %s", tags.Code, tags.Body)
	}
	wantRefusal(t, getOn(t, h, "left-pad/2.0.0"), "the version document of 2.0.0")
	if r := getOn(t, h, "left-pad/next"); r.Code != http.StatusNotFound {
		t.Fatalf("the document of a dropped tag: %d %s", r.Code, r.Body)
	}
	if r := getOn(t, h, "left-pad/latest"); r.Code != 200 || !strings.Contains(r.Body.String(), `"version":"1.1.0"`) {
		t.Fatalf("the document of latest: %d %s", r.Code, r.Body)
	}
	plain := getOn(t, deliveryHost(nil), "left-pad")
	if plain.Code != 200 || plain.Header().Get("ETag") == resp.Header().Get("ETag") {
		t.Fatalf("the document without rules has the validator of the filtered one: %d %q", plain.Code, plain.Header().Get("ETag"))
	}
}

// TestR_NPM_049_APackageWithoutADeliveredVersionAnswersTheRefusal: a package the rules refuse whole, and one whose only allowing rule
// names a version the upstream does not hold, answer the refusal at the package document and its dist-tags, not a 404 (R-NPM-049,
// R-CUR-004). Unit, milliseconds, bytes.
func TestR_NPM_049_APackageWithoutADeliveredVersionAnswersTheRefusal(t *testing.T) {
	for name, d := range map[string]*formattest.DeliveryRules{
		"blocked whole": {Mode: rc.AllExceptBlocked, Rules: []rules.Rule{{Kind: rc.KindPackage, Name: "left-pad", Effect: rc.Block}}},
		"allowed only in a version the upstream lacks": {
			Mode: rc.OnlyAllowed, Rules: []rules.Rule{{Kind: rc.KindVersion, Name: "left-pad", Version: "9.9.9", Effect: rc.Allow}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := deliveryHost(d)
			wantRefusal(t, getOn(t, h, "left-pad"), "the package document")
			wantRefusal(t, getOn(t, h, "-/package/left-pad/dist-tags"), "the dist-tags")
		})
	}
}

// TestR_NPM_049_AVirtualMergeTakesWhatEachMemberDelivers: each remote member's part of a merge lists what that member's rules deliver,
// so a version one member refuses comes from another that delivers it, is absent when no member delivers it, and a merge whose every
// part the rules emptied answers the first such member's refusal, which names it (R-CUR-003 point 3, R-CUR-004, Q-CUR-8). Unit,
// milliseconds, bytes.
func TestR_NPM_049_AVirtualMergeTakesWhatEachMemberDelivers(t *testing.T) {
	remote := func(id, key string, d *formattest.DeliveryRules) repository.Repository {
		r := repository.Repository{ID: id, Name: qr(key), Class: formats.Remote, Format: "npm", Lifecycle: repository.Active, Config: []byte(`{}`)}
		if d == nil {
			return r
		}
		r, err := repository.WithRules(r, d.Mode, d.Rules, rules.Canon{})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	merged := func(members []repository.Repository, docs ...formats.MemberDocument) ([]byte, error) {
		h := formattest.NewHost(New(), formattest.HostOptions{Class: formats.Virtual, RepoKey: "npm-all", Data: npmtest.New()})
		*h.Repo() = repository.Assemble(*h.Repo(), members, nil, 60)
		rq, err := h.NewRequest(context.Background(), http.MethodGet, "left-pad", nil)
		if err != nil {
			t.Fatal(err)
		}
		return New().Virtual().Merge(context.Background(), rq, formats.Path{Path: "left-pad"}, docs)
	}
	first := remote("3", "first", blockVersion("2.0.0"))
	second := remote("4", "second", nil)
	part := func(key string, pos int, versions ...string) formats.MemberDocument {
		return formats.MemberDocument{Member: formats.RepoRef{Name: qr(key)}, Tier: formats.TierRemote, Position: pos, Doc: markedDocument("left-pad", key, versions...)}
	}

	doc, err := merged([]repository.Repository{first, second}, part("first", 0, "1.0.0", "2.0.0"), part("second", 1, "2.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	if got := entrySource(doc, "2.0.0"); got != "second" || entrySource(doc, "1.0.0") != "first" {
		t.Fatalf("2.0.0 comes from %q: %s", got, doc)
	}
	doc, err = merged([]repository.Repository{first}, part("first", 0, "1.0.0", "2.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(documentVersions(doc), "2.0.0") {
		t.Fatalf("the merge lists the refused version: %s", doc)
	}
	_, err = merged([]repository.Repository{first}, part("first", 0, "2.0.0"))
	if e, ok := errs.As(err); !ok || e.Code != errs.CodeNotAllowedInRepository || !strings.Contains(e.Detail, "it/first") {
		t.Fatalf("a merge the rules emptied: %v", err)
	}
}
