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
	"github.com/guided-traffic/quader/test/npmstub/corpus"
)

// The requests for an allow and what was fetched for examination (remote-curation.md R-CUR-014 to R-CUR-016) against the integration
// stack and the counting npm upstream: npm-remote delivers only what a rule allows; lead holds manage on it, alice and carol read it.
// One replica of the role all runs the rows; the tasks are run through the system tasks' API instead of waiting for their schedules.
// Data: the stub's corpus, a few tarballs of a few kilobytes.

// requestsEnv is downloadsEnv with npm-remote in the mode onlyAllowed.
func requestsEnv(t *testing.T) (*renv, *testenv.Replica) {
	t.Helper()
	r := newRemoteEnv(t, remoteOptions{Valkey: true, Remote: map[string]any{"delivery": testenv.OnlyAllowed()}, Env: []string{
		"QUADER_LIMITS_FLUSH_INTERVAL=200ms", "QUADER_LIMITS_USAGE_FLUSH_INTERVAL=1s", "QUADER_JOBS_POLL_INTERVAL=200ms",
	}})
	r.st.Policy("npm-remote-managers", []string{"read", "manage"}, []string{rRemote}, r.lead)
	rep := r.st.Start(t, testenv.ReplicaOptions{Name: "all-1", Role: "all", Endpoints: customersEndpoint})
	return r, rep
}

// otherPackage is a package of the corpus without a scope, other than the names given, with at least n versions.
func otherPackage(t *testing.T, n int, not ...string) *corpus.Package {
	t.Helper()
	for _, p := range corpus.Default().Packages {
		if !strings.HasPrefix(p.Name, "@") && len(p.Versions) >= n && len(p.Versions) < 30 && !slices.Contains(not, p.Name) {
			return p
		}
	}
	t.Fatal("the corpus has no such package")
	return nil
}

func remoteTarball(name, version string) string {
	return name + "/-/" + corpus.TarballFile(name, version)
}

// runTask asks for a run of a system task; a run that is queued already (409) runs as well.
func (r *renv) runTask(rep *testenv.Replica, name string) {
	r.t.Helper()
	if a := r.st.Do(rep, http.MethodPost, "/api/v1/system/tasks/"+name+"/run", r.opsPAT, nil); a.Status != http.StatusAccepted && a.Status != http.StatusConflict {
		r.t.Fatalf("run %s: %d %s", name, a.Status, a.Body)
	}
}

// examined is the count of examination rows of a version of npm-remote.
func (r *renv) examined(name, version string) int64 {
	r.t.Helper()
	return r.st.Number(`SELECT count(*) FROM remote_examinations x JOIN repositories r ON r.id = x.repo_id
		WHERE r.key = $1 AND x.name = $2 AND x.version = $3`, rRemote, name, version)
}

// requestsList reads the list of npm-remote's request entries as the token's holder.
func (r *renv) requestsList(rep *testenv.Replica, token, query string) []map[string]any {
	r.t.Helper()
	a := r.st.Do(rep, http.MethodGet, downloadsPath+"/delivery-requests"+query, token, nil)
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if a.Status != http.StatusOK || json.Unmarshal(a.Body, &page) != nil {
		r.t.Fatalf("the list: %d %s", a.Status, a.Body)
	}
	return page.Items
}

func entryOf(items []map[string]any, id string) map[string]any {
	for _, it := range items {
		if it["id"] == id {
			return it
		}
	}
	return nil
}

// TestR_CUR_014_AnExaminationIsStoredScannedAndNotDelivered: in the mode onlyAllowed lead downloads a version ahead: it is stored and held
// for examination with its tarball, its row says it is not delivered, the packages API lists it not available with the reason
// notAllowed, and a client's request is refused; an allow serves it without an upstream request. About 3 s.
func TestR_CUR_014_AnExaminationIsStoredScannedAndNotDelivered(t *testing.T) {
	r, rep := requestsEnv(t)
	p := upstreamPackage(t, 2)
	v := p.Versions[0].Version
	file := remoteTarball(p.Name, v)

	a := r.startDownload(rep, r.tokL, map[string]any{"level": "version", "name": p.Name, "version": v})
	if a.Status != http.StatusAccepted {
		t.Fatalf("lead's download of %s@%s: %d %s", p.Name, v, a.Status, a.Body)
	}
	row := a.JSON(t)["download"].(map[string]any)
	if row["delivery"] != "notAllowed" {
		t.Errorf("the row of a version the rules do not deliver: %v", row)
	}
	waitRow(t, r, rep, row["id"].(string), "the examination done", func(m map[string]any) bool { return m["state"] == "done" })

	t.Run("R-CUR-014 R-CUR-016 the version is stored and held for examination with its file", func(t *testing.T) {
		if n := r.itemCount(rRemote, file); n != 1 {
			t.Fatalf("the tarball is stored %d times", n)
		}
		files := r.st.Text(`SELECT array_to_string(x.files, ',') FROM remote_examinations x JOIN repositories r ON r.id = x.repo_id
			WHERE r.key = $1 AND x.name = $2 AND x.version = $3`, rRemote, p.Name, v)
		if !strings.Contains(files, file) {
			t.Errorf("the examination row names %q, want the tarball", files)
		}
	})
	t.Run("R-CUR-014 the packages API lists it not available, notAllowed", func(t *testing.T) {
		var id string
		testenv.WaitFor(t, "the stored package in the packages API", 10*time.Second, func() bool {
			a := r.st.Do(rep, http.MethodGet, "/api/v1/packages/lookup?format=npm&name="+p.Name, r.tokL, nil)
			id, _ = a.JSON(t)["id"].(string)
			return a.Status == http.StatusOK && id != ""
		})
		a := r.st.Do(rep, http.MethodGet, "/api/v1/packages/"+id+"/versions/"+v, r.tokL, nil)
		var d struct {
			Repositories []struct {
				Key, UnavailableReason string
				Available              bool
			} `json:"repositories"`
		}
		if a.Status != http.StatusOK || json.Unmarshal(a.Body, &d) != nil {
			t.Fatalf("the version: %d %s", a.Status, a.Body)
		}
		found := false
		for _, rp := range d.Repositories {
			if strings.HasSuffix(rp.Key, rRemote) {
				found = true
				if rp.Available || rp.UnavailableReason != "notAllowed" {
					t.Errorf("npm-remote's entry %+v, want not available, notAllowed", rp)
				}
			}
		}
		if !found {
			t.Errorf("npm-remote is not among the version's repositories: %s", a.Body)
		}
	})
	t.Run("R-CUR-014 a client's request is refused; an allow serves it without an upstream request", func(t *testing.T) {
		if a := r.get(rep, rRemote, file, r.tokA); a.Status != http.StatusForbidden {
			t.Fatalf("alice's tarball before the allow: %d %.300s, want 403", a.Status, a.Body)
		}
		before := r.stub.Counts().Tarball(p.Name, corpus.TarballFile(p.Name, v))
		key := testenv.DefaultProject + "/" + rRemote
		r.st.MustPutDeliveryRule(rep, r.tokL, key, testenv.VersionRule(p.Name, v, "allow"))
		r.st.WaitDelivered(t, rep, r.tokL, key, p.Name, v, true)
		if a := r.get(rep, rRemote, file, r.tokA); a.Status != http.StatusOK {
			t.Fatalf("alice's tarball after the allow: %d %.300s", a.Status, a.Body)
		}
		if n := r.stub.Counts().Tarball(p.Name, corpus.TarballFile(p.Name, v)); n != before {
			t.Errorf("the upstream was asked %d times for the examined tarball", n-before)
		}
		if n := r.examined(p.Name, v); n != 0 {
			t.Errorf("the allow left %d examination rows of the version, want none", n)
		}
	})
}

// TestR_CUR_015_ARequestIsExaminedAllowedOrDeclined: alice asks for a package; the newest version that is no prerelease is downloaded
// for examination, charged to alice; lead's list shows the entry with alice and, after a client's refused request, the refusal; lead
// allows it: the rule exists, the entry is allowed, a client fetches the examined tarball without an upstream request. A second entry
// (a version) is declined with a reason and its examined tarball is gone; alice reads both states, carol sees none. About 6 s.
func TestR_CUR_015_ARequestIsExaminedAllowedOrDeclined(t *testing.T) {
	r, rep := requestsEnv(t)
	p := upstreamPackage(t, 2)
	o := otherPackage(t, 2, p.Name)
	reqs := downloadsPath + "/delivery-requests"

	t.Run("R-CUR-005 lead, who may set the rules, does not ask; a delivered subject is not asked for", func(t *testing.T) {
		if a := r.st.Do(rep, http.MethodPost, reqs, r.tokL, map[string]any{"name": p.Name}); a.Status != http.StatusForbidden {
			t.Errorf("lead's ask: %d %s, want 403", a.Status, a.Body)
		}
	})

	a := r.st.Do(rep, http.MethodPost, reqs, r.tokA, map[string]any{"name": p.Name, "note": "for the build"})
	if a.Status != http.StatusCreated {
		t.Fatalf("alice's ask for %s: %d %s", p.Name, a.Status, a.Body)
	}
	allowID := a.JSON(t)["id"].(string)
	var examinedVersion string
	t.Run("R-CUR-015 the newest version is downloaded for examination, charged to alice", func(t *testing.T) {
		testenv.WaitFor(t, "the request's examination done", 60*time.Second, func() bool {
			examinedVersion = r.st.Text(`SELECT COALESCE(max(version), '') FROM remote_download WHERE origin = 'request' AND request_id = $1::uuid
				AND state = 'done'`, allowID)
			if examinedVersion == "" {
				r.runDispatch(rep)
			}
			return examinedVersion != ""
		})
		if n := r.examined(p.Name, examinedVersion); n != 1 {
			t.Errorf("%d examination rows of %s@%s, want one", n, p.Name, examinedVersion)
		}
		testenv.WaitFor(t, "alice's background download in the usage", 30*time.Second, func() bool {
			return r.st.Number(`SELECT COALESCE(sum(background_fetches), 0) FROM usage_bucket WHERE granularity = '5m'
				AND subject_kind = 'user' AND subject_ref = $1`, r.alice.ID) == 1
		})
		if a := r.st.Do(rep, http.MethodPost, reqs, r.tokA, map[string]any{"name": p.Name}); a.Status != http.StatusOK {
			t.Errorf("alice's second ask of the same package: %d %s, want 200 unchanged", a.Status, a.Body)
		}
	})
	t.Run("R-CUR-015 lead's list shows the entry with alice and a client's refusal", func(t *testing.T) {
		if a := r.view(rep, rRemote, p.Name, r.tokC); a.Status != http.StatusForbidden {
			t.Fatalf("carol's npm view: %d, want the rules' 403", a.Status)
		}
		var e map[string]any
		testenv.WaitFor(t, "the refusal counted in the entry", 20*time.Second, func() bool {
			e = entryOf(r.requestsList(rep, r.tokL, ""), allowID)
			return e != nil && e["refusals"] == float64(1)
		})
		askers := e["askers"].([]any)
		if e["askerCount"] != float64(1) || len(askers) != 1 || askers[0].(map[string]any)["name"] != "alice" ||
			askers[0].(map[string]any)["note"] != "for the build" || e["examination"] == nil {
			t.Errorf("the entry: %v", e)
		}
	})
	t.Run("R-CUR-015 lead allows it: the rule, the state, the tarball served without an upstream request", func(t *testing.T) {
		before := r.stub.Counts().Tarball(p.Name, corpus.TarballFile(p.Name, examinedVersion))
		a := r.st.Do(rep, http.MethodPost, reqs+"/"+allowID+"/allow", r.tokL, map[string]any{"note": "fine"})
		if a.Status != http.StatusOK {
			t.Fatalf("the allow: %d %s", a.Status, a.Body)
		}
		got := a.JSON(t)
		if got["request"].(map[string]any)["state"] != "allowed" || got["rule"].(map[string]any)["requestId"] != allowID {
			t.Errorf("the answer: %v", got)
		}
		r.st.WaitDelivered(t, rep, r.tokL, testenv.DefaultProject+"/"+rRemote, p.Name, examinedVersion, true)
		if a := r.get(rep, rRemote, remoteTarball(p.Name, examinedVersion), r.tokA); a.Status != http.StatusOK {
			t.Fatalf("alice's tarball after the allow: %d %.300s", a.Status, a.Body)
		}
		if n := r.stub.Counts().Tarball(p.Name, corpus.TarballFile(p.Name, examinedVersion)); n != before {
			t.Errorf("the upstream was asked %d times for the examined tarball", n-before)
		}
		if n := r.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'remote.request.allowed'`); n != 1 {
			t.Errorf("%d events remote.request.allowed, want 1", n)
		}
	})

	v := o.Versions[0].Version
	var declineID string
	t.Run("R-CUR-015 R-CUR-016 a declined entry keeps its reason and its examined tarball is gone", func(t *testing.T) {
		a := r.st.Do(rep, http.MethodPost, reqs, r.tokA, map[string]any{"name": o.Name, "version": v})
		if a.Status != http.StatusCreated {
			t.Fatalf("alice's ask for %s@%s: %d %s", o.Name, v, a.Status, a.Body)
		}
		declineID = a.JSON(t)["id"].(string)
		row, _ := a.JSON(t)["examination"].(map[string]any)
		if row == nil || row["origin"] != "request" {
			t.Fatalf("the ask's examination: %v", a.JSON(t))
		}
		waitRow(t, r, rep, row["id"].(string), "the version's examination done", func(m map[string]any) bool { return m["state"] == "done" })
		if r.itemCount(rRemote, remoteTarball(o.Name, v)) != 1 || r.examined(o.Name, v) != 1 {
			t.Fatal("the examination stored nothing")
		}
		d := r.st.Do(rep, http.MethodPost, reqs+"/"+declineID+"/decline", r.tokL, map[string]any{"reason": "use the fork"})
		if d.Status != http.StatusOK {
			t.Fatalf("the decline: %d %s", d.Status, d.Body)
		}
		if gone := d.JSON(t)["discarded"].(map[string]any); gone["files"] != float64(1) {
			t.Errorf("the decline discarded %v", gone)
		}
		if r.itemCount(rRemote, remoteTarball(o.Name, v)) != 0 || r.examined(o.Name, v) != 0 {
			t.Error("the examined tarball of the declined entry is stored")
		}
		if n := r.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'remote.examination.discarded' AND details->>'cause' = 'declined'`); n != 1 {
			t.Errorf("%d discards of cause declined, want 1", n)
		}
	})
	t.Run("R-CUR-015 alice reads both states; carol sees none", func(t *testing.T) {
		mine := r.requestsList(rep, r.tokA, "")
		allowed, declined := entryOf(mine, allowID), entryOf(mine, declineID)
		if allowed == nil || allowed["state"] != "allowed" || declined == nil || declined["state"] != "declined" || declined["declineReason"] != "use the fork" {
			t.Errorf("alice's list: %v", mine)
		}
		if n := len(r.requestsList(rep, r.tokC, "")); n != 0 {
			t.Errorf("carol sees %d entries, want none", n)
		}
	})
}

// TestR_CUR_016_WhatWasExaminedIsRemovedAfterItsDaysOrADiscard: an examined version older than the repository's days is removed by the
// task remote-examination-cleanup, which records the discard of the cause retention; the row of a version the rules deliver goes
// and its tarball stays; a person's discard removes another examined version and writes the journal's entry. About 3 s.
func TestR_CUR_016_WhatWasExaminedIsRemovedAfterItsDaysOrADiscard(t *testing.T) {
	r, rep := requestsEnv(t)
	p := upstreamPackage(t, 3)
	v1, v2, v3 := p.Versions[0].Version, p.Versions[1].Version, p.Versions[2].Version
	for _, v := range []string{v1, v2, v3} {
		a := r.startDownload(rep, r.tokL, map[string]any{"level": "version", "name": p.Name, "version": v})
		if a.Status != http.StatusAccepted {
			t.Fatalf("lead's download of %s: %d %s", v, a.Status, a.Body)
		}
		waitRow(t, r, rep, a.JSON(t)["download"].(map[string]any)["id"].(string), "the examination of "+v+" done",
			func(m map[string]any) bool { return m["state"] == "done" })
	}
	key := testenv.DefaultProject + "/" + rRemote
	// v2 comes to be delivered: the rule's write removes its examination row (R-CUR-016). The row is written again, as a widening the
	// write does not see would leave it (the mode set to allExceptBlocked), for the task to drop (Q-CUR-142).
	r.st.MustPutDeliveryRule(rep, r.tokL, key, testenv.VersionRule(p.Name, v2, "allow"))
	r.st.WaitDelivered(t, rep, r.tokL, key, p.Name, v2, true)
	if n := r.examined(p.Name, v2); n != 0 {
		t.Fatalf("the allow left %d examination rows of %s", n, v2)
	}
	r.st.Exec(`INSERT INTO remote_examinations (repo_id, name, version) SELECT id, $2, $3 FROM repositories WHERE key = $1`, rRemote, p.Name, v2)
	r.st.Exec(`UPDATE remote_examinations SET fetched_at = now() - interval '31 days' WHERE name = $1 AND version = ANY($2::text[])`,
		p.Name, []string{v1, v2})

	t.Run("R-CUR-016 the task removes the examined version older than its days and keeps the delivered one", func(t *testing.T) {
		r.runTask(rep, "remote-examination-cleanup")
		testenv.WaitFor(t, "the old examination removed", 60*time.Second, func() bool {
			return r.itemCount(rRemote, remoteTarball(p.Name, v1)) == 0 && r.examined(p.Name, v2) == 0
		})
		if r.examined(p.Name, v1) != 0 {
			t.Error("the removed version keeps its examination row")
		}
		if r.itemCount(rRemote, remoteTarball(p.Name, v2)) != 1 {
			t.Error("the delivered version's tarball was removed")
		}
		if r.itemCount(rRemote, remoteTarball(p.Name, v3)) != 1 || r.examined(p.Name, v3) != 1 {
			t.Error("the young examination was removed")
		}
		if n := r.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'remote.examination.discarded' AND details->>'cause' = 'retention'
			AND actor_name = 'examination-cleanup'`); n != 1 {
			t.Errorf("%d discards of the cause retention by system:examination-cleanup, want 1", n)
		}
	})
	t.Run("R-CUR-016 a person's discard removes an examined version and writes the journal's entry", func(t *testing.T) {
		if a := r.st.Do(rep, http.MethodPost, downloadsPath+"/examinations/discard", r.tokA, map[string]any{"name": p.Name, "version": v3}); a.Status != http.StatusForbidden {
			t.Errorf("alice's discard: %d, want 403", a.Status)
		}
		if a := r.st.Do(rep, http.MethodPost, downloadsPath+"/examinations/discard", r.tokL, map[string]any{"name": p.Name, "version": v2}); a.Status != http.StatusConflict ||
			!strings.Contains(string(a.Body), "delivered") {
			t.Errorf("a discard of a delivered version: %d %s, want 409 delivered", a.Status, a.Body)
		}
		a := r.st.Do(rep, http.MethodPost, downloadsPath+"/examinations/discard", r.tokL, map[string]any{"name": p.Name, "version": v3})
		if a.Status != http.StatusOK || a.JSON(t)["files"] != float64(1) {
			t.Fatalf("lead's discard: %d %s", a.Status, a.Body)
		}
		if r.itemCount(rRemote, remoteTarball(p.Name, v3)) != 0 || r.examined(p.Name, v3) != 0 {
			t.Error("the discarded version is stored")
		}
		if n := r.st.Number(`SELECT count(*) FROM revocation_outbox WHERE event = 'remote.examination.discarded'
			AND entry->>'name' = $1 AND entry->>'version' = $2`, p.Name, v3); n != 1 {
			t.Errorf("%d journal entries of the discard, want 1", n)
		}
	})
}
