package testenv

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A child quader process gets the caller's environment without the QUADER_ variables of the caller's shell, the stack's
// QUADER_TEST_ ones and the settings the test names (F-177). Milliseconds.
func TestChildEnvLeavesOutTheCallersQuaderVariables(t *testing.T) {
	t.Setenv("QUADER_PERF_GATE", "1")
	t.Setenv("QUADER_DATABASE_HOST", "elsewhere")
	t.Setenv("QUADER_TEST_PG_HOST", "localhost")
	t.Setenv("QUADER_CAC_X", "y")
	t.Setenv("W4_KEPT", "z")
	got := ChildEnv("QUADER_SERVER_LISTEN=127.0.0.1:0")
	for _, gone := range []string{"QUADER_PERF_GATE=1", "QUADER_DATABASE_HOST=elsewhere"} {
		if slices.Contains(got, gone) {
			t.Errorf("%s reached the child", gone)
		}
	}
	for _, kept := range []string{"QUADER_TEST_PG_HOST=localhost", "QUADER_CAC_X=y", "W4_KEPT=z", "QUADER_SERVER_LISTEN=127.0.0.1:0"} {
		if !slices.Contains(got, kept) {
			t.Errorf("%s did not reach the child", kept)
		}
	}
}

// F-177: no test of the integration tree starts a process with the caller's whole environment. os.Environ is read in ChildEnv
// alone, so a new start, of a quader process, a build or a third party's suite, takes ChildEnv or names its environment itself.
// It reads the tree's files as text: milliseconds.
func TestNoIntegrationTestReadsTheCallersWholeEnvironment(t *testing.T) {
	var found []string
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || filepath.Base(path) == "childenv.go" ||
			filepath.Base(path) == "childenv_test.go" && filepath.Base(filepath.Dir(path)) == "testenv" {
			return err
		}
		b, err := os.ReadFile(path) //nolint:gosec // a file of the tree this test walks
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "os.Environ()") {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Errorf("these files read os.Environ(); a process a test starts takes testenv.ChildEnv:\n%s", strings.Join(found, "\n"))
	}
}

// F-047: FreeAddr hands no address out twice in a process, although the kernel may give a closed port to the next listener on
// port 0 at once; a replica whose two listeners got one address refused its configuration. Milliseconds.
func TestFreeAddrHandsNoAddressOutTwice(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		a := FreeAddr(t)
		if seen[a] {
			t.Fatalf("FreeAddr returned %s twice", a)
		}
		seen[a] = true
	}
}
