//go:build integration

package nuget

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/nugetstub"
)

// TestR_CUR_003_ANuGetRemoteInModeOnlyAllowedDeliversTheAllowedVersionOnly: with one version of Newtonsoft.Json allowed, the version
// list names that version only, its package is fetched and served, the other version's package and an ID without a rule answer the 403
// of R-CUR-004 without an upstream request (remote-curation.md R-CUR-003, R-NUGET-035). The rule is seeded in NuGet's canonical forms
// (the lower-case ID, the normalized version). Data: the stub's corpus, a few kilobytes.
func TestR_CUR_003_ANuGetRemoteInModeOnlyAllowedDeliversTheAllowedVersionOnly(t *testing.T) {
	e := newRemoteEnv(t, remoteOptions{})
	const only = "nuget-only"
	content := []any{map[string]any{"pattern": "localhost", "sendCredentials": false}}
	cfg := map[string]any{}
	if err := json.Unmarshal([]byte(e.remoteConfig(300, content)), &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["remote"].(map[string]any)["delivery"] = testenv.OnlyAllowed()
	raw, _ := json.Marshal(cfg)
	e.st.Repository(only, "remote", "nuget", string(raw))
	e.st.DeliveryRule(only, testenv.VersionRule("newtonsoft.json", "13.0.3", "allow"))
	reader := testenv.Person{ID: e.st.Text(`SELECT id::text FROM principal WHERE kind = 'user' AND name = 'reader'`), Name: "reader", Kind: "user"}
	e.st.Policy("only-readers", []string{"read"}, []string{only}, reader)
	rep := e.start(1)[0]
	refused := func(t *testing.T, a testenv.Answer) {
		t.Helper()
		if a.Status != 403 || !strings.Contains(string(a.Body), "not_allowed_in_repository") || a.Header.Get("Cache-Control") != "no-store" ||
			!strings.Contains(a.Header.Get("X-NuGet-Warning"), "is not allowed in it/"+only) {
			t.Errorf("%d %q %q %s, want the rules' 403", a.Status, a.Header.Get("Cache-Control"), a.Header.Get("X-NuGet-Warning"), a.Body)
		}
	}

	t.Run("R-CUR-003 the version list names the allowed version only", func(t *testing.T) {
		a := e.get(rep, only, "v3/flatcontainer/newtonsoft.json/index.json")
		var d struct {
			Versions []string `json:"versions"`
		}
		if a.Status != 200 || json.Unmarshal(a.Body, &d) != nil || !slices.Equal(d.Versions, []string{"13.0.3"}) {
			t.Fatalf("%d %s, want the versions [13.0.3]", a.Status, a.Body)
		}
	})
	t.Run("R-CUR-003 the allowed version's package is fetched and served", func(t *testing.T) {
		if a := e.get(rep, only, nupkgPath(nugetstub.NewtonsoftID, "13.0.3")); a.Status != 200 {
			t.Fatalf("%d %.300s", a.Status, a.Body)
		}
		if n := e.stub.Counts().Nupkg(nugetstub.NewtonsoftID, "13.0.3"); n != 1 {
			t.Errorf("the upstream was asked %d times for the allowed package, want once", n)
		}
	})
	t.Run("R-CUR-003 R-CUR-004 the other version's package is refused and the upstream is not asked", func(t *testing.T) {
		refused(t, e.get(rep, only, nupkgPath(nugetstub.NewtonsoftID, "13.0.1")))
		if n := e.stub.Counts().Nupkg(nugetstub.NewtonsoftID, "13.0.1"); n != 0 {
			t.Errorf("the upstream was asked %d times for the refused package", n)
		}
	})
	t.Run("R-CUR-003 R-CUR-004 an ID without a rule is refused at its version list and the upstream is not asked", func(t *testing.T) {
		refused(t, e.get(rep, only, "v3/flatcontainer/contoso.core.abstractions/index.json"))
		if n := e.stub.Counts().VersionLists("Contoso.Core.Abstractions"); n != 0 {
			t.Errorf("the upstream was asked %d times for the version list of an ID without a rule", n)
		}
	})
}
