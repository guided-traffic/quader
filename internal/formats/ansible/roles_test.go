package ansible

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/ansible/ansibletest"
	"github.com/guided-traffic/quader/internal/formats/ansible/rows"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

func hasCode(err error, code string) bool {
	e, ok := errs.As(err)
	return ok && e.Code == code
}

func roleFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "roles", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestR_ANS_056_TheFactsOfARoleAreReadFromTheLookup: the lookup the public Galaxy answered (testdata/roles) gives the owner, the name,
// the description, the tags, the GitHub repository, the commit and the upstream's download count; a role without a usable id, owner or
// name is left out, and the upstream's text is bounded.
func TestR_ANS_056_TheFactsOfARoleAreReadFromTheLookup(t *testing.T) {
	got := parseRoleFacts(roleFixture(t, "lookup.json"))
	if len(got) != 1 {
		t.Fatalf("roles = %+v", got)
	}
	r := got[0]
	if r.ID != 10923 || r.Owner != "geerlingguy" || r.Name != "docker" || r.Description != "Docker for Linux." ||
		r.GitHubUser != "geerlingguy" || r.GitHubRepo != "ansible-role-docker" || r.GitHubBranch != "master" ||
		r.Downloads != 30832123 || len(r.Commit) != 40 || r.Imported.IsZero() || len(r.Dependencies) != 0 {
		t.Errorf("role = %+v", r)
	}
	if want := []string{"web", "system", "containers", "docker", "orchestration", "compose", "server"}; !reflect.DeepEqual(r.Tags, want) {
		t.Errorf("tags = %v", r.Tags)
	}

	odd := `{"results":[
		{"id":"x","username":"a","name":"b"},
		{"id":7,"username":"a b","name":"c"},
		{"id":8,"summary_fields":{"namespace":{"name":"acme"},"tags":["ok",3,"","` + strings.Repeat("t", 65) + `"],
		 "dependencies":["acme.base",{"name":"acme.other"},7]},"name":"web","description":"` + strings.Repeat("d", 5000) + `",
		 "github_user":"acme\u0000","download_count":-3}
	]}`
	got = parseRoleFacts([]byte(odd))
	if len(got) != 1 {
		t.Fatalf("roles of the odd lookup = %+v", got)
	}
	r = got[0]
	if r.Owner != "acme" || r.Name != "web" || len([]rune(r.Description)) != maxRoleDescription || r.GitHubUser != "" || r.Downloads != -1 ||
		!reflect.DeepEqual(r.Tags, []string{"ok"}) || !reflect.DeepEqual(r.Dependencies, []string{"acme.base", "acme.other"}) {
		t.Errorf("odd role = %+v", r)
	}
}

// TestR_ANS_056_TheREADMEIsTheContentsHTML: the content document gives the README as the upstream's HTML and the file's name; a role
// without one, a README above the bound and a document that is no object are told apart.
func TestR_ANS_056_TheREADMEIsTheContentsHTML(t *testing.T) {
	c, err := parseRoleContent(roleFixture(t, "content.json"))
	if err != nil || c.File != "README.md" || !strings.HasPrefix(c.HTML, "<h1>Ansible Role: Docker</h1>") {
		t.Errorf("content = %+v %v", c, err)
	}
	if c, err := parseRoleContent([]byte(`{"readme":"","readme_html":null}`)); err != nil || c.HTML != "" {
		t.Errorf("a role without a README: %+v %v", c, err)
	}
	huge, _ := json.Marshal(map[string]string{"readme_html": strings.Repeat("x", rows.MaxRoleReadmeBytes+1)}) //nolint:errcheck // a map of strings
	if _, err := parseRoleContent(huge); !hasCode(err, errs.CodeUpstreamItemTooLarge) {
		t.Errorf("a README above the bound: %v", err)
	}
	if _, err := parseRoleContent([]byte(`[]`)); errs.KindOf(err) != errs.UpstreamFailed {
		t.Errorf("a document that is no object: %v", err)
	}
}

// TestR_ANS_056_ARoleSearchPassesTheClientsWordsInTheirBounds: the parameters ansible-galaxy sends pass, the others are dropped, and a
// value outside its bound is 400.
func TestR_ANS_056_ARoleSearchPassesTheClientsWordsInTheirBounds(t *testing.T) {
	q, _ := url.ParseQuery("&autocomplete=docker&tags_autocomplete=web+db&page_size=1000&order_by=-download_count&foo=bar") //nolint:errcheck // a literal
	got, err := roleSearchParams(q)
	if err != nil || got.Encode() != "autocomplete=docker&page_size=1000&tags_autocomplete=web+db" {
		t.Errorf("params = %q %v", got.Encode(), err)
	}
	for _, bad := range []string{"page_size=1001", "page=0", "page=x", "autocomplete=" + strings.Repeat("a", 129), "username_autocomplete=a%00b"} {
		q, _ := url.ParseQuery(bad) //nolint:errcheck // a literal
		if _, err := roleSearchParams(q); errs.KindOf(err) != errs.Invalid {
			t.Errorf("%s: %v, want 400", bad, err)
		}
	}
}

// TestR_ANS_056_ARoleSearchAnswerKeepsWhatTheCallerMaySee: the answer the public Galaxy gave keeps the roles keep admits, each with its
// addresses on the repository and without a download_url, the count less the roles left out, and its next link on the repository's
// search with the parameters that passed (the upstream's own is an http address of the Galaxy).
func TestR_ANS_056_ARoleSearchAnswerKeepsWhatTheCallerMaySee(t *testing.T) {
	const repo = "https://quader.example/api/ansible/p/remote/"
	doc := roleFixture(t, "search.json")
	params := url.Values{"autocomplete": {"docker"}, "page_size": {"3"}}
	out, err := renderRoleSearch(doc, repo, params, func(pkg string) bool { return pkg != "role/50" })
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Count   int              `json:"count"`
		Next    string           `json:"next"`
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(out, &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 1589 || len(page.Results) != 1 || page.Results[0]["id"] != float64(57) {
		t.Fatalf("count %d, results %v", page.Count, page.Results)
	}
	if page.Results[0]["url"] != repo+"v1/roles/57/" || page.Results[0]["username"] != "0x646e78" {
		t.Errorf("hit = %v", page.Results[0])
	}
	if _, ok := page.Results[0]["download_url"]; ok {
		t.Error("a hit names a download_url")
	}
	if page.Next != repo+"v1/search/roles/?autocomplete=docker&page=2&page_size=3" {
		t.Errorf("next = %q", page.Next)
	}
	if strings.Contains(string(out), "galaxy.ansible.com/api/v1") {
		t.Errorf("the answer names an address of the upstream's API: %s", out)
	}
	// A page whose every hit is left out counts none: ansible-galaxy fails on hits it is promised and not given.
	out, err = renderRoleSearch(doc, repo, params, func(string) bool { return false })
	if err != nil || !strings.Contains(string(out), `"count":0`) {
		t.Errorf("a page left empty: %s %v", out, err)
	}
	if _, err := renderRoleSearch([]byte(`[]`), repo, params, func(string) bool { return true }); errs.KindOf(err) != errs.UpstreamFailed {
		t.Errorf("an answer that is no object: %v", err)
	}
}

// TestR_ANS_056_ARolesPageShowsItsFacts: a version of a role is described by its facts; the GitHub repository is a link built from its
// names, never taken from the upstream as an address, and none is built from a name GitHub does not allow.
func TestR_ANS_056_ARolesPageShowsItsFacts(t *testing.T) {
	v := rows.RoleView{VersionID: 3, Package: "role/16", Role: rows.Role{
		ID: 16, Owner: "geerlingguy", Name: "pip", Description: "Pip.", GitHubUser: "geerlingguy", GitHubRepo: "ansible-role-pip",
		Tags: []string{"python"}, Dependencies: []string{"geerlingguy.repo-epel"}, Downloads: 12,
		Readme: rows.RoleReadme{State: rows.RoleReadmeAvailable, File: "README.md", HTML: "<h1>pip</h1>"},
	}}
	d := describeRole(v)
	role, _ := d.Metadata["role"].(map[string]any)
	if d.Description != "Pip." || !reflect.DeepEqual(d.Keywords, []string{"python"}) || !d.HasReadme || !d.ReadmeHTML || d.Readme != "<h1>pip</h1>" ||
		d.Metadata["repositoryUrl"] != "https://github.com/geerlingguy/ansible-role-pip" || role["packageName"] != "role/16" ||
		role["owner"] != "geerlingguy" || role["upstreamDownloads"] != int64(12) {
		t.Errorf("view = %+v", d)
	}
	for _, bad := range [][2]string{{"javascript:x", "r"}, {"a", "b/c"}, {"", "r"}, {"a", "-r"}, {"a b", "r"}} {
		if u := roleRepositoryURL(bad[0], bad[1]); u != "" {
			t.Errorf("%q %q: %q", bad[0], bad[1], u)
		}
	}
	v.Readme, v.Downloads = rows.RoleReadme{State: rows.RoleReadmeUnavailable, Reason: rows.DocReasonTooLarge}, -1
	d = describeRole(v)
	role, _ = d.Metadata["role"].(map[string]any)
	if d.HasReadme || role["readme"] != rows.RoleReadmeUnavailable || role["readmeReason"] != rows.DocReasonTooLarge || role["upstreamDownloads"] != nil {
		t.Errorf("a role whose README failed: %+v", d)
	}
}

// TestR_ANS_056_TheREADMEIsAskedForOncePerCommitOfARoleWithAPage: the README is queued only for a role whose package exists, once while
// it is queued, and again when the lookup names another commit than the one it was fetched for.
func TestR_ANS_056_TheREADMEIsAskedForOncePerCommitOfARoleWithAPage(t *testing.T) {
	ctx := context.Background()
	st := &ansibletest.Store{}
	role := rows.Role{ID: 16, Owner: "geerlingguy", Name: "pip", Commit: "aaa"}
	if err := st.PutRoles(ctx, nil, 1, []rows.Role{role}); err != nil {
		t.Fatal(err)
	}
	if err := queueRoleReadme(ctx, nil, st, 1, 16, true); err != nil || len(st.RoleJobs) != 0 {
		t.Fatalf("a role without a page: %v %v", st.RoleJobs, err)
	}
	st.MarkPackaged(1, 16)
	if err := queueRoleReadme(ctx, nil, st, 1, 16, false); err != nil || len(st.RoleJobs) != 0 {
		t.Fatalf("a repository that fetches no documentation: %v %v", st.RoleJobs, err)
	}
	for range 2 {
		if err := queueRoleReadme(ctx, nil, st, 1, 16, true); err != nil {
			t.Fatal(err)
		}
	}
	if len(st.RoleJobs) != 1 {
		t.Fatalf("jobs = %v, want one while it is queued", st.RoleJobs)
	}
	if err := st.SetRoleReadme(ctx, nil, 1, 16, rows.RoleReadme{State: rows.RoleReadmeAvailable, HTML: "<p>x</p>"}); err != nil {
		t.Fatal(err)
	}
	if err := queueRoleReadme(ctx, nil, st, 1, 16, true); err != nil || len(st.RoleJobs) != 1 {
		t.Fatalf("a README of the role's commit is asked for again: %v %v", st.RoleJobs, err)
	}
	role.Commit = "bbb"
	if err := st.PutRoles(ctx, nil, 1, []rows.Role{role}); err != nil {
		t.Fatal(err)
	}
	if err := queueRoleReadme(ctx, nil, st, 1, 16, true); err != nil || len(st.RoleJobs) != 2 {
		t.Errorf("another commit: %v %v", st.RoleJobs, err)
	}
}

// TestR_ANS_056_TheRoleContentIsAMetadataDocumentOfTheRolesPackage: the content document is classified as the role's metadata, is
// fetched from the v1 API's content address.
func TestR_ANS_056_TheRoleContentIsAMetadataDocumentOfTheRolesPackage(t *testing.T) {
	f := New()
	p := formats.Path{Path: RoleContentDocumentPath("16")}
	if info := f.Classify(p); info.Kind != formats.PathMetadata || info.Package != "role/16" {
		t.Errorf("Classify = %+v", info)
	}
	tg, err := parseTarget(p)
	if err != nil || tg.kind != kindRoleContent || tg.pkg() != "role/16" {
		t.Fatalf("target = %+v %v", tg, err)
	}
	root, _ := url.Parse("https://galaxy.example/api/") //nolint:errcheck // a literal
	u, err := upstreamURL(root, &discovery{v3: "v3/", v1: "v1/"}, tg, documentVariant)
	if err != nil || u.String() != "https://galaxy.example/api/v1/roles/16/content/" {
		t.Errorf("upstream = %v %v", u, err)
	}
}

// roleUpstream answers the stored documents of a role by path and variant.
type roleUpstream struct {
	galaxy
	docs map[string]string
}

func (u *roleUpstream) Document(_ context.Context, path, variant string) ([]byte, bool, error) {
	d, ok := u.docs[path+" "+variant]
	return []byte(d), ok, nil
}

// TestR_ANS_056_TheTabUpstreamListsARolesVersionsByTheNameTheRulesSee: the catalogue looks up "role/<id>" as the refusal of a rule
// names it, with the versions of every page of the role's list, newest first by their date; a download from the tab names the archive of
// the version. Two pages of two versions.
func TestR_ANS_056_TheTabUpstreamListsARolesVersionsByTheNameTheRulesSee(t *testing.T) {
	u := &roleUpstream{docs: map[string]string{
		"roles/16/versions.json page=1": `{"count":3,"next_link":"/api/v1/roles/16/versions/?page=2","results":[
			{"name":"1.0.0","download_url":"https://github.com/a/b/archive/1.0.0.tar.gz","commit_date":"2026-01-01T00:00:00Z"},
			{"name":"bad ref","download_url":"https://github.com/a/b/archive/x.tar.gz","commit_date":"2026-01-02T00:00:00Z"}]}`,
		"roles/16/versions.json page=2": `{"count":3,"next_link":null,"results":[
			{"name":"2.0.0","download_url":"https://github.com/a/b/archive/2.0.0.tar.gz","release_date":"2026-03-01T00:00:00Z"}]}`,
	}}
	info, err := (remoteAdapter{f: New()}).Package(context.Background(), u, "role/16", discover.VersionsRequest{Limit: 10})
	if err != nil || !info.Found || info.Name != "role/16" || len(info.Versions) != 2 || info.Versions[0].Version != "2.0.0" {
		t.Fatalf("package = %+v %v", info, err)
	}
	p, ok, err := (remoteAdapter{}).VersionProbe(context.Background(), u, "role/16", "1.0.0")
	if err != nil || !ok || p.Name != "role/16" || len(p.Files) != 1 || p.Files[0].Path != "roles/16/1.0.0.tar.gz" {
		t.Errorf("probe = %+v %v %v", p, ok, err)
	}
	if info, err := (remoteAdapter{}).Package(context.Background(), u, "role/17", discover.VersionsRequest{Limit: 10}); err != nil || info.Found {
		t.Errorf("a role the upstream does not have: %+v %v", info, err)
	}
}
