package ansible

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/ansible/ansibletest"
	"github.com/guided-traffic/quader/internal/formats/ansible/collection/collectiontest"
	"github.com/guided-traffic/quader/internal/formats/ansible/rows"
)

const testChangelog = "releases:\n  1.0.0:\n    changes:\n      release_summary: First release.\n" +
	"    modules:\n    - {name: ping, description: Pings}\n"

// TestR_ANS_053_APublishStoresTheChangelogTheFragmentsAndTheEntryPoints: a publish records the changelog of the archive with the version, a
// module's option of a fragment of the collection is merged, a role's argument specs are its entry points; a version without a changelog is
// recorded as having none. Unit (the format against its in-memory port), a few milliseconds, archives of about a kilobyte.
func TestR_ANS_053_APublishStoresTheChangelogTheFragmentsAndTheEntryPoints(t *testing.T) {
	h := newHost(t)
	s := collectionSpec("1.0.0")
	s.Files["changelogs/changelog.yaml"] = testChangelog
	s.Files["plugins/doc_fragments/conn.py"] = "class ModuleDocFragment(object):\n    DOCUMENTATION = r'''\noptions:\n  url: {type: str}\n'''\n"
	s.Files["plugins/modules/ping.py"] = "DOCUMENTATION = '''\nmodule: ping\nextends_documentation_fragment: [acme.tools.conn]\n'''\n"
	s.Files["roles/web/meta/argument_specs.yml"] = "argument_specs:\n  main:\n    short_description: Installs\n    options: {port: {type: int}}\n"
	if r, err := publish(h, collectiontest.Build(s), true, ""); err != nil || r.Code != http.StatusAccepted {
		t.Fatalf("publish: %v %v", r, err)
	}
	st := h.FormatData(ID).(*ansibletest.Store)
	ctx, repo := context.Background(), h.Ref().ID
	c, ok, _ := st.DocChangelog(ctx, repo, "acme.tools", "1.0.0")
	if !ok || c.Source != "changelogs/changelog.yaml" || !strings.Contains(c.Body, "acme\\.tools\\.ping \\- Pings") {
		t.Errorf("changelog = %+v %v", c, ok)
	}
	e, _, _ := st.DocEntry(ctx, repo, "acme.tools", "1.0.0", "module", "ping")
	if !strings.Contains(string(e.Doc), `"name":"url"`) || strings.Contains(string(e.Doc), "extends_documentation_fragment") {
		t.Errorf("module = %s", e.Doc)
	}
	r, _, _ := st.DocEntry(ctx, repo, "acme.tools", "1.0.0", "role", "web")
	var ds struct {
		Doc struct {
			EntryPoints []map[string]any `json:"entry_points"`
		} `json:"doc"`
	}
	if json.Unmarshal(r.Doc, &ds) != nil || len(ds.Doc.EntryPoints) != 1 || r.ShortDescription != "Installs" {
		t.Errorf("role = %+v", r)
	}

	if r, err := publish(h, collectiontest.Build(collectionSpec("1.0.1")), true, ""); err != nil || r.Code != http.StatusAccepted {
		t.Fatalf("publish: %v %v", r, err)
	}
	if c, recorded := st.Changelogs[changelogKeyOf(repo, "acme.tools", "1.0.1")]; !recorded || c != nil {
		t.Errorf("a version without a changelog: %+v, recorded %v", c, recorded)
	}
}

func changelogKeyOf(repo int64, name, version string) string {
	return strconv.FormatInt(repo, 10) + "/" + name + "/" + version
}

// TestR_ANS_053_TheFillOfARemotesArchiveReadsItsChangelog: the inspection of a cached archive carries its changelog and nothing else of
// its documentation; an archive without one carries none, read. Unit, a millisecond.
func TestR_ANS_053_TheFillOfARemotesArchiveReadsItsChangelog(t *testing.T) {
	a := remoteAdapter{f: New()}
	path := formats.Path{Path: "collections/acme/tools/acme-tools-1.0.0.tar.gz"}
	s := collectiontest.Spec{Namespace: "acme", Name: "tools", Version: "1.0.0", Files: map[string]string{"CHANGELOG.rst": "v1.0.0\n======\n"}}
	ins, err := a.InspectFill(context.Background(), formats.RemoteContext{}, path, spooled{collectiontest.Build(s)})
	if err != nil {
		t.Fatal(err)
	}
	p := ins.Payload.(*inspected)
	if !p.changelogRead || p.changelog == nil || p.changelog.Format != rows.DocFormatText || p.docs != nil {
		t.Errorf("payload = %+v", p)
	}
	ins, _ = a.InspectFill(context.Background(), formats.RemoteContext{}, path, spooled{collectiontest.Build(collectiontest.Spec{Namespace: "acme", Name: "tools", Version: "1.0.0"})})
	if p := ins.Payload.(*inspected); !p.changelogRead || p.changelog != nil {
		t.Errorf("none = %+v", p)
	}
}
