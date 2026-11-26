//go:build integration

package ansiblelocal

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/formats/ansible/collection/collectiontest"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// collection publishes a collection of acme or another namespace with tags and a description, with alice's token.
func (e *env) collection(ns, name, description string, tags ...string) {
	e.t.Helper()
	s := spec("1.0.0")
	s.Namespace, s.Name = ns, name
	s.Info = map[string]any{"description": description, "tags": tags}
	if a := e.publish(e.tokA, collectiontest.Build(s), true); a.Status != http.StatusAccepted {
		e.t.Fatalf("publish %s.%s: %d %s", ns, name, a.Status, a.Body)
	}
}

func names(t *testing.T, a testenv.Answer) []string {
	t.Helper()
	var out []string
	items, _ := a.JSON(t)["items"].([]any)
	for _, it := range items {
		out = append(out, it.(map[string]any)["name"].(string))
	}
	return out
}

// TestR_ANS_057_CollectionsAreFoundAsOnGalaxy: the package list filters by a namespace and by a verified signature, finds a collection
// by every word as a prefix of its namespace and name, its tags or its description, and ranks a name before a description; a hit carries
// its tags and, for a version with a verified signature, its badge (R-ANS-057, R-UI-220).
func TestR_ANS_057_CollectionsAreFoundAsOnGalaxy(t *testing.T) {
	e := newEnv(t)
	e.collection("acme", "tools", "Tools for hosts.", "linux", "tools")
	e.collection("acme", "net", "Manage docker networks.", "networking")
	e.collection("other", "docker", "Run containers.", "containers")
	// The management API's requests go to a replica of the role api, which serves no package protocol and builds no format host
	// (R-ARCH-001, Q-ARCH-1070).
	apiRep := e.st.Start(t, testenv.ReplicaOptions{Name: "api-a", Role: "api"})

	list := func(q string) []string {
		t.Helper()
		a := e.st.Do(apiRep, "GET", "/api/v1/packages?format=ansible&"+q, e.tokC, nil)
		if a.Status != http.StatusOK {
			t.Fatalf("%s: %d %s", q, a.Status, a.Body)
		}
		return names(t, a)
	}
	if got := list("namespace=acme"); !slices.Equal(got, []string{"acme.net", "acme.tools"}) {
		t.Errorf("namespace acme: %v", got)
	}
	for _, bad := range []string{
		"/api/v1/packages?namespace=acme", "/api/v1/packages?format=ansible&namespace=acme.tools",
		"/api/v1/packages?format=nuget&namespace=acme", "/api/v1/packages?format=ansible&namespace=acme&namePrefix=a",
	} {
		if a := e.st.Do(e.r, "GET", bad, e.tokC, nil); a.Status != http.StatusBadRequest {
			t.Errorf("%s: %d %s", bad, a.Status, a.Body)
		}
	}
	// A name that holds the text before a description that holds it; a word is a prefix of a word (networks), a tag is found.
	if got := list("q=docker&sort=relevance"); !slices.Equal(got, []string{"other.docker", "acme.net"}) {
		t.Errorf("docker by relevance: %v", got)
	}
	if got := list("q=netw"); !slices.Equal(got, []string{"acme.net"}) {
		t.Errorf("a prefix of a word of the description: %v", got)
	}
	if got := list("q=linux+tool&sort=relevance"); !slices.Equal(got, []string{"acme.tools"}) {
		t.Errorf("a tag and a word of the name: %v", got)
	}
	if got := list("q=acme&sort=relevance&limit=1"); len(got) != 1 || got[0] != "acme.net" {
		t.Errorf("the first page of acme by relevance: %v", got)
	}

	a := e.st.Do(apiRep, "GET", "/api/v1/packages?format=ansible&namespace=acme", e.tokC, nil)
	first := a.JSON(t)["items"].([]any)[0].(map[string]any)
	if kw, _ := first["keywords"].([]any); len(kw) != 1 || kw[0] != "networking" || first["signature"] != nil {
		t.Errorf("a hit without a signature: %v", first)
	}
	if got := list("signed=true"); len(got) != 0 {
		t.Errorf("signed before any signature: %v", got)
	}
	// A valid signature of acme.net's archive, attached in the repository and trusted by the installation as internal.
	e.st.Exec(`WITH b AS (SELECT i.blob_sha256 FROM items i JOIN package_versions pv ON pv.id = i.version_id JOIN packages p ON p.id = pv.package_id
	                       WHERE p.name = 'acme.net' AND i.kind = 'file' LIMIT 1),
	                o AS (INSERT INTO signature_objects (sha256, scheme, raw, size) VALUES (sha256('sig'::bytea), 'openpgp', 'sig'::bytea, 3) RETURNING sha256),
	                s AS (INSERT INTO signatures (object_sha256, subject_sha256, role, state) SELECT o.sha256, b.blob_sha256, 'publisher', 'valid' FROM o, b RETURNING id),
	                a AS (INSERT INTO signature_attachments (repo_id, signature_id, location, source) SELECT $1, s.id, 'acme/net/sig', 'upload' FROM s RETURNING signature_id)
	           INSERT INTO signature_trust (signature_id, project_id, state, anchor_epoch) SELECT signature_id, NULL, 'internal', 1 FROM a`, e.repoID())
	if got := list("signed=true"); !slices.Equal(got, []string{"acme.net"}) {
		t.Errorf("signed: %v", got)
	}
	a = e.st.Do(apiRep, "GET", "/api/v1/packages?format=ansible&namespace=acme", e.tokC, nil)
	if first := a.JSON(t)["items"].([]any)[0].(map[string]any); first["signature"] != "verified_internal" {
		t.Errorf("the badge of a verified version: %v", first)
	}
	// The filter deprecated reads the same latest version as signed, and both stand beside the namespace and the ranked text (R-API-085).
	e.st.Exec(`INSERT INTO ansible_collection_deprecations (repo_id, name) VALUES ($1, 'acme.net'), ($1, 'other.docker')`, e.repoID())
	if got := list("namespace=acme&deprecated=true&signed=true&q=docker&sort=relevance"); !slices.Equal(got, []string{"acme.net"}) {
		t.Errorf("namespace, deprecated, signed and a text together: %v", got)
	}
	if got := list("deprecated=true&q=docker&sort=relevance"); !slices.Equal(got, []string{"other.docker", "acme.net"}) {
		t.Errorf("deprecated by relevance: %v", got)
	}
}

// TestR_UI_220_ANamespaceSaysWhoMayPublishToAReaderOfTheConfiguration: the page of a namespace names the local repositories whose
// configuration the caller may read, with their claims that cover names of the namespace, their package ownership and their automatic
// claims; a caller who may read only the packages gets none of it; a text that is no namespace of the format is 404.
func TestR_UI_220_ANamespaceSaysWhoMayPublishToAReaderOfTheConfiguration(t *testing.T) {
	e := newEnv(t)
	// The management API's requests go to a replica of the role api, which serves no package protocol and builds no format host
	// (R-ARCH-001, Q-ARCH-1070).
	apiRep := e.st.Start(t, testenv.ReplicaOptions{Name: "api-a", Role: "api"})
	e.st.Exec(`INSERT INTO namespace_claims (repo_id, pattern, source, scope) VALUES ($1, 'acme.*', 'manual', 'members'), ($1, 'other.x', 'manual', 'global')`, e.repoID())
	get := func(tok, q string) map[string]any {
		t.Helper()
		a := e.st.Do(apiRep, "GET", "/api/v1/packages/namespace?"+q, tok, nil)
		if a.Status != http.StatusOK {
			t.Fatalf("%s: %d %s", q, a.Status, a.Body)
		}
		return a.JSON(t)
	}
	if doc := get(e.tokA, "format=ansible&name=ACME"); doc["name"] != "acme" || doc["kind"] != "namespace" || doc["publishing"] != nil {
		t.Errorf("a reader of the packages: %v", doc)
	}
	doc := get(e.tokAdmin, "format=ansible&name=acme&project="+project)
	repos, _ := doc["publishing"].(map[string]any)["repositories"].([]any)
	if len(repos) != 1 {
		t.Fatalf("a reader of the configuration: %v", doc)
	}
	r := repos[0].(map[string]any)
	claims := fmt.Sprint(r["claims"])
	if r["key"] != repo || r["packageOwnership"] != "off" || !strings.Contains(claims, "acme.*") || !strings.Contains(claims, "all") || strings.Contains(claims, "other.x") {
		t.Errorf("the repository: %v", r)
	}
	if doc := get(e.tokAdmin, "format=ansible&name=acme&project=elsewhere"); doc["publishing"] != nil {
		t.Errorf("another project: %v", doc)
	}
	for _, q := range []string{"format=ansible&name=acme.tools", "format=nuget&name=acme", "format=npm&name=acme", "format=oci&name=team//app"} {
		if a := e.st.Do(apiRep, "GET", "/api/v1/packages/namespace?"+q, e.tokAdmin, nil); a.Status != http.StatusNotFound {
			t.Errorf("%s: %d %s", q, a.Status, a.Body)
		}
	}
}

// TestR_ANS_058_TheDocumentationIsSearchedAcrossCollections: the search finds the modules of the newest version of each collection the
// caller may read, by name first; an older version's entries are no hit; a role with two entry points is one hit, found by a word of
// an entry point, and the changelog is none; a caller who reads the artifacts and not the wiki finds nothing, in the list and in the
// suggestions (R-ANS-058, R-ANS-037, R-ANS-052, R-ANS-053).
func TestR_ANS_058_TheDocumentationIsSearchedAcrossCollections(t *testing.T) {
	e := newEnvOpts(t, envOpts{seed: func(e *env) {
		e.st.Exec(`UPDATE project SET visibility = 'internal' WHERE key = $1`, project)
		e.st.Exec(`UPDATE repositories SET visibility_artifacts = 'internal', visibility_wiki = 'private' WHERE key = $1`, repo)
	}})
	if a := e.publish(e.tokA, docsSpecBytes("1.0.0"), true); a.Status != http.StatusAccepted {
		t.Fatalf("publish 1.0.0: %d %s", a.Status, a.Body)
	}
	newer := spec("2.0.0")
	newer.Files["plugins/modules/mod_3.py"] = "DOCUMENTATION = r'''\nmodule: mod_3\nshort_description: Checks a host again\n'''\n"
	newer.Files["plugins/modules/mod_30.py"] = "DOCUMENTATION = r'''\nmodule: mod_30\nshort_description: Thirty\n'''\n"
	newer.Files["roles/web/meta/argument_specs.yml"] = "argument_specs:\n  main:\n    short_description: Installs\n" +
		"  reload:\n    short_description: Reloads gracefully\n"
	newer.Files["changelogs/changelog.yaml"] = "releases:\n  2.0.0:\n    changes:\n      release_summary: Zeppelin.\n"
	if a := e.publish(e.tokA, collectiontest.Build(newer), true); a.Status != http.StatusAccepted {
		t.Fatalf("publish 2.0.0: %d %s", a.Status, a.Body)
	}
	// The management API's requests go to a replica of the role api, which serves no package protocol and builds no format host
	// (R-ARCH-001, Q-ARCH-1070).
	apiRep := e.st.Start(t, testenv.ReplicaOptions{Name: "api-a", Role: "api"})
	a := e.st.Do(apiRep, "GET", "/api/v1/search/documentation?q=mod_3", e.tokC, nil)
	items, _ := a.JSON(t)["items"].([]any)
	if a.Status != http.StatusOK || len(items) != 2 {
		t.Fatalf("mod_3: %d %s", a.Status, a.Body)
	}
	hit := items[0].(map[string]any)
	if hit["name"] != "mod_3" || hit["type"] != "module" || hit["package"] != "acme.tools" || hit["version"] != "2.0.0" ||
		hit["shortDescription"] != "Checks a host again" || hit["project"] != project || hit["repository"] != repo {
		t.Errorf("the first hit: %v", hit)
	}
	if items[1].(map[string]any)["name"] != "mod_30" {
		t.Errorf("the second hit: %v", items[1])
	}
	if _, ok := hit["deprecation"]; ok {
		t.Errorf("a collection that is not deprecated: %v", hit)
	}
	// The hit of a deprecated collection carries its deprecation, read with the entries (R-ANS-060).
	e.st.Exec(`INSERT INTO ansible_collection_deprecations (repo_id, name, message) VALUES ($1, 'acme.tools', 'Use acme.network.')`, e.repoID())
	dep := e.st.Do(apiRep, "GET", "/api/v1/search/documentation?q=mod_3", e.tokC, nil).JSON(t)["items"].([]any)[0].(map[string]any)["deprecation"]
	if d, _ := dep.(map[string]any); d["scope"] != "package" || d["message"] != "Use acme.network." {
		t.Errorf("the deprecation of the hit's collection: %v", dep)
	}
	// A role's entry points are words of the role's one entry, not entries of their own: one hit, which opens the role.
	role := e.st.Do(apiRep, "GET", "/api/v1/search/documentation?q=gracefully", e.tokC, nil)
	if items, _ := role.JSON(t)["items"].([]any); len(items) != 1 || items[0].(map[string]any)["type"] != "role" ||
		items[0].(map[string]any)["name"] != "web" {
		t.Errorf("a word of a role's entry point: %s", role.Body)
	}
	if a := e.st.Do(apiRep, "GET", "/api/v1/search/documentation?q=zeppelin", e.tokC, nil); len(a.JSON(t)["items"].([]any)) != 0 {
		t.Errorf("the changelog is no entry: %s", a.Body)
	}
	if a := e.st.Do(apiRep, "GET", "/api/v1/search/documentation?q=module+checks", e.tokC, nil); len(a.JSON(t)["items"].([]any)) != 0 {
		t.Errorf("the words of 1.0.0's modules find nothing in 2.0.0: %s", a.Body)
	}
	page := e.st.Do(apiRep, "GET", "/api/v1/search/documentation?q=mod&limit=1", e.tokC, nil)
	next, _ := page.JSON(t)["nextCursor"].(string)
	if next == "" {
		t.Fatalf("a page of one: %s", page.Body)
	}
	if rest := e.st.Do(apiRep, "GET", "/api/v1/search/documentation?q=mod&limit=1&cursor="+next, e.tokC, nil); rest.Status != http.StatusOK ||
		rest.JSON(t)["items"].([]any)[0].(map[string]any)["name"] == page.JSON(t)["items"].([]any)[0].(map[string]any)["name"] {
		t.Errorf("the second page: %d %s", rest.Status, rest.Body)
	}
	sug := e.st.Do(apiRep, "GET", "/api/v1/search/suggestions?q=mod_3&kind=documentation", e.tokC, nil)
	if docs, _ := sug.JSON(t)["documentation"].(map[string]any); sug.Status != http.StatusOK || len(docs["items"].([]any)) != 2 || docs["more"] != false {
		t.Errorf("the suggestions: %d %s", sug.Status, sug.Body)
	}

	outsider := e.st.Token(e.st.User("outsider"), scope.Default())
	if l := e.st.Do(apiRep, "GET", "/api/v1/packages?format=ansible", outsider, nil); len(names(t, l)) != 1 {
		t.Fatalf("the outsider reads the artifacts: %s", l.Body)
	}
	if a := e.st.Do(apiRep, "GET", "/api/v1/search/documentation?q=mod_3", outsider, nil); a.Status != http.StatusOK || len(a.JSON(t)["items"].([]any)) != 0 {
		t.Errorf("the outsider, who may not read the wiki: %d %s", a.Status, a.Body)
	}
	if a := e.st.Do(apiRep, "GET", "/api/v1/search/suggestions?q=mod_3&kind=documentation", outsider, nil); len(a.JSON(t)["documentation"].(map[string]any)["items"].([]any)) != 0 {
		t.Errorf("the outsider's suggestions: %s", a.Body)
	}
	if a := e.st.Do(apiRep, "GET", "/api/v1/search/documentation?q=m", e.tokC, nil); a.Status != http.StatusBadRequest {
		t.Errorf("a text of one character: %d %s", a.Status, a.Body)
	}
}
