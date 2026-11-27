//go:build integration

package deblocal

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/packages/view"
	"github.com/guided-traffic/quader/test/debstub/debgen"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

const docsCopyright = "Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/\n\nFiles: *\nCopyright: 2026 Quader\n" +
	"License: Apache-2.0\n\nLicense: Apache-2.0\n The text.\n"

// TestR_DEB_038_039_AnUploadedPackageShowsWhatItTellsAboutItself: the version of an uploaded package answers its relations parsed and
// its long description in paragraphs (R-DEB-038); the job the upload queued reads the data archive, so the contents operation answers
// the changelog, the copyright with its licence (which is the version's licence too) and the file list, filtered (R-DEB-039). The
// changelog and the copyright are wiki content: a reader of the artifacts who may not read the wiki gets the file list without them. A
// name the package provides finds it, and the source package lists its binaries. A file whose data archive the reader refuses is stored,
// served and listed, and its contents say why to who reads the configuration only.
func TestR_DEB_038_039_AnUploadedPackageShowsWhatItTellsAboutItself(t *testing.T) {
	e := newEnvOpts(t, envOpts{seed: func(e *env) {
		e.st.Exec(`UPDATE project SET visibility = 'internal' WHERE key = $1`, project)
		e.st.Exec(`UPDATE repositories SET visibility_artifacts = 'internal', visibility_wiki = 'private' WHERE key = $1`, repo)
	}})
	body := build(t, "quader-docs", "1.2.0-1", func(s *debgen.Spec) {
		s.Source = "quader-suite"
		s.Depends = "libc6 (>= 2.34), python3:any | python3-minimal"
		s.Provides = "quader-doc-reader"
		s.LongDescription = []string{"The first paragraph.", "", "A list:", " * one", " * two"}
		s.Changelog = "quader-docs (1.2.0-1) unstable; urgency=medium\n\n  * Read the data archive.\n\n -- Q <q@example.org>  Thu, 01 Jan 2026 00:00:00 +0000\n"
		s.Copyright = docsCopyright
		s.Files = []string{"/usr/share/quader-docs/a.txt", "/usr/share/quader-docs/b.txt"}
	})
	e.mustUpload(e.tokA, "quader-docs_1.2.0-1_amd64.deb", body, http.StatusCreated, []string{"stable"}, "main")
	// A package whose data archive is no tar: stored and listed, its contents refused.
	broken := brokenData(t, "quader-broken", "1.0")
	e.mustUpload(e.tokA, "quader-broken_1.0_amd64.deb", broken, http.StatusCreated, []string{"stable"}, "main")

	id := view.ID("deb", "quader-docs")
	// The management API's requests go to a replica of the role api, which serves no package protocol and builds no format host
	// (R-ARCH-001, Q-ARCH-1070).
	apiRep := e.st.Start(t, testenv.ReplicaOptions{Name: "api-a", Role: "api"})
	ver := e.st.Do(apiRep, "GET", "/api/v1/packages/"+url.PathEscape(id)+"/versions/1.2.0-1", e.tokA, nil)
	if ver.Status != http.StatusOK {
		t.Fatalf("the version: %d %s", ver.Status, ver.Body)
	}
	md, _ := ver.JSON(t)["metadata"].(map[string]any)
	rel, _ := md["relations"].(map[string]any)
	depends, _ := rel["depends"].([]any)
	if len(depends) != 2 || !strings.Contains(jsonOf(t, depends[1]), `{"arch":"any","name":"python3"}`) ||
		!strings.Contains(jsonOf(t, depends[1]), `{"name":"python3-minimal"}`) {
		t.Errorf("depends %s", jsonOf(t, rel["depends"]))
	}
	if long := jsonOf(t, md["longDescription"]); !strings.Contains(long, `"verbatim":true`) || !strings.Contains(long, "The first paragraph.") {
		t.Errorf("longDescription %s", long)
	}
	// Who uses what (api-and-ui.md R-API-086, Q-API-457): a package that depends on a name quader-docs provides uses quader-docs, and the
	// upload of quader-docs uses python3 (its qualifier ":any" dropped) or python3-minimal.
	viewer := build(t, "quader-viewer", "1.0", func(s *debgen.Spec) { s.Depends = "quader-doc-reader" })
	e.mustUpload(e.tokA, "quader-viewer_1.0_amd64.deb", viewer, http.StatusCreated, []string{"stable"}, "main")
	// Each member on its own: the answer carries more members (the malware state since W-36's P3) between them.
	for _, c := range []struct {
		of   string
		want []string
	}{
		{"quader-docs", []string{`"key":"quader-viewer"`, `"name":"quader-viewer","packageId":"` + view.ID("deb", "quader-viewer") + `","relations":[{"kind":"Depends","range":"","via":"quader-doc-reader"}],"version":"1.0"}`}},
		{"python3", []string{`"key":"quader-docs"`, `"name":"quader-docs","packageId":"` + view.ID("deb", "quader-docs") + `","relations":[{"kind":"Depends","range":"","via":"python3"}],"version":"1.2.0-1"}`}},
	} {
		a := e.st.Do(apiRep, "GET", "/api/v1/packages/"+view.ID("deb", c.of)+"/dependents", e.tokA, nil)
		ok := a.Status == http.StatusOK
		for _, w := range c.want {
			ok = ok && strings.Contains(string(a.Body), w)
		}
		if !ok {
			t.Errorf("the users of %s: %d %s", c.of, a.Status, a.Body)
		}
	}
	if a := e.st.Do(apiRep, "GET", "/api/v1/packages?format=deb&dependsOn=quader-docs", e.tokA, nil); a.Status != http.StatusOK ||
		!strings.Contains(string(a.Body), `"name":"quader-viewer"`) || strings.Contains(string(a.Body), `"name":"quader-docs"`) {
		t.Errorf("the list filtered by dependsOn=quader-docs: %d %s", a.Status, a.Body)
	}

	path := "/api/v1/packages/" + url.PathEscape(id) + "/versions/1.2.0-1/contents"
	var c map[string]any
	eventually(t, 60*time.Second, "the data archive read", func() bool {
		a := e.st.Do(apiRep, "GET", path, e.tokA, nil)
		if a.Status != http.StatusOK {
			t.Fatalf("contents: %d %s", a.Status, a.Body)
		}
		c = a.JSON(t)
		return c["state"] == "available"
	})
	if !strings.Contains(jsonOf(t, c["changelog"]), "Read the data archive.") || !strings.Contains(jsonOf(t, c["copyright"]), `"licenses":["Apache-2.0"]`) {
		t.Errorf("changelog %s, copyright %s", jsonOf(t, c["changelog"]), jsonOf(t, c["copyright"]))
	}
	files, _ := c["files"].(map[string]any)
	if files["total"] != float64(5) || !strings.Contains(jsonOf(t, files["items"]), `"path":"/usr/bin/quader-docs"`) {
		t.Errorf("files %s", jsonOf(t, files))
	}
	filtered := e.st.Do(apiRep, "GET", path+"?q=B.TXT&limit=1", e.tokA, nil).JSON(t)
	if ff, _ := filtered["files"].(map[string]any); ff["matched"] != float64(1) || !strings.Contains(jsonOf(t, ff["items"]), "b.txt") {
		t.Errorf("filtered %s", jsonOf(t, filtered["files"]))
	}
	if lic := e.st.Do(apiRep, "GET", "/api/v1/packages/"+url.PathEscape(id)+"/versions/1.2.0-1", e.tokA, nil).JSON(t)["licenses"]; jsonOf(t, lic) != `["Apache-2.0"]` {
		t.Errorf("the version's licences %v", lic)
	}

	outsider := e.st.Token(e.st.User("outsider"), scope.Default())
	o := e.st.Do(apiRep, "GET", path, outsider, nil)
	if o.Status != http.StatusOK || strings.Contains(string(o.Body), `"changelog":`) || strings.Contains(string(o.Body), `"copyright":`) ||
		!strings.Contains(string(o.Body), "/usr/bin/quader-docs") {
		t.Errorf("a reader without the wiki: %d %s", o.Status, o.Body)
	}

	// Provides makes the name findable, and the source package lists its binaries.
	for _, q := range []string{"q=quader-doc-reader", "keyword=quader-suite"} {
		list := e.st.Do(apiRep, "GET", "/api/v1/packages?format=deb&"+q, e.tokA, nil)
		if !strings.Contains(string(list.Body), `"name":"quader-docs"`) || strings.Contains(string(list.Body), "quader-broken") {
			t.Errorf("%s: %s", q, list.Body)
		}
	}

	brokenPath := "/api/v1/packages/" + url.PathEscape(view.ID("deb", "quader-broken")) + "/versions/1.0/contents"
	eventually(t, 60*time.Second, "the refusal", func() bool {
		return e.st.Do(apiRep, "GET", brokenPath, e.tokA, nil).JSON(t)["state"] == "unavailable"
	})
	if b := e.st.Do(apiRep, "GET", brokenPath, e.tokAdmin, nil).JSON(t); b["reason"] != "invalid_archive" || b["detail"] == nil {
		t.Errorf("the reason to an administrator: %v", b)
	}
	if b := e.st.Do(apiRep, "GET", brokenPath, e.tokA, nil).JSON(t); b["reason"] != nil {
		t.Errorf("the reason to a writer who does not read the configuration: %v", b)
	}
	if a := e.st.Do(e.r, "GET", "/api/deb/"+project+"/"+repo+"/pool/q/quader-broken/quader-broken_1.0_amd64.deb", e.tokA, nil); a.Status != http.StatusOK {
		t.Errorf("the refused file is served: %d", a.Status)
	}

	// A file stored before the reader existed has no row: the task remote-describe reads it without an upload.
	e.st.Exec(`DELETE FROM deb_contents`)
	if a := e.st.Do(apiRep, "GET", path, e.tokA, nil).JSON(t); a["state"] != "pending" {
		t.Errorf("without its row: %v", a["state"])
	}
	// A run the scheduler queued on the new database answers the operator's run with 409; either run reads the files.
	if a := e.st.Do(apiRep, http.MethodPost, "/api/v1/system/tasks/remote-describe/run", e.tokAdmin, nil); a.Status != http.StatusAccepted &&
		a.Status != http.StatusConflict {
		t.Fatalf("run remote-describe: %d %s", a.Status, a.Body)
	}
	// Three files: quader-docs, quader-broken and quader-viewer.
	eventually(t, 60*time.Second, "the files read again", func() bool { return e.st.Number(`SELECT count(*) FROM deb_contents`) == 3 })
}

// brokenData is a package file whose data archive is gzip but no tar.
func brokenData(t *testing.T, name, version string) []byte {
	t.Helper()
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	_, _ = w.Write([]byte(strings.Repeat("no tar here ", 100)))
	_ = w.Close()
	ctl := debgen.ControlText(debgen.Hello(name, version, "amd64"), 1)
	return arArchive(
		arMember{"debian-binary", []byte("2.0\n")},
		arMember{"control.tar.gz", tarGz(t, "./control", ctl)},
		arMember{"data.tar.gz", gz.Bytes()},
	)
}

func eventually(t *testing.T, within time.Duration, what string, ok func() bool) {
	t.Helper()
	testenv.WaitFor(t, what, within, ok)
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
