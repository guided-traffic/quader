//go:build integration

package ansibleremote

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_CUR_003_AnAnsibleRemoteInModeOnlyAllowedDeliversTheAllowedVersionOnly: with 1.0.0 of acme.tools allowed, the version list
// names that version only, its archive is fetched and served, another version's archive and a collection without a rule answer the 403
// of R-CUR-004 in Galaxy's shape without an upstream request (remote-curation.md R-CUR-003, R-ANS-047). Data: the stub's corpus, an
// archive of a few kilobytes.
func TestR_CUR_003_AnAnsibleRemoteInModeOnlyAllowedDeliversTheAllowedVersionOnly(t *testing.T) {
	e := newEnv(t, options{})
	const only = "ans-only"
	e.repo(only, "verify-if-available")
	e.st.Exec(`UPDATE repositories SET config = jsonb_set(config, '{remote,delivery}', '{"mode":"onlyAllowed"}') WHERE key = $1`, only)
	e.st.DeliveryRule(only, testenv.VersionRule("acme.tools", "1.0.0", "allow"))
	reader := testenv.Person{ID: e.st.Text(`SELECT id::text FROM principal WHERE kind = 'user' AND name = 'reader'`), Name: "reader", Kind: "user"}
	e.st.Policy("only-readers", []string{"read"}, []string{only}, reader)
	r := e.start(1)[0]
	refused := func(t *testing.T, a testenv.Answer, what string) {
		t.Helper()
		if a.Status != http.StatusForbidden || !strings.Contains(string(a.Body), `"not_allowed_in_repository"`) || a.Header.Get("Cache-Control") != "no-store" ||
			!strings.Contains(string(a.Body), "is not allowed in it/"+only) {
			t.Errorf("%s: %d %q %s, want the rules' 403", what, a.Status, a.Header.Get("Cache-Control"), a.Body)
		}
	}

	t.Run("R-CUR-003 the version list names the allowed version only", func(t *testing.T) {
		a := e.get(r, only, "v3/collections/acme/tools/versions/")
		if a.Status != http.StatusOK {
			t.Fatalf("%d %s", a.Status, a.Body)
		}
		var got []string
		for _, v := range list(a.JSON(t)["data"]) {
			got = append(got, obj(v)["version"].(string))
		}
		if !slices.Equal(got, []string{"1.0.0"}) {
			t.Errorf("versions %v, want [1.0.0]", got)
		}
	})
	t.Run("R-CUR-003 the allowed version's archive is fetched and served", func(t *testing.T) {
		if a := e.get(r, only, "v3/artifacts/collections/acme-tools-1.0.0.tar.gz"); a.Status != http.StatusOK {
			t.Fatalf("%d %.300s", a.Status, a.Body)
		}
		if n := e.requests("/api/v3/plugin/ansible/content/published/collections/artifacts/acme-tools-1.0.0.tar.gz"); n != 1 {
			t.Errorf("the upstream was asked %d times for the allowed archive, want once", n)
		}
	})
	t.Run("R-CUR-003 R-CUR-004 another version's archive is refused and the upstream is not asked", func(t *testing.T) {
		refused(t, e.get(r, only, "v3/artifacts/collections/acme-tools-1.1.0.tar.gz"), "acme.tools 1.1.0")
		if n := e.requests("/api/v3/plugin/ansible/content/published/collections/artifacts/acme-tools-1.1.0.tar.gz") +
			e.requests("/api/v3/collections/acme/tools/versions/1.1.0/"); n != 0 {
			t.Errorf("the upstream was asked %d times about the refused version", n)
		}
	})
	t.Run("R-CUR-003 R-CUR-004 a collection without a rule is refused at its document and the upstream is not asked", func(t *testing.T) {
		before := e.stub.Counts().Prefixed("/api/v3/collections/other/stuff/", http.MethodGet)
		refused(t, e.get(r, only, "v3/collections/other/stuff/"), "other.stuff")
		if n := e.stub.Counts().Prefixed("/api/v3/collections/other/stuff/", http.MethodGet); n != before {
			t.Errorf("the upstream was asked %d times about a collection without a rule", n-before)
		}
	})
}
