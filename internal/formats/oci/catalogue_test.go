package oci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// hubAnswer is an upstream that answers every Get with one canned status and body.
type hubAnswer struct {
	status int
	body   string
}

func (hubAnswer) Base() *url.URL            { u, _ := url.Parse("https://registry-1.docker.io/"); return u }
func (hubAnswer) Settings() json.RawMessage { return nil }
func (h hubAnswer) Get(context.Context, *url.URL, string, int64) (*discover.Response, error) {
	return &discover.Response{Status: h.status, Body: []byte(h.body)}, nil
}

func (hubAnswer) Document(context.Context, string, string) ([]byte, bool, error) {
	return nil, false, nil
}

func (hubAnswer) Artifact(context.Context, string, int64) ([]byte, bool, error) {
	return nil, false, nil
}

// TestR_OCI_038_DockerHubsSearchFallsBackWhenItsAnswerIsNotAPage: Docker Hub's repository search is undocumented, so an answer that is not
// a page of the expected shape (a 404, a body that does not parse, a body without results) is a *discover.UnavailableError of shape, which
// the engine turns into "no search" for its cache period (R-CUR-008, Q-CUR-15). Milliseconds, answers of a few bytes.
func TestR_OCI_038_DockerHubsSearchFallsBackWhenItsAnswerIsNotAPage(t *testing.T) {
	for name, h := range map[string]hubAnswer{
		"a 404":                  {404, `{"message":"not found"}`},
		"a body that is no JSON": {200, `<html>maintenance</html>`},
		"a page without results": {200, `{"count":3}`},
		"a server error":         {502, ``},
	} {
		t.Run("R-OCI-038 "+name, func(t *testing.T) {
			_, err := remoteAdapter{}.Search(context.Background(), h, discover.SearchQuery{Text: "nginx", Limit: 5})
			var ue *discover.UnavailableError
			if !errors.As(err, &ue) || !ue.Shape || ue.Reason != hubShape {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

// canned is an upstream that answers the recorded answers of testdata/catalogue by URL (404 for any other) and records what it is asked.
type canned struct {
	base    string
	answers map[string]discover.Response
	asked   []string
}

func (c *canned) Base() *url.URL          { u, _ := url.Parse(c.base); return u }
func (*canned) Settings() json.RawMessage { return nil }
func (*canned) Document(context.Context, string, string) ([]byte, bool, error) {
	return nil, false, nil
}

func (*canned) Artifact(context.Context, string, int64) ([]byte, bool, error) { return nil, false, nil }

func (c *canned) Get(_ context.Context, u *url.URL, _ string, _ int64) (*discover.Response, error) {
	c.asked = append(c.asked, u.String())
	if a, ok := c.answers[u.String()]; ok {
		return &a, nil
	}
	return &discover.Response{Status: http.StatusNotFound}, nil
}

func recorded(t *testing.T, status int, name string) discover.Response {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "catalogue", name))
	if err != nil {
		t.Fatal(err)
	}
	return discover.Response{Status: status, Body: b}
}

// TestR_CUR_007_ALevelTheRegistryDoesNotHaveIsUnknown: a Docker Hub, Quay or GitLab level that lists nothing says whether the operator
// has it, from the answers the operators gave on 2026-11-24 (Q-CUR-165). Docker Hub's listing answers an unknown namespace with an empty
// page and its account (`/v2/users/{namespace}`) with 404; a name with a character no namespace holds is not asked for, and the name
// without it is suggested when Docker Hub has it; a namespace that is there and lists nothing stays empty; an underscore is asked for.
// Milliseconds, answers of a few bytes.
func TestR_CUR_007_ALevelTheRegistryDoesNotHaveIsUnknown(t *testing.T) {
	const hub = "https://hub.docker.com/v2/"
	children := func(t *testing.T, c *canned, level string) discover.LevelPage {
		t.Helper()
		p, err := remoteAdapter{}.Children(context.Background(), c, discover.LevelRequest{Level: discover.LevelRef{Path: []string{level}}, Limit: 5})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Run("R-CUR-007 a Docker Hub namespace it does not have", func(t *testing.T) {
		c := &canned{base: "https://registry-1.docker.io/", answers: map[string]discover.Response{
			hub + "namespaces/guidedtraffik/repositories?page=1&page_size=5": recorded(t, 200, "hub-ns-guidedtraffik.json"),
			hub + "users/guidedtraffik":                                      recorded(t, 404, "hub-user-guidedtraffik.json"),
		}}
		p := children(t, c, "guidedtraffik")
		if p.Unknown == nil || *p.Unknown != (discover.UnknownLevel{}) || len(c.asked) != 2 {
			t.Fatalf("page %+v, unknown %+v, asked %v", p, p.Unknown, c.asked)
		}
	})
	t.Run("R-CUR-007 a hyphen is no Docker Hub namespace's and is not asked for", func(t *testing.T) {
		c := &canned{base: "https://registry-1.docker.io/", answers: map[string]discover.Response{
			// What the owner's installation asked on 2026-11-24 and Docker Hub answered: 200 with nothing.
			hub + "namespaces/guided-traffic/repositories?page=1&page_size=5": recorded(t, 200, "hub-ns-guided-traffic.json"),
			hub + "users/guidedtraffic":                                       {Status: http.StatusPermanentRedirect, Header: http.Header{"Location": {"/v2/orgs/guidedtraffic"}}},
		}}
		p := children(t, c, "Guided-Traffic")
		want := discover.UnknownLevel{Rule: discover.NameRuleLowercaseDigits, Forbidden: "-", Suggestion: "guidedtraffic"}
		if p.Unknown == nil || *p.Unknown != want || len(c.asked) != 1 || c.asked[0] != hub+"users/guidedtraffic" {
			t.Fatalf("unknown %+v, asked %v", p.Unknown, c.asked)
		}
	})
	t.Run("R-CUR-007 no suggestion where the name without the forbidden character is unknown too", func(t *testing.T) {
		c := &canned{base: "https://registry-1.docker.io/", answers: map[string]discover.Response{
			hub + "users/guidedtraffik": recorded(t, 404, "hub-user-guidedtraffik.json"),
		}}
		p := children(t, c, "guided.traffik")
		want := discover.UnknownLevel{Rule: discover.NameRuleLowercaseDigits, Forbidden: "."}
		if p.Unknown == nil || *p.Unknown != want || len(c.asked) != 1 {
			t.Fatalf("unknown %+v, asked %v", p.Unknown, c.asked)
		}
	})
	t.Run("R-CUR-007 a Docker Hub namespace that is there and lists nothing is empty", func(t *testing.T) {
		c := &canned{base: "https://registry-1.docker.io/", answers: map[string]discover.Response{
			hub + "namespaces/guidedtraffic/repositories?page=1&page_size=5": recorded(t, 200, "hub-ns-guidedtraffik.json"),
			hub + "users/guidedtraffic":                                      {Status: http.StatusPermanentRedirect},
		}}
		if p := children(t, c, "guidedtraffic"); p.Unknown != nil || !p.Done {
			t.Fatalf("page %+v", p)
		}
	})
	t.Run("R-CUR-007 an underscore is asked for", func(t *testing.T) {
		c := &canned{base: "https://registry-1.docker.io/"}
		children(t, c, "old_name")
		if len(c.asked) == 0 || c.asked[0] != hub+"namespaces/old_name/repositories?page=1&page_size=5" {
			t.Fatalf("asked %v", c.asked)
		}
	})
	t.Run("R-CUR-007 a Quay organization that lists no public image is empty", func(t *testing.T) {
		c := &canned{base: "https://quay.io/", answers: map[string]discover.Response{
			"https://quay.io/api/v1/repository?namespace=coreos&public=true": recorded(t, 200, "quay-ns-guidedtraffic.json"),
			"https://quay.io/api/v1/organization/coreos":                     recorded(t, 200, "quay-org-coreos.json"),
		}}
		if p := children(t, c, "coreos"); p.Unknown != nil || len(c.asked) != 2 {
			t.Fatalf("page %+v, asked %v", p, c.asked)
		}
	})
	t.Run("R-CUR-007 another 404 of the project API is no unknown project", func(t *testing.T) {
		c := &canned{base: "https://registry.gitlab.com/", answers: map[string]discover.Response{
			"https://gitlab.com/api/v4/projects/group%2Fproject/registry/repositories?page=1&per_page=5": {Status: 404, Body: []byte(`{"message":"404 Not found"}`)},
		}}
		_, err := remoteAdapter{}.Children(context.Background(), c, discover.LevelRequest{Level: discover.LevelRef{Path: []string{"group/project"}}, Limit: 5})
		if e, ok := errs.As(err); !ok || e.Code != errs.CodeCatalogueNotFound {
			t.Fatalf("err = %v", err)
		}
	})
}

// TestR_SIG_024_AttachmentTagsAreNoVersionsOfTheUpstreamCatalogue: an attachment tag (the referrers tag, cosign's .sig, .att, .sbom) is
// no version the upstream catalogue offers (R-SIG-024, R-CUR-007): neither from a distribution registry's tags list nor from Docker Hub's
// or Quay's API, so the Upstream tab and "download every version" never take a signature for a version. Milliseconds, canned answers of a
// few hundred bytes.
func TestR_SIG_024_AttachmentTagsAreNoVersionsOfTheUpstreamCatalogue(t *testing.T) {
	const hexPart = "5c700ccd073ae0f9bd8d6ed37bfadbabcfbe2a0cbd10564c6a94601233c3ed2a"
	attachments := []string{"sha256-" + hexPart, "sha256-" + hexPart + ".sig", "sha256-" + hexPart + ".att", "sha256-" + hexPart + ".sbom"}
	var hubRows, quayRows []string
	for i, tag := range append([]string{"1.0"}, attachments...) {
		hubRows = append(hubRows, fmt.Sprintf(`{"name":%q,"last_updated":"2026-11-0%dT10:00:00Z","full_size":1}`, tag, i+1))
		quayRows = append(quayRows, fmt.Sprintf(`{"name":%q,"last_modified":"Mon, 0%d Nov 2026 10:00:00 -0000","size":1}`, tag, i+2))
	}
	for name, c := range map[string]*canned{
		"a distribution registry": {base: "https://registry.example/", answers: map[string]discover.Response{
			"https://registry.example/v2/acme/app/tags/list?n=1000": {Status: 200,
				Body: []byte(`{"name":"acme/app","tags":["1.0","` + strings.Join(attachments, `","`) + `"]}`)},
		}},
		"Docker Hub": {base: "https://registry-1.docker.io/", answers: map[string]discover.Response{
			"https://hub.docker.com/v2/namespaces/acme/repositories/app/tags?page=1&page_size=100": {Status: 200,
				Body: []byte(`{"count":5,"next":null,"results":[` + strings.Join(hubRows, ",") + `]}`)},
		}},
		"Quay": {base: "https://quay.io/", answers: map[string]discover.Response{
			"https://quay.io/api/v1/repository/acme/app/tag/?limit=100&onlyActiveTags=true&page=1": {Status: 200,
				Body: []byte(`{"tags":[` + strings.Join(quayRows, ",") + `],"has_additional":false}`)},
		}},
	} {
		t.Run("R-SIG-024 "+name, func(t *testing.T) {
			p, err := remoteAdapter{}.Package(context.Background(), c, "acme/app", discover.VersionsRequest{Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, v := range p.Versions {
				got = append(got, v.Version)
			}
			if !slices.Equal(got, []string{"1.0"}) || (p.Total >= 0 && p.Total != 1) {
				t.Fatalf("versions %v, total %d, want 1.0 alone; asked %v", got, p.Total, c.asked)
			}
		})
	}
}
