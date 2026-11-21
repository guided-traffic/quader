//go:build integration

package signatures

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// Stage 3, package 2 against the stack (signatures.md R-SIG-070 to R-SIG-073): the releases of a repository's gate, the impact of a change
// of anchors before it is saved, the preview before the gate is switched on, and the API's refused downloads and members. One web replica
// and a worker; signatures are made in the test with two OpenPGP keys of its own (R-SIG-042's third way).

// gateUsers are the callers of the stage-3 operations beside the trust environment's: an auditor whose token carries the triage right,
// a security-admin without read on web's repositories, and a reader of web who may not push.
type gateUsers struct {
	tokAuditor, tokSam, tokRex string
}

// newReleasesEnv is the trust environment with files-second (a second generic repository of web) and the two callers of gateUsers, all
// seeded before the replicas start.
func newReleasesEnv(t *testing.T) (*trustEnv, gateUsers) {
	t.Helper()
	st := testenv.NewStack(t, testenv.StackOptions{Name: "signatures"})
	rita, owen, mia, dave := st.User("rita"), st.User("owen"), st.User("mia"), st.User("dave")
	olga, sam, rex := st.User("olga"), st.User("sam"), st.User("rex")
	st.Bind("admin", rita)
	st.Bind("auditor", olga)
	st.Bind("security-admin", sam)
	st.Repository("web/files-internal", "local", "generic", `{}`)
	st.Repository("web/files-second", "local", "generic", `{}`)
	st.Repository("web/docker-local", "local", "oci", `{"signatures":{"onlyVerified":true}}`)
	st.Member("web", rex, "reader")
	st.Member("web", owen, "owner")
	st.Member("web", mia, "developer")
	e := &trustEnv{t: t, st: st}
	e.tokRita = st.Token(rita, scope.Scope{Inherit: true, Admin: true})
	e.tokOwen = st.Token(owen, scope.Scope{Inherit: true, Admin: true})
	e.tokMia = st.Token(mia, scope.Default())
	e.tokDave = st.Token(dave, scope.Default())
	u := gateUsers{
		tokAuditor: st.Token(olga, scope.Scope{Inherit: true, System: []string{"settings:read", "content:metadata-read", "vulnerabilities:triage"}}),
		tokSam:     st.Token(sam, scope.Scope{Inherit: true, System: []string{"vulnerabilities:triage"}}),
		tokRex:     st.Token(rex, scope.Default()),
	}
	env := []string{"QUADER_API_RATE_LIMITS_ENABLED=false", "QUADER_JOBS_POLL_INTERVAL=200ms", "QUADER_CLUSTER_EPOCH_POLL_INTERVAL=500ms"}
	e.r = st.Start(t, testenv.ReplicaOptions{Name: "web1", Env: env})
	st.Start(t, testenv.ReplicaOptions{Name: "worker1", Role: "worker", Env: env})
	return e, u
}

const signedTrust = `SELECT count(*)::text FROM signature_trust t JOIN signatures s ON s.id = t.signature_id
	JOIN items i ON i.blob_sha256 = s.subject_sha256 JOIN repositories r ON r.id = i.repo_id
	WHERE r.key = $1 AND i.path = $2 AND t.project_id IS NULL AND t.state = 'internal'`

// TestR_SIG_070_ReleasesPreviewImpactAndTheAPI: an auditor releases an unsigned file and a file whose signer no anchor knows yet in
// files-internal (201, signature.release.granted); the unsigned one is served there and refused in files-second, which holds the same
// bytes; an Owner's release answers 403, a security-admin without read 404; the withdrawal refuses it again and journals
// signature.release.withdrawn (R-SIG-070). With the release key and a second key as anchors, the impact of removing the release key's
// anchor counts the two files only it knows (one per gated repository), not the file the second key signed too nor the released one, and a
// block of its fingerprint counts the same two (R-SIG-071). The preview of files-internal answers 3 passing and 1 missing with any anchor,
// and 2 passing, 1 not_chosen and 1 missing with only the second key's anchor (R-SIG-072; the specification's company CA is a second
// OpenPGP key here). The refused downloads list one row per artifact with its count for an administrator and an auditor, none for a
// caller without a grant, 401 anonymously; the items answer and the file's signatures answer carry refused and gate; the releases list
// answers the auditor (R-SIG-073). The pusher of an image reads it in a gated OCI repository to sign it, a reader is refused with the
// image's name (R-SIG-064, Q-SIG-211, Q-SIG-213). Integration, about 6 s, files and an image of a few bytes to a kilobyte.
func TestR_SIG_070_ReleasesPreviewImpactAndTheAPI(t *testing.T) {
	e, u := newReleasesEnv(t)
	k1, k2 := newReleaseKey(t), newReleaseKey(t)
	file := func(b byte) []byte {
		out := make([]byte, 1024)
		out[0] = b
		return out
	}
	a, b, r, c, unsigned := file(1), file(2), file(3), file(4), file(5)
	e.put(e.tokRita, "web/files-internal/tools/a.bin", a)
	e.put(e.tokRita, "web/files-internal/tools/a.bin.asc", k1.sign(t, a))
	e.put(e.tokRita, "web/files-internal/tools/b.bin", b)
	e.put(e.tokRita, "web/files-internal/tools/b.bin.asc", k1.sign(t, b))
	e.put(e.tokRita, "web/files-internal/tools/b.bin.sig", k2.sign(t, b))
	e.put(e.tokRita, "web/files-internal/tools/r.bin", r)
	e.put(e.tokRita, "web/files-internal/tools/r.bin.asc", k1.sign(t, r))
	e.put(e.tokRita, "web/files-internal/tools/u.bin", unsigned)
	e.put(e.tokRita, "web/files-second/tools/c.bin", c)
	e.put(e.tokRita, "web/files-second/tools/c.bin.asc", k1.sign(t, c))
	e.put(e.tokRita, "web/files-second/tools/u.bin", unsigned)
	e.waitFor("the check of r.bin's signature", stateOf, "no_key", "files-internal", "tools/r.bin")
	for _, repo := range []string{"files-internal", "files-second"} {
		path := "/api/v1/projects/web/repositories/" + repo
		cur := e.call("GET", path, e.tokRita, nil, http.StatusOK)
		e.call("PATCH", path, e.tokRita, map[string]any{"signatures": map[string]any{"onlyVerified": true}}, http.StatusOK, "If-Match", cur.Header.Get("ETag"))
	}
	releases := "/api/v1/projects/web/repositories/files-internal/signatures/releases"
	sha := func(path string) string {
		return e.st.Text(`SELECT encode(i.blob_sha256, 'hex') FROM items i JOIN repositories r ON r.id = i.repo_id WHERE r.key = 'files-internal' AND i.path = $1`, path)
	}
	download := func(repo, path string) int {
		return e.st.Do(e.r, "GET", "/files/web/"+repo+"/"+path, e.tokRita, nil).Status
	}

	t.Run("R-SIG-070 a release passes in its repository alone, an Owner cannot release, a withdrawal is journaled", func(t *testing.T) {
		body := func(path string) map[string]string {
			return map[string]string{"sha256": sha(path), "reason": "confirmed with the vendor"}
		}
		e.call("POST", releases, e.tokOwen, body("tools/u.bin"), http.StatusForbidden)
		e.call("POST", releases, u.tokSam, body("tools/u.bin"), http.StatusNotFound)
		rel := e.call("POST", releases, u.tokAuditor, body("tools/u.bin"), http.StatusCreated).JSON(t)
		if rel["refusedReason"] != "missing" || rel["repository"] != "web/files-internal" {
			t.Fatalf("%v", rel)
		}
		if got := e.call("POST", releases, u.tokAuditor, body("tools/r.bin"), http.StatusCreated).JSON(t); got["refusedReason"] != "untrusted" {
			t.Fatalf("%v", got)
		}
		e.call("POST", releases, u.tokAuditor, body("tools/u.bin"), http.StatusConflict)
		if download("files-internal", "tools/u.bin") != http.StatusOK || download("files-second", "tools/u.bin") != http.StatusForbidden {
			t.Fatal("the release is not the repository's alone")
		}
		e.call("DELETE", releases+"/"+sha("tools/u.bin"), u.tokAuditor, nil, http.StatusNoContent)
		if download("files-internal", "tools/u.bin") != http.StatusForbidden {
			t.Fatal("the withdrawn release still passes")
		}
		if n := e.st.Number(`SELECT count(*) FROM revocation_outbox WHERE event = 'signature.release.withdrawn' AND entry->>'repository' = 'web/files-internal'`); n != 1 {
			t.Fatalf("%d journal entries of the withdrawal", n)
		}
		if n := e.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'signature.release.granted'`); n != 2 {
			t.Fatalf("%d grants recorded", n)
		}
	})

	e.call("POST", "/api/v1/signatures/anchors", e.tokRita, map[string]any{
		"key": "release-key", "title": "Release key", "kind": "openpgp", "openpgp": map[string]string{"keys": string(k1.public)},
	}, http.StatusCreated)
	e.call("POST", "/api/v1/signatures/anchors", e.tokRita, map[string]any{
		"key": "second-key", "title": "Second key", "kind": "openpgp", "openpgp": map[string]string{"keys": string(k2.public)},
	}, http.StatusCreated)
	e.waitFor("the trust of a.bin", signedTrust, "1", "files-internal", "tools/a.bin")
	e.waitFor("the trust of b.bin's two signatures", signedTrust, "2", "files-internal", "tools/b.bin")
	e.waitFor("the trust of c.bin", signedTrust, "1", "files-second", "tools/c.bin")

	t.Run("R-SIG-070 a file the gate passes cannot be released", func(t *testing.T) {
		e.call("POST", releases, u.tokAuditor, map[string]string{"sha256": sha("tools/a.bin"), "reason": "x"}, http.StatusConflict)
	})
	t.Run("R-SIG-071 the impact counts what only the release key passes", func(t *testing.T) {
		id := e.st.Number(`SELECT id FROM signature_anchors WHERE key = 'release-key'`)
		e.call("POST", "/api/v1/signatures/impact", e.tokOwen, map[string]any{"anchor": id, "change": "remove"}, http.StatusForbidden)
		imp := e.call("POST", "/api/v1/signatures/impact", e.tokRita, map[string]any{"anchor": id, "change": "remove"}, http.StatusOK).JSON(t)
		if imp["artifacts"] != float64(2) || imp["repositories"] != float64(2) || imp["complete"] != true || imp["bound"] != "exact" {
			t.Fatalf("remove: %v", imp)
		}
		blk := e.call("POST", "/api/v1/signatures/impact", e.tokRita, map[string]any{"block": map[string]string{"kind": "fingerprint", "value": k1.fp}}, http.StatusOK).JSON(t)
		if blk["artifacts"] != float64(2) {
			t.Fatalf("block: %v", blk)
		}
	})
	t.Run("R-SIG-072 the preview counts per reason with any and with chosen anchors", func(t *testing.T) {
		preview := "/api/v1/projects/web/repositories/files-internal/signatures/preview"
		var p struct {
			Examined, Artifacts, Pass int
			Complete                  bool
			Refused                   map[string]int
			Examples                  []map[string]any
		}
		decode := func(a testenv.Answer) {
			t.Helper()
			p.Refused = nil
			if err := json.Unmarshal(a.Body, &p); err != nil {
				t.Fatal(err)
			}
		}
		decode(e.call("GET", preview, e.tokOwen, nil, http.StatusOK))
		if p.Artifacts != 4 || p.Pass != 3 || p.Refused["missing"] != 1 || !p.Complete || len(p.Examples) != 1 || p.Examples[0]["path"] != "tools/u.bin" {
			t.Fatalf("any anchor: %+v", p)
		}
		decode(e.call("GET", preview+"?anchors=installation/second-key", e.tokOwen, nil, http.StatusOK))
		if p.Pass != 2 || p.Refused["notChosen"] != 1 || p.Refused["missing"] != 1 {
			t.Fatalf("the second key only: %+v", p)
		}
		e.call("GET", preview, e.tokMia, nil, http.StatusForbidden)
	})
	t.Run("R-SIG-073 the refused downloads, the members refused and gate, and the releases list", func(t *testing.T) {
		download("files-internal", "tools/u.bin")
		if got := e.st.Text(`SELECT f.reason || ' ' || f.count::text FROM signature_refusals f JOIN repositories r ON r.id = f.repo_id WHERE r.key = 'files-internal'`); got != "missing 2" {
			t.Fatalf("the refused download: %q", got)
		}
		count := func(token string) int {
			var page struct{ Items []map[string]any }
			if err := json.Unmarshal(e.call("GET", "/api/v1/signatures/refusals", token, nil, http.StatusOK).Body, &page); err != nil {
				t.Fatal(err)
			}
			return len(page.Items)
		}
		if n := count(e.tokRita); n != 2 {
			t.Fatalf("an administrator sees %d refused downloads, want 2 (files-internal and files-second)", n)
		}
		if n := count(u.tokAuditor); n != 2 {
			t.Fatalf("an auditor sees %d", n)
		}
		if n := count(e.tokDave); n != 0 {
			t.Fatalf("a caller without a grant sees %d", n)
		}
		e.call("GET", "/api/v1/signatures/refusals", "", nil, http.StatusUnauthorized)
		item := e.call("GET", "/api/v1/projects/web/repositories/files-internal/items?path=tools/u.bin", e.tokRita, nil, http.StatusOK).JSON(t)
		if sig, _ := item["signatures"].(map[string]any); sig["refused"] != true || sig["refusedReason"] != "missing" {
			t.Fatalf("the item's member: %v", item["signatures"])
		}
		rep := e.call("GET", "/api/v1/projects/web/repositories/files-internal/signatures?path=tools/a.bin", e.tokRita, nil, http.StatusOK).JSON(t)
		if g, _ := rep["gate"].(map[string]any); g["decision"] != "pass" || g["passedBy"].(map[string]any)["kind"] != "signature" {
			t.Fatalf("the file's gate: %v", rep["gate"])
		}
		var list struct{ Items []map[string]any }
		if err := json.Unmarshal(e.call("GET", releases, u.tokAuditor, nil, http.StatusOK).Body, &list); err != nil {
			t.Fatal(err)
		}
		if len(list.Items) != 1 || list.Items[0]["sha256"] != sha("tools/r.bin") {
			t.Fatalf("the releases: %v", list.Items)
		}
		e.call("GET", releases, e.tokDave, nil, http.StatusNotFound)
	})
	t.Run("R-SIG-064 a writer reads an image of a gated repository to sign it; a reader is refused in the client's words", func(t *testing.T) {
		cfg, layer := []byte(`{"architecture":"amd64","os":"linux"}`), []byte("a layer of a few bytes")
		digest := func(b []byte) string { return "sha256:" + hex.EncodeToString(sha256Of(b)) }
		v2 := "/v2/web/docker-local/app"
		for _, b := range [][]byte{cfg, layer} {
			if a := e.st.Do(e.r, "POST", v2+"/blobs/uploads/?digest="+digest(b), e.tokRita, b, "Content-Type", "application/octet-stream"); a.Status != http.StatusCreated {
				t.Fatalf("blob: %d %s", a.Status, a.Body)
			}
		}
		manifest, _ := json.Marshal(map[string]any{
			"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
			"config": map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": digest(cfg), "size": len(cfg)},
			"layers": []any{map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar", "digest": digest(layer), "size": len(layer)}},
		})
		if a := e.st.Do(e.r, "PUT", v2+"/manifests/1.0", e.tokRita, manifest, "Content-Type", "application/vnd.oci.image.manifest.v1+json"); a.Status != http.StatusCreated {
			t.Fatalf("manifest: %d %s", a.Status, a.Body)
		}
		byDigest := v2 + "/manifests/" + digest(manifest)
		for _, method := range []string{"HEAD", "GET"} {
			if a := e.st.Do(e.r, method, byDigest, e.tokRita, nil); a.Status != http.StatusOK {
				t.Fatalf("the writer's %s to sign: %d %s", method, a.Status, a.Body)
			}
		}
		a := e.st.Do(e.r, "GET", v2+"/manifests/1.0", u.tokRex, nil)
		if a.Status != http.StatusForbidden || !strings.Contains(string(a.Body), "Quader refused app:1.0 of web/docker-local: it has no signature of its publisher or its build") {
			t.Fatalf("the reader: %d %s", a.Status, a.Body)
		}
	})
}

func sha256Of(b []byte) []byte { d := sha256.Sum256(b); return d[:] }
