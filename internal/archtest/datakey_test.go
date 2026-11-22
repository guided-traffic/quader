package archtest

import (
	"strings"
	"testing"
)

// TestR_STOR_096_CopyDataKeyHasTwoCallers: blob.Store.CopyDataKey hands out the data key of a stored object, so only the store and
// the handover's package name it (R-STOR-083, R-STOR-096, Q-STOR-320, Q-STOR-340).
func TestR_STOR_096_CopyDataKeyHasTwoCallers(t *testing.T) {
	t.Run("R-STOR-083 a call from any other package fails: a call, a method value, a call through an interface, a subpackage of the store, a command and a call by reflection", func(t *testing.T) {
		vs, err := CheckDataKeyCallers(fixtureDir(t, "copydatakey"))
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, v := range vs {
			got[v.From] = true
			if v.Rule != "R-STOR-096" || !strings.Contains(v.To, "CopyDataKey") {
				t.Errorf("violation %s", v)
			}
		}
		for _, want := range []string{
			"internal/webish/call.go", "internal/webish/value.go", "internal/webish/iface.go", "internal/webish/reflect.go",
			"internal/blob/keys/sub.go", "cmd/tool/main.go",
		} {
			if !got[want] {
				t.Errorf("%s not named:\n%s", want, strings.Join(Strings(vs), "\n"))
			}
		}
		if len(got) != 6 {
			t.Errorf("named %d files, want 6 (the store, internal/downloadlinks, a comment and a test file are fine):\n%s", len(got), strings.Join(Strings(vs), "\n"))
		}
	})
	t.Run("R-STOR-096 the rule holds on the current tree", func(t *testing.T) {
		root, _, _ := realTree(t)
		vs, err := CheckDataKeyCallers(root)
		if err != nil {
			t.Fatal(err)
		}
		wantClean(t, vs)
	})
}
