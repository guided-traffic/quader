package ansible

import (
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// documents is a lent upstream that answers documents by path and refuses one path as the catalogue's upstream refuses an address the
// repository does not reach.
type documents struct {
	docs    map[string][]byte
	refused string
}

func (*documents) Base() *url.URL {
	u, _ := url.Parse("https://galaxy.example/api/")
	return u
}
func (*documents) Settings() json.RawMessage { return nil }
func (*documents) Get(context.Context, *url.URL, string, int64) (*discover.Response, error) {
	return &discover.Response{Status: 404}, nil
}

func (d *documents) Document(_ context.Context, path, _ string) ([]byte, bool, error) {
	if path == d.refused {
		return nil, false, errs.E(errs.UpstreamFailed, errs.CodeForeignHostDenied, "refused",
			errs.WithCause(refusedAddress("https://docs.other.example/role/16/content/")))
	}
	b, ok := d.docs[path]
	return b, ok, nil
}

func (*documents) Artifact(context.Context, string, int64) ([]byte, bool, error) { return nil, false, nil }

// refusedAddress stands for the engine's cause of a refusal, which names the address it did not ask.
type refusedAddress string

func (r refusedAddress) Error() string          { return "not asked: " + string(r) }
func (r refusedAddress) RefusedAddress() string { return string(r) }

// TestR_CUR_028_AGalaxysFactsAndReadmes: the facts of a collection version come from its version document's metadata (the links, the
// dependencies; an empty licence list names none); a role's README is the HTML of its content document; a document the engine refused
// to ask answers the host it names. Milliseconds, answers of a few hundred bytes.
func TestR_CUR_028_AGalaxysFactsAndReadmes(t *testing.T) {
	a := aboutOfVersion(recordedGalaxy(t, "gal-version.json"), "2.0.1")
	want := discover.About{Version: "2.0.1", Links: []discover.Link{
		{Kind: discover.LinkRepository, URL: "https://github.com/osism/ansible-collection-sonic"},
		{Kind: discover.LinkIssues, URL: "https://github.com/osism/ansible-collection-sonic/issues"},
	}, Dependencies: []discover.Dependency{{Name: "ansible.netcommon", Range: ">=2.0.0"}}}
	if !reflect.DeepEqual(a, want) {
		t.Fatalf("R-CUR-028 the facts of osism.sonic 2.0.1: %+v", a)
	}
	ctx := context.Background()
	up := &documents{docs: map[string][]byte{
		RoleContentDocumentPath("16"): []byte(`{"readme":"README.md","readme_html":"<h1>geerlingguy.docker</h1>"}`),
	}}
	r, err := remoteAdapter{}.Readme(ctx, up, "role/16", "7.4.1")
	if err != nil || !r.Found || !r.HTML || r.Text != "<h1>geerlingguy.docker</h1>" || r.Version != "7.4.1" {
		t.Fatalf("R-CUR-028 a role's README: %+v %v", r, err)
	}
	up.refused = RoleContentDocumentPath("16")
	if r, err := (remoteAdapter{}).Readme(ctx, up, "role/16", "7.4.1"); err != nil || r.Found || r.Unreachable != "docs.other.example" {
		t.Fatalf("R-CUR-028 a role's README the engine refused to ask: %+v %v", r, err)
	}
}
