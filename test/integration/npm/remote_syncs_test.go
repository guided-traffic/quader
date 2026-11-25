//go:build integration

package npm

import (
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/npmstub/corpus"
)

// Downloads kept in sync (remote-curation.md R-CUR-027) against the integration stack and the counting npm upstream: a sync of the
// newest two versions of a package runs at once, runs again when it is due and queues only what is new and delivered, a run without
// news reads the package's document once and fetches nothing, and who may set one up and how many. The test makes the sync due by
// moving its next_run_at and runs the task remote-downloads-dispatch through the system tasks' API instead of waiting for the interval.
// Data: three tarballs of the stub's corpus and one the stub publishes, a few hundred bytes each.

const syncsPath = downloadsPath + "/download-syncs"

// syncRun waits until the sync's newest run is another than not and has ended with every row done, running the dispatch meanwhile;
// it answers that run.
func syncRun(t *testing.T, r *renv, rep *testenv.Replica, syncID, not string) map[string]any {
	t.Helper()
	var run map[string]any
	testenv.WaitFor(t, "the sync's run ended", 60*time.Second, func() bool {
		page := r.st.Do(rep, http.MethodGet, syncsPath, r.tokL, nil).JSON(t)
		items, _ := page["items"].([]any)
		for _, it := range items {
			sy := it.(map[string]any)
			if sy["id"] != syncID {
				continue
			}
			run, _ = sy["lastBulk"].(map[string]any)
		}
		if run == nil || run["id"] == not {
			r.runDispatch(rep)
			return false
		}
		rows, _ := run["rows"].(map[string]any)
		active := rows["waiting"] != nil || rows["running"] != nil
		if run["state"] == "expanding" || run["state"] == "queued" || active {
			r.runDispatch(rep)
			return false
		}
		return true
	})
	return run
}

func TestR_CUR_027_ASyncRunsAgainAndStoresWhatIsNewAndDelivered(t *testing.T) {
	r, rep := downloadsEnv(t)
	p := upstreamPackage(t, 3)
	key := testenv.DefaultProject + "/" + rRemote
	tarball := func(v string) string { return p.Name + "/-/" + corpus.TarballFile(p.Name, v) }

	a := r.st.Do(rep, http.MethodPost, syncsPath, r.tokL, map[string]any{"level": "package", "name": p.Name, "maxVersions": 2})
	if a.Status != http.StatusCreated {
		t.Fatalf("lead's sync: %d %s", a.Status, a.Body)
	}
	created := a.JSON(t)
	sy := created["sync"].(map[string]any)
	first := created["bulk"].(map[string]any)
	syncID := sy["id"].(string)
	if first["origin"] != "sync" || first["sync"] != syncID || sy["state"] != "active" || sy["nextRunAt"] == nil {
		t.Fatalf("the sync and its first run: %s", a.Body)
	}

	t.Run("the first run stores the newest two", func(t *testing.T) {
		run := syncRun(t, r, rep, syncID, "")
		if run["id"] != first["id"] || run["versionsQueued"] != float64(2) {
			t.Fatalf("the first run: %v", run)
		}
		for _, v := range p.Versions[len(p.Versions)-2:] {
			if n := r.itemCount(rRemote, tarball(v.Version)); n != 1 {
				t.Errorf("%s is stored %d times, want once", v.Version, n)
			}
		}
		if n := r.st.Number(`SELECT count(*) FROM remote_download WHERE origin = 'sync' AND bulk_id = $1::uuid`, run["id"]); n != 2 {
			t.Errorf("%d rows of the origin sync, want 2", n)
		}
	})

	t.Run("the next run queues only the new version the rules deliver", func(t *testing.T) {
		for _, v := range []string{"99.0.0", "99.1.0"} {
			if err := r.stub.Publish(p.Name, v, nil); err != nil {
				t.Fatal(err)
			}
		}
		r.st.MustPutDeliveryRule(rep, r.tokL, key, testenv.VersionRule(p.Name, "99.1.0", "block"))
		r.st.WaitDelivered(t, rep, r.tokL, key, p.Name, "99.1.0", false)
		blocked := r.stub.Counts().Tarball(p.Name, corpus.TarballFile(p.Name, "99.1.0"))
		r.st.Exec(`UPDATE remote_download_syncs SET next_run_at = now()`)
		run := syncRun(t, r, rep, syncID, first["id"].(string))
		if run["versionsFound"] != float64(2) || run["versionsQueued"] != float64(1) || run["versionsRefused"] != float64(1) || run["startedBy"] != nil {
			t.Fatalf("the second run: %v, want two found, the new one queued, the blocked one refused, started by the system", run)
		}
		if n := r.itemCount(rRemote, tarball("99.0.0")); n != 1 {
			t.Errorf("the new version is stored %d times", n)
		}
		if n := r.stub.Counts().Tarball(p.Name, corpus.TarballFile(p.Name, "99.1.0")); n != blocked {
			t.Errorf("the blocked version's tarball was asked %d times", n-blocked)
		}
		if n := r.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'remote.download.started' AND actor_type = 'system' AND actor_name = 'download-sync'`); n != 1 {
			t.Errorf("%d events of the dispatch's run, want one with the actor system:download-sync", n)
		}
	})

	// The cost of a run that finds nothing new (R-CUR-027, Q-CUR-244): one read of the package's document for the expansion, no
	// version's document and no tarball.
	t.Run("a run without news reads the package's document once and fetches nothing", func(t *testing.T) {
		before := r.stub.Counts()
		docs, tarballs := before.Documents(p.Name), before.Tarballs(p.Name)
		prev := r.st.Do(rep, http.MethodGet, syncsPath, r.tokL, nil).JSON(t)["items"].([]any)[0].(map[string]any)["lastBulk"].(map[string]any)
		r.st.Exec(`UPDATE remote_download_syncs SET next_run_at = now()`)
		run := syncRun(t, r, rep, syncID, prev["id"].(string))
		after := r.stub.Counts()
		if run["versionsQueued"] != float64(0) || after.Documents(p.Name)-docs != 1 || after.Tarballs(p.Name) != tarballs {
			t.Fatalf("a run without news: %v; %d document reads, %d tarballs; want none queued, one read, no tarball",
				run, after.Documents(p.Name)-docs, after.Tarballs(p.Name)-tarballs)
		}
		t.Logf("a run without news asked the upstream: %d document request(s) of %s, %d of them conditional, %d tarball(s), %d request(s) in all",
			after.Documents(p.Name)-docs, p.Name, after.ConditionalDocuments(p.Name)-before.ConditionalDocuments(p.Name),
			after.Tarballs(p.Name)-tarballs, after.Total()-before.Total())
	})

	// Who and how many (R-CUR-005, Q-CUR-241, Q-CUR-248): a reader is refused, one sync per subject, at most
	// proxy.downloads.max_syncs_per_repository (2 in this environment).
	t.Run("who sets up a sync and how many", func(t *testing.T) {
		body := map[string]any{"level": "package", "name": p.Name}
		if a := r.st.Do(rep, http.MethodPost, syncsPath, r.tokA, body); a.Status != http.StatusForbidden {
			t.Errorf("a reader's sync: %d %s, want 403", a.Status, a.Body)
		}
		if a := r.st.Do(rep, http.MethodPost, syncsPath, r.tokL, body); a.Status != http.StatusConflict || a.JSON(t)["sync"] != syncID {
			t.Errorf("a second sync of the subject: %d %s, want 409 naming the first", a.Status, a.Body)
		}
		if a := r.st.Do(rep, http.MethodPost, syncsPath, r.tokL, map[string]any{"level": "version", "name": p.Name, "version": "99.0.0"}); a.Status != http.StatusUnprocessableEntity {
			t.Errorf("a sync of one version: %d %s, want 422 at /level", a.Status, a.Body)
		}
		if a := r.st.Do(rep, http.MethodPost, syncsPath, r.tokL, map[string]any{"level": "package", "name": "acme-other"}); a.Status != http.StatusCreated {
			t.Fatalf("the second sync: %d %s", a.Status, a.Body)
		}
		if a := r.st.Do(rep, http.MethodPost, syncsPath, r.tokL, map[string]any{"level": "package", "name": "acme-third"}); a.Status != http.StatusUnprocessableEntity ||
			a.JSON(t)["code"] != "too_many_syncs" {
			t.Errorf("the third sync of max 2: %d %s, want 422 too_many_syncs", a.Status, a.Body)
		}
		if a := r.st.Do(rep, http.MethodPost, syncsPath+"/"+syncID+"/pause", r.tokA, nil); a.Status != http.StatusForbidden {
			t.Errorf("a reader's pause: %d %s", a.Status, a.Body)
		}
		if a := r.st.Do(rep, http.MethodPost, syncsPath+"/"+syncID+"/pause", r.tokL, nil); a.Status != http.StatusOK || a.JSON(t)["state"] != "paused" {
			t.Errorf("lead's pause: %d %s", a.Status, a.Body)
		}
		if a := r.st.Do(rep, http.MethodDelete, syncsPath+"/"+syncID, r.tokL, nil); a.Status != http.StatusNoContent {
			t.Errorf("lead's removal: %d %s", a.Status, a.Body)
		}
		if n := r.st.Number(`SELECT count(*) FROM remote_download_bulks WHERE origin = 'sync' AND sync_id IS NULL`); n != 3 {
			t.Errorf("%d runs of the removed sync stay as bulks, want 3", n)
		}
	})
}
