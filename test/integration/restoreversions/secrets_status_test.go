//go:build integration

package restoreversions

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// R-ARCH-021, R-API-049: `quader admin secrets status` reads the configured key ring, the database and the registry, and reports what
// GET /api/v1/system/encryption reports: the same keys with the same sealed values and canaries, the same live replicas. It changes
// nothing. The binary is the one this package builds for the commands of the restore procedure.
func TestR_ARCH_021_TheSecretsStatusCommandReportsWhatTheEndpointReports(t *testing.T) {
	s := testenv.NewStack(t, testenv.StackOptions{Name: "secretsstatus"})
	admin := s.User("rita")
	s.Bind("admin", admin)
	tok := s.Token(admin, scope.Scope{Inherit: true, Admin: true})
	r := s.Start(t, testenv.ReplicaOptions{Name: "web1"}) // its start seals the canary of its ring and publishes the ring
	type report struct {
		ActiveKeyID string `json:"activeKeyId"`
		Keys        []struct {
			ID           string `json:"id"`
			SealedValues int64  `json:"sealedValues"`
			Canary       string `json:"canary"`
		} `json:"keys"`
		Replicas []struct {
			Name   string   `json:"name"`
			KeyIDs []string `json:"keyIds"`
		} `json:"replicas"`
		RingsCompatible bool `json:"ringsCompatible"`
	}
	before := s.Number(`SELECT count(*) FROM secret_key_canary`)
	code, out, errOut := run(t, s, "secrets", "status", "--json")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	var cli, api report
	if err := json.Unmarshal([]byte(out), &cli); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	a := s.Do(r, "GET", "/api/v1/system/encryption", tok, nil)
	if a.Status != 200 || json.Unmarshal(a.Body, &api) != nil {
		t.Fatalf("the endpoint: %d %s", a.Status, a.Body)
	}
	cj, _ := json.Marshal(cli)
	aj, _ := json.Marshal(api)
	if string(cj) != string(aj) {
		t.Fatalf("the command and the endpoint differ:\ncommand  %s\nendpoint %s", cj, aj)
	}
	if len(cli.Keys) == 0 || cli.Keys[0].Canary != "ok" || len(cli.Replicas) != 1 || cli.Replicas[0].Name != "web1" || !cli.RingsCompatible {
		t.Fatalf("the report: %s", cj)
	}
	if after := s.Number(`SELECT count(*) FROM secret_key_canary`); after != before {
		t.Fatalf("the command changed the canaries: %d, then %d", before, after)
	}
	_, text, _ := run(t, s, "secrets", "status")
	if !strings.Contains(text, "KEY ID") || !strings.Contains(text, "rings compatible  yes") || strings.Contains(text, "v1:") {
		t.Fatalf("the table:\n%s", text)
	}
}
