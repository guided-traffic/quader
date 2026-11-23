package archtest

import (
	"strings"
	"testing"
)

// R-OPS-009: the code of the binary logs through the logger it is given; the process's default logger carries none of the replica's
// fields. The check finds a package-level function of log/slog that logs or returns the default logger, through an alias too, and any
// function of the standard package log but log.New, in internal/ and cmd/ (the formats' tree alone until 2026-11-23). Seconds: it
// shares the typed load of the module with the other checks of the package.
func TestR_OPS_009_NoFormatLogsThroughTheDefaultLogger(t *testing.T) {
	t.Run("the fixture", func(t *testing.T) {
		pkgs, err := LoadTyped(fixtureDir(t, "defaultlogger"))
		if err != nil {
			t.Fatal(err)
		}
		vs := CheckDefaultLogger("example.test/fix", pkgs)
		got := map[string]bool{}
		for _, v := range vs {
			got[v.From+" "+v.To] = true
			if v.Rule != defaultLoggerRule {
				t.Errorf("violation %s", v)
			}
		}
		for _, want := range []string{
			"internal/formats/bad/bad.go log/slog.Error", "internal/formats/bad/bad.go log/slog.Default", "internal/formats/bad/bad.go log.Printf",
			"internal/other/other.go log/slog.Error",
		} {
			if !got[want] {
				t.Errorf("%s not named:\n%s", want, strings.Join(Strings(vs), "\n"))
			}
		}
		if len(vs) != 4 {
			t.Errorf("%d violations, want the three of bad.go and the one of other.go (log.New of good.go and the tool outside the tree "+
				"are none):\n%s", len(vs), strings.Join(Strings(vs), "\n"))
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
