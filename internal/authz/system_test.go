package authz

import (
	"testing"

	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/errs"
)

// scanSub is the subject of a scan token for one repository, as edge.SubjectOf builds it on the scan listener.
func scanSub(repo string) Subject {
	return Subject{Kind: principal.KindSystem, Name: SystemVulnScannerTrivy, AuthMethod: "bearer", Endpoint: config.ScanEndpoint, SystemRepo: qr(repo)}
}

// systemFixture: a private local and a private remote OCI repository, a disabled one, a virtual one, an npm repository, and a
// policy that grants nobody anything on them.
func systemFixture() *fixture {
	f := newFixture().
		repo("docker-local", "local", "oci", "").
		repo("docker-remote", "remote", "oci", `{"remote":{"cacheFillAction":"read"}}`).
		repo("docker-off", "local", "oci", `{"disabled":true}`).
		repo("docker-all", "virtual", "oci", `{"virtual":{"members":["docker-local"]}}`).
		repo("npm-local", "local", "npm", "")
	f.bind(RoleAdmin, idOps)
	return f
}

func TestR_OCI_037_TheScanSubjectReadsTheRepositoryItsTokenNamesAndNothingElse(t *testing.T) {
	e := newTestEngine(t, systemFixture(), nil)
	ctx := t.Context()
	idx, err := e.Begin(ctx, scanSub("docker-local"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		sub  Subject
		c    Coordinate
		want Outcome
	}{
		{"read on a private local repository", scanSub("docker-local"), read("docker-local", "app", "app/_manifests/sha256:x"), Allow},
		{"read on a private remote repository", scanSub("docker-remote"), read("docker-remote", "app", "app/_blobs/sha256:x"), Allow},
		{"another repository than the token names", scanSub("docker-local"), read("docker-remote", "app", ""), DenyHidden},
		{"a disabled repository", scanSub("docker-off"), read("docker-off", "app", ""), DenyHidden},
		{"a repository of another format", scanSub("npm-local"), read("npm-local", "left-pad", ""), DenyHidden},
		{"a virtual repository", scanSub("docker-all"), read("docker-all", "app", ""), DenyHidden},
		{"a repository that does not exist", scanSub("docker-gone"), read("docker-gone", "app", ""), DenyHidden},
		{"a write", scanSub("docker-local"), write("docker-local", "app", "app/_blobs/sha256:x", Deploy), DenyHidden},
		{"delete", scanSub("docker-local"), write("docker-local", "app", "", Delete), DenyHidden},
		{"another system subject without a rule", Subject{Kind: principal.KindSystem, Name: "nobody", SystemRepo: qr("docker-local")}, read("docker-local", "app", ""), DenyHidden},
	}
	for _, tc := range cases {
		t.Run("Q-OCI-228 "+tc.name, func(t *testing.T) {
			if d := e.DecideOn(ctx, idx, tc.sub, tc.c); d.Outcome != tc.want {
				t.Fatalf("decision %+v, want %d", d, tc.want)
			}
		})
	}
	t.Run("R-AUTHZ-008 the scan subject never populates: an uncached layer of a remote is populate_not_permitted", func(t *testing.T) {
		d := e.PopulateOn(ctx, idx, scanSub("docker-remote"), read("docker-remote", "app", "app/_blobs/sha256:x"))
		if ee, ok := errs.As(d.Err()); !ok || ee.Status() != 403 || ee.Code != errs.CodePopulateNotPermitted {
			t.Fatalf("decision %+v err %v", d, d.Err())
		}
	})
	t.Run("R-AUTHZ-002 it holds no capability, no role and is not internal", func(t *testing.T) {
		sub := scanSub("docker-local")
		if e.HoldsOn(idx, sub, string(SettingsRead)) || sub.Internal() || idx.rolesOf(sub) != 0 {
			t.Fatal("the scan subject holds something")
		}
	})
}
