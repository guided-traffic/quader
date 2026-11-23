//go:build integration

package mirror

import (
	"testing"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_CUR_017_AMirrorStoresWhatItsScopeAndTheRulesBothAdmit: a mirror of the npm stub with the scope remote.mirror.include
// ["@acme/*"] and the mode onlyAllowed with "package @acme/a allow" stores @acme/a only; adding "package @acme/b allow" changes the
// rules' revision, which the state's fingerprint covers, so the next pass is full and stores @acme/b; a version the stub publishes after
// that is stored by the next incremental pass (remote-curation.md R-CUR-017). Data: three packages of one version each, a few kilobytes.
func TestR_CUR_017_AMirrorStoresWhatItsScopeAndTheRulesBothAdmit(t *testing.T) {
	r := newRig(t, rigOptions{public: true, include: []string{"@acme/*"}, remote: map[string]any{"delivery": testenv.OnlyAllowed()}})
	r.st.DeliveryRule(repoKey, testenv.PackageRule("@acme/a", "allow"))
	r.publish("@acme/a", "1.0.0")
	r.publish("@acme/b", "1.0.0")
	run, web := r.runner("runner"), r.web("web")
	t0 := r.pass(run)
	r.waitPass(t0, "full", "complete", passWithin)

	t.Run("R-CUR-017 the pass stores the allowed package of the scope and asks nothing of the other", func(t *testing.T) {
		if !r.storedItem(tarballPath("@acme/a", "1.0.0")) {
			t.Error("@acme/a is not stored")
		}
		if r.storedItem(tarballPath("@acme/b", "1.0.0")) {
			t.Error("@acme/b is stored although no rule allows it")
		}
		if n := r.stub.Counts().Documents("@acme/b") + r.stub.Counts().Tarballs("@acme/b"); n != 0 {
			t.Errorf("%d requests for @acme/b, which the rules do not deliver", n)
		}
	})
	t.Run("R-CUR-017 an allow added makes the next pass full and stores the package", func(t *testing.T) {
		r.st.MustPutDeliveryRule(web, r.admin, repoKey, testenv.PackageRule("@acme/b", "allow"))
		r.waitDelivered(run, "@acme/b")
		at := r.pass(run)
		r.waitPass(at, "full", "complete", passWithin)
		if !r.storedItem(tarballPath("@acme/b", "1.0.0")) {
			t.Error("@acme/b is not stored after its allow")
		}
	})
	t.Run("R-CUR-017 a version published after a complete pass is stored by the next incremental pass", func(t *testing.T) {
		r.publish("@acme/a", "1.1.0")
		at := r.pass(run)
		r.waitPass(at, "incremental", "complete", passWithin)
		if !r.storedItem(tarballPath("@acme/a", "1.1.0")) {
			t.Error("@acme/a 1.1.0 is not stored")
		}
	})
}
