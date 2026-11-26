//go:build integration

package ansiblelocal

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/formats/ansible/collection/collectiontest"
	"github.com/guided-traffic/quader/internal/formats/ansible/docgen"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// contentSpec is a collection of about two kilobytes with what the documentation site shows beyond a plugin's own documentation: a
// module that extends a fragment of the collection and one of ansible.builtin, a role with two entry points, and a changelog.yaml.
func contentSpec(version string) collectiontest.Spec {
	s := spec(version)
	s.Files["plugins/doc_fragments/conn.py"] = "class ModuleDocFragment(object):\n    DOCUMENTATION = r'''\noptions:\n  api_url:\n" +
		"    type: str\n    required: true\n    description: The API.\nnotes:\n  - From the fragment.\n'''\n"
	s.Files["plugins/modules/ping.py"] = "DOCUMENTATION = r'''\nmodule: ping\nshort_description: Pings\n" +
		"extends_documentation_fragment: [acme.tools.conn, ansible.builtin.files]\noptions:\n  host: {type: str, description: The host.}\n'''\n"
	s.Files["roles/web/meta/argument_specs.yml"] = "argument_specs:\n  main:\n    short_description: Installs\n" +
		"    description: [Set O(tls.cert).]\n    options:\n      tls:\n        type: dict\n        description: TLS.\n" +
		"        options:\n          cert: {type: path, required: true, description: The certificate.}\n" +
		"  reload:\n    short_description: Reloads\n    options:\n      graceful: {type: bool, default: true, description: Waits.}\n"
	s.Files["changelogs/changelog.yaml"] = "releases:\n  1.0.0:\n    changes:\n      release_summary: First.\n" +
		"  1.1.0:\n    changes:\n      bugfixes:\n      - ping - a <b>tag</b> stays text.\n"
	return s
}

// TestR_ANS_051_R_ANS_052_R_ANS_053_TheFragmentsTheEntryPointsAndTheChangelogAreServed: the operations answer the merged option and the
// fragment that was not merged, the role's entry points with the anchor of an entry point's option, and the changelog in the tree and as
// a file, rendered with its tag as text; the reindex and a starting worker rebuild a changelog an older generator recorded.
// Integration, about 3 s (one stack, a second replica started), archives of about 2 KiB.
func TestR_ANS_051_R_ANS_052_R_ANS_053_TheFragmentsTheEntryPointsAndTheChangelogAreServed(t *testing.T) {
	e := newEnv(t)
	if a := e.publish(e.tokA, collectiontest.Build(contentSpec("1.1.0")), true); a.Status != 202 {
		t.Fatalf("publish: %d %s", a.Status, a.Body)
	}
	p := "/api/v1/packages/" + e.packageID() + "/versions/1.1.0"

	ping := e.st.Do(e.r, "GET", p+"/docs/entries/module/ping", e.tokC, nil).JSON(t)
	opts := fmt.Sprint(ping["options"])
	if !strings.Contains(opts, "api_url") || fmt.Sprint(ping["extendsFragments"]) != "[ansible.builtin.files]" ||
		!strings.Contains(fmt.Sprint(ping["notes"]), "From the fragment") {
		t.Errorf("module = %v", ping)
	}

	web := e.st.Do(e.r, "GET", p+"/docs/entries/role/web", e.tokC, nil).JSON(t)
	eps, _ := web["entryPoints"].([]any)
	if len(eps) != 2 || web["shortDescription"] != "Installs" {
		t.Fatalf("role = %v", web)
	}
	main := eps[0].(map[string]any)
	if main["name"] != "main" || !strings.Contains(fmt.Sprint(main["description"]), `href="#parameter-main--tls/cert"`) ||
		!strings.Contains(fmt.Sprint(main["options"]), "cert") {
		t.Errorf("main = %v", main)
	}

	tree := e.st.Do(e.r, "GET", p+"/docs", e.tokC, nil).JSON(t)
	cl, _ := tree["changelog"].(map[string]any)
	if cl["name"] != "changelog" || cl["source"] != "changelogs/changelog.yaml" || cl["format"] != "markdown" || cl["truncated"] != false {
		t.Errorf("tree's changelog = %v", tree["changelog"])
	}
	file := e.st.Do(e.r, "GET", p+"/docs/files/changelog", e.tokC, nil)
	f := file.JSON(t)
	html, _ := f["html"].(string)
	if file.Status != 200 || f["contentType"] != "text/markdown" || strings.Index(html, "v1.1.0") > strings.Index(html, "v1.0.0") ||
		!strings.Contains(html, "&lt;b&gt;tag&lt;/b&gt;") || strings.Contains(html, "<b>") {
		t.Errorf("changelog: %d %s", file.Status, file.Body)
	}

	// A changelog an older generator recorded is read again: by the reindex, and by the reindex a starting worker queues itself.
	id := e.repoID()
	stale := func() {
		e.st.Exec(`UPDATE ansible_changelogs SET generator = $2, body = 'old' WHERE repo_id = $1`, id, docgen.Version-1)
	}
	current := func() bool {
		return e.st.Number(`SELECT count(*) FROM ansible_changelogs WHERE repo_id = $1 AND generator = $2 AND body <> 'old'`, id, docgen.Version) == 1
	}
	stale()
	e.st.Start(t, testenv.ReplicaOptions{Name: "web-late", Role: "all"})
	waitFor(t, "the reindex a starting worker queued", 60*time.Second, current)
}
