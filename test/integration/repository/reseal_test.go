//go:build integration

package repository

import (
	"testing"

	"github.com/guided-traffic/quader/internal/auth/scope"
)

// TestSecretsReencryptTask runs the task on a worker of the harness: the
// registered sealed columns in this process are the repository secrets, the private keys of the Debian signing keys, the
// uploaded key of the signing identity and the two columns of the storage keys, so the job walks them with repository.ResealSecrets,
// debdata.ResealSigningKeys, signingkey.Reseal and keys.Reseal and records what it did (R-ARCH-021 rotation step 3, R-OPS-019,
// R-SIG-047, R-STOR-084). The remote settings themselves are tested with the
// component environment (remote_test.go: TestRemoteSecrets).
func TestSecretsReencryptTask(t *testing.T) {
	e := newStackEnv(t)
	admin := e.stack.User("reseal-admin")
	e.stack.Bind("admin", admin)
	tok := e.stack.Token(admin, scope.Scope{Inherit: true, Admin: true})
	web := e.newReplica("reseal-web", stackOpts{})
	worker := e.newReplica("reseal-worker", stackOpts{role: "worker"})

	res := e.stack.Do(web.Replica, "POST", "/api/v1/system/tasks/secrets-reencrypt/run", tok, nil)
	if res.Status != 202 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	id := res.JSON(t)["job"].(map[string]any)["id"].(string)
	job := e.waitJob(worker.App.Queue(), id)
	if job.State != "succeeded" {
		t.Fatalf("job = %+v", job)
	}
	got := e.stack.Do(web.Replica, "GET", "/api/v1/system/jobs/"+id, tok, nil).JSON(t)["result"].(map[string]any)
	// The five sealed columns: repository_secrets.sealed, deb_signing_keys.private_key, signing_identities.sealed_key, and the storage
	// keys' storage_keys.sealed_key and upload_sessions.sealed_data_key (storage.md R-STOR-083).
	if got["columns"] != float64(5) || got["failed"] != float64(0) || got["resealed"] != float64(0) {
		t.Fatalf("result = %v", got)
	}
}
