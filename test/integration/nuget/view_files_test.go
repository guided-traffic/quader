//go:build integration

package nuget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/audit"
	nugetdata "github.com/guided-traffic/quader/internal/formatdata/nuget"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// png is the smallest file that starts like a PNG: the icon route decides the type by these bytes alone.
const png = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"

// richPackage is a library with every fact R-NUGET-038 and R-NUGET-039 read: an icon, a licence file, the repository's branch and
// commit, release notes, folders of two frameworks, a dependency group of a third, build logic, an analyzer and an install script.
func richPackage(t *testing.T, id, icon string) []byte {
	t.Helper()
	return nupkg(t, pkgSpec{
		id: id, version: "1.0.0",
		metadata: `<icon>images/icon.png</icon><license type="file">LICENSE.txt</license>` +
			`<repository type="git" url="https://github.com/acme/rich.git" branch="refs/heads/main" commit="0123456789abcdef"/>` +
			`<releaseNotes>One` + "\n" + `Two</releaseNotes>` +
			`<dependencies><group targetFramework=".NETFramework4.7.2"><dependency id="Contoso.Util" version="1.0.0"/></group></dependencies>`,
		entries: map[string]string{
			"images/icon.png": icon, "LICENSE.txt": "MIT License\n<b>not markup</b>\n", "lib/netstandard2.0/" + id + ".dll": "MZ",
			"build/" + id + ".targets": "<Project/>", "analyzers/dotnet/cs/" + id + ".Generators.dll": "MZ", "tools/install.ps1": "param()",
		},
	})
}

// TestR_NUGET_038_TheFactsAndTheEmbeddedFilesOnAReplicaOfTheRoleAPI: what the screens read of a NuGet version is answered by a replica of
// the role api, which serves no package protocol (R-ARCH-001, Q-ARCH-1070): the facts of the file list, the licence file and the icon out
// of the stored package, each under the read check with the headers of untrusted content, an icon that is no PNG or JPEG and a licence
// that is no text refused as absent, the kind of a .NET tool in the list, and the install forms of its kind (R-NUGET-037). Cost: three
// pushes of a few hundred bytes and some fifteen requests against two replicas, about a second.
func TestR_NUGET_038_TheFactsAndTheEmbeddedFilesOnAReplicaOfTheRoleAPI(t *testing.T) {
	e := newWriteEnv(t)
	a := e.replica("web-a")
	m := e.st.Start(t, testenv.ReplicaOptions{Name: "api-a", Role: "api"})
	e.mustPush(a, "nuget-local", e.tokDev, richPackage(t, "Acme.Rich", png))
	e.mustPush(a, "nuget-local", e.tokDev, richPackage(t, "Acme.Svg", `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	e.mustPush(a, "nuget-local", e.tokDev, nupkg(t, pkgSpec{
		id: "Acme.Tool", version: "2.0.0", metadata: `<packageTypes><packageType name="DotnetTool"/></packageTypes>`,
		entries: map[string]string{"tools/net8.0/any/Acme.Tool.dll": "MZ"},
	}))
	idOf := func(name string) string {
		return e.st.Do(m, "GET", "/api/v1/packages/lookup?format=nuget&name="+name, e.tokReader, nil).JSON(t)["id"].(string)
	}
	rich := "/api/v1/packages/" + idOf("Acme.Rich") + "/versions/1.0.0"

	t.Run("R-NUGET-038 R-NUGET-039 the details name the frameworks, what runs, the licence file, the icon and the revision", func(t *testing.T) {
		md, _ := e.st.Do(m, "GET", rich, e.tokReader, nil).JSON(t)["metadata"].(map[string]any)
		strs := func(k string) []string {
			var out []string
			for _, v := range md[k].([]any) {
				out = append(out, v.(string))
			}
			return out
		}
		if !slices.Equal(strs("frameworks"), []string{"net8.0", "netstandard2.0"}) || !slices.Equal(strs("dependencyFrameworks"), []string{".NETFramework4.7.2"}) {
			t.Errorf("frameworks %v %v", md["frameworks"], md["dependencyFrameworks"])
		}
		if !slices.Equal(strs("buildFiles"), []string{"build/Acme.Rich.targets"}) || !slices.Equal(strs("analyzers"), []string{"analyzers/dotnet/cs/Acme.Rich.Generators.dll"}) ||
			!slices.Equal(strs("scripts"), []string{"tools/install.ps1"}) {
			t.Errorf("what runs: %v %v %v", md["buildFiles"], md["analyzers"], md["scripts"])
		}
		if md["licenseFile"] != "LICENSE.txt" || md["hasIcon"] != true || md["repositoryCommit"] != "0123456789abcdef" || md["repositoryBranch"] != "refs/heads/main" ||
			md["releaseNotes"] != "One\nTwo" {
			t.Errorf("members %v", md)
		}
	})
	t.Run("R-NUGET-038 the icon is a PNG by its bytes, kept for the reader alone and checked again each time", func(t *testing.T) {
		ans := e.st.Do(m, "GET", rich+"/icon", e.tokReader, nil)
		h := ans.Header
		if ans.Status != 200 || h.Get("Content-Type") != "image/png" || h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Cache-Control") != "private, no-cache" ||
			!strings.Contains(h.Get("Content-Security-Policy"), "sandbox") || string(ans.Body) != png {
			t.Fatalf("%d %v %q", ans.Status, h, ans.Body)
		}
		if again := e.st.Do(m, "GET", rich+"/icon", e.tokReader, nil, "If-None-Match", h.Get("ETag")); again.Status != 304 {
			t.Errorf("a matching tag: %d", again.Status)
		}
		if who := e.st.Do(m, "GET", rich+"/icon", e.tokNobody, nil, "If-None-Match", h.Get("ETag")); who.Status != 404 {
			t.Errorf("a caller who may not read the version, with the tag of one who may: %d", who.Status)
		}
		svg := e.st.Do(m, "GET", "/api/v1/packages/"+idOf("Acme.Svg")+"/versions/1.0.0/icon", e.tokReader, nil)
		if svg.Status != 404 || bytes.Contains(svg.Body, []byte("<script>")) {
			t.Errorf("an SVG icon: %d %s", svg.Status, svg.Body)
		}
	})
	t.Run("R-NUGET-038 the licence file is text, never markup", func(t *testing.T) {
		ans := e.st.Do(m, "GET", rich+"/license", e.tokReader, nil)
		if ans.Status != 200 || ans.Header.Get("Content-Type") != "text/plain; charset=utf-8" || ans.Header.Get("X-Content-Type-Options") != "nosniff" ||
			string(ans.Body) != "MIT License\n<b>not markup</b>\n" {
			t.Errorf("%d %v %q", ans.Status, ans.Header, ans.Body)
		}
		tool := "/api/v1/packages/" + idOf("Acme.Tool") + "/versions/2.0.0/license"
		if none := e.st.Do(m, "GET", tool, e.tokReader, nil); none.Status != 404 {
			t.Errorf("a version without a licence file: %d", none.Status)
		}
	})
	t.Run("R-NUGET-037 a .NET tool's kind is in the list, and its client's forms are those of its kind", func(t *testing.T) {
		l := e.st.Do(m, "GET", "/api/v1/packages?format=nuget&q=Acme.Tool", e.tokReader, nil)
		if !strings.Contains(string(l.Body), `"kind":"dotnet-tool"`) {
			t.Errorf("list %s", l.Body)
		}
		s := e.st.Do(m, "GET", "/api/v1/projects/it/repositories/nuget-local/client-setup?client=dotnet&credentials=none&package=Acme.Tool&version=2.0.0&kind=dotnet-tool", e.tokReader, nil)
		if !strings.Contains(string(s.Body), "dotnet tool install --global Acme.Tool --version 2.0.0") || strings.Contains(string(s.Body), "dotnet add package") {
			t.Errorf("setup %d %s", s.Status, s.Body)
		}
	})
}

// TestR_NUGET_039_AVersionStoredBeforeTheReaderIsReadByItsJob: a version whose metadata has no file list is selected by the task
// remote-describe's NuGet entry, its job reads the names of the stored package's central directory and writes the list once, and the
// store then holds nothing left to read. Cost: one push and a ranged read of a package of a few hundred bytes, well under a second.
func TestR_NUGET_039_AVersionStoredBeforeTheReaderIsReadByItsJob(t *testing.T) {
	e := newWriteEnv(t)
	a := e.replica("web-a")
	pkg := richPackage(t, "Acme.Old", png)
	e.mustPush(a, "nuget-local", e.tokDev, pkg)
	vid := e.st.Number(`SELECT version_id FROM nuget_version WHERE lower_id = 'acme.old'`)
	e.st.Exec(`UPDATE nuget_version SET metadata = metadata - 'contents' WHERE version_id = $1`, vid)

	db, err := postgres.Open(context.Background(), e.st.DatabaseConfig(), "nuget-contents", "worker", testenv.Quiet())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	sum := sha256.Sum256(pkg)
	reg := jobs.NewRegistry()
	nugetdata.RegisterContentsJob(reg, nil)
	reg.Freeze()
	st, err := nugetdata.New(nugetdata.Options{DB: db, Recorder: audit.Nop{}, Blobs: blobBytes{string(sum[:]): pkg}, Jobs: jobs.NewQueue(jobs.QueueOptions{Registry: reg})})
	if err != nil {
		t.Fatal(err)
	}
	ctx, port := context.Background(), nugetdata.ContentsPort{S: st}
	if left, err := port.HasUndescribed(ctx); err != nil || !left {
		t.Fatalf("left %v %v", left, err)
	}
	if next, queued, done, err := port.QueueUndescribed(ctx, 0, 100, 200); err != nil || queued != 1 || !done || next != vid {
		t.Fatalf("queued %d done %v next %d (want %d): %v", queued, done, next, vid, err)
	}
	if n := e.st.Number(`SELECT count(*) FROM job WHERE kind = 'nuget-contents' AND dedup_key = 'nuget-contents:' || $1::bigint`, vid); n != 1 {
		t.Errorf("jobs %d", n)
	}
	if err := st.ReadContents(ctx, vid); err != nil {
		t.Fatal(err)
	}
	if got := e.st.Text(`SELECT metadata -> 'contents' ->> 'build' FROM nuget_version WHERE version_id = $1`, vid); got != `["build/Acme.Old.targets"]` {
		t.Errorf("build %s", got)
	}
	if left, err := port.HasUndescribed(ctx); err != nil || left {
		t.Errorf("left after the job: %v %v", left, err)
	}
}
