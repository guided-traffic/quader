//go:build integration

package ociremote

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/ocistub"
)

// TestR_CUR_003_AnOCIRemoteInModeOnlyAllowedDeliversTheAllowedTagOnly: with the tag 1.0 of acme/a allowed (the version is the tag,
// Q-CUR-10), the tags list names that tag only, its manifest is fetched and served, another tag's manifest and an image without a rule
// answer the 403 DENIED of R-CUR-004 without an upstream request (remote-curation.md R-CUR-003, R-OCI-038). Data: the stub's corpus, one
// platform manifest of a few hundred bytes.
func TestR_CUR_003_AnOCIRemoteInModeOnlyAllowedDeliversTheAllowedTagOnly(t *testing.T) {
	e := newEnv(t, options{})
	const only = "oci-only"
	e.repo(only, e.run.URL, "none", 300,
		map[string]any{"pattern": ocistub.LoopbackRealmHost, "sendCredentials": false}, map[string]any{"pattern": ocistub.LoopbackCDNHost, "sendCredentials": false})
	e.st.Exec(`UPDATE repositories SET config = jsonb_set(config, '{remote,delivery}', '{"mode":"onlyAllowed"}') WHERE key = $1`, only)
	e.st.DeliveryRule(only, testenv.VersionRule("acme/a", "1.0", "allow"))
	reader := testenv.Person{ID: e.st.Text(`SELECT id::text FROM principal WHERE kind = 'user' AND name = 'reader'`), Name: "reader", Kind: "user"}
	e.st.Policy("only-readers", []string{"read"}, []string{only}, reader)
	rep := e.start(1)[0]
	refused := func(t *testing.T, a testenv.Answer, what string) {
		t.Helper()
		if a.Status != http.StatusForbidden || !strings.Contains(string(a.Body), `"DENIED"`) || a.Header.Get("Cache-Control") != "no-store" ||
			!strings.Contains(string(a.Body), "is not allowed in it/"+only) {
			t.Errorf("%s: %d %q %s, want the rules' 403 DENIED", what, a.Status, a.Header.Get("Cache-Control"), a.Body)
		}
	}

	t.Run("R-CUR-003 the tags list names the allowed tag only", func(t *testing.T) {
		a := e.get(rep, only, "acme/a/tags/list")
		var d struct {
			Tags []string `json:"tags"`
		}
		if a.Status != http.StatusOK || json.Unmarshal(a.Body, &d) != nil || !slices.Equal(d.Tags, []string{"1.0"}) {
			t.Fatalf("%d %s, want the tags [1.0]", a.Status, a.Body)
		}
	})
	t.Run("R-CUR-003 the allowed tag's manifest is fetched and served", func(t *testing.T) {
		if a := e.get(rep, only, "acme/a/manifests/1.0"); a.Status != http.StatusOK {
			t.Fatalf("%d %.300s", a.Status, a.Body)
		}
		if n := e.stub.Counts().ManifestGets("acme/a", "1.0"); n != 1 {
			t.Errorf("the upstream was asked %d times for the allowed tag, want once", n)
		}
	})
	t.Run("R-CUR-003 R-CUR-004 another tag's manifest is refused and the upstream is not asked", func(t *testing.T) {
		refused(t, e.get(rep, only, "acme/a/manifests/1.1"), "acme/a:1.1")
		if n := e.stub.Counts().ManifestGets("acme/a", "1.1") + e.stub.Counts().ManifestHeads("acme/a", "1.1"); n != 0 {
			t.Errorf("the upstream was asked %d times for the refused tag", n)
		}
	})
	t.Run("R-CUR-003 R-CUR-004 an image without a rule is refused at its tags list and the upstream is not asked", func(t *testing.T) {
		refused(t, e.get(rep, only, "other/c/tags/list"), "other/c")
		if n := e.stub.Counts().RepositoryRequests("other/c"); n != 0 {
			t.Errorf("the upstream was asked %d times about an image without a rule", n)
		}
	})
}
