package testenv

import (
	"os"
	"strings"
)

// ChildEnv is the environment of a quader process a test starts: the test process's own (PATH, HOME, GOCOVERDIR, the stack's
// QUADER_TEST_ variables) without any other variable of the prefix QUADER_, then extra, the settings the test names. The product
// reads a QUADER_ variable that is a setting and refuses one that is no setting, so a variable exported in a developer's shell or
// by a wrapper (QUADER_PERF_GATE, QUADER_IT_NO_DOCKER, QUADER_DEB_PROOF_DIR, a stray QUADER_DATABASE_HOST) ended such a process at
// start or changed its configuration without a word (F-177). A `go build` keeps os.Environ: it reads no QUADER_ variable.
func ChildEnv(extra ...string) []string {
	var out []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "QUADER_") && !strings.HasPrefix(name, "QUADER_TEST_") && !strings.HasPrefix(name, "QUADER_CAC_") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, extra...)
}
