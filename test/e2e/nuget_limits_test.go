//go:build e2e

package e2e

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/nugetstub"
)

// TestNuGetLimits_R_LIM_020_WhatDotnetShowsAtARefusal is NuGet's one case of the download limits (R-LIM-039): how dotnet restore reacts
// to a refusal. The figures themselves are the integration set's. Each step has a repository of its own with a rule for every technical
// token (the publisher's token is one): a request figure of one a second, whose 429 carries Retry-After 1, and a byte figure of one byte
// an hour, which refuses the package after its version list. One package of about a kilobyte.
func TestNuGetLimits_R_LIM_020_WhatDotnetShowsAtARefusal(t *testing.T) {
	fx := nugetFx(t)
	id := "E2e.Limits" + fx.run
	pkg := nugetstub.Nupkg(&nugetstub.Package{ID: id, Description: "a package of the limits' case"}, nugetstub.Version{Version: "1.0.0", Listed: true})
	repo := func(t *testing.T, purpose string, figures map[string]any) string {
		key := "nuget-limits-" + purpose + "-" + fx.run
		fx.adm.do(http.MethodPost, reposRoute, map[string]any{"key": key, "class": "local", "format": "nuget"}).want(t, "create "+key, 201)
		t.Cleanup(func() { deleteRepository(t, fx.adm, key) })
		ensurePolicy(t, fx.adm, key, []string{key}, [2]any{"sa:" + nugetPubSA, []string{"read"}})
		nugetPushAs(t, fx.adm, key, pkg)
		awaitPathOnEveryReplica(t, fx.token, nugetAddr(key)+"/v3/index.json")
		limitRule(t, fx.adm, key, "technicalToken", figures)
		return key
	}
	restore := func(t *testing.T, key string) (string, int, time.Duration) {
		c := fx.client(t, 0, "limits-"+key)
		c.config(nugetFeed(t, "localhost", key), true)
		proj := c.consumer("consumer", id, "1.0.0")
		start := time.Now()
		out, err := c.try("dotnet", "restore", proj, "--nologo")
		return out, exitOf(err), time.Since(start)
	}

	// dotnet retries a 429 about a second apart without reading Retry-After; a refused request is in no window (R-LIM-017), so a retry a
	// second later is admitted and the restore succeeds (measured 2026-11-20: two retries, 5.5 s)
	t.Run("R-LIM-020 requestsPerSecond: dotnet restore retries the 429 and succeeds", func(t *testing.T) {
		key := repo(t, "rps", map[string]any{"requestsPerSecond": 1, "requestsBurst": 1})
		out, code, d := restore(t, key)
		retries := strings.Count(out, "Retrying '")
		noteRefusal(t, out, clientRefusal{
			Client: "dotnet restore", Figure: "requestsPerSecond", Printed: firstLineWith(out, "Retrying '"),
			Exit: code, Seconds: d.Seconds(), Retried: retries > 0,
		})
		if code != 0 || retries == 0 {
			t.Fatalf("dotnet restore at one request a second exited %d after %s with %d retries, want a retry of the 429 and success:\n%s", code, d, retries, tailLines(out, 30))
		}
	})

	t.Run("R-LIM-021 cachedBytesPerHour: the package is refused and dotnet restore fails with 429", func(t *testing.T) {
		key := repo(t, "bytes", map[string]any{"cachedBytesPerHour": 1})
		// the version list counts its bytes but is never refused by a byte figure (R-LIM-007): after it the hour is used up
		newAPI(t).withToken(fx.token).get(nugetAddr(key)+"/v3/flatcontainer/"+strings.ToLower(id)+"/index.json").want(t, "the version list", 200)
		awaitFlush(t) // the bytes reach the window at the next round of the charge buffer
		out, code, d := restore(t, key)
		noteRefusal(t, out, clientRefusal{
			Client: "dotnet restore", Figure: "cachedBytesPerHour", Printed: firstLineWith(out, "429"),
			Exit: code, Seconds: d.Seconds(), Retried: strings.Count(out, "429") > 1,
		})
		if code == 0 || !strings.Contains(out, "429") {
			t.Fatalf("dotnet restore with the hour's bytes used up exited %d:\n%s", code, tailLines(out, 30))
		}
	})
}

// nugetPushAs pushes a package through the publish resource of a feed (R-NUGET-018).
func nugetPushAs(t testing.TB, a *api, key string, pkg []byte) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="package"; filename="package"`)
	h.Set("Content-Type", "application/octet-stream")
	part, _ := mw.CreatePart(h)
	_, _ = part.Write(pkg)
	_ = mw.Close()
	a.do(http.MethodPut, nugetAddr(key)+"/v3/package", buf.Bytes(), "Content-Type", mw.FormDataContentType()).want(t, "push to "+key, 201, 202)
}
