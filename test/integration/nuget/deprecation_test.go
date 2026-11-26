//go:build integration

package nuget

import (
	"strings"
	"testing"
)

// TestR_NUGET_036_ALocalVersionIsDeprecatedThroughTheManagementAPIAndServed: the management operation deprecates one version of a local
// repository with NuGet's reasons, a message and an alternate package (R-API-085, R-NUGET-036). The registration serves it in the
// catalog entry the clients read (dotnet list package --deprecated reads the inlined index), the screens' lists mark it, the filter
// deprecated finds the package once its latest version is deprecated, a repeat changes nothing, and the DELETE clears it. Who may not
// deploy is refused with 403 whether or not it may read (R-AUTHZ-016), and what NuGet cannot carry is refused with the reason. Cost: three tiny pushes and
// some twenty requests against one replica, about one second.
func TestR_NUGET_036_ALocalVersionIsDeprecatedThroughTheManagementAPIAndServed(t *testing.T) {
	e := newWriteEnv(t)
	a := e.replica("web-a")
	e.mustPush(a, "nuget-local", e.tokDev, nupkg(t, pkgSpec{id: "Acme.Old", version: "1.0.0"}))
	e.mustPush(a, "nuget-local", e.tokDev, nupkg(t, pkgSpec{id: "Acme.Old", version: "2.0.0"}))
	e.mustPush(a, "nuget-local", e.tokDev, nupkg(t, pkgSpec{id: "Acme.New", version: "3.0.0"}))
	const op = "/api/v1/projects/it/repositories/nuget-local/packages/Acme.Old/deprecation"
	body := map[string]any{"version": "1.0.0", "reasons": []string{"legacy"}, "message": "Use Acme.New.", "alternatePackage": map[string]any{"name": "Acme.New"}}

	t.Run("R-API-085 who may not deploy is refused, whether or not it may read (R-AUTHZ-016)", func(t *testing.T) {
		if ans := e.st.Do(a, "PUT", op, e.tokReader, body); ans.Status != 403 {
			t.Errorf("a reader: %d %s", ans.Status, ans.Body)
		}
		if ans := e.st.Do(a, "PUT", op, e.tokNobody, body); ans.Status != 403 {
			t.Errorf("a person without a grant: %d %s", ans.Status, ans.Body)
		}
	})
	t.Run("R-NUGET-036 what the catalog entry cannot carry is refused", func(t *testing.T) {
		for name, b := range map[string]map[string]any{
			"no version":  {"reasons": []string{"Legacy"}},
			"no reason":   {"version": "1.0.0", "message": "x"},
			"a bad range": {"version": "1.0.0", "reasons": []string{"Other"}, "alternatePackage": map[string]any{"name": "Acme.New", "range": "[2,1]"}},
		} {
			if ans := e.st.Do(a, "PUT", op, e.tokDev, b); ans.Status != 422 {
				t.Errorf("%s: %d %s", name, ans.Status, ans.Body)
			}
		}
	})
	t.Run("R-NUGET-036 deprecate one version", func(t *testing.T) {
		rev := e.rev("nuget-local", "acme.old")
		ans := e.st.Do(a, "PUT", op, e.tokDev, body)
		if ans.Status != 200 {
			t.Fatalf("%d %s", ans.Status, ans.Body)
		}
		if got := string(ans.Body); !strings.Contains(got, `"reasons":["Legacy"]`) || !strings.Contains(got, `"range":"*"`) || !strings.Contains(got, `"scope":"version"`) {
			t.Errorf("answer %s", got)
		}
		if again := e.st.Do(a, "PUT", op, e.tokDev, body); again.Status != 200 {
			t.Fatalf("repeat: %d %s", again.Status, again.Body)
		}
		if e.rev("nuget-local", "acme.old") != rev+1 || e.events("package.metadata_changed", "nuget-local", "acme.old/1.0.0") != 1 {
			t.Error("the revision did not rise once, or the event is not there once")
		}
		reg := e.st.Do(a, "GET", "/api/nuget/it/nuget-local/v3/registration-semver2/acme.old/index.json", e.tokReader, nil)
		// The member as the clients read it (the order of its keys is the stored one, PostgreSQL's).
		if b := string(reg.Body); !strings.Contains(b, `"reasons":["Legacy"]`) || !strings.Contains(b, `"message":"Use Acme.New."`) || !strings.Contains(b, `"alternatePackage":{"id":"Acme.New","range":"*"}`) {
			t.Errorf("the registration carries no deprecation: %d %s", reg.Status, reg.Body)
		}
	})
	t.Run("R-API-085 the screens mark it, link the alternate and filter by the latest version", func(t *testing.T) {
		id := e.st.Do(a, "GET", "/api/v1/packages/lookup?format=nuget&name=Acme.Old", e.tokDev, nil).JSON(t)["id"].(string)
		vers := e.st.Do(a, "GET", "/api/v1/packages/"+id+"/versions", e.tokDev, nil)
		if !strings.Contains(string(vers.Body), `"version":"1.0.0"`) || strings.Count(string(vers.Body), `"deprecation":`) != 1 {
			t.Errorf("versions %s", vers.Body)
		}
		d := e.st.Do(a, "GET", "/api/v1/packages/"+id+"/versions/1.0.0", e.tokDev, nil).JSON(t)
		dep, _ := d["deprecation"].(map[string]any)
		alt, _ := dep["alternatePackage"].(map[string]any)
		if dep["message"] != "Use Acme.New." || alt["name"] != "Acme.New" || alt["packageId"] == nil {
			t.Errorf("details %v", d["deprecation"])
		}
		if l := e.st.Do(a, "GET", "/api/v1/packages?format=nuget&deprecated=true", e.tokDev, nil); strings.Contains(string(l.Body), "Acme.Old") {
			t.Errorf("a package whose latest version is not deprecated is listed: %s", l.Body)
		}
		v2 := map[string]any{"version": "2.0.0", "reasons": []string{"Other"}}
		if ans := e.st.Do(a, "PUT", op, e.tokDev, v2); ans.Status != 200 {
			t.Fatalf("2.0.0: %d %s", ans.Status, ans.Body)
		}
		l := e.st.Do(a, "GET", "/api/v1/packages?format=nuget&deprecated=true", e.tokDev, nil)
		if items, _ := l.JSON(t)["items"].([]any); len(items) != 1 || items[0].(map[string]any)["deprecation"] == nil {
			t.Errorf("the filter: %s", l.Body)
		}
		s := e.st.Do(a, "GET", "/api/v1/search/suggestions?q=acme.old", e.tokDev, nil)
		if !strings.Contains(string(s.Body), `"deprecated":true`) {
			t.Errorf("the suggestion is not marked: %s", s.Body)
		}
	})
	t.Run("R-NUGET-036 clear", func(t *testing.T) {
		if ans := e.st.Do(a, "DELETE", op+"?version=1.0.0", e.tokDev, nil); ans.Status != 204 {
			t.Fatalf("%d %s", ans.Status, ans.Body)
		}
		if ans := e.st.Do(a, "DELETE", op+"?version=1.0.0", e.tokDev, nil); ans.Status != 204 {
			t.Fatalf("repeat: %d %s", ans.Status, ans.Body)
		}
		if got := e.st.Text(`SELECT details->>'change' FROM audit_event WHERE action = 'package.metadata_changed' AND path = 'acme.old/1.0.0' ORDER BY occurred_at DESC LIMIT 1`); got != "undeprecate" {
			t.Errorf("change = %q", got)
		}
		reg := e.st.Do(a, "GET", "/api/nuget/it/nuget-local/v3/registration-semver2/acme.old/index.json", e.tokReader, nil)
		if strings.Count(string(reg.Body), `"deprecation"`) != 1 {
			t.Errorf("1.0.0 is still deprecated, or 2.0.0 is not: %s", reg.Body)
		}
	})
}
