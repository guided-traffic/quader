//go:build integration

package debvirtual

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	debdata "github.com/guided-traffic/quader/internal/formatdata/deb"
	"github.com/guided-traffic/quader/test/debstub"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// ready uploads a local package and waits until the virtual repository lists it next to the stub's paragraphs.
func (e *venv) ready(name string) {
	e.t.Helper()
	if a := e.upload(rLocal, e.admin, name, "1.0-1", "stable"); a.Status != 201 {
		e.t.Fatalf("upload = %d %s", a.Status, a.Body)
	}
	testenv.WaitFor(e.t, "the virtual index to list "+name, 90*time.Second, func() bool {
		if s, ok := e.suite(rApt, "stable"); !ok || s.Generation == 0 {
			return false
		}
		_, found := find(e.packages(rApt, "stable", e.admin), name, "1.0-1")
		return found
	})
}

// rebuild lets the merge TTL pass and enqueues the builds of the virtual repositories that are due, which the task deb-index-refresh does
// every minute.
func (e *venv) rebuild() {
	e.t.Helper()
	time.Sleep(1300 * time.Millisecond)
	if err := e.r.App.DebIndexer().EnqueueVirtualsDue(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

// R-DEB-027, Q-DEB-19: the generated files of a virtual repository need read on every member; the package files follow the read on the
// member they come from.
func TestR_DEB_027_TheGateAndTheMemberPaths(t *testing.T) {
	e := newEnv(t, options{})
	e.ready("local-tool")
	idx := "dists/stable/main/binary-amd64/Packages.gz"

	for _, p := range []string{"dists/stable/InRelease", idx, "signing-key.asc"} {
		if a := e.get(rApt, p, e.alice); a.Status != 200 {
			t.Errorf("%s with read on every member: %d", p, a.Status)
		}
		for name, tok := range map[string]string{"bob (no read on the remote member)": e.bob, "carol (no read on any member)": e.carol} {
			if a := e.get(rApt, p, tok); a.Status != 403 || codeOf(a) != "deb_members_unreadable" {
				t.Errorf("%s as %s: %d %s", p, name, a.Status, codeOf(a))
			}
		}
		if a := e.get(rApt, p, ""); a.Status != 401 {
			t.Errorf("%s anonymous: %d", p, a.Status)
		}
	}

	list := e.packages(rApt, "stable", e.admin)
	local, _ := find(list, "local-tool", "")
	hello, _ := find(list, "quader-hello", "")

	a := e.get(rApt, local.Value("Filename"), e.alice)
	if a.Status != 200 || a.Header.Get("Quader-Resolved-From") != rLocal {
		t.Errorf("the local member's file: %d from %q", a.Status, a.Header.Get("Quader-Resolved-From"))
	}
	e.stub.Reset()
	a = e.get(rApt, hello.Value("Filename"), e.alice)
	if a.Status != 200 || a.Header.Get("Quader-Resolved-From") != rRemote {
		t.Fatalf("the remote member's file: %d from %q %s", a.Status, a.Header.Get("Quader-Resolved-From"), a.Body)
	}
	// Only the package files are counted: a build of a virtual repository revalidates its members' releases in the background at any
	// time (a 304 of apt-bulk's InRelease fell between the two reads once), which is not a read of the file.
	if n := e.stub.Counts().OfKind(debstub.ClassPool, ""); n == 0 {
		t.Error("the first read of the remote member's file did not reach the upstream")
	}
	e.stub.Reset()
	if a = e.get(rApt, hello.Value("Filename"), e.alice); a.Status != 200 {
		t.Fatalf("the second read = %d", a.Status)
	}
	if n := e.stub.Counts().OfKind(debstub.ClassPool, ""); n != 0 {
		t.Errorf("the second read reached the upstream %d times: %+v", n, e.stub.Counts().Entries)
	}
	// A member who is no member, and a path that is no member path.
	if a = e.get(rApt, "members/other/pool/x/x/x_1_amd64.deb", e.alice); a.Status != 404 {
		t.Errorf("a non-member: %d", a.Status)
	}
	if a = e.get(rApt, "members/"+rLocal+"/dists/stable/InRelease", e.alice); a.Status != 404 {
		t.Errorf("a member path outside pool/: %d", a.Status)
	}
	// bob reads the local member and not the remote one: the local file is his, the remote file is a 404 (R-AUTHZ-016).
	if a = e.get(rApt, local.Value("Filename"), e.bob); a.Status != 200 {
		t.Errorf("bob, local member file: %d", a.Status)
	}
	if a = e.get(rApt, hello.Value("Filename"), e.bob); a.Status != 404 && a.Status != 403 {
		t.Errorf("bob, remote member file: %d", a.Status)
	}
	// pool/ by its own path resolves through the tiers: the local member first.
	a = e.get(rApt, "pool/l/local-tool/local-tool_1.0-1_amd64.deb", e.alice)
	if a.Status != 200 || a.Header.Get("Quader-Resolved-From") != rLocal {
		t.Errorf("pool/ of a local file: %d from %q", a.Status, a.Header.Get("Quader-Resolved-From"))
	}
	// The generated files are not writable through the virtual repository.
	if a = e.st.Do(e.r, http.MethodPut, e.url(rApt, idx), e.admin, []byte("x")); a.Status/100 == 2 {
		t.Errorf("a PUT of an index: %d", a.Status)
	}
}

// R-DEB-028: a package uploaded to the virtual repository goes to its deployment target and appears in the merged index; a virtual
// repository without a target refuses with no_deploy_target.
func TestR_DEB_028_UploadsGoToTheDeploymentTarget(t *testing.T) {
	e := newEnv(t, options{})
	e.ready("first-tool")
	if a := e.upload(rApt, e.alice, "second-tool", "2.0-1", "stable"); a.Status != 201 {
		t.Fatalf("upload through the virtual repository = %d %s", a.Status, a.Body)
	}
	if a := e.get(rLocal, "pool/s/second-tool/second-tool_2.0-1_amd64.deb", e.alice); a.Status != 200 {
		t.Errorf("the file is not in the deployment target: %d", a.Status)
	}
	testenv.WaitFor(t, "the virtual index to list the upload", 60*time.Second, func() bool {
		_, ok := find(e.packages(rApt, "stable", e.admin), "second-tool", "2.0-1")
		return ok
	})
	// Without the deploy action on the target, the upload is refused.
	if a := e.upload(rApt, e.bob, "third-tool", "1.0-1", "stable"); a.Status != 403 && a.Status != 404 {
		t.Errorf("an upload by a reader: %d", a.Status)
	}
	a := e.upload(rNoDeploy, e.admin, "fourth-tool", "1.0-1", "stable")
	if a.Status != 405 && a.Status != 403 {
		t.Errorf("an upload to a virtual repository without a target: %d %s", a.Status, a.Body)
	}
}

// Q-DEB-18, R-DEB-025: the virtual repository's patterns and the members' claims decide which member a name is merged from. A name that
// the local member claims leaves the remote member's paragraphs out of the merged index.
func TestR_DEB_025_AClaimKeepsTheNameOfTheOtherMembersOut(t *testing.T) {
	e := newEnv(t, options{mergeTTL: 1})
	e.ready("local-tool")
	if _, ok := find(e.packages(rApt, "stable", e.admin), "quader-hello", ""); !ok {
		t.Fatal("the stub's quader-hello is not listed before the claim")
	}
	gen := mustSuite(t, e, rApt).Generation
	claims := "/api/v1/projects/" + project + "/repositories/" + rLocal + "/claims"
	a := e.st.Do(e.r, http.MethodPut, claims, e.admin,
		map[string]any{"claims": []map[string]string{{"pattern": "quader-hello", "scope": "members"}}}, "If-Match", "*")
	if a.Status/100 != 2 {
		t.Fatalf("claim = %d %s", a.Status, a.Body)
	}
	e.rebuild()
	e.published(rApt, "stable", 90*time.Second, func(s suiteState) bool { return s.Generation > gen })
	list := e.packages(rApt, "stable", e.admin)
	if _, ok := find(list, "quader-hello", ""); ok {
		t.Error("quader-hello is still listed although deb-local claims it")
	}
	if _, ok := find(list, "quader-tools", ""); !ok {
		t.Error("a name nobody claims left the index")
	}
}

func mustSuite(t *testing.T, e *venv, repo string) suiteState {
	t.Helper()
	s, ok := e.suite(repo, "stable")
	if !ok {
		t.Fatal("no suite stable in " + repo)
	}
	return s
}

// R-DEB-029, Q-DEB-34: a local member of a virtual repository that also has a remote member refuses an upload of a name the remote member
// lists, with 403 shadows_remote_package, and takes a name it does not list.
func TestR_DEB_029_ALocalMemberCannotShadowTheNamesOfItsRemoteMember(t *testing.T) {
	e := newEnv(t, options{})
	e.ready("seed-tool")
	// The remote member's index rows exist once its index is fetched through the member path.
	if a := e.get(rRemote, "dists/stable/main/binary-amd64/Packages.gz", e.admin); a.Status != 200 {
		t.Fatalf("the remote member's index = %d", a.Status)
	}
	var last testenv.Answer
	testenv.WaitFor(t, "the shadowing check to answer", 90*time.Second, func() bool {
		last = e.upload(rLocal, e.alice, "quader-hello", "9.9-1", "stable")
		return codeOf(last) != "shadow_check_unavailable"
	})
	if last.Status != 403 || codeOf(last) != "shadows_remote_package" {
		t.Errorf("an upload of a name the remote member lists = %d %s", last.Status, last.Body)
	}
	if a := e.upload(rLocal, e.alice, "only-local", "1.0-1", "stable"); a.Status != 201 {
		t.Errorf("an upload of a name only the local member has = %d %s", a.Status, a.Body)
	}
}

// mark is the database clock, read after a change has answered. The change enqueued the build that answers it in its own transaction
// (Q-DEB-134): as a new job, or by moving the build that waited under the repository's key to now (the coalesce rule of Q-ARCH-384), so
// that job was created before the mark, and it starts only after the change committed.
func (e *venv) mark() string {
	e.t.Helper()
	return e.st.Text(`SELECT clock_timestamp()::text`)
}

// buildsEnded waits until every build of the repository that was enqueued before mark has ended: the change's own build among them,
// and a build that started before the change committed, which read the state before it and whose result is not the change's. A build
// enqueued later (the periodic task's) started after the change and decides the same. The wait is for the jobs and not for a state
// that a fixed period is guessed to reach: a loaded runner makes the build late, never wrong, and the bound is the hang of a job that
// never ends. What the build decided is asserted afterwards, at once.
func (e *venv) buildsEnded(repo, mark string) {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Minute)
	for e.st.Number(`SELECT count(*) FROM job
		 WHERE kind = $1 AND dedup_key = 'index:' || (SELECT id::text FROM repositories WHERE key = $2)
		   AND created_at < $3::timestamptz AND state IN ('queued', 'running', 'cancelling')`, debdata.IndexJobKind, repo, mark) > 0 {
		if time.Now().After(deadline) {
			e.t.Fatalf("the builds of %s enqueued before %s did not end within 5 minutes:\n%s", repo, mark, e.builds(repo))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// builds lists the index builds of a repository with their times, for the message of a failure.
func (e *venv) builds(repo string) string {
	e.t.Helper()
	return e.st.Text(`SELECT COALESCE(string_agg(format('%s %s attempt %s created %s run_at %s started %s finished %s %s', id, state, attempt,
			created_at, run_at, COALESCE(started_at::text, '-'), COALESCE(finished_at::text, '-'), COALESCE(error::text, '')), E'\n'
			ORDER BY created_at, id), '(none)')
		  FROM job WHERE kind = $1 AND dedup_key = 'index:' || (SELECT id::text FROM repositories WHERE key = $2)`, debdata.IndexJobKind, repo)
}

// failedWith waits until the builds enqueued before mark, the change's among them, have ended, then checks that the suite is failed for
// the reason and that the last generation stays published.
func (e *venv) failedWith(reason string, before suiteState, mark string) {
	e.t.Helper()
	e.buildsEnded(rApt, mark)
	s := mustSuite(e.t, e, rApt)
	if s.State != "failed" || !strings.HasPrefix(s.Reason, reason) {
		e.t.Fatalf("the build after the change at %s left the suite %s with reason %q, want failed with %s; the builds of %s:\n%s",
			mark, s.State, s.Reason, reason, rApt, e.builds(rApt))
	}
	if s.Generation != before.Generation {
		e.t.Errorf("the generation moved from %d to %d", before.Generation, s.Generation)
	}
	if _, ok := find(e.packages(rApt, "stable", e.admin), "local-tool", "1.0-1"); !ok {
		e.t.Error("the last published index is gone")
	}
}

// R-DEB-025, R-DEB-020: a remote member whose stored release no current key verifies is not merged. The keys of the member are changed to
// one that did not sign the release, which takes the verification of the stored release away; the change enqueues the build at once.
func TestR_DEB_025_AReleaseNoCurrentKeyVerifiesIsNotMerged(t *testing.T) {
	e := newEnv(t, options{})
	e.ready("local-tool")
	before := mustSuite(t, e, rApt)
	ring, err := e.stub.Keyring("b")
	if err != nil {
		t.Fatal(err)
	}
	a := e.st.Do(e.r, http.MethodPatch, "/api/v1/projects/"+project+"/repositories/"+rRemote, e.admin,
		map[string]any{"deb": map[string]any{"upstreamKeys": string(ring)}}, "Content-Type", "application/merge-patch+json", "If-Match", "*")
	if a.Status != 200 {
		t.Fatalf("change the keys = %d %s", a.Status, a.Body)
	}
	e.failedWith("member_unverified", before, e.mark())
}

// R-DEB-025, Q-DEB-43: a release older than the Date the last build recorded is a rollback and is not merged. The stored releases of the
// remote member are purged, which takes the floor of its own anti-rollback check away, so that the older release is stored and only the
// record of the virtual build catches it.
func TestR_DEB_025_AReleaseOlderThanTheRecordedDateIsNotMerged(t *testing.T) {
	e := newEnv(t, options{remoteTTL: 1, mergeTTL: 1})
	e.ready("local-tool")
	if _, err := e.stub.NewGeneration("stable"); err != nil {
		t.Fatal(err)
	}
	first := mustSuite(t, e, rApt)
	e.rebuild()
	before := e.published(rApt, "stable", 90*time.Second, func(s suiteState) bool { return s.Generation > first.Generation })
	if err := e.stub.SetMode(debstub.Mode{Date: debstub.DateOlder}); err != nil {
		t.Fatal(err)
	}
	e.st.Exec(`DELETE FROM deb_remote_releases WHERE repo_id = (SELECT id FROM repositories WHERE key = $1)`, rRemote)
	e.rebuild()
	e.failedWith("member_rollback", before, e.mark())
}

// R-DEB-025: the earliest Valid-Until of the members' releases is the one of the merged release.
func TestR_DEB_025_TheEarliestValidUntilIsCarriedOver(t *testing.T) {
	e := newEnv(t, options{remoteTTL: 1, mergeTTL: 1})
	e.ready("local-tool")
	before := mustSuite(t, e, rApt)
	if err := e.stub.SetMode(debstub.Mode{ValidUntil: debstub.ValidUntilSixDays}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.stub.NewGeneration("stable"); err != nil {
		t.Fatal(err)
	}
	e.rebuild()
	e.published(rApt, "stable", 90*time.Second, func(s suiteState) bool { return s.Generation > before.Generation })
	rel := e.verifies(rApt, "stable")
	v, ok := rel.Get("Valid-Until")
	if !ok {
		t.Fatalf("the merged release has no Valid-Until: %v", rel.Fields)
	}
	until, err := time.Parse("Mon, 02 Jan 2006 15:04:05 MST", v)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(until); d < 5*24*time.Hour || d > 7*24*time.Hour {
		t.Errorf("Valid-Until %s is %s away, want about six days", v, d)
	}
}

// R-DEB-025: a suite the members do not have contributes nothing and is no failure; the inputs that did not change publish nothing new.
func TestR_DEB_025_UnchangedInputsPublishNothingNew(t *testing.T) {
	e := newEnv(t, options{mergeTTL: 1})
	e.ready("local-tool")
	first := mustSuite(t, e, rApt)
	e.rebuild()
	time.Sleep(4 * time.Second)
	again := mustSuite(t, e, rApt)
	if again.Generation != first.Generation || again.State == "failed" {
		t.Errorf("a check of unchanged inputs: generation %d -> %d, state %s %s", first.Generation, again.Generation, again.State, again.Reason)
	}
}
