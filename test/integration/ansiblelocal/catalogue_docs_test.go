//go:build integration

package ansiblelocal

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/bastionmain/catalogue"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// The search inside a version's documentation at a Bastion (bastion.md R-BAS-124, R-BAS-125): the Bastion's application narrows the
// tree in the browser by the rule of the main installation's statement (docs-model.ts narrowDocTree), over the words of each entry's
// search vector that the catalogue's builder puts into the tree. This test holds the two together through one fixture: it publishes a
// small collection, builds the tree as the builder does (the management API's answer with the words of BastionCatalogueDocSearchWords),
// asks the main installation's search a list of texts, and finds both in the fixture the web unit test reads. One collection of five
// entries; about a second beside the stack's start. QUADER_IT_WRITE_DOCS_FIXTURE=1 writes the fixture instead of comparing it.

// docsFixtureFile is the fixture, a TypeScript module whose JSON stands between the two markers.
const docsFixtureFile = "../../../web/src/app/api/testing/docs-search-fixture.ts"

const (
	fixtureBegin = "// BEGIN FIXTURE\n"
	fixtureEnd   = "// END FIXTURE\n"
)

// docsQueries are the texts asked: a name, a word of a description and an option, words in other cases, a term inside a name, a
// version's digits, punctuation, a file's title, and a text nothing holds.
var docsQueries = []string{
	"host", "HOST answers", "fstype", "file", "1.2", "v1", "_check", "tim", "install", "acme", "ext4 xfs", "copy-file", "Sort_Order", "zz",
}

// docsSearchSpec is acme.tools 1.0.0 with three modules, a callback, a README and a file of docs/.
func docsSearchSpec() []byte {
	s := spec("1.0.0")
	delete(s.Files, "plugins/modules/x.py")
	s.Files["plugins/modules/host_check.py"] = "DOCUMENTATION = r'''\nmodule: host_check\nshort_description: Checks whether a host answers\n" +
		"description:\n  - Sends a probe to O(host) within O(timeout_seconds).\noptions:\n  host:\n    type: str\n    description: The host's name or address.\n" +
		"    required: true\n  timeout_seconds:\n    type: int\n    default: 5\n    description: How long to wait.\n'''\n" +
		"RETURN = '''\nreachable:\n  description: Whether the host answered.\n  returned: always\n  type: bool\n'''\n"
	s.Files["plugins/modules/mount.py"] = "DOCUMENTATION = r'''\nmodule: mount\nshort_description: Mounts a file system (ext4, xfs)\n" +
		"options:\n  src:\n    type: path\n    description: The device.\n  fstype:\n    type: str\n    description: The type.\n" +
		"  opts:\n    type: str\n    description: The options.\n'''\n"
	s.Files["plugins/modules/copy_file.py"] = "DOCUMENTATION = r'''\nmodule: copy_file\nshort_description: Copies a file to a host, as v1.2.3 did\n" +
		"options:\n  dest:\n    type: path\n    description: Where to.\n  backup:\n    type: bool\n    description: Keep the old one.\n'''\n"
	s.Files["plugins/callback/timer.py"] = "DOCUMENTATION = r'''\nname: timer\nshort_description: Times tasks\noptions:\n  sort_order:\n" +
		"    type: str\n    description: The order.\n'''\n"
	s.Files["docs/install.md"] = "# Install\n\nRun it.\n"
	return specBytes(s)
}

// docsCase is what the main installation's search answered for a text: the entries (type/name), the files and the count of the
// entries left.
type docsCase struct {
	Q          string   `json:"q"`
	Entries    []string `json:"entries"`
	Files      []string `json:"files"`
	Changelog  bool     `json:"changelog"`
	MatchCount int      `json:"matchCount"`
}

type docsFixture struct {
	Tree  json.RawMessage `json:"tree"`
	Cases []docsCase      `json:"cases"`
}

// TestR_BAS_124_TheBrowsersSearchOfAVersionIsTheMainInstallations: the tree with the catalogue's search words and the main
// installation's answers to the texts are the fixture's, which the web unit test narrows with the browser's rule and finds equal.
func TestR_BAS_124_TheBrowsersSearchOfAVersionIsTheMainInstallations(t *testing.T) {
	e := newEnv(t)
	if a := e.publish(e.tokA, docsSearchSpec(), true); a.Status != 202 {
		t.Fatalf("publish: %d %s", a.Status, a.Body)
	}
	p := "/api/v1/packages/" + e.packageID() + "/versions/1.0.0/docs"
	whole := e.st.Do(e.r, "GET", p, e.tokC, nil)
	if whole.Status != 200 {
		t.Fatalf("the tree: %d %s", whole.Status, whole.Body)
	}
	var tree map[string]any
	if err := json.Unmarshal(whole.Body, &tree); err != nil {
		t.Fatal(err)
	}
	delete(tree, "builtAt") // a time the builder moves into the version's state (R-BAS-103)
	raw, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}

	db, err := postgres.Open(context.Background(), e.st.DatabaseConfig(), "docsfixture", "worker", testenv.Quiet())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	words, err := catalogue.NewDB(db).DocSearchWords(context.Background(), project+"/"+repo, "acme.tools", "1.0.0")
	if err != nil || len(words) != 4 {
		t.Fatalf("the search words: %v %v", words, err)
	}
	withWords, err := catalogue.TreeWithSearchWords(raw, words)
	if err != nil {
		t.Fatal(err)
	}

	got := docsFixture{Tree: withWords}
	for _, q := range docsQueries {
		a := e.st.Do(e.r, "GET", p+"?q="+url.QueryEscape(q), e.tokC, nil)
		var ans struct {
			Groups []struct {
				Types []struct {
					Entries []struct{ Type, Name string } `json:"entries"`
				} `json:"types"`
			} `json:"groups"`
			Files      []struct{ Name string } `json:"files"`
			Changelog  *struct{}               `json:"changelog"`
			MatchCount int                     `json:"matchCount"`
		}
		if a.Status != 200 || json.Unmarshal(a.Body, &ans) != nil {
			t.Fatalf("%q: %d %s", q, a.Status, a.Body)
		}
		c := docsCase{Q: q, Entries: []string{}, Files: []string{}, Changelog: ans.Changelog != nil, MatchCount: ans.MatchCount}
		for _, g := range ans.Groups {
			for _, ty := range g.Types {
				for _, en := range ty.Entries {
					c.Entries = append(c.Entries, en.Type+"/"+en.Name)
				}
			}
		}
		for _, f := range ans.Files {
			c.Files = append(c.Files, f.Name)
		}
		got.Cases = append(got.Cases, c)
	}

	if os.Getenv("QUADER_IT_WRITE_DOCS_FIXTURE") == "1" {
		writeDocsFixture(t, got)
		return
	}
	want := readDocsFixture(t)
	if !sameJSON(t, got.Tree, want.Tree) {
		t.Errorf("the tree with its search words differs from the fixture's:\n%s", got.Tree)
	}
	if !reflect.DeepEqual(got.Cases, want.Cases) {
		t.Errorf("the main installation's answers differ from the fixture's:\n got %+v\nwant %+v", got.Cases, want.Cases)
	}
}

func readDocsFixture(t *testing.T) docsFixture {
	t.Helper()
	src, err := os.ReadFile(docsFixtureFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	i, j := strings.Index(s, fixtureBegin), strings.Index(s, fixtureEnd)
	if i < 0 || j < i {
		t.Fatalf("%s holds no fixture between its markers", docsFixtureFile)
	}
	// The JSON is the object literal of the module's one export: from its first brace to its last.
	part := s[i+len(fixtureBegin) : j]
	open, end := strings.Index(part, "{"), strings.LastIndex(part, "}")
	if open < 0 || end < open {
		t.Fatalf("%s holds no object between its markers", docsFixtureFile)
	}
	body := part[open : end+1]
	var f docsFixture
	if err := json.Unmarshal([]byte(body), &f); err != nil {
		t.Fatalf("the fixture: %v", err)
	}
	return f
}

func writeDocsFixture(t *testing.T, f docsFixture) {
	t.Helper()
	var tree any
	if err := json.Unmarshal(f.Tree, &tree); err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(map[string]any{"tree": tree, "cases": f.Cases}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(docsFixtureFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	i, j := strings.Index(s, fixtureBegin), strings.Index(s, fixtureEnd)
	if i < 0 || j < i {
		t.Fatalf("%s holds no markers", docsFixtureFile)
	}
	out := s[:i+len(fixtureBegin)] + "export const DOCS_SEARCH_FIXTURE: DocsSearchFixture = " + string(b) + ";\n" + s[j:]
	if err := os.WriteFile(docsFixtureFile, []byte(out), 0o644); err != nil { //nolint:gosec // a source file of the repository
		t.Fatal(err)
	}
	t.Logf("wrote %s", docsFixtureFile)
}

func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(bytes.TrimSpace(b), &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
