package archtest

import (
	"strings"
	"testing"
)

// R-OPS-009: a format logs through the logger of its request or component; the process's default logger carries none of the replica's
// fields. The check finds a package-level function of log/slog that logs or returns the default logger, through an alias too, and any
// function of the standard package log, in the formats' tree only.
func TestR_OPS_009_NoFormatLogsThroughTheDefaultLogger(t *testing.T) {
	t.Run("the fixture", func(t *testing.T) {
		pkgs, err := LoadTyped(fixtureDir(t, "defaultlogger"))
		if err != nil {
			t.Fatal(err)
		}
		vs := CheckDefaultLogger("example.test/fix", pkgs)
		got := map[string]bool{}
		for _, v := range vs {
			got[v.To] = true
			if v.From != "internal/formats/bad/bad.go" || v.Rule != defaultLoggerRule {
				t.Errorf("violation %s", v)
			}
		}
		for _, want := range []string{"log/slog.Error", "log/slog.Default", "log.Printf"} {
			if !got[want] {
				t.Errorf("%s not named:\n%s", want, strings.Join(Strings(vs), "\n"))
			}
		}
		if len(vs) != 3 {
			t.Errorf("%d violations, want the three of bad.go:\n%s", len(vs), strings.Join(Strings(vs), "\n"))
		}
	})
	t.Run("the current tree", func(t *testing.T) {
		root, module, _ := realTree(t)
		pkgs, err := LoadTyped(root)
		if err != nil {
			t.Fatal(err)
		}
		wantClean(t, CheckDefaultLogger(module, pkgs))
	})
}
