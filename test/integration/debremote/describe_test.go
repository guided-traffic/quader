//go:build integration

package debremote

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_DEB_037_ACachedPackageFileIsDescribedByItsControlParagraph: a package file a remote repository cached has the row of its
// control paragraph a local one has (R-DEB-037), so the package API answers its description, its control fields and the suite and
// component of the index that listed it; the removal of the cached file takes the row with it.
func TestR_DEB_037_ACachedPackageFileIsDescribedByItsControlParagraph(t *testing.T) {
	e := newEnv(t)
	repo := e.fresh(true)
	e.fetchIndexes(repo)
	eventually(t, 10*time.Second, "the rows", func() bool { return e.rows("deb_remote_entries", repo) > 0 })
	path := e.st.Text(`SELECT path FROM deb_remote_entries WHERE repo_id = $1 AND package = 'quader-hello' AND architecture = 'amd64'`, e.repoID(repo))
	if path == "" {
		t.Fatal("the stable index lists no quader-hello for amd64")
	}
	// The management API's requests go to a replica of the role api, which serves no package protocol and builds no format host
	// (R-ARCH-001, Q-ARCH-1070).
	apiRep := e.st.Start(t, testenv.ReplicaOptions{Name: "api-a", Role: "api"})
	if a := e.get(repo, path); a.Status != http.StatusOK {
		t.Fatalf("the package file: %d", a.Status)
	}
	a := e.st.Do(apiRep, "GET", "/api/v1/packages?format=deb&repository="+url.QueryEscape(project+"/"+repo), e.admin, nil)
	items, _ := a.JSON(t)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("the remote lists %v", items)
	}
	pkg, _ := items[0].(map[string]any)
	id, _ := pkg["id"].(string)
	version, _ := pkg["latestVersion"].(string)
	var md map[string]any
	eventually(t, 30*time.Second, "the control paragraph", func() bool {
		a := e.st.Do(apiRep, "GET", "/api/v1/packages/"+id+"/versions/"+url.PathEscape(version), e.admin, nil)
		if a.Status != http.StatusOK {
			t.Fatalf("the version: %d %s", a.Status, a.Body)
		}
		md, _ = a.JSON(t)["metadata"].(map[string]any)
		return md["maintainer"] != nil
	})
	// The package's description is its newest version's: the first line of the cached file's Description.
	if p := e.st.Do(apiRep, "GET", "/api/v1/packages/"+id, e.admin, nil).JSON(t); p["description"] != "prints a fixed line" {
		t.Errorf("description %v, want the paragraph's first line", p["description"])
	}
	if md["maintainer"] != "quader test archive <debstub@quader.invalid>" {
		t.Errorf("metadata.maintainer %v", md["maintainer"])
	}
	// The relations are sent parsed (R-DEB-038).
	if rel, _ := md["relations"].(map[string]any); !strings.Contains(jsonText(t, rel["depends"]), `{"name":"libquader1","op":">=","version":"1.2"}`) {
		t.Errorf("metadata.relations %v", md["relations"])
	}
	// The job that wrote the row queued the reading of the cached file's data archive (R-DEB-039): the file list answers.
	eventually(t, 30*time.Second, "the data archive read", func() bool {
		a := e.st.Do(apiRep, "GET", "/api/v1/packages/"+id+"/versions/"+url.PathEscape(version)+"/contents", e.admin, nil)
		return a.Status == http.StatusOK && strings.Contains(string(a.Body), `"path":"/usr/bin/quader-hello"`)
	})
	if arch, _ := md["architectures"].([]any); len(arch) != 1 || arch[0] != "amd64" {
		t.Errorf("architectures %v, want [amd64]", md["architectures"])
	}
	if c, _ := md["coordinates"].([]any); len(c) != 1 || c[0] != "stable/main" {
		t.Errorf("coordinates %v, want [stable/main]: the index that listed the file", md["coordinates"])
	}

	// A file cached before the job existed has no row: the task remote-describe, which an operator runs (and a worker queues when it
	// starts), describes it from the stored file without a request to the upstream; a second run finds nothing left and queues nothing.
	e.st.Exec(`DELETE FROM deb_binaries WHERE repo_id = $1`, e.repoID(repo))
	upstream := e.stub.Counts().Total()
	// The run queues the package, unless a run the scheduler queued (on a new database its latest due slot) queued it already.
	if res := runTask(t, e, "remote-describe"); !strings.Contains(res, `"queued": 1`) && !strings.Contains(res, `"queued": 0`) {
		t.Errorf("the first run's result %s, want one package queued or none", res)
	}
	eventually(t, 30*time.Second, "the row of the file cached before", func() bool { return e.rows("deb_binaries", repo) == 1 })
	if res := runTask(t, e, "remote-describe"); !strings.Contains(res, `"queued": 0`) {
		t.Errorf("the second run's result %s, want nothing queued", res)
	}
	if after := e.stub.Counts().Total(); after != upstream {
		t.Errorf("the description of the cached file made %d requests to the upstream", after-upstream)
	}

	// The removal of the cached file takes its row.
	if a := e.st.Do(apiRep, "DELETE", "/api/v1/projects/"+project+"/repositories/"+repo+"/items?path="+url.QueryEscape(path), e.admin, nil); a.Status >= 300 {
		t.Fatalf("delete the cached file: %d %s", a.Status, a.Body)
	}
	eventually(t, 20*time.Second, "the row to go", func() bool { return e.rows("deb_binaries", repo) == 0 })
}

// runTask runs a maintenance task as an operator does and returns the result of its job once it succeeded.
func runTask(t *testing.T, e *denv, task string) string {
	t.Helper()
	// A run the scheduler queued (on a new database its latest due slot) ends first: a task runs once at a time.
	eventually(t, 60*time.Second, "the task to be idle", func() bool {
		return e.st.Number(`SELECT count(*) FROM job WHERE kind = $1 AND state IN ('queued', 'running')`, task) == 0
	})
	before := e.st.Number(`SELECT count(*) FROM job WHERE kind = $1`, task)
	if a := e.st.Do(e.r, http.MethodPost, "/api/v1/system/tasks/"+task+"/run", e.admin, nil); a.Status != http.StatusAccepted {
		t.Fatalf("run %s: %d %s", task, a.Status, a.Body)
	}
	eventually(t, 60*time.Second, "the run of "+task, func() bool {
		return e.st.Number(`SELECT count(*) FROM job WHERE kind = $1`, task) > before &&
			e.st.Number(`SELECT count(*) FROM job WHERE kind = $1 AND state IN ('queued', 'running')`, task) == 0
	})
	if s := e.st.Text(`SELECT state::text FROM job WHERE kind = $1 ORDER BY created_at DESC LIMIT 1`, task); s != "succeeded" {
		t.Fatalf("the run of %s ended %s: %s", task, s, e.st.Text(`SELECT COALESCE(error::text, '') FROM job WHERE kind = $1 ORDER BY created_at DESC LIMIT 1`, task))
	}
	return e.st.Text(`SELECT COALESCE(result::text, '') FROM job WHERE kind = $1 ORDER BY created_at DESC LIMIT 1`, task)
}

// jsonText is v as JSON without the escaping of HTML characters.
func jsonText(t *testing.T, v any) string {
	t.Helper()
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(b.String())
}
