//go:build integration

package npm

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// A probe through a virtual repository starts the download at the member that serves it (repositories-and-proxy.md R-REPO-065,
// Q-REPO-367): the version document of npm-all, whose members are npm-local and npm-remote, downloads the version's tarball into
// npm-remote, charged to the caller's budget key at npm-all with the usage recorded at npm-remote (usage-and-limits.md Q-LIM-2,
// R-LIM-037); a version the local member holds starts nothing.
func TestQ_REPO_367_TheVersionDocumentOfAVirtualRepositoryDownloadsTheTarballAtTheRemoteMember(t *testing.T) {
	r := newRemoteEnv(t, remoteOptions{Valkey: true, Env: []string{
		"QUADER_LIMITS_FLUSH_INTERVAL=200ms", "QUADER_LIMITS_USAGE_FLUSH_INTERVAL=1s", "QUADER_JOBS_POLL_INTERVAL=200ms",
	}})
	rep := r.st.Start(t, testenv.ReplicaOptions{Name: "all-1", Role: "all", Endpoints: customersEndpoint})
	body, _ := json.Marshal(map[string]any{
		"who": map[string]any{"kind": "user", "user": "alice"}, "profile": "standard", "overrides": map[string]any{}, "note": "",
	})
	if a := r.st.Do(rep, http.MethodPost, "/api/v1/projects/"+testenv.DefaultProject+"/repositories/"+rAll+"/limits/assignments", r.opsPAT, body,
		"Idempotency-Key", "alice-at-npm-all"); a.Status != http.StatusCreated {
		t.Fatalf("the assignment at the virtual repository: %d %s", a.Status, a.Body)
	}
	time.Sleep(1500 * time.Millisecond) // the change area reaches the replica's index
	jobs := func() int64 {
		return r.st.Number(`SELECT count(*) FROM remote_download WHERE origin = 'probe'`)
	}

	p := upstreamPackage(t, 3)
	v := p.Versions[1].Version
	tarball := p.Name + "/-/" + p.Name + "-" + v + ".tgz"
	if a := r.get(rep, rAll, p.Name+"/"+v, r.tokA); a.Status != http.StatusOK {
		t.Fatalf("the version document through %s: %d %.300s", rAll, a.Status, a.Body)
	}
	testenv.WaitFor(t, "the tarball downloaded into the remote member", 60*time.Second, func() bool {
		return r.itemCount(rRemote, tarball) == 1
	})
	if n := r.stub.Counts().Tarball(p.Name, p.Name+"-"+v+".tgz"); n != 1 {
		t.Errorf("the upstream was asked %d times for the tarball, want once", n)
	}
	if n := r.itemCount(rLocal, tarball); n != 0 {
		t.Errorf("the tarball was stored in the local member")
	}

	t.Run("the charge is alice's budget key at npm-all, the usage recorded at npm-remote", func(t *testing.T) {
		alice := r.alice.ID
		id := func(key string) string { return r.st.Text(`SELECT id::text FROM repositories WHERE key = $1`, key) }
		testenv.WaitFor(t, "the usage of the background download", 30*time.Second, func() bool {
			return r.st.Number(`SELECT COALESCE(sum(background_fetches), 0) FROM usage_bucket WHERE granularity = '5m'
				AND subject_kind = 'user' AND subject_ref = $1 AND place = 'repository:' || $2 AND repository_id = $3::bigint`,
				alice, id(rAll), id(rRemote)) == 1
		})
	})

	t.Run("a version the local member holds starts nothing", func(t *testing.T) {
		before := jobs()
		r.publishTo(rep, rLocal, r.tokA, pkg{Name: "@acme/probe", Version: "1.0.0"})
		if a := r.get(rep, rAll, "@acme%2fprobe/1.0.0", r.tokA); a.Status != http.StatusOK {
			t.Fatalf("the local version through %s: %d %.300s", rAll, a.Status, a.Body)
		}
		time.Sleep(3 * time.Second)
		if n := jobs(); n != before {
			t.Errorf("%d background downloads after the probe of a local version, %d before", n, before)
		}
	})
}
