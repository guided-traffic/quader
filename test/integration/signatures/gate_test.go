//go:build integration

package signatures

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// The gate per repository, stage 3, package 1 against the stack (signatures.md R-SIG-061 to R-SIG-069): the member signatures.onlyVerified
// and signatures.anchors with who may change it, and the gate at the serve point of /files/ with its answers, a block that acts at once on
// another replica before any trust row changed, and the bounded wait while the checks are held. The gate is the same for every format;
// the formats' classification is the kit's (check 18) and the answers' shapes the kit's check 17. Signatures are made in the test with a
// release key of its own (R-SIG-042).

// gateEnv is trustEnv with a second web replica and a short wait: web1 and web2 serve, worker1 runs the jobs.
type gateEnv struct {
	*trustEnv
	r2     *testenv.Replica
	worker *testenv.Replica
	env    []string
}

func newGateEnv(t *testing.T) *gateEnv {
	t.Helper()
	st := testenv.NewStack(t, testenv.StackOptions{Name: "signatures"})
	rita, olga, owen := st.User("rita"), st.User("olga"), st.User("owen")
	st.Bind("admin", rita)
	st.Bind("auditor", olga)
	st.Repository("web/files-internal", "local", "generic", `{}`)
	st.Repository("web/files-open", "local", "generic", `{}`)
	st.Member("web", owen, "owner")
	e := &gateEnv{trustEnv: &trustEnv{t: t, st: st}}
	e.tokRita = st.Token(rita, scope.Scope{Inherit: true, Admin: true})
	e.tokOlga = st.Token(olga, scope.Scope{Inherit: true, System: []string{"settings:read", "content:metadata-read"}})
	e.tokOwen = st.Token(owen, scope.Scope{Inherit: true, Admin: true})
	e.env = []string{
		"QUADER_API_RATE_LIMITS_ENABLED=false", "QUADER_JOBS_POLL_INTERVAL=200ms", "QUADER_CLUSTER_EPOCH_POLL_INTERVAL=500ms",
		"QUADER_SIGNATURES_GATE_DOWNLOAD_WAIT=2s",
	}
	e.r = st.Start(t, testenv.ReplicaOptions{Name: "web1", Env: e.env})
	e.r2 = st.Start(t, testenv.ReplicaOptions{Name: "web2", Env: e.env})
	e.worker = st.Start(t, testenv.ReplicaOptions{Name: "worker1", Role: "worker", Env: e.env})
	return e
}

// patchRepo sends a merge patch of a repository document under its current ETag.
func (e *gateEnv) patchRepo(token, repo, body string) testenv.Answer {
	e.t.Helper()
	path := "/api/v1/projects/web/repositories/" + repo
	cur := e.st.Do(e.r, "GET", path, token, nil)
	return e.st.Do(e.r, "PATCH", path, token, body, "Content-Type", "application/merge-patch+json", "If-Match", cur.Header.Get("ETag"))
}

func wantStatus(t *testing.T, a testenv.Answer, status int, code string) {
	t.Helper()
	if a.Status != status || (code != "" && !strings.Contains(string(a.Body), code)) {
		t.Fatalf("%d %s, want %d %s", a.Status, a.Body, status, code)
	}
}

// anchorRelease adds the release key as an anchor of the installation and answers its reference.
func (e *gateEnv) anchorRelease(k releaseKey) string {
	e.t.Helper()
	e.call("POST", "/api/v1/signatures/anchors", e.tokRita, map[string]any{
		"key": "release-key", "title": "Release key", "kind": "openpgp", "openpgp": map[string]string{"keys": string(k.public)},
	}, http.StatusCreated)
	return "installation/release-key"
}

// download is a GET (or HEAD) of /files/ on a replica.
func (e *gateEnv) download(r *testenv.Replica, method, path string, hdr ...string) testenv.Answer {
	e.t.Helper()
	return e.st.Do(r, method, "/files/web/"+path, e.tokRita, nil, hdr...)
}

// eventually repeats a download until it answers the status, at most 10 s: a change reaches every replica within the propagation bound.
func (e *gateEnv) eventually(r *testenv.Replica, path string, status int) testenv.Answer {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var a testenv.Answer
	for time.Now().Before(deadline) {
		if a = e.download(r, "GET", path); a.Status == status {
			return a
		}
		time.Sleep(200 * time.Millisecond)
	}
	e.t.Fatalf("GET %s: %d %s, want %d", path, a.Status, a.Body, status)
	return a
}

// TestR_SIG_061_TheGateIsARepositorySettingAnOwnerMayNarrow: the Owner of web switches the gate on for files-internal (200,
// repository.narrowed with the part signatures, journaled), narrows it to the release key's anchor, adds a reference (403
// signature_weakening_refused) and switches it off (403); an administrator adds the reference (200, block: lifted); a reference to an
// anchor that does not exist in the scope answers 422 invalid_value at its index (R-SIG-061, Q-SIG-77). Integration, about 2 s.
func TestR_SIG_061_TheGateIsARepositorySettingAnOwnerMayNarrow(t *testing.T) {
	e := newGateEnv(t)
	k := newReleaseKey(t)
	ref := e.anchorRelease(k)
	e.call("POST", "/api/v1/signatures/anchors", e.tokRita, map[string]any{
		"key": "second-key", "title": "Second key", "kind": "openpgp", "openpgp": map[string]string{"keys": string(newReleaseKey(t).public)},
	}, http.StatusCreated)

	on := e.patchRepo(e.tokOwen, "files-internal", `{"signatures":{"onlyVerified":true}}`)
	wantStatus(t, on, 200, `"onlyVerified":true`)
	if n := e.st.Number(`SELECT count(*) FROM revocation_outbox WHERE event = 'repository.narrowed' AND entry->'signatures'->'after'->>'onlyVerified' = 'true'`); n != 1 {
		t.Fatalf("%d narrowing entries", n)
	}
	wantStatus(t, e.patchRepo(e.tokOwen, "files-internal", `{"signatures":{"onlyVerified":true,"anchors":["`+ref+`"]}}`), 200, ref)
	wantStatus(t, e.patchRepo(e.tokOwen, "files-internal", `{"signatures":{"onlyVerified":true,"anchors":["`+ref+`","installation/second-key"]}}`),
		403, "signature_weakening_refused")
	wantStatus(t, e.patchRepo(e.tokOwen, "files-internal", `{"signatures":{"onlyVerified":false}}`), 403, "signature_weakening_refused")
	wantStatus(t, e.patchRepo(e.tokRita, "files-internal", `{"signatures":{"onlyVerified":true,"anchors":["`+ref+`","installation/second-key"]}}`),
		200, "second-key")
	if n := e.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'repository.updated' AND details->>'block' = 'lifted' AND details->'weakened' ? 'signatures'`); n != 1 {
		t.Fatalf("%d lifted events", n)
	}
	bad := e.patchRepo(e.tokRita, "files-internal", `{"signatures":{"onlyVerified":true,"anchors":["`+ref+`","project/other-team"]}}`)
	wantStatus(t, bad, 422, "/signatures/anchors/1")
}

// TestR_SIG_064_TheGateAtTheServePointOfFiles: with the gate on files-internal and the release key as an anchor of the installation, an
// unsigned file answers 403 signature_not_verified (GET, HEAD and If-None-Match with its ETag) with the reason missing, no-store and the
// address of its page, and a refusal is listed once with its count; a file with a valid .asc of the release key is served, and its .asc
// too; files-open without the gate serves both (R-SIG-064, R-SIG-068, R-SIG-073's table). A block of the key's fingerprint through web1
// refuses the signed file on web2 before a trust row changed, the worker held (R-SIG-066). While the checks are held, a new signed file
// waits download_wait (2 s) and answers 503 signature_check_pending with Retry-After 30; once a worker runs it is served (R-SIG-067).
// Integration, about 9 s, files of a kilobyte.
func TestR_SIG_064_TheGateAtTheServePointOfFiles(t *testing.T) {
	e := newGateEnv(t)
	k := newReleaseKey(t)
	e.anchorRelease(k)
	signed, unsigned := make([]byte, 1024), make([]byte, 1024)
	signed[0], unsigned[0] = 1, 2
	for _, repo := range []string{"files-internal", "files-open"} {
		e.put(e.tokRita, "web/"+repo+"/tools/signed.bin", signed)
		e.put(e.tokRita, "web/"+repo+"/tools/signed.bin.asc", k.sign(t, signed))
		e.put(e.tokRita, "web/"+repo+"/tools/unsigned.bin", unsigned)
	}
	e.waitFor("the trust of the signed file", installationTrust, "internal", "files-internal", "tools/signed.bin")
	wantStatus(t, e.patchRepo(e.tokRita, "files-internal", `{"signatures":{"onlyVerified":true}}`), 200, "")

	t.Run("R-SIG-064 R-SIG-068 an unsigned file is refused for GET, HEAD and a conditional request", func(t *testing.T) {
		a := e.eventually(e.r, "files-internal/tools/unsigned.bin", http.StatusForbidden)
		p := a.JSON(t)
		if p["code"] != "signature_not_verified" || p["reason"] != "missing" || !strings.Contains(a.Header.Get("Cache-Control"), "no-store") ||
			!strings.Contains(p["detail"].(string), "web/files-internal") || !strings.Contains(p["detail"].(string), "/ui/projects/web/repositories/files-internal/files/tools/unsigned.bin") {
			t.Fatalf("%v %v", a.Header, p)
		}
		etag := e.download(e.r, "GET", "files-open/tools/unsigned.bin").Header.Get("ETag")
		wantStatus(t, e.download(e.r, "HEAD", "files-internal/tools/unsigned.bin"), 403, "")
		wantStatus(t, e.download(e.r, "GET", "files-internal/tools/unsigned.bin", "If-None-Match", etag), 403, "")
		if got := e.st.Text(`SELECT reason || ' ' || count::text FROM signature_refusals`); got != "missing 3" {
			t.Fatalf("the refused download listed %q, want missing 3", got)
		}
		// The event is recorded asynchronously, once a minute per repository, path and client address.
		e.waitFor("the refusal's event", `SELECT count(*)::text FROM audit_event WHERE action = 'signature.download_refused'`, "1")
	})
	t.Run("R-SIG-064 a signed file and its signature are served; an ungated repository serves everything", func(t *testing.T) {
		wantStatus(t, e.download(e.r, "GET", "files-internal/tools/signed.bin"), 200, "")
		wantStatus(t, e.download(e.r, "GET", "files-internal/tools/signed.bin.asc"), 200, "")
		for _, p := range []string{"tools/signed.bin", "tools/unsigned.bin"} {
			wantStatus(t, e.download(e.r, "GET", "files-open/"+p), 200, "")
		}
	})
	t.Run("R-SIG-066 a block acts at once on another replica, before a trust row changed", func(t *testing.T) {
		wantStatus(t, e.download(e.r2, "GET", "files-internal/tools/signed.bin"), 200, "")
		if err := e.worker.Stop(); err != nil {
			t.Fatal(err)
		}
		e.call("POST", "/api/v1/signatures/blocks", e.tokRita, map[string]string{"kind": "fingerprint", "value": k.fp, "reason": "stolen"},
			http.StatusCreated)
		a := e.eventually(e.r2, "files-internal/tools/signed.bin", http.StatusForbidden)
		if p := a.JSON(t); p["reason"] != "blocked" {
			t.Fatalf("%v", p)
		}
		if got := e.st.Text(installationTrust, "files-internal", "tools/signed.bin"); got != "internal" {
			t.Fatalf("the trust row changed (%s): the worker ran", got)
		}
	})
	t.Run("R-SIG-067 a pending check waits download_wait, answers 503, and is served once the check ran", func(t *testing.T) {
		fresh := []byte("fresh file")
		e.put(e.tokRita, "web/files-internal/tools/fresh.bin", fresh)
		e.put(e.tokRita, "web/files-internal/tools/fresh.bin.asc", newReleaseKey(t).sign(t, fresh))
		started := time.Now()
		a := e.download(e.r, "GET", "files-internal/tools/fresh.bin")
		took := time.Since(started)
		if a.Status != http.StatusServiceUnavailable || !strings.Contains(string(a.Body), "signature_check_pending") ||
			a.Header.Get("Retry-After") != "30" || took < 1500*time.Millisecond || took > 6*time.Second {
			t.Fatalf("%d %s Retry-After %q after %s", a.Status, a.Body, a.Header.Get("Retry-After"), took)
		}
		e.worker = e.st.Start(t, testenv.ReplicaOptions{Name: "worker2", Role: "worker", Env: e.env})
		// The new signer is no anchor: once checked the file is refused as untrusted, which is a decision, not a wait.
		a = e.eventually(e.r, "files-internal/tools/fresh.bin", http.StatusForbidden)
		if p := a.JSON(t); p["reason"] != "untrusted" {
			t.Fatalf("%v", p)
		}
	})
}
