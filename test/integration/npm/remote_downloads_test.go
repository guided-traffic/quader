//go:build integration

package npm

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/npmstub"
	"github.com/guided-traffic/quader/test/npmstub/corpus"
)

// The download queue of a remote repository (remote-curation.md R-CUR-010 to R-CUR-012) against the integration stack and the counting
// npm upstream of the test process: a person's download ahead of one version, a bulk of a package, the failure of an attempt in its own
// words, the retries, the list's filter, a retry and a cancel through the API, and what a reader sees of the starter. One replica of
// the role all runs the rows; the dispatch task is run through the system tasks' API where a test waits for a next attempt instead of
// the minute of its schedule.

// downloadsEnv is newRemoteEnv with lead as a Maintainer (manage) of npm-remote and one replica of the role all, which keeps at most two
// downloads in sync per repository (R-CUR-027's bound, reached by TestR_CUR_027_…).
func downloadsEnv(t *testing.T) (*renv, *testenv.Replica) {
	t.Helper()
	r := newRemoteEnv(t, remoteOptions{Valkey: true, Env: []string{
		"QUADER_LIMITS_FLUSH_INTERVAL=200ms", "QUADER_LIMITS_USAGE_FLUSH_INTERVAL=1s", "QUADER_JOBS_POLL_INTERVAL=200ms",
		"QUADER_PROXY_DOWNLOADS_MAX_SYNCS_PER_REPOSITORY=2",
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

	// The decider of the queue's operations is the delivery rules' (R-CUR-005, R-CUR-010): a version a rule blocks is started by the
	// holders of manage only, and its row reads delivery blocked. Data: one tarball of the stub's corpus.
	t.Run("R-CUR-010 R-CUR-005 a version a rule blocks is started by manage only and its row says blocked", func(t *testing.T) {
		// The oldest version, which alice stored above: the bulk below takes the two newest.
		v := p.Versions[0].Version
		key := testenv.DefaultProject + "/" + rRemote
		r.st.MustPutDeliveryRule(rep, r.tokL, key, testenv.VersionRule(p.Name, v, "block"))
		r.st.WaitDelivered(t, rep, r.tokL, key, p.Name, v, false)
		body := map[string]any{"level": "version", "name": p.Name, "version": v}
		if a := r.startDownload(rep, r.tokA, body); a.Status != http.StatusForbidden {
			t.Fatalf("alice's download of a blocked version: %d %s, want 403", a.Status, a.Body)
		}
		// alice's second download above may still run: lead is admitted either way, and the answer names that row (409 already_queued).
		a := r.startDownload(rep, r.tokL, body)
		if a.Status != http.StatusAccepted && a.Status != http.StatusConflict {
			t.Fatalf("lead's download of a blocked version: %d %s", a.Status, a.Body)
		}
		if row, _ := a.JSON(t)["download"].(map[string]any); row["delivery"] != "blocked" {
			t.Errorf("the row of a blocked version: %v", row)
		}
	})

	t.Run("every version of a package needs manage", func(t *testing.T) {
		body := map[string]any{"level": "package", "name": p.Name, "maxVersions": 2}
		if a := r.startDownload(rep, r.tokA, body); a.Status != http.StatusForbidden {
			t.Fatalf("alice's bulk: %d %s, want 403", a.Status, a.Body)
		}
		pv := r.st.Do(rep, http.MethodPost, downloadsPath+"/downloads/preview", r.tokL, body)
		// The preview counts the newest maxVersions of the package by one catalogue call (R-CUR-010): npm announces no tarball size.
		// What the counts leave open is a code the screen says in its words (R-UI-214).
		if m := pv.JSON(t); pv.Status != http.StatusOK || m["packages"] != float64(1) || m["versions"] != float64(2) || m["bytes"] != nil ||
			m["sizeUnknown"] != float64(2) || m["note"] != "sizes_unannounced" {
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

	// A range of versions in the format's order (R-CUR-010, Q-CUR-190 to Q-CUR-193): the preview counts it and keeps its bounds, the
	// bulk stores them and queues only the versions between them; a bound the format does not rank and a range that holds no version
	// are refused at their pointers. Data: two tarballs of another package of the stub's corpus.
	t.Run("R-CUR-010 a bulk takes a range of versions in the format's order", func(t *testing.T) {
		q := rangePackage(t, p.Name)
		v := func(i int) string { return q.Versions[i].Version }
		for _, tc := range []struct {
			body    map[string]any
			pointer string
			code    string
		}{
			{map[string]any{"level": "package", "name": q.Name, "fromVersion": "latest"}, "/fromVersion", "invalid_version"},
			{map[string]any{"level": "package", "name": q.Name, "fromVersion": v(3), "toVersion": v(1)}, "/toVersion", "out_of_range"},
			{map[string]any{"level": "version", "name": q.Name, "version": v(1), "toVersion": v(1)}, "/toVersion", "validation_failed"},
		} {
			a := r.st.Do(rep, http.MethodPost, downloadsPath+"/downloads/preview", r.tokL, tc.body)
			if a.Status/100 != 4 || !strings.Contains(string(a.Body), `"pointer":"`+tc.pointer+`"`) || !strings.Contains(string(a.Body), tc.code) {
				t.Errorf("the preview of %v: %d %s, want the refusal at %s with %s", tc.body, a.Status, a.Body, tc.pointer, tc.code)
			}
		}
		// From v(1), included, to below v(3): v(2) and v(1); a bound need not be listed (the second preview's upper bound).
		body := map[string]any{"level": "package", "name": q.Name, "fromVersion": v(1), "toVersion": v(3), "toInclusive": false}
		pv := r.st.Do(rep, http.MethodPost, downloadsPath+"/downloads/preview", r.tokL, body)
		m := pv.JSON(t)
		bounds, _ := m["bounds"].(map[string]any)
		if pv.Status != http.StatusOK || m["versions"] != float64(2) || m["notVersions"] != float64(0) ||
			bounds["fromVersion"] != v(1) || bounds["toVersion"] != v(3) || bounds["toInclusive"] != false {
			t.Fatalf("the preview of the range: %d %s", pv.Status, pv.Body)
		}
		open := map[string]any{"level": "package", "name": q.Name, "fromVersion": v(1), "toVersion": "999.0.0"}
		if m := r.st.Do(rep, http.MethodPost, downloadsPath+"/downloads/preview", r.tokL, open).JSON(t); m["versions"] != float64(len(q.Versions)-1) {
			t.Errorf("the preview up to a version the upstream does not list: %v", m)
		}
		a := r.startDownload(rep, r.tokL, body)
		if a.Status != http.StatusAccepted {
			t.Fatalf("lead's bulk of the range: %d %s", a.Status, a.Body)
		}
		id := a.JSON(t)["bulk"].(map[string]any)["id"].(string)
		r.runDispatch(rep)
		testenv.WaitFor(t, "the range's two rows queued", 60*time.Second, func() bool {
			got := r.st.Do(rep, http.MethodGet, downloadsPath+"/download-bulks/"+id, r.tokL, nil).JSON(t)
			if got["state"] == "expanding" {
				r.runDispatch(rep)
			}
			return got["versionsQueued"] == float64(2)
		})
		got := r.st.Do(rep, http.MethodGet, downloadsPath+"/download-bulks/"+id, r.tokL, nil).JSON(t)
		if b, _ := got["bounds"].(map[string]any); b["fromVersion"] != v(1) || b["toVersion"] != v(3) || b["toInclusive"] != false {
			t.Errorf("the bulk's bounds: %v", got["bounds"])
		}
		if n := r.st.Number(`SELECT count(*) FROM remote_download WHERE bulk_id = $1::uuid AND version IN ($2, $3)`, id, v(1), v(2)); n != 2 {
			t.Errorf("%d rows of the bulk are %s and %s, want both", n, v(1), v(2))
		}
	})
}

// rangePackage is a package of the stub's corpus other than not with at least four versions and no pre-release, ascending by SemVer.
func rangePackage(t testing.TB, not string) *corpus.Package {
	t.Helper()
	for _, p := range corpus.Default().Packages {
		if p.Name == not || strings.HasPrefix(p.Name, "@") || len(p.Versions) < 4 || len(p.Versions) >= 30 {
			continue
		}
		if !slices.ContainsFunc(p.Versions, func(v corpus.Version) bool { return strings.Contains(v.Version, "-") }) {
			return p
		}
	}
	t.Fatal("the corpus has no such package")
	return nil
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

// An upstream's rate limit holds the repository's queue (R-CUR-011 "An upstream's rate limit", R-CUR-012, Q-CUR-170): the stub answers
// the next document request 429 with Retry-After: 30, which is longer than a run waits out. The row waits again without counting the
// attempt and names the upstream's answer; the list answers the limit; a second row and the dispatch start nothing and the stub is not
// asked while it holds; once its time came (set by the test instead of waited for), one row asks, completes, ends the limit, and the
// other starts in the place it frees. The shared engine runs every format's queue, so this one case stands for all of them. Data: two
// tarballs of the stub's corpus, a few hundred bytes each; about 4 s.
func TestR_CUR_011_AnUpstreamsRateLimitHoldsTheQueue(t *testing.T) {
	r, rep := downloadsEnv(t)
	p := upstreamPackage(t, 2)
	v, v2 := p.Versions[0].Version, p.Versions[1].Version
	if err := r.stub.SetMode(npmstub.Mode{Throttle: &npmstub.Throttle{Kind: npmstub.Throttle429, Class: npmstub.KindDocument, RetryAfterSeconds: 30}}); err != nil {
		t.Fatal(err)
	}
	a := r.startDownload(rep, r.tokL, map[string]any{"level": "version", "name": p.Name, "version": v})
	if a.Status != http.StatusAccepted {
		t.Fatalf("the download: %d %s", a.Status, a.Body)
	}
	id := a.JSON(t)["download"].(map[string]any)["id"].(string)

	got := waitRow(t, r, rep, id, "the row met the limit", func(m map[string]any) bool { return m["state"] == "waiting" && m["failure"] != nil })
	f := got["failure"].(map[string]any)
	headers, _ := f["headers"].(map[string]any)
	if f["class"] != "upstream_rate_limited" || f["status"] != float64(429) || f["retryAfter"] != float64(30) || headers["Retry-After"] != "30" ||
		got["attempts"] != float64(0) || got["nextAttemptAt"] == nil {
		t.Fatalf("the limited row: %v", got)
	}
	list := r.st.Do(rep, http.MethodGet, downloadsPath+"/downloads", r.tokC, nil).JSON(t)
	limit, _ := list["limit"].(map[string]any)
	if limit["holds"] != true || limit["hinted"] != true || limit["waits"] != float64(1) || limit["upstream"] == "" {
		t.Fatalf("the list's limit, as a reader reads it: %v", list["limit"])
	}

	documents := r.stub.Counts().Documents(p.Name)
	b := r.startDownload(rep, r.tokL, map[string]any{"level": "version", "name": p.Name, "version": v2})
	if b.Status != http.StatusAccepted {
		t.Fatalf("the second download: %d %s", b.Status, b.Body)
	}
	id2 := b.JSON(t)["download"].(map[string]any)["id"].(string)
	r.runDispatch(rep)
	time.Sleep(time.Second)
	if a, b := r.row(rep, id), r.row(rep, id2); a["state"] != "waiting" || b["state"] != "waiting" || b["attempts"] != float64(0) ||
		r.stub.Counts().Documents(p.Name) != documents {
		t.Fatalf("under the limit: %v %v, documents asked %d more", a["state"], b["state"], r.stub.Counts().Documents(p.Name)-documents)
	}

	if err := r.stub.SetMode(npmstub.Mode{}); err != nil {
		t.Fatal(err)
	}
	r.st.Exec(`UPDATE remote_download_limits SET until = now()`)
	done := dispatchUntil(t, r, rep, id, "the first row done after the limit", func(m map[string]any) bool { return m["state"] == "done" })
	if done["attempts"] != float64(1) {
		t.Errorf("the limited attempt counted: %v", done["attempts"])
	}
	waitRow(t, r, rep, id2, "the second row done in the place the first freed", func(m map[string]any) bool { return m["state"] == "done" })
	if list := r.st.Do(rep, http.MethodGet, downloadsPath+"/downloads", r.tokL, nil).JSON(t); list["limit"] != nil {
		t.Errorf("the limit stays after an answer without it: %v", list["limit"])
	}
}
