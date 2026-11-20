//go:build integration

// Package upstream tests the built-in project upstream against the integration stack (repositories-and-proxy.md R-REPO-066 to
// R-REPO-074, authorization.md R-AUTHZ-041): the reconcile of `quader migrate` against the migration session of a test database, the
// operations of the well-known repositories and the refusals over HTTP on two in-process web replicas that share it, and the
// database's guards. No upstream is contacted.
package upstream

import (
	"os"
	"testing"

	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestMain fails the whole package, before any test runs, when the environment names a plaintext or unverified endpoint (INV-20):
// the integration stack is TLS-only.
func TestMain(m *testing.M) {
	// A repository write notifies the change area repo_config, which the repository area registers; a build of this tree without that
	// work registers it here.
	if _, ok := cluster.LookupArea("repo_config"); !ok {
		cluster.RegisterArea(cluster.Area{Name: "repo_config", Owner: "R-REPO-008", Class: cluster.ChangeArea, PayloadVersion: 1})
	}
	os.Exit(testenv.Main(m))
}
