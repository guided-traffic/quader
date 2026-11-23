//go:build e2e

package e2e

import (
	"crypto/sha1" //nolint:gosec // dist.shasum is a SHA-1
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// npmRetried matches the HTTP log line npm writes for each attempt answered 429 (--loglevel http): "attempt 1 failed with 429".
var npmRetried = regexp.MustCompile(`attempt [0-9]+ failed with 429`)

// TestNPMLimits_R_LIM_020_WhatNpmShowsAtARefusal is npm's one case of the download limits (R-LIM-039): how the client reacts to a
// refusal. The figures themselves are the integration set's. Each step has a repository of its own with a rule for every technical
// token (the row's token is one): a request figure of one a second, whose 429 carries Retry-After 1, and a byte figure of one byte an
// hour, which refuses the tarball after the package document. Payloads of a few hundred bytes.
//
// npm retries a 429 twice with its own back-off and does not read Retry-After (measured with its defaults on 2026-11-20: the refused
// tarball failed after 70 s, 10 s and 60 s of back-off). The case shortens the back-off to one and two seconds, so that it shows the
// same attempts in seconds; what the defaults cost is in the manual's table.
func TestNPMLimits_R_LIM_020_WhatNpmShowsAtARefusal(t *testing.T) {
	fx := npmFx(t)
	c := newNPMClient(t, fx, "npm11", 0)
	name := "lim-" + fx.run
	repo := func(t *testing.T, purpose string, figures map[string]any) string {
		key := "npm-limits-" + purpose + "-" + fx.run
		fx.adm.do(http.MethodPost, reposRoute, map[string]any{"key": key, "class": "local", "format": "npm"}).want(t, "create "+key, 201)
		t.Cleanup(func() { deleteRepository(t, fx.adm, key) })
		ensurePolicy(t, fx.adm, key, []string{key}, [2]any{"sa:" + npmPublisher, []string{"read"}})
		npmPublishAs(t, fx.adm, key, name, "1.0.0")
		awaitReadableOnEveryReplica(t, c.token, key)
		limitRule(t, fx.adm, key, "technicalToken", figures)
		return key
	}
	install := func(t *testing.T, key string) npmRun {
		dir := c.project("limits-"+key, map[string]any{"dependencies": map[string]string{name: "1.0.0"}})
		base := "//localhost:" + edgePort(t) + npmAddr(key) + "/"
		c.write(dir+"/.npmrc", "registry=https:"+base+"\n"+base+":_authToken=${QUADER_TOKEN}\n")
		// a cache of the step's own, so npm asks the registry; npm's back-off shortened (see above)
		return c.exec(dir, []string{"npm_config_cache=" + dir + "/cache", "npm_config_fetch_retry_mintimeout=1000", "npm_config_fetch_retry_maxtimeout=2000"},
			"npm11", "install", "--no-audit", "--no-fund", "--loglevel", "http")
	}

	t.Run("R-LIM-020 requestsPerSecond: npm retries the 429 and the install succeeds", func(t *testing.T) {
		key := repo(t, "rps", map[string]any{"requestsPerSecond": 1, "requestsBurst": 1})
		r := install(t, key)
		retries := len(npmRetried.FindAllString(r.Out, -1))
		noteRefusal(t, r.Out, clientRefusal{
			Client: "npm 11 install", Figure: "requestsPerSecond", Printed: firstLineWith(r.Out, "failed with 429"),
			Exit: r.Code, Seconds: r.Dur.Seconds(), Retried: retries > 0,
		})
		if r.Code != 0 || retries == 0 {
			t.Fatalf("npm install at one request a second exited %d after %s with %d answers 429, want a retry and success:\n%s", r.Code, r.Dur, retries, tailLines(r.Out, 30))
		}
	})

	t.Run("R-LIM-021 cachedBytesPerHour: npm retries the refused tarball twice, prints E429 and exits 1", func(t *testing.T) {
		key := repo(t, "bytes", map[string]any{"cachedBytesPerHour": 1})
		// the package document counts its bytes but is never refused by a byte figure (R-LIM-007): after it the hour is used up
		newAPI(t).withToken(c.token).get(npmAddr(key)+"/"+name).want(t, "the package document", 200)
		awaitFlush(t) // the bytes reach the window at the next round of the charge buffer
		r := install(t, key)
		attempts := len(npmRetried.FindAllString(r.Out, -1))
		noteRefusal(t, r.Out, clientRefusal{
			Client: "npm 11 install", Figure: "cachedBytesPerHour", Printed: firstLineWith(r.Out, "npm error 429"),
			Exit: r.Code, Seconds: r.Dur.Seconds(), Retried: attempts > 1,
		})
		if r.Code != 1 || !strings.Contains(r.Out, "npm error code E429") || attempts != 3 {
			t.Fatalf("npm install with the hour's bytes used up exited %d with %d attempts answered 429, want exit 1 after three:\n%s", r.Code, attempts, tailLines(r.Out, 30))
		}
	})
}

// npmPublishAs publishes a package of a few hundred bytes the way libnpmpublish does (a PUT of the document with the tarball attached).
func npmPublishAs(t testing.TB, a *api, key, name, version string) {
	t.Helper()
	tgz := npmTarball(t, name, version, nil)
	s512 := sha512.Sum512(tgz)
	s1 := sha1.Sum(tgz) //nolint:gosec // dist.shasum is a SHA-1
	file := name + "-" + version + ".tgz"
	doc := map[string]any{
		"_id": name, "name": name, "dist-tags": map[string]string{"latest": version}, "access": nil,
		"versions": map[string]any{version: map[string]any{
			"name": name, "version": version, "_id": name + "@" + version,
			"dist": map[string]any{
				"integrity": "sha512-" + base64.StdEncoding.EncodeToString(s512[:]), "shasum": hex.EncodeToString(s1[:]),
				"tarball": "http://x/" + name + "/-/" + file,
			},
		}},
		"_attachments": map[string]any{file: map[string]any{"content_type": "application/octet-stream", "data": base64.StdEncoding.EncodeToString(tgz), "length": len(tgz)}},
	}
	a.do(http.MethodPut, npmAddr(key)+"/"+name, doc).want(t, "publish "+name+" to "+key, 200, 201)
}
