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

// The documentation of the public catalogue (R-BAS-124): it travels only while docs is on and the repository's wiki is public, a
// version carries no wiki member without it, and an Ansible collection's tree carries the search words of the main installation's
// vector. A fake management API of one collection; milliseconds, bytes.

const collectionID = "pkg_acmetools"

// docsAPI answers the reads of one Ansible collection, acme.tools 1.0.0 in acme/galaxy, whose wiki is wiki.
type docsAPI struct{ wiki string }

func (f docsAPI) Get(_ context.Context, _, target string) (catalogue.Answer, error) {
	u, err := url.Parse(target)
	if err != nil {
		return catalogue.Answer{}, err
	}
	v := "/api/v1/packages/" + collectionID + "/versions/1.0.0"
	body := map[string]string{
		"/api/v1/formats":                                `{"formats":[{"name":"ansible","wiki":true,"clients":[{"id":"ansible-galaxy"}]}]}`,
		"/api/v1/projects":                               `{"items":[{"key":"acme"}]}`,
		"/api/v1/projects/acme/repositories":             `{"items":[{"key":"galaxy","format":"ansible","class":"local","visibility":{"artifacts":"public","wiki":"` + f.wiki + `"}}]}`,
		"/api/v1/packages":                               `{"items":[{"id":"` + collectionID + `","format":"ansible","name":"acme.tools","latestVersion":"1.0.0"}]}`,
		"/api/v1/packages/" + collectionID:               `{"id":"` + collectionID + `","name":"acme.tools"}`,
		"/api/v1/packages/" + collectionID + "/versions": `{"items":[{"version":"1.0.0"}]}`,
		v: `{"version":"1.0.0","hasReadme":true,"repositories":[{"key":"acme/galaxy","class":"local"}],
			"metadata":{"docs":{"state":"available","source":"generated","entries":2},"tags":["web"]}}`,
		v + "/readme": `{"html":"<p>acme.tools</p>"}`,
		v + "/docs": `{"state":"available","repository":"acme/galaxy","files":[{"name":"README.md","title":"acme.tools"}],
			"groups":[{"category":"module","label":"Modules","count":2,"types":[{"type":"module","count":2,"entries":[
			{"type":"module","name":"host_check","shortDescription":"Checks a host","hasDocs":true},
			{"type":"module","name":"mount","shortDescription":"Mounts","hasDocs":true}]}]}]}`,
		v + "/docs/entries/module/host_check": `{"type":"module","name":"host_check","hasDocs":true}`,
		v + "/docs/entries/module/mount":      `{"type":"module","name":"mount","hasDocs":true}`,
		v + "/docs/files/README.md":           `{"name":"README.md","html":"<p>acme.tools</p>"}`,
	}[u.Path]
	if body == "" {
		return catalogue.Answer{Status: http.StatusNotFound, ContentType: "application/problem+json", Body: []byte(`{"status":404}`)}, nil
	}
	return catalogue.Answer{Status: http.StatusOK, ContentType: "application/json", Body: []byte(body)}, nil
}

// TestR_BAS_124_TheDocumentationTravelsOnlyWithDocsOnAndAPublicWiki: with docs on and a public wiki the README, the tree, its
// entries and its files are built and the version announces them; with docs off, and with a wiki that is not public, none of them
// is, and the version carries no wiki member (hasReadme false, no metadata.docs), whatever the answers held.
func TestR_BAS_124_TheDocumentationTravelsOnlyWithDocsOnAndAPublicWiki(t *testing.T) {
	v := "packages/" + collectionID + "/versions/1.0.0"
	wikiPaths := []string{v + "/readme", v + "/docs", v + "/docs/entries/module/host_check", v + "/docs/files/README.md"}
	for _, c := range []struct {
		name string
		wiki string
		docs bool
		want bool
	}{
		{"docs on, a public wiki", "public", true, true},
		{"docs off", "public", false, false},
		{"an internal wiki", "internal", true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			store := cataloguetest.New()
			run(t, builder(store, docsAPI{wiki: c.wiki}), reg(true, c.docs))
			docs := store.Raw(regID)
			for _, p := range wikiPaths {
				if _, ok := docs[p]; ok != c.want {
					t.Errorf("%s held: %v, want %v", p, ok, c.want)
				}
			}
			version := string(docs[v])
			if announced := strings.Contains(version, `"hasReadme":true`) && strings.Contains(version, `"docs":{`); announced != c.want {
				t.Errorf("the version announces the documentation: %v, want %v: %s", announced, c.want, version)
			}
			if !strings.Contains(version, `"tags":["web"]`) {
				t.Errorf("the version lost what is no wiki: %s", version)
			}
		})
	}
}

// TestR_BAS_124_TheTreeCarriesTheSearchWordsOfTheMainInstallation: every entry of the tree carries searchWords, the words of its
// search vector at the main installation, lower case, sorted, each once and without a word that begins another; an entry without
// words carries an empty text.
func TestR_BAS_124_TheTreeCarriesTheSearchWordsOfTheMainInstallation(t *testing.T) {
	store := cataloguetest.New()
	store.SearchWords["acme/galaxy\x00acme.tools\x001.0.0"] = map[string][]string{
		"module/host_check": {"host", "check", "checks", "a", "hostname", "host"},
	}
	run(t, builder(store, docsAPI{wiki: "public"}), reg(true, true))
	var tree struct {
		Groups []struct {
			Types []struct {
				Entries []map[string]any `json:"entries"`
			} `json:"types"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(store.Raw(regID)["packages/"+collectionID+"/versions/1.0.0/docs"], &tree); err != nil {
		t.Fatal(err)
	}
	entries := tree.Groups[0].Types[0].Entries
	if entries[0]["searchWords"] != "a checks hostname" || entries[1]["searchWords"] != "" {
		t.Fatalf("the entries: %v", entries)
	}
}

// TestR_BAS_124_SearchWordsKeepTheWordsThatNoOtherBegins: the reduction keeps exactly what a prefix match can tell apart.
func TestR_BAS_124_SearchWordsKeepTheWordsThatNoOtherBegins(t *testing.T) {
	for in, want := range map[string]string{
		"":                       "",
		"Host host hostname":     "hostname",
		"b a ab abc bc":          "abc bc",
		"mount mounts mo src s1": "mounts s1 src",
	} {
		if got := catalogue.SearchWords(strings.Fields(in)); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
