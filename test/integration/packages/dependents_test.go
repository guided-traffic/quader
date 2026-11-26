//go:build integration

package packages

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// dependents is the answer of GET /api/v1/packages/{packageId}/dependents.
type dependents struct {
	Items []struct {
		Name      string `json:"name"`
		Version   string `json:"version"`
		Relations []struct {
			Kind, Range, Via string
		} `json:"relations"`
	} `json:"items"`
	Total int64 `json:"total"`
	Whole bool  `json:"whole"`
}

// TestR_API_086_UsedByListsThePackagesTheCallerMayReadThatDependOnAPackage: the npm publishes write who depends on what in their commit;
// the read on a replica of the role api lists each dependent once, by its newest version that depends, with the kind and the range; a
// dependent in a repository the caller may not read is neither listed nor counted; the package list's filter dependsOn keeps the same
// packages; and the task remote-describe writes the rows of versions stored without them, from the stored manifests. Integration, area
// packages, eight npm publishes of a few hundred bytes, one run of the task; about two seconds.
func TestR_API_086_UsedByListsThePackagesTheCallerMayReadThatDependOnAPackage(t *testing.T) {
	e := newEnv(t)
	e.seed()
	deps := func(kind string, m map[string]string) map[string]any { return map[string]any{kind: m} }
	// npm-local (bob and erin read it): app-a depends on @team-a/ui in its newest version too, with another range; npm-team (erin, not
	// bob): tool-b needs it for its development only; left-pad depends on nothing.
	e.publish("npm-local", spec{Name: "app-a", Version: "1.0.0", Extra: deps("dependencies", map[string]string{"@team-a/ui": "^1.3.0"})})
	e.publish("npm-local", spec{Name: "app-a", Version: "1.1.0", Extra: map[string]any{
		"dependencies": map[string]string{"@team-a/ui": "^1.4.0", "left-pad": "1.x"}, "peerDependencies": map[string]string{"@team-a/ui": ">=1"},
	}})
	e.publish("npm-team", spec{Name: "tool-b", Version: "0.1.0", Extra: deps("devDependencies", map[string]string{"@team-a/ui": "*"})})
	// An operator who runs the task; bound before the replica of the role api starts, which reads the bindings at its start.
	ops := e.st.User("ops")
	e.st.Bind("admin", ops)
	tok := e.st.Token(ops, scope.Scope{Inherit: true, Admin: true})
	api := e.st.Start(t, testenv.ReplicaOptions{Name: "api-a", Role: "api"})
	id := e.idOf(e.tokE, "@team-a/ui")
	read := func(token, path string) dependents {
		t.Helper()
		a := e.st.Do(api, http.MethodGet, path, token, nil)
		if a.Status != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, a.Status, a.Body)
		}
		var d dependents
		mustJSON(t, a.Body, &d)
		return d
	}
	line := func(d dependents) string {
		var parts []string
		for _, it := range d.Items {
			var rel []string
			for _, r := range it.Relations {
				rel = append(rel, r.Kind+" "+r.Range)
			}
			parts = append(parts, it.Name+"@"+it.Version+" ("+strings.Join(rel, ", ")+")")
		}
		return strings.Join(parts, "; ")
	}

	t.Run("a reader of both repositories sees both dependents, each by its newest version that depends, with every relation", func(t *testing.T) {
		d := read(e.tokE, "/api/v1/packages/"+id+"/dependents")
		want := "app-a@1.1.0 (dependencies ^1.4.0, peerDependencies >=1); tool-b@0.1.0 (devDependencies *)"
		if got := line(d); got != want || d.Total != 2 || d.Whole {
			t.Fatalf("got %q total %d whole %v, want %q total 2", got, d.Total, d.Whole, want)
		}
	})
	t.Run("a dependent in a repository the caller may not read is neither listed nor counted", func(t *testing.T) {
		d := read(e.tokB, "/api/v1/packages/"+id+"/dependents")
		if got := line(d); got != "app-a@1.1.0 (dependencies ^1.4.0, peerDependencies >=1)" || d.Total != 1 {
			t.Fatalf("got %q total %d", got, d.Total)
		}
		if d := read(e.tokD, "/api/v1/packages/"+id+"/dependents"); len(d.Items) != 0 || d.Total != 0 {
			t.Fatalf("a caller without a grant: %+v", d)
		}
	})
	t.Run("a project narrows the dependents as the package list does", func(t *testing.T) {
		if d := read(e.tokE, "/api/v1/packages/"+id+"/dependents?project=nothing"); d.Total != 0 {
			t.Fatalf("%+v", d)
		}
		if d := read(e.tokE, "/api/v1/packages/"+id+"/dependents?project=it"); d.Total != 2 {
			t.Fatalf("%+v", d)
		}
	})
	t.Run("the list filtered by dependsOn keeps the same packages, and needs one format with dependencies", func(t *testing.T) {
		q := "/api/v1/packages?format=npm&dependsOn=" + url.QueryEscape("@team-a/ui")
		if got := names(e.all(e.tokE, q)); !slices.Equal(got, []string{"app-a", "tool-b"}) {
			t.Fatalf("erin: %v", got)
		}
		if got := names(e.all(e.tokB, q)); !slices.Equal(got, []string{"app-a"}) {
			t.Fatalf("bob: %v", got)
		}
		if got := names(e.all(e.tokE, "/api/v1/packages?format=npm&dependsOn=left-pad")); !slices.Equal(got, []string{"app-a"}) {
			t.Fatalf("left-pad: %v", got)
		}
		for _, bad := range []string{"/api/v1/packages?dependsOn=left-pad", "/api/v1/packages?format=oci&dependsOn=left-pad"} {
			if a := e.get(e.tokE, bad); a.Status != http.StatusBadRequest {
				t.Errorf("%s: %d %s", bad, a.Status, a.Body)
			}
		}
	})
	t.Run("the task remote-describe writes the rows of versions stored without them, from the stored manifests", func(t *testing.T) {
		e.st.Exec(`DELETE FROM package_dependency_reads`)
		e.st.Exec(`DELETE FROM package_dependencies`)
		if d := read(e.tokE, "/api/v1/packages/"+id+"/dependents"); d.Total != 0 {
			t.Fatalf("before the task: %+v", d)
		}
		testenv.WaitFor(t, "the task to be idle", 60*time.Second, func() bool {
			return e.st.Number(`SELECT count(*) FROM job WHERE kind = 'remote-describe' AND state IN ('queued', 'running')`) == 0
		})
		// A replica of the role worker claims the job of the maintenance queue; its start queues a run, as versions are left to read, and
		// the operator's run is that one (409) or its own.
		e.st.Start(t, testenv.ReplicaOptions{Name: "worker-a", Role: "worker"})
		if a := e.st.Do(api, http.MethodPost, "/api/v1/system/tasks/remote-describe/run", tok, nil); a.Status != http.StatusAccepted && a.Status != http.StatusConflict {
			t.Fatalf("run: %d %s", a.Status, a.Body)
		}
		t.Cleanup(func() {
			if t.Failed() {
				t.Logf("jobs: %s", e.st.Text(`SELECT COALESCE(string_agg(kind || ' ' || state || ' ' || COALESCE(error::text, '') || ' ' || COALESCE(result::text, ''), E'\n'), '') FROM job WHERE kind = 'remote-describe'`))
			}
		})
		testenv.WaitFor(t, "the rows of the versions stored before", 60*time.Second, func() bool {
			return read(e.tokE, "/api/v1/packages/"+id+"/dependents").Total == 2
		})
		if n := e.st.Number(`SELECT count(*) FROM package_versions pv WHERE NOT EXISTS
		                     (SELECT 1 FROM package_dependency_reads r WHERE r.version_id = pv.id)`); n != 0 {
			t.Errorf("%d versions are not marked read", n)
		}
	})
}
