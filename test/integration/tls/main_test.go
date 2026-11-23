//go:build integration

// Package tls proves the TLS material of R-ARCH-035 against the integration
// stack and the real binary: quader's listeners serve TLS and rotate their
// certificate under load without a restart (E0.15 at process level), and the
// PostgreSQL, Valkey and object-store clients read their CA bundles again
// without a restart (R-HA-015, R-HA-056, R-ARCH-022). Every dependency of the
// stack is TLS-only; testenv.Main refuses an environment that names a
// plaintext endpoint.
package tls

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// binary is the quader executable built once for the package.
var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "quader-it-tls-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "quader")
	cmd := exec.Command("go", "build", "-o", binary, "github.com/guided-traffic/quader/cmd/quader")
	cmd.Env = testenv.ChildEnv("CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "go build: %v\n%s", err, out)
		os.Exit(1)
	}
	code := testenv.Main(m)
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
