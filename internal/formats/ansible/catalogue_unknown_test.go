package ansible

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// galaxy answers the recorded answers of the public Galaxy by URL (404 for any other) and its root document, and records what it is asked.
type galaxy struct {
	t       *testing.T
	answers map[string]discover.Response
	asked   []string
}

func (*galaxy) Base() *url.URL {
	u, _ := url.Parse("https://galaxy.ansible.com/api/")
	return u
}
func (*galaxy) Settings() json.RawMessage { return nil }
func (g *galaxy) Get(_ context.Context, u *url.URL, _ string, _ int64) (*discover.Response, error) {
	g.asked = append(g.asked, u.String())
	if a, ok := g.answers[u.String()]; ok {
		return &a, nil
	}
	return &discover.Response{Status: http.StatusNotFound}, nil
}

func (g *galaxy) Document(_ context.Context, path, _ string) ([]byte, bool, error) {
	if path != RootDocument {
		return nil, false, nil
	}
	return recordedGalaxy(g.t, "gal-root.json"), true, nil
}

func (*galaxy) Artifact(context.Context, string, int64) ([]byte, bool, error) {
	return nil, false, nil
}

func recordedGalaxy(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "catalogue", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestR_CUR_007_AGalaxyNamespaceItDoesNotHaveIsUnknown: a namespace with a hyphen breaks the Galaxy's rule (lowercase letters, digits
// and underscores) and is not asked for; the name with an underscore for the hyphen is suggested when the Galaxy has it, as it has
// netbox_community, the namespace of the GitHub organisation netbox-community; a namespace that lists nothing is unknown when its
// document answers 404 and empty when it answers 200 (Q-CUR-165), from the answers the Galaxy gave on 2026-11-24. Milliseconds, answers
// of a few hundred bytes.
func TestR_CUR_007_AGalaxyNamespaceItDoesNotHaveIsUnknown(t *testing.T) {
	const api = "https://galaxy.ansible.com/api/v3/"
	children := func(t *testing.T, g *galaxy, ns string) discover.LevelPage {
		t.Helper()
		p, err := remoteAdapter{}.Children(context.Background(), g, discover.LevelRequest{Level: discover.LevelRef{Path: []string{ns}}, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Run("R-CUR-007 a hyphen breaks the rule and the underscore's namespace is suggested", func(t *testing.T) {
		g := &galaxy{t: t, answers: map[string]discover.Response{
			api + "namespaces/netbox_community/": {Status: 200, Body: recordedGalaxy(t, "gal-namespace-netbox_community.json")},
		}}
		p := children(t, g, "netbox-community")
		want := discover.UnknownLevel{Rule: discover.NameRuleLowercaseDigitsUnderscore, Forbidden: "-", Suggestion: "netbox_community"}
		if p.Unknown == nil || *p.Unknown != want || len(g.asked) != 1 {
			t.Fatalf("unknown %+v, asked %v", p.Unknown, g.asked)
		}
	})
	t.Run("R-CUR-007 a namespace the Galaxy does not have", func(t *testing.T) {
		g := &galaxy{t: t, answers: map[string]discover.Response{
			api + "collections/?limit=2&namespace=guidedtraffic&offset=0": {Status: 200, Body: recordedGalaxy(t, "gal-ns-guidedtraffic.json")},
			api + "namespaces/guidedtraffic/":                             {Status: 404, Body: recordedGalaxy(t, "gal-namespace-guidedtraffic.json")},
		}}
		if p := children(t, g, "guidedtraffic"); p.Unknown == nil || p.Unknown.Rule != "" || len(g.asked) != 2 {
			t.Fatalf("page %+v, asked %v", p, g.asked)
		}
	})
	t.Run("R-CUR-007 a namespace that is there and lists nothing is empty", func(t *testing.T) {
		g := &galaxy{t: t, answers: map[string]discover.Response{
			api + "collections/?limit=2&namespace=netbox_community&offset=0": {Status: 200, Body: recordedGalaxy(t, "gal-ns-guidedtraffic.json")},
			api + "namespaces/netbox_community/":                             {Status: 200, Body: recordedGalaxy(t, "gal-namespace-netbox_community.json")},
		}}
		if p := children(t, g, "netbox_community"); p.Unknown != nil || !p.Done {
			t.Fatalf("page %+v", p)
		}
	})
}
