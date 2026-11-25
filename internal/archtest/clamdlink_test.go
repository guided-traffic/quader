package archtest

import (
	"strings"
	"testing"
)

// TestR_MAL_007_TheProductReachesClamdByTheSocketOfThePodOnly: the malware scanner reaches clamd over the Unix socket of its pod and
// nothing else. The owner's condition of 2026-11-25, when ClamAV was to run on his development machine: "Ich will dass davon nichts in
// die ausgelieferten Images wandert und ich will keinen neuen offenen Port im produktiven quader." The UI development profile runs the
// chart's pod on Docker for it (Q-ARCH-1065); this test keeps the product from growing a TCP link to clamd instead. The fixture names a
// dial of the client's tree over TCP and one over a network from the configuration, a call of SetDialer and a command of clamd's
// protocol outside the client; the dial of the socket, the protocol inside the client's tree, a TCP dial that is not clamd's and a tool
// of the repository pass. Seconds: the typed load of the module, shared with the other checks of the package.
func TestR_MAL_007_TheProductReachesClamdByTheSocketOfThePodOnly(t *testing.T) {
	t.Run("the fixture", func(t *testing.T) {
		pkgs, err := LoadTyped(fixtureDir(t, "clamdlink"))
		if err != nil {
			t.Fatal(err)
		}
		vs := CheckClamdLink("example.test/fix", pkgs)
		got := map[string]bool{}
		for _, v := range vs {
			got[v.From+" "+v.To] = true
			if v.Rule != clamdLinkRule {
				t.Errorf("violation %s", v)
			}
		}
		for _, want := range []string{
			"internal/malware/clamd/clamd.go DialTimeout",
			"internal/malware/clamd/clamd.go Dial",
			"internal/malware/scanner/scanner.go example.test/fix/internal/malware/clamd.SetDialer",
			"internal/malware/scanner/scanner.go INSTREAM",
		} {
			if !got[want] {
				t.Errorf("%s not named:\n%s", want, strings.Join(Strings(vs), "\n"))
			}
		}
		if len(vs) != 4 {
			t.Errorf("%d violations, want the four above (New's dial of the socket, the protocol inside the client's tree, the TCP dial of "+
				"cmd/q and the tool are none):\n%s", len(vs), strings.Join(Strings(vs), "\n"))
		}
	})
	t.Run("the current tree", func(t *testing.T) {
		root, module, _ := realTree(t)
		pkgs, err := LoadTyped(root)
		if err != nil {
			t.Fatal(err)
		}
		wantClean(t, CheckClamdLink(module, pkgs))
	})
}
