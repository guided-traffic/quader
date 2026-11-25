package archtest

import (
	"strings"
	"testing"
)

// R-OPS-013 rule 7 (Q-OPS-426): a log field named url holds an address in the one form, built by the functions of
// internal/observability, so that a signed address an upstream handed back never reaches the log with its credential. The check finds
// the key "url" in a call of log/slog (a method of *slog.Logger, slog.String, slog.Any) whose value is not such a call. Seconds: a
// fixture of three files and the typed load of the module that the other checks of the package make as well.
func TestR_OPS_013_ALoggedURLIsBuiltByTheOneFunction(t *testing.T) {
	t.Run("the fixture", func(t *testing.T) {
		pkgs, err := LoadTyped(fixtureDir(t, "urlfield"))
		if err != nil {
			t.Fatal(err)
		}
		vs := CheckURLLogFields("example.test/fix", pkgs)
		got := map[string]bool{}
		for _, v := range vs {
			got[v.From+" "+v.To] = true
			if v.Rule != urlFieldRule {
				t.Errorf("violation %s", v)
			}
		}
		for _, want := range []string{
			"internal/bad/bad.go u.String()", "internal/bad/bad.go observability.Other(u)", "internal/bad/bad.go u",
		} {
			if !got[want] {
				t.Errorf("%s not named:\n%s", want, strings.Join(Strings(vs), "\n"))
			}
		}
		if len(vs) != 3 {
			t.Errorf("%d violations, want the three of bad.go (good.go builds every url by the rule):\n%s", len(vs), strings.Join(Strings(vs), "\n"))
		}
	})
	t.Run("the current tree", func(t *testing.T) {
		root, module, _ := realTree(t)
		pkgs, err := LoadTyped(root)
		if err != nil {
			t.Fatal(err)
		}
		wantClean(t, CheckURLLogFields(module, pkgs))
	})
}
