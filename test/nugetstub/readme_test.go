package nugetstub

import (
	"net/http"
	"strings"
	"testing"
)

// TestR_CUR_028_TheStubServesAVersionsReadme: the service index names ReadmeUriTemplate/6.13.0 on the content host, which answers the
// README of a version the corpus has and 404 for another. Milliseconds.
func TestR_CUR_028_TheStubServesAVersionsReadme(t *testing.T) {
	f := newFixture(t, Config{})
	tmpl := f.resource("ReadmeUriTemplate/6.13.0")
	if want := f.content + "/v3-flatcontainer/{lower_id}/{lower_version}/readme"; tmpl != want {
		t.Fatalf("the template is %q, want %q", tmpl, want)
	}
	ok := f.get(strings.NewReplacer("{lower_id}", "newtonsoft.json", "{lower_version}", "13.0.1").Replace(tmpl))
	if ok.status != http.StatusOK || !strings.Contains(string(ok.body), "# newtonsoft.json 13.0.1") {
		t.Fatalf("the README: %d %s", ok.status, ok.body)
	}
	if r := f.get(strings.NewReplacer("{lower_id}", "newtonsoft.json", "{lower_version}", "9.9.9").Replace(tmpl)); r.status != http.StatusNotFound {
		t.Fatalf("a version the corpus does not have: %d", r.status)
	}
}
