//go:build integration

// Package signatures tests signatures on artifacts against the stack (signatures.md, area signatures). Stage 1's first package builds
// the store and the hooks: a commit records what it carries and who uploaded it, in the commit's transaction. Nothing checks a signature
// yet (the job is the next package's), so these tests read the rows the hooks wrote.
package signatures

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/openpgp"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

func TestMain(m *testing.M) { os.Exit(testenv.Main(m)) }

type env struct {
	t   *testing.T
	st  *testenv.Stack
	r   *testenv.Replica
	tok string
}

// newEnv is one installation with two generic repositories of the project web and an administrator, rita.
func newEnv(t *testing.T) *env {
	t.Helper()
	st := testenv.NewStack(t, testenv.StackOptions{Name: "signatures"})
	rita := st.User("rita")
	st.Bind("admin", rita)
	st.Repository("web/files-internal", "local", "generic", `{}`)
	st.Repository("web/files-other", "local", "generic", `{}`)
	e := &env{t: t, st: st, tok: st.Token(rita, scope.Scope{Inherit: true, Admin: true})}
	e.r = st.Start(t, testenv.ReplicaOptions{Name: "web1", Env: []string{"QUADER_API_RATE_LIMITS_ENABLED=false"}})
	return e
}

func (e *env) deploy(repo, path string, content []byte) {
	e.t.Helper()
	a := e.st.Do(e.r, "PUT", "/files/web/"+repo+"/"+path, e.tok, content, "Content-Type", "application/octet-stream")
	if a.Status != http.StatusCreated && a.Status != http.StatusOK {
		e.t.Fatalf("deploy %s/%s: %d %s", repo, path, a.Status, a.Body)
	}
}

func (e *env) remove(repo, path string) {
	e.t.Helper()
	if a := e.st.Do(e.r, "DELETE", "/files/web/"+repo+"/"+path, e.tok, nil); a.Status != http.StatusNoContent && a.Status != http.StatusOK {
		e.t.Fatalf("delete %s/%s: %d %s", repo, path, a.Status, a.Body)
	}
}

func (e *env) count(sql string, args ...any) string {
	e.t.Helper()
	return e.st.Text(sql, args...)
}

// releaseKeySign signs data detached with a release key made by the test (Ed25519, milliseconds; the owner's third way, R-SIG-042).
func releaseKeySign(t *testing.T, data []byte) []byte {
	t.Helper()
	secret, _, _, err := openpgp.Generate("Release key", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	key, err := openpgp.ParseSecret(secret, nil, time.Now(), openpgp.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	sig, err := openpgp.DetachSign(data, []*openpgp.Key{key}, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

// TestR_SIG_003_ASignatureIsStoredOncePerObjectAndSubjectAndAttachedPerRepository: the same file and its signature in two repositories,
// stored in either order, make one object, one signature and two attachments, each following its signature file; a file replaced by
// other bytes takes its attachment to a new signature against the new bytes and leaves the other repository's; a signature file
// deleted takes its attachment with it (R-SIG-003, R-SIG-004, R-SIG-032, Q-SIG-47). Integration, about 2 s, files of a kilobyte.
func TestR_SIG_003_ASignatureIsStoredOncePerObjectAndSubjectAndAttachedPerRepository(t *testing.T) {
	e := newEnv(t)
	file := make([]byte, 1024)
	asc := releaseKeySign(t, file)
	e.deploy("files-internal", "tools/tool.tar.gz", file)
	e.deploy("files-internal", "tools/tool.tar.gz.asc", asc)
	// the other order in the second repository: the signature file first, named again when its file arrives
	e.deploy("files-other", "tools/tool.tar.gz.asc", asc)
	e.deploy("files-other", "tools/tool.tar.gz", file)

	if n := e.count(`SELECT count(*) FROM signature_objects`); n != "1" {
		t.Fatalf("%s signature objects, want 1", n)
	}
	if got := e.count(`SELECT o.scheme || ' ' || s.role || ' ' || s.state || ' ' || s.part FROM signatures s JOIN signature_objects o ON o.sha256 = s.object_sha256`); got != "openpgp publisher pending 0" {
		t.Fatalf("signature = %q", got)
	}
	if n := e.count(`SELECT count(*) FROM signature_attachments a JOIN items i ON i.id = a.item_id
		WHERE a.source = 'sibling' AND a.location = 'tools/tool.tar.gz.asc' AND i.path = 'tools/tool.tar.gz.asc' AND a.repo_id = i.repo_id`); n != "2" {
		t.Fatalf("%s attachments that follow their signature file, want 2", n)
	}
	if got := e.count(`SELECT count(*) FROM signatures s JOIN items i ON i.blob_sha256 = s.subject_sha256
		JOIN repositories r ON r.id = i.repo_id WHERE r.key = 'files-internal' AND i.path = 'tools/tool.tar.gz'`); got != "1" {
		t.Fatalf("the subject is not the file's blob: %s", got)
	}

	t.Run("a replaced file takes its attachment to a signature against the new bytes", func(t *testing.T) {
		e.deploy("files-internal", "tools/tool.tar.gz", []byte("other bytes"))
		if n := e.count(`SELECT count(*) FROM signatures`); n != "2" {
			t.Fatalf("%s signatures, want 2 (one per subject)", n)
		}
		if n := e.count(`SELECT count(*) FROM signature_attachments a JOIN repositories r ON r.id = a.repo_id JOIN signatures s ON s.id = a.signature_id
			JOIN items i ON i.repo_id = r.id AND i.path = 'tools/tool.tar.gz' WHERE s.subject_sha256 = i.blob_sha256`); n != "2" {
			t.Fatalf("%s attachments against the bytes each repository holds, want 2", n)
		}
	})
	t.Run("a deleted signature file takes its attachment", func(t *testing.T) {
		e.remove("files-other", "tools/tool.tar.gz.asc")
		if n := e.count(`SELECT count(*) FROM signature_attachments a JOIN repositories r ON r.id = a.repo_id WHERE r.key = 'files-other'`); n != "0" {
			t.Fatalf("%s attachments left in files-other", n)
		}
	})
}

// TestR_SIG_010_TheOriginOfAnUploadIsASnapshot: an upload records who uploaded at the item; for a credential that has an issuance
// record of a CI job, the allowlisted claims as they were, kept after the record is gone; never the e-mail (R-SIG-010, Q-SIG-40).
// Integration, about 1 s, bytes.
func TestR_SIG_010_TheOriginOfAnUploadIsASnapshot(t *testing.T) {
	e := newEnv(t)
	e.deploy("files-internal", "a.bin", []byte("a"))
	if got := e.count(`SELECT o.principal_kind || ' ' || o.principal_name || ' ' || (o.ci IS NULL) || ' ' || (o.token_id IS NOT NULL)
		FROM artifact_origins o JOIN items i ON i.id = o.item_id WHERE i.path = 'a.bin'`); got != "user rita true true" {
		t.Fatalf("origin = %q", got)
	}

	// The credential's issuance record, as the CI exchange writes one (gitlab-ci-oidc.md R-CI-020).
	issuer := e.count(`INSERT INTO ci_issuers (id, name, kind, issuer, allowed_audiences, required_claims, pin_claims, single_use_subject_tokens, public_issuer)
		VALUES (gen_random_uuid(), 'gitlab-main', 'gitlab', 'https://gitlab.example', '{https://quader.test}', '{}', '{project_id}', false, false)
		RETURNING id::text`)
	e.st.Exec(`INSERT INTO ci_token_issuances (token_id, issuer_id, mapping_ids, mapping_names, subject_kind, project_id, project_path, pipeline_id,
		job_id, ref, ref_type, ref_protected, user_login, claims, grants, granted_scope, issued_at, expires_at)
		SELECT t.id, $1::uuid, ARRAY[gen_random_uuid()], ARRAY['release'], 'id_token', '42', 'platform/tool', '9001', '77', 'main', 'branch', true,
		       'dev', '{"namespace_path":"platform","user_email":"dev@example.com"}'::jsonb, '{}'::jsonb, 'read', now(), now() + interval '1 hour'
		  FROM api_token t JOIN principal p ON p.id = t.principal_id WHERE p.name = 'rita'`, issuer)
	e.deploy("files-internal", "b.bin", []byte("b"))
	e.st.Exec(`DELETE FROM ci_token_issuances`)
	want := `{"issuer": "gitlab-main", "project_path": "platform/tool", "project_id": "42", "namespace_path": "platform", "pipeline_id": "9001",
		"job_id": "77", "ref": "main", "ref_type": "branch", "ref_protected": true, "user_login": "dev"}`
	if got := e.count(`SELECT (o.ci = $1::jsonb)::text FROM artifact_origins o JOIN items i ON i.id = o.item_id WHERE i.path = 'b.bin'`, want); got != "true" {
		t.Fatalf("ci = %s\nwant %s", e.count(`SELECT o.ci::text FROM artifact_origins o JOIN items i ON i.id = o.item_id WHERE i.path = 'b.bin'`), want)
	}
}
