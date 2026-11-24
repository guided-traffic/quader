//go:build e2e

package e2e

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/test/npmstub/corpus"
)

// TestNPMCuration_R_CUR_004_WhatNpmShowsOfARemoteThatDeliversOnlyWhatIsAllowed is npm's one case of the curation of a remote repository
// (remote-curation.md R-CUR-022): what only the real client shows. The rules' decision, the filtered documents, the queue and the
// requests are the unit tests', the kit's and the integration area npm's. A remote of the counting test upstream in mode onlyAllowed
// over a package of this run with the versions 1.0.0 and 1.1.0, a few hundred bytes each:
//
//   - npm install of the package fails with E403 and prints the rules' text (R-CUR-004);
//   - after an allow of 1.0.0 through the API, npm resolves ^1.0.0 from the filtered document to 1.0.0, not to the newer 1.1.0 (R-CUR-003);
//   - 1.1.0 downloaded ahead through the queue (stored for examination: the rules do not deliver it yet, R-CUR-014) and then allowed with
//     the package is installed from the cache: the upstream served its tarball once, to the download (R-CUR-010).
func TestNPMCuration_R_CUR_004_WhatNpmShowsOfARemoteThatDeliversOnlyWhatIsAllowed(t *testing.T) {
	fx := npmFx(t)
	defer caseClock(t)()
	s := stub(t)
	c := newNPMClient(t, fx, "npm11", 0)
	name := "e2e-curation-" + fx.run
	s.publish(t, name, "1.0.0", nil)
	s.publish(t, name, "1.1.0", nil, "latest")
	repos := fx.upstreamRepos(t, "curation", onlyAllowed())
	doc := npmAddr(repos.remote) + "/" + name

	install := func(t *testing.T, label string) (npmRun, string) {
		dir := c.project("curation-"+label, map[string]any{"dependencies": map[string]string{name: "^1.0.0"}})
		base := "//localhost:" + edgePort(t) + npmAddr(repos.remote) + "/"
		c.write(dir+"/.npmrc", "registry=https:"+base+"\n"+base+":_authToken=${QUADER_TOKEN}\n")
		// a cache of the step's own, so npm asks the registry
		return c.exec(dir, []string{"npm_config_cache=" + dir + "/cache"}, "npm11", "install", "--no-audit", "--no-fund"), dir
	}
	installed := func(t *testing.T, dir string) string {
		var p struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal([]byte(c.read(dir+"/node_modules/"+name+"/package.json")), &p); err != nil {
			t.Fatalf("the installed package.json of %s: %v", name, err)
		}
		return p.Version
	}
	// lists is the predicate of a replica that serves the filtered package document with the version.
	lists := func(version string) func(reply) bool {
		return func(r reply) bool {
			var d struct {
				Versions map[string]json.RawMessage `json:"versions"`
			}
			return r.Status == 200 && json.Unmarshal(r.Body, &d) == nil && d.Versions[version] != nil
		}
	}

	t.Run("R-CUR-004 npm install of a package the remote does not deliver fails with E403 and the rules' text", func(t *testing.T) {
		r, _ := install(t, "refused")
		noteCuration(t, r.Out, clientCuration{Client: "npm 11 install", Printed: firstLineWith(r.Out, "is not allowed in"), Exit: r.Code, Seconds: r.Dur.Seconds()})
		if r.Code == 0 || !strings.Contains(r.Out, "npm error code E403") || !strings.Contains(r.Out, name+" any version is not allowed in "+qn(repos.remote)) {
			t.Fatalf("npm install of a package the rules refuse exited %d, want E403 with the rules' text:\n%s", r.Code, tailLines(r.Out, 30))
		}
		if !strings.Contains(r.Out, "/upstream?name="+name) {
			t.Errorf("npm does not print where to ask for the package:\n%s", tailLines(r.Out, 30))
		}
	})

	t.Run("R-CUR-003 after an allow of 1.0.0 npm resolves ^1.0.0 to it, not to the newer 1.1.0", func(t *testing.T) {
		allowRule(t, fx.adm, repos.remote, name, "1.0.0")
		awaitOnEveryPackagesReplica(t, c.token, "the document of "+name+" lists 1.0.0", doc, lists("1.0.0"))
		r, dir := install(t, "one-version")
		if r.Code != 0 {
			t.Fatalf("npm install after the allow exited %d:\n%s", r.Code, tailLines(r.Out, 30))
		}
		if got := installed(t, dir); got != "1.0.0" {
			t.Fatalf("npm installed %s %s, want the allowed 1.0.0", name, got)
		}
	})

	t.Run("R-CUR-010 R-CUR-014 1.1.0 downloaded ahead and then allowed is installed from the cache", func(t *testing.T) {
		file := corpus.TarballFile(name, "1.1.0")
		before := s.counts(t).Tarball(name, file)
		downloadAhead(t, fx.adm, repos.remote, name, "1.1.0")
		if n := s.counts(t).Tarball(name, file) - before; n != 1 {
			t.Fatalf("the download ahead asked the upstream %d times for %s, want once", n, file)
		}
		allowRule(t, fx.adm, repos.remote, name, "")
		awaitOnEveryPackagesReplica(t, c.token, "the document of "+name+" lists 1.1.0", doc, lists("1.1.0"))
		r, dir := install(t, "ahead")
		if r.Code != 0 {
			t.Fatalf("npm install after the allow of the package exited %d:\n%s", r.Code, tailLines(r.Out, 30))
		}
		if got := installed(t, dir); got != "1.1.0" {
			t.Fatalf("npm installed %s %s, want the newest allowed 1.1.0", name, got)
		}
		if n := s.counts(t).Tarball(name, file) - before; n != 1 {
			t.Fatalf("the upstream served %s %d times, want once (the download ahead): npm's fetch did not come from the cache", file, n)
		}
	})
}
