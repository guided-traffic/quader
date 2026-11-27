package catalogue_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/bastionmain/catalogue"
	"github.com/guided-traffic/quader/internal/bastionmain/catalogue/cataloguetest"
)

// The catalogue of a Bastion's project view (R-BAS-130, R-BAS-131) against a fake management API of one project: an npm package whose
// version 1.0.0 is in a local repository and in the virtual one over it and whose 2.0.0 is in the virtual one alone, fetched from the
// public npm registry; an image with three tags on two manifests; a generic repository with one file. No database; the documents are
// bytes.

const (
	npmID   = "pkg_leftpad"
	imageID = "pkg_agent"
)

// projectAPI answers the reads of the project view's catalogue by path and query.
type projectAPI struct{}

func (projectAPI) Get(_ context.Context, base, target string) (catalogue.Answer, error) {
	u, err := url.Parse(target)
	if err != nil {
		return catalogue.Answer{}, err
	}
	body, ok := projectBody(base, u)
	if !ok {
		return catalogue.Answer{Status: http.StatusNotFound, ContentType: "application/problem+json", Body: []byte(`{"status":404}`)}, nil
	}
	return catalogue.Answer{Status: http.StatusOK, ContentType: "application/json", Body: []byte(body)}, nil
}

// summary is a package of a list, with the latest version and the count of versions its scope reads.
func summary(id, format, name, latest string, versions int) string {
	b, _ := json.Marshal(map[string]any{"id": id, "format": format, "name": name, "latestVersion": latest, "versionCount": versions})
	return string(b)
}

func projectBody(base string, u *url.URL) (string, bool) {
	q := u.Query()
	npm := summary(npmID, "npm", "left-pad", "2.0.0", 2)
	image := summary(imageID, "oci", "tools/agent", "3.0", 3)
	switch u.Path {
	case "/api/v1/formats":
		return `{"formats":[{"name":"npm","clients":[{"id":"npm"}],"upstreamPresets":[{"url":"https://registry.npmjs.org"}]},
			{"name":"oci","clients":[{"id":"docker"},{"id":"podman"}],"upstreamPresets":[]},
			{"name":"generic","patternSubject":"path","clients":[{"id":"curl"}],"upstreamPresets":[]}]}`, true
	case "/api/v1/projects":
		return `{"items":[{"key":"acme","displayName":"Acme","description":""}]}`, true
	case "/api/v1/projects/acme/repositories":
		return `{"items":[{"key":"files","format":"generic","class":"local","description":""},
			{"key":"images","format":"oci","class":"local","description":"Base images"},
			{"key":"npm-all","format":"npm","class":"virtual","description":"Everything npm"},
			{"key":"npm-local","format":"npm","class":"local","description":"Ours, see ` + mainOrigin + `/ui/en/projects/acme"}]}`, true
	case "/api/v1/packages":
		switch {
		case q.Get("repository") == "acme/npm-local":
			return `{"items":[` + summary(npmID, "npm", "left-pad", "1.0.0", 1) + `]}`, true
		case q.Get("repository") == "acme/npm-all":
			return `{"items":[` + npm + `]}`, true
		case q.Get("repository") == "acme/images":
			return `{"items":[` + image + `]}`, true
		case q.Get("repository") != "":
			return `{"items":[]}`, true
		case q.Get("project") == "acme":
			return `{"items":[` + summary(npmID, "npm", "left-pad", "2.0.0", 2) + `,` + image + `]}`, true
		}
		return `{"items":[` + npm + `,` + image + `]}`, true
	case "/api/v1/packages/" + npmID:
		return `{"id":"` + npmID + `","name":"left-pad"}`, true
	case "/api/v1/packages/" + npmID + "/versions":
		return `{"items":[{"version":"2.0.0"},{"version":"1.0.0"}]}`, true
	case "/api/v1/packages/" + npmID + "/versions/1.0.0":
		return `{"version":"1.0.0","repositories":[{"key":"acme/npm-local","class":"local"},{"key":"acme/npm-all","class":"virtual"}]}`, true
	case "/api/v1/packages/" + npmID + "/versions/2.0.0":
		return `{"version":"2.0.0","repositories":[{"key":"acme/npm-all","class":"virtual"}],
			"files":[{"path":"left-pad-2.0.0.tgz","upstreamUrl":"https://registry.npmjs.org/left-pad/-/left-pad-2.0.0.tgz",
			"fetchedFrom":"https://registry.npmjs.org/left-pad"}]}`, true
	case "/api/v1/packages/" + imageID:
		return `{"id":"` + imageID + `","name":"tools/agent"}`, true
	case "/api/v1/packages/" + imageID + "/versions":
		if q.Get("group") == "content" {
			return `{"items":[{"version":"3.0","digest":"sha256:aa","aliases":["latest"]},{"version":"2.0","digest":"sha256:bb","aliases":[]}]}`, true
		}
		return `{"items":[{"version":"3.0"},{"version":"latest"},{"version":"2.0"}]}`, true
	case "/api/v1/packages/" + imageID + "/versions/3.0", "/api/v1/packages/" + imageID + "/versions/latest",
		"/api/v1/packages/" + imageID + "/versions/2.0":
		return `{"version":"` + strings.TrimPrefix(u.Path, "/api/v1/packages/"+imageID+"/versions/") + `","repositories":[{"key":"acme/images","class":"local"}]}`, true
	case "/api/v1/projects/acme/repositories/files/children":
		return `{"items":[{"path":"tool.bin","kind":"file"}]}`, true
	case "/api/v1/projects/acme/repositories/files/items":
		return `{"path":"tool.bin","digests":{"sha256":"aa"}}`, true
	}
	if strings.HasSuffix(u.Path, "/client-setup") {
		// The set-up names what it was asked for, so a document says which holder, client, package and version it is.
		b, _ := json.Marshal(map[string]any{"snippets": []map[string]string{{"content": base + " " + u.Path + " client=" + q.Get("client") +
			" package=" + q.Get("package") + " version=" + q.Get("version") + " path=" + q.Get("path")}}})
		return string(b), true
	}
	return "", false
}

func projectReg(hide bool) catalogue.Registration {
	r := reg(true, false)
	r.HideRepositories = hide
	return r
}

// TestR_BAS_130_TheCatalogueHoldsAProjectsAndARepositorysIndex: every project of the public set has its index and every repository its
// own, each the main installation's answer with the scope, so a repository's latest version and count are its own (a generic one's an
// empty page); an image has its list by content, one entry per manifest with its other tags; the public set carries each repository's
// description, its links on the Bastion.
func TestR_BAS_130_TheCatalogueHoldsAProjectsAndARepositorysIndex(t *testing.T) {
	store := cataloguetest.New()
	rep := run(t, builder(store, projectAPI{}), projectReg(false))
	if rep.Failed != 0 || len(rep.Built) != 1 {
		t.Fatalf("report %+v", rep)
	}
	docs := store.Raw(regID)
	for path, want := range map[string][]string{
		"projects/acme/index/0":               {`"latestVersion":"2.0.0"`, `"versionCount":2`, `"name":"tools/agent"`},
		"repositories/acme/npm-local/index/0": {`"latestVersion":"1.0.0"`, `"versionCount":1`},
		"repositories/acme/npm-all/index/0":   {`"latestVersion":"2.0.0"`, `"versionCount":2`},
		"repositories/acme/images/index/0":    {`"name":"tools/agent"`},
		"packages/" + imageID + "/images":     {`"version":"3.0"`, `"aliases":["latest"]`, `"version":"2.0"`},
	} {
		doc, ok := docs[path]
		if !ok {
			t.Errorf("no document %s", path)
			continue
		}
		for _, w := range want {
			if !strings.Contains(string(doc), w) {
				t.Errorf("%s lacks %s: %s", path, w, doc)
			}
		}
	}
	if strings.Contains(string(docs["repositories/acme/npm-local/index/0"]), "2.0.0") {
		t.Errorf("the local repository's index counts the virtual one's version: %s", docs["repositories/acme/npm-local/index/0"])
	}
	var images struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(docs["packages/"+imageID+"/images"], &images); err != nil || len(images.Items) != 2 {
		t.Errorf("the image's list by content holds %d entries, want one per manifest: %v", len(images.Items), err)
	}
	if _, ok := docs["packages/"+npmID+"/images"]; ok {
		t.Error("an npm package has a list by content")
	}
	if s := string(docs["repositories/acme/files/index/0"]); s != `{"items":[]}` {
		t.Errorf("a generic repository's index is an empty page: %q", s)
	}
	pub, err := store.Public(context.Background(), regID)
	if err != nil || len(pub.Repositories) != 4 {
		t.Fatalf("the public set: %+v %v", pub, err)
	}
	if d := pub.Repositories[3].Description; d != "Ours, see "+bastionURL+"/ui/en/projects/acme" {
		t.Errorf("the description of acme/npm-local: %q", d)
	}
	if d := pub.Repositories[1].Description; d != "Base images" {
		t.Errorf("the description of acme/images: %q", d)
	}
}

// TestR_BAS_130_EveryHolderHasItsSetUp: a version has its set-up in every public repository that holds it, the local one and the virtual
// one over it, and in none that does not; an image has a set-up without a version per holder and client, from which a pull by digest
// starts; an npm package has none; a generic file's set-up is as before.
func TestR_BAS_130_EveryHolderHasItsSetUp(t *testing.T) {
	store := cataloguetest.New()
	run(t, builder(store, projectAPI{}), projectReg(false))
	docs := store.Raw(regID)
	for _, p := range []string{
		"setup/acme/npm-local/npm/" + npmID + "/1.0.0", "setup/acme/npm-all/npm/" + npmID + "/1.0.0", "setup/acme/npm-all/npm/" + npmID + "/2.0.0",
		"setup/acme/images/docker/" + imageID + "/3.0", "setup/acme/images/docker/" + imageID, "setup/acme/images/podman/" + imageID,
		"files/acme/files/setup/curl/tool.bin",
	} {
		if _, ok := docs[p]; !ok {
			t.Errorf("no document %s", p)
		}
	}
	for _, p := range []string{"setup/acme/npm-local/npm/" + npmID + "/2.0.0", "setup/acme/npm-local/npm/" + npmID, "setup/acme/npm-all/npm/" + npmID} {
		if _, ok := docs[p]; ok {
			t.Errorf("%s: a repository that does not hold the version, or an npm package without a version", p)
		}
	}
	if s := string(docs["setup/acme/npm-all/npm/"+npmID+"/1.0.0"]); !strings.Contains(s, "/repositories/npm-all/client-setup") || !strings.Contains(s, "version=1.0.0") {
		t.Errorf("the virtual repository's set-up of 1.0.0: %s", s)
	}
	if s := string(docs["setup/acme/images/docker/"+imageID]); !strings.Contains(s, "package=tools/agent version= ") || !strings.Contains(s, "client=docker") {
		t.Errorf("the image's set-up without a version was asked with one: %s", s)
	}
}

// TestR_BAS_131_HiddenRepositoriesBuildNoRepositoryIndexAndNameNoUpstream: with Hide repositories on, a run builds no repository's index
// and deletes the ones a run before built, keeps the project's index, and every upstream is null, a public registry's too; switched off
// again, the indexes and the public registry's upstream come back.
func TestR_BAS_131_HiddenRepositoriesBuildNoRepositoryIndexAndNameNoUpstream(t *testing.T) {
	store := cataloguetest.New()
	b := builder(store, projectAPI{})
	run(t, b, projectReg(false))
	v2 := "packages/" + npmID + "/versions/2.0.0"
	if s := string(store.Raw(regID)[v2]); !strings.Contains(s, `"upstreamUrl":"https://registry.npmjs.org/`) {
		t.Fatalf("shown: the public registry's upstream is gone: %s", s)
	}
	seq := store.Seq(regID)

	rep := run(t, b, projectReg(true))
	docs := store.Raw(regID)
	for p := range docs {
		if strings.HasPrefix(p, "repositories/") {
			t.Errorf("hidden: %s is still held", p)
		}
	}
	if _, ok := docs["projects/acme/index/0"]; !ok || rep.Deleted < 4 {
		t.Errorf("hidden: the project's index is gone or the repositories' indexes were not deleted (%+v)", rep)
	}
	if s := string(docs[v2]); !strings.Contains(s, `"upstreamUrl":null`) || !strings.Contains(s, `"fetchedFrom":null`) {
		t.Errorf("hidden: the version names an upstream: %s", s)
	}
	ch, err := store.Changes(context.Background(), regID, seq, 1000)
	if err != nil || ch.Validate(seq) != nil {
		t.Fatalf("the change list after the switch: %+v %v", ch, err)
	}
	deleted := 0
	for _, e := range ch.Entries {
		if e.Deleted && strings.HasPrefix(e.Path, "repositories/") {
			deleted++
		}
	}
	if deleted != 4 {
		t.Errorf("hidden: %d repositories' indexes deleted in the change list, want 4", deleted)
	}

	run(t, b, projectReg(false))
	if _, ok := store.Raw(regID)["repositories/acme/npm-local/index/0"]; !ok {
		t.Error("shown again: no repository's index")
	}
	if s := string(store.Raw(regID)[v2]); !strings.Contains(s, "registry.npmjs.org") {
		t.Errorf("shown again: the public registry's upstream did not come back: %s", s)
	}
}
