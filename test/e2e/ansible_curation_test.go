//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/ansiblestub"
)

// TestAnsibleCuration_R_CUR_004_WhatAnsibleGalaxyShowsOfARemoteThatDeliversOnlyWhatIsAllowed is Ansible Galaxy's one case of the
// curation of a remote repository (remote-curation.md R-CUR-022): what only the real client shows. The rules' decision, the filtered
// documents, the queue and the requests are the unit tests', the kit's and the integration area ansibleremote's. A remote of the
// counting Galaxy test upstream in mode onlyAllowed, ansible-galaxy of ansible-core 2.21, the stub's acme.tools (1.0.0, 1.1.0, 2.0.0
// with the dependency acme.base, and the pre-release 2.1.0-rc.1), collections of a few kilobytes:
//
//   - ansible-galaxy collection install acme.tools fails and prints the rules' text (R-CUR-004);
//   - after an allow of acme.tools 1.1.0 through the API, the install without a version takes 1.1.0, the newest allowed, not 2.0.0
//     (R-CUR-003);
//   - acme.tools 2.0.0 downloaded ahead through the queue (stored for examination, R-CUR-014) and then allowed with the collection (and
//     its dependency acme.base) is installed from the cache: the upstream served its archive once, to the download (R-CUR-010).
func TestAnsibleCuration_R_CUR_004_WhatAnsibleGalaxyShowsOfARemoteThatDeliversOnlyWhatIsAllowed(t *testing.T) {
	fx := ansibleFx(t)
	defer caseClock(t)()
	s := ansibleStub(t)
	repo := fx.remoteRepo(t, "curation", onlyAllowed())
	cfg := ansibleCfg(ansibleRepoURL(t, repo))
	env := tokenEnvVar + "=" + fx.token
	c := fx.client(t, ansibleCore221)
	versions := ansibleRepoPath(repo) + "/v3/collections/acme/tools/versions/?limit=100"
	// lists is the predicate of a replica that serves the filtered version list of acme.tools with the version.
	lists := func(version string) func(reply) bool {
		return func(r reply) bool {
			var d struct {
				Data []struct {
					Version string `json:"version"`
				} `json:"data"`
			}
			if r.Status != 200 || json.Unmarshal(r.Body, &d) != nil {
				return false
			}
			for _, v := range d.Data {
				if v.Version == version {
					return true
				}
			}
			return false
		}
	}
	// archives is the number of times the upstream delivered the archive of a version of acme.tools.
	archives := func(version string) int {
		n := 0
		for _, e := range s.ansibleCounts(t).Entries {
			if e.Kind == ansiblestub.ClassArtifact && e.Method == http.MethodGet && strings.HasSuffix(e.Path, "/acme-tools-"+version+".tar.gz") {
				n += e.Count
			}
		}
		return n
	}
	install := "ansible-galaxy collection install acme.tools -p coll >/dev/null\n" + manifestVersions("acme.tools")

	t.Run("R-CUR-004 ansible-galaxy collection install of a collection the remote does not deliver fails and prints the rules' text", func(t *testing.T) {
		start := time.Now()
		out, err := c.sh(withCfg(cfg, "ansible-galaxy collection install acme.tools -p coll"), env)
		noteCuration(t, out, clientCuration{Client: "ansible-galaxy collection install (ansible-core 2.21)", Printed: firstLineWith(out, "is not allowed in"), Exit: exitOf(err), Seconds: time.Since(start).Seconds()})
		if err == nil || !strings.Contains(out, "is not allowed in "+qn(repo)) {
			t.Fatalf("ansible-galaxy collection install of a collection the rules refuse: %v, want a failure with the rules' text:\n%s", err, tailLines(out, 20))
		}
	})

	t.Run("R-CUR-003 after an allow of acme.tools 1.1.0 the install without a version takes it, not the newer 2.0.0", func(t *testing.T) {
		allowRule(t, fx.adm, repo, "acme.tools", "1.1.0")
		awaitOnEveryPackagesReplica(t, fx.token, "the versions of acme.tools list 1.1.0", versions, lists("1.1.0"))
		if out := c.must(withCfg(cfg, install), env); !strings.Contains(out, "acme.tools 1.1.0") {
			t.Fatalf("the installed version after the allow of 1.1.0:\n%s", out)
		}
	})

	t.Run("R-CUR-010 R-CUR-014 acme.tools 2.0.0 downloaded ahead and then allowed is installed from the cache", func(t *testing.T) {
		before := archives("2.0.0")
		downloadAhead(t, fx.adm, repo, "acme.tools", "2.0.0")
		if n := archives("2.0.0") - before; n != 1 {
			t.Fatalf("the download ahead asked the upstream %d times for the archive of acme.tools 2.0.0, want once", n)
		}
		allowRule(t, fx.adm, repo, "acme.tools", "")
		allowRule(t, fx.adm, repo, "acme.base", "")
		awaitOnEveryPackagesReplica(t, fx.token, "the versions of acme.tools list 2.0.0", versions, lists("2.0.0"))
		if out := c.must(withCfg(cfg, install), env); !strings.Contains(out, "acme.tools 2.0.0") {
			t.Fatalf("the installed version after the allow of the collection, want the newest allowed 2.0.0:\n%s", out)
		}
		if n := archives("2.0.0") - before; n != 1 {
			t.Fatalf("the upstream served the archive of acme.tools 2.0.0 %d times, want once (the download ahead): the install did not come from the cache", n)
		}
	})
}
