//go:build integration

package npm

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/npmstub"
)

// The download queue of a remote repository (remote-curation.md R-CUR-010 to R-CUR-012) against the integration stack and the counting
// npm upstream of the test process: a person's download ahead of one version, a bulk of a package, the failure of an attempt in its own
// words, the retries, the list's filter, a retry and a cancel through the API, and what a reader sees of the starter. One replica of
// the role all runs the rows; the dispatch task is run through the system tasks' API where a test waits for a next attempt instead of
// the minute of its schedule.

// downloadsEnv is newRemoteEnv with lead as a Maintainer (manage) of npm-remote and one replica of the role all.
func downloadsEnv(t *testing.T) (*renv, *testenv.Replica) {
	t.Helper()
	r := newRemoteEnv(t, remoteOptions{Valkey: true, Env: []string{
		"QUADER_LIMITS_FLUSH_INTERVAL=200ms", "QUADER_LIMITS_USAGE_FLUSH_INTERVAL=1s", "QUADER_JOBS_POLL_INTERVAL=200ms",
	}})
	r.st.Policy("npm-remote-managers", []string{"read", "manage"}, []string{rRemote}, r.lead)
	rep := r.st.Start(t, testenv.ReplicaOptions{Name: "all-1", Role: "all", Endpoints: customersEndpoint})
	return r, rep
}

const downloadsPath = "/api/v1/projects/" + testenv.DefaultProject + "/repositories/" + rRemote

// startDownload posts a download ahead and answers the status and the body.
func (r *renv) startDownload(rep *testenv.Replica, token string, body map[string]any) testenv.Answer {
	r.t.Helper()
	return r.st.Do(rep, http.MethodPost, downloadsPath+"/downloads", token, body)
}

// row reads one row of the queue through the API as lead.
func (r *renv) row(rep *testenv.Replica, id string) map[string]any {
	r.t.Helper()
	a := r.st.Do(rep, http.MethodGet, downloadsPath+"/downloads/"+id, r.tokL, nil)
	if a.Status != http.StatusOK {
		r.t.Fatalf("GET row %s: %d %s", id, a.Status, a.Body)
	}
	return a.JSON(r.t)
}

// runDispatch makes every waiting row due and asks for a run of the task remote-downloads-dispatch, which starts it (R-CUR-011): a
// run that is queued already (409, the schedule's of the minute) starts it as well.
func (r *renv) runDispatch(rep *testenv.Replica) {
	r.t.Helper()
	r.st.Exec(`UPDATE remote_download SET not_before = now() WHERE state = 'waiting'`)
	if a := r.st.Do(rep, http.MethodPost, "/api/v1/system/tasks/remote-downloads-dispatch/run", r.opsPAT, nil); a.Status != http.StatusAccepted &&
		a.Status != http.StatusConflict {
		r.t.Fatalf("run the dispatch: %d %s", a.Status, a.Body)
	}
}

// dispatchUntil runs the dispatch until the row satisfies ok: a run that was running already when the row became due misses it.
func dispatchUntil(t *testing.T, r *renv, rep *testenv.Replica, id, what string, ok func(map[string]any) bool) map[string]any {
	t.Helper()
	var got map[string]any
	testenv.WaitFor(t, what, 60*time.Second, func() bool {
		got = r.row(rep, id)
		if ok(got) {
			return true
		}
		if got["state"] == "waiting" {
			r.runDispatch(rep)
		}
		return false
	})
	return got
}

func waitRow(t *testing.T, r *renv, rep *testenv.Replica, id, what string, ok func(map[string]any) bool) map[string]any {
	t.Helper()
	var got map[string]any
	testenv.WaitFor(t, what, 60*time.Second, func() bool {
		got = r.row(rep, id)
		return ok(got)
	})
	return got
}

func TestR_CUR_010_ADownloadAheadOfOneVersionAndOfAPackage(t *testing.T) {
	r, rep := downloadsEnv(t)
	p := upstreamPackage(t, 3)

	t.Run("a reader who may populate downloads one version, charged to the reader", func(t *testing.T) {
		v := p.Versions[0].Version
		a := r.startDownload(rep, r.tokA, map[string]any{"level": "version", "name": p.Name, "version": v})
		if a.Status != http.StatusAccepted {
			t.Fatalf("alice's download of %s@%s: %d %s", p.Name, v, a.Status, a.Body)
		}
		row := a.JSON(t)["download"].(map[string]any)
		if row["origin"] != "person" || row["delivery"] != "delivered" {
			t.Errorf("the row: %v", row)
		}
		waitRow(t, r, rep, row["id"].(string), "alice's row done", func(m map[string]any) bool { return m["state"] == "done" })
		if n := r.itemCount(rRemote, p.Name+"/-/"+p.Name+"-"+v+".tgz"); n != 1 {
			t.Errorf("the tarball is stored %d times", n)
		}
		testenv.WaitFor(t, "alice's background download in the usage", 30*time.Second, func() bool {
			return r.st.Number(`SELECT COALESCE(sum(background_fetches), 0) FROM usage_bucket WHERE granularity = '5m'
				AND subject_kind = 'user' AND subject_ref = $1`, r.alice.ID) == 1
		})
		if a := r.startDownload(rep, r.tokA, map[string]any{"level": "version", "name": p.Name, "version": v}); a.Status != http.StatusAccepted {
			t.Errorf("a stored version downloaded again: %d %s", a.Status, a.Body)
		}
	})

	t.Run("every version of a package needs manage", func(t *testing.T) {
		body := map[string]any{"level": "package", "name": p.Name, "maxVersions": 2}
		if a := r.startDownload(rep, r.tokA, body); a.Status != http.StatusForbidden {
			t.Fatalf("alice's bulk: %d %s, want 403", a.Status, a.Body)
		}
		pv := r.st.Do(rep, http.MethodPost, downloadsPath+"/downloads/preview", r.tokL, body)
		if pv.Status != http.StatusOK || pv.JSON(t)["packages"] != float64(1) {
			t.Fatalf("the preview: %d %s", pv.Status, pv.Body)
		}
		a := r.startDownload(rep, r.tokL, body)
		if a.Status != http.StatusAccepted || !strings.Contains(a.Header.Get("Location"), "/download-bulks/") {
			t.Fatalf("lead's bulk: %d %s", a.Status, a.Body)
		}
		bulk := a.JSON(t)["bulk"].(map[string]any)
		r.runDispatch(rep)
		testenv.WaitFor(t, "the bulk's two rows done", 60*time.Second, func() bool {
			got := r.st.Do(rep, http.MethodGet, downloadsPath+"/download-bulks/"+bulk["id"].(string), r.tokL, nil).JSON(t)
			rows, _ := got["rows"].(map[string]any)
			if got["state"] == "queued" || got["state"] == "expanding" {
				r.runDispatch(rep)
			}
			return got["versionsQueued"] == float64(2) && rows["done"] == float64(2) && (got["state"] == "done" || got["state"] == "bounded")
		})
		newest := p.Versions[len(p.Versions)-1].Version
		if n := r.itemCount(rRemote, p.Name+"/-/"+p.Name+"-"+newest+".tgz"); n != 1 {
			t.Errorf("the newest version %s is not stored by the bulk", newest)
		}
		if n := r.st.Number(`SELECT count(*) FROM remote_download WHERE origin = 'bulk' AND started_by = $1::uuid`, r.lead.ID); n != 2 {
			t.Errorf("%d rows of the bulk name lead, want 2", n)
		}
	})
}

func TestR_CUR_011_AFailureInItsOwnWordsAndTheRetries(t *testing.T) {
	r, rep := downloadsEnv(t)
	p := upstreamPackage(t, 3)
	v, v2 := p.Versions[0].Version, p.Versions[1].Version
	if err := r.stub.SetMode(npmstub.Mode{Down: npmstub.DownUnavailable}); err != nil {
		t.Fatal(err)
	}
	a := r.startDownload(rep, r.tokL, map[string]any{"level": "version", "name": p.Name, "version": v})
	if a.Status != http.StatusAccepted {
		t.Fatalf("the download: %d %s", a.Status, a.Body)
	}
	id := a.JSON(t)["download"].(map[string]any)["id"].(string)

	t.Run("the 503 is kept as upstream_error at the step status, with the address without its query and the excerpt", func(t *testing.T) {
		got := waitRow(t, r, rep, id, "the first attempt failed", func(m map[string]any) bool {
			return m["state"] == "waiting" && m["failure"] != nil
		})
		f := got["failure"].(map[string]any)
		addr, _ := f["address"].(string)
		if f["class"] != "upstream_error" || f["step"] != "status" || f["status"] != float64(503) || f["attempt"] != float64(1) ||
			!strings.HasPrefix(addr, r.run.URL) || strings.Contains(addr, "?") || !strings.Contains(f["excerpt"].(string), "the test upstream is down") {
			t.Fatalf("the failure: %v", f)
		}
		if got["nextAttemptAt"] == nil || got["attempts"] != float64(1) {
			t.Errorf("the row waits for no next attempt: %v", got)
		}
	})
	t.Run("a second attempt fails, the third after the upstream is back ends done", func(t *testing.T) {
		dispatchUntil(t, r, rep, id, "the second attempt failed", func(m map[string]any) bool {
			return m["state"] == "waiting" && m["attempts"] == float64(2)
		})
		if err := r.stub.SetMode(npmstub.Mode{}); err != nil {
			t.Fatal(err)
		}
		got := dispatchUntil(t, r, rep, id, "the third attempt done", func(m map[string]any) bool { return m["state"] == "done" })
		if got["attempts"] != float64(3) || got["outcome"] != "completed" || got["stored"] != float64(1) {
			t.Errorf("the row: %v", got)
		}
	})

	var authRow string
	t.Run("a 401 ends the row failed at once with upstream_auth", func(t *testing.T) {
		if err := r.stub.SetMode(npmstub.Mode{RequireAuthorization: "Bearer not-sent"}); err != nil {
			t.Fatal(err)
		}
		a := r.startDownload(rep, r.tokL, map[string]any{"level": "version", "name": p.Name, "version": v2})
		if a.Status != http.StatusAccepted {
			t.Fatalf("the download: %d %s", a.Status, a.Body)
		}
		authRow = a.JSON(t)["download"].(map[string]any)["id"].(string)
		got := waitRow(t, r, rep, authRow, "the row failed", func(m map[string]any) bool { return m["state"] == "failed" })
		if f := got["failure"].(map[string]any); f["class"] != "upstream_auth" || f["status"] != float64(401) || got["attempts"] != float64(1) {
			t.Errorf("the failure: %v", got)
		}
	})

	t.Run("R-CUR-012 the list filters by class; a reader sees the rows without the starter's name", func(t *testing.T) {
		a := r.st.Do(rep, http.MethodGet, downloadsPath+"/downloads?class=upstream_auth", r.tokL, nil)
		var page struct {
			Items  []map[string]any `json:"items"`
			Counts map[string]int64 `json:"counts"`
		}
		if a.Status != http.StatusOK || json.Unmarshal(a.Body, &page) != nil || len(page.Items) != 1 || page.Items[0]["id"] != authRow {
			t.Fatalf("the filtered list: %d %s", a.Status, a.Body)
		}
		if page.Counts["failed"] != 1 || page.Counts["done"] != 1 {
			t.Errorf("the counts: %v", page.Counts)
		}
		if who := page.Items[0]["startedBy"].(map[string]any); who["name"] != "lead" {
			t.Errorf("lead sees the starter %v", who)
		}
		c := r.st.Do(rep, http.MethodGet, downloadsPath+"/downloads", r.tokC, nil)
		if c.Status != http.StatusOK || json.Unmarshal(c.Body, &page) != nil || len(page.Items) != 2 {
			t.Fatalf("carol's list: %d %s", c.Status, c.Body)
		}
		for _, it := range page.Items {
			if who := it["startedBy"].(map[string]any); who["id"] != nil || who["name"] != nil {
				t.Errorf("a reader sees the starter %v", who)
			}
		}
		if a := r.st.Do(rep, http.MethodPost, downloadsPath+"/downloads/"+authRow+"/retry", r.tokC, nil); a.Status != http.StatusForbidden {
			t.Errorf("carol's retry of lead's row: %d, want 403", a.Status)
		}
	})

	t.Run("R-CUR-012 a retry runs the row again and writes its event", func(t *testing.T) {
		if err := r.stub.SetMode(npmstub.Mode{}); err != nil {
			t.Fatal(err)
		}
		a := r.st.Do(rep, http.MethodPost, downloadsPath+"/downloads/"+authRow+"/retry", r.tokL, nil)
		if a.Status != http.StatusOK {
			t.Fatalf("the retry: %d %s", a.Status, a.Body)
		}
		waitRow(t, r, rep, authRow, "the retried row done", func(m map[string]any) bool { return m["state"] == "done" })
		if n := r.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'remote.download.retried'`); n != 1 {
			t.Errorf("%d events remote.download.retried, want 1", n)
		}
		if a := r.st.Do(rep, http.MethodPost, downloadsPath+"/downloads/"+authRow+"/retry", r.tokL, nil); a.Status != http.StatusConflict ||
			!strings.Contains(string(a.Body), "not_retryable") {
			t.Errorf("a retry of a done row: %d %s", a.Status, a.Body)
		}
	})

	t.Run("R-CUR-012 a cancel of a waiting row leaves the upstream untouched", func(t *testing.T) {
		if err := r.stub.SetMode(npmstub.Mode{Down: npmstub.DownUnavailable}); err != nil {
			t.Fatal(err)
		}
		v3 := p.Versions[2].Version
		a := r.startDownload(rep, r.tokL, map[string]any{"level": "version", "name": p.Name, "version": v3})
		if a.Status != http.StatusAccepted {
			t.Fatalf("the download: %d %s", a.Status, a.Body)
		}
		id := a.JSON(t)["download"].(map[string]any)["id"].(string)
		waitRow(t, r, rep, id, "the row waits for its next attempt", func(m map[string]any) bool { return m["state"] == "waiting" && m["attempts"] == float64(1) })
		before := r.stub.Counts().Tarball(p.Name, p.Name+"-"+v3+".tgz")
		if c := r.st.Do(rep, http.MethodPost, downloadsPath+"/downloads/"+id+"/cancel", r.tokL, nil); c.Status != http.StatusOK || c.JSON(t)["state"] != "cancelled" {
			t.Fatalf("the cancel: %d %s", c.Status, c.Body)
		}
		if err := r.stub.SetMode(npmstub.Mode{}); err != nil {
			t.Fatal(err)
		}
		r.runDispatch(rep)
		time.Sleep(time.Second)
		if n := r.stub.Counts().Tarball(p.Name, p.Name+"-"+v3+".tgz"); n != before {
			t.Errorf("the upstream was asked for the cancelled version's tarball %d times", n-before)
		}
		if n := r.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'remote.download.cancelled'`); n != 1 {
			t.Errorf("%d events remote.download.cancelled, want 1", n)
		}
	})
}
