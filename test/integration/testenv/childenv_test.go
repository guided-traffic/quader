package testenv

import (
	"slices"
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
