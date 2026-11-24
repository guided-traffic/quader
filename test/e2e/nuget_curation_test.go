//go:build e2e

package e2e

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/nugetstub"
)

// TestNuGetCuration_R_CUR_004_WhatDotnetShowsOfARemoteThatDeliversOnlyWhatIsAllowed is NuGet's one case of the curation of a remote
// repository (remote-curation.md R-CUR-022): what only the real client shows. The rules' decision, the filtered documents, the queue and
// the requests are the unit tests', the kit's and the integration area nuget's. A remote of the counting test upstream in mode
// onlyAllowed over the stub's Contoso.Util with the versions 2.0.0 and 2.1.0, packages of about a kilobyte, restored with the floating
// version 2.* (NuGet takes the highest version a floating range matches; a plain minimum takes the lowest):
//
//   - dotnet restore fails and prints the rules' text (R-CUR-004);
//   - after an allow of 2.0.0 through the API, 2.* restores 2.0.0 from the filtered version list, not the newer 2.1.0 (R-CUR-003);
//   - 2.1.0 downloaded ahead through the queue (stored for examination, R-CUR-014) and then allowed with the package is restored from
//     the cache: the upstream served its .nupkg once, to the download (R-CUR-010).
func TestNuGetCuration_R_CUR_004_WhatDotnetShowsOfARemoteThatDeliversOnlyWhatIsAllowed(t *testing.T) {
	fx := nugetFx(t)
	defer caseClock(t)()
	s := nugetStub(t)
	id := nugetstub.UtilID
	repos := fx.upstreamRepos(t, "curation", onlyAllowed())
	feed := nugetFeed(t, "localhost", repos.remote)
	list := nugetAddr(repos.remote) + "/v3/flatcontainer/" + strings.ToLower(id) + "/index.json"

	restore := func(t *testing.T, label string) (*nugetClient, string, time.Duration, error) {
		c := fx.client(t, 0, "curation-"+label)
		c.config(feed, true)
		proj := c.consumer("p", id, "2.*")
		start := time.Now()
		out, err := c.try("dotnet", "restore", proj, "--no-cache")
		return c, out, time.Since(start), err
	}
	// lists is the predicate of a replica that serves the filtered version list with the version.
	lists := func(version string) func(reply) bool {
		return func(r reply) bool {
			var d struct {
				Versions []string `json:"versions"`
			}
			return r.Status == 200 && json.Unmarshal(r.Body, &d) == nil && slices.Contains(d.Versions, version)
		}
	}

	t.Run("R-CUR-004 dotnet restore of a package the remote does not deliver fails and prints the rules' text", func(t *testing.T) {
		_, out, d, err := restore(t, "refused")
		noteCuration(t, out, clientCuration{Client: "dotnet restore", Printed: firstLineWith(out, "is not allowed in"), Exit: exitOf(err), Seconds: d.Seconds()})
		if err == nil || !strings.Contains(out, "is not allowed in "+qn(repos.remote)) {
			t.Fatalf("dotnet restore of a package the rules refuse: %v, want a failure with the rules' text:\n%s", err, tailLines(out, 30))
		}
	})

	t.Run("R-CUR-003 after an allow of 2.0.0 the floating 2.* restores it, not the newer 2.1.0", func(t *testing.T) {
		allowRule(t, fx.adm, repos.remote, id, "2.0.0")
		awaitOnEveryPackagesReplica(t, fx.token, "the version list of "+id+" lists 2.0.0", list, lists("2.0.0"))
		c, out, _, err := restore(t, "one-version")
		if err != nil {
			t.Fatalf("dotnet restore after the allow: %v\n%s", err, tailLines(out, 30))
		}
		if !c.hasPackage(id, "2.0.0") || c.hasPackage(id, "2.1.0") {
			t.Fatalf("dotnet restore of %s 2.* after the allow of 2.0.0 did not extract 2.0.0 alone:\n%s", id, tailLines(out, 20))
		}
	})

	t.Run("R-CUR-010 R-CUR-014 2.1.0 downloaded ahead and then allowed is restored from the cache", func(t *testing.T) {
		before := s.nugetCounts(t).Nupkg(id, "2.1.0")
		downloadAhead(t, fx.adm, repos.remote, id, "2.1.0")
		if n := s.nugetCounts(t).Nupkg(id, "2.1.0") - before; n != 1 {
			t.Fatalf("the download ahead asked the upstream %d times for the .nupkg of %s 2.1.0, want once", n, id)
		}
		allowRule(t, fx.adm, repos.remote, id, "")
		awaitOnEveryPackagesReplica(t, fx.token, "the version list of "+id+" lists 2.1.0", list, lists("2.1.0"))
		c, out, _, err := restore(t, "ahead")
		if err != nil {
			t.Fatalf("dotnet restore after the allow of the package: %v\n%s", err, tailLines(out, 30))
		}
		if !c.hasPackage(id, "2.1.0") {
			t.Fatalf("dotnet restore of %s 2.* did not extract the newest allowed 2.1.0:\n%s", id, tailLines(out, 20))
		}
		if n := s.nugetCounts(t).Nupkg(id, "2.1.0") - before; n != 1 {
			t.Fatalf("the upstream served the .nupkg of %s 2.1.0 %d times, want once (the download ahead): the restore did not come from the cache", id, n)
		}
	})
}
