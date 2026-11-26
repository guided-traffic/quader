//go:build integration

package audit

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha1" //nolint:gosec // npm's shasum is SHA-1; the publish carries it
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/scope"
	_ "github.com/guided-traffic/quader/internal/formats/all"
	"github.com/guided-traffic/quader/internal/formats/ansible/collection/collectiontest"
	"github.com/guided-traffic/quader/test/debstub/debgen"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// actsWorld is an installation with one local repository of every format, a replica of the role all that serves the protocols and
// writes the events, and a replica of the role api that reads the audit log (R-OPS-056).
type actsWorld struct {
	t           *testing.T
	st          *testenv.Stack
	pkgs, api   *testenv.Replica
	admin, read string
}

const (
	actsOCI, actsNPM, actsNuGet, actsDeb, actsAnsible, actsGeneric = "docker-local", "npm-local", "nuget-local", "deb-local", "ansible-local", "generic-local"
)

func newActsWorld(t *testing.T) *actsWorld {
	t.Helper()
	st := testenv.NewStack(t, testenv.StackOptions{Name: "auditacts", Env: []string{"QUADER_VULNERABILITIES_SOURCES_OSV_MODE=off"}})
	w := &actsWorld{t: t, st: st}
	ops, reader := st.User("ops"), st.User("reader")
	st.Bind("admin", ops)
	w.admin = st.Token(ops, scope.Scope{Inherit: true, Admin: true})
	w.read = st.Token(reader, scope.Default())
	st.Repository(actsOCI, "local", "oci", "{}")
	st.Repository(actsNPM, "local", "npm", "{}")
	st.Repository(actsNuGet, "local", "nuget", `{"local":{"packageOwnership":"off"},"nuget":{"deleteMode":"delete"}}`)
	st.Repository(actsDeb, "local", "deb", "{}")
	st.Repository(actsAnsible, "local", "ansible", "{}")
	st.Repository(actsGeneric, "local", "generic", "{}")
	all := []string{actsOCI, actsNPM, actsNuGet, actsDeb, actsAnsible, actsGeneric}
	st.Policy("readers", []string{"read"}, all, reader)
	w.pkgs = st.Start(t, testenv.ReplicaOptions{Name: "packages-a", Role: "all", Env: []string{"QUADER_DEB_INDEX_WAIT_MAX=2s", "QUADER_DEB_INDEX_DELAY=1s"}})
	w.api = st.Start(t, testenv.ReplicaOptions{Name: "api-a", Role: "api"})
	keys := "/api/v1/projects/" + testenv.DefaultProject + "/repositories/" + actsDeb + "/deb/signing-keys"
	for _, p := range []string{keys, keys + "/promote"} {
		var body any
		if p == keys {
			body = map[string]any{"generate": true}
		}
		if a := st.Do(w.pkgs, "POST", p, w.admin, body); a.Status/100 != 2 {
			t.Fatalf("the Debian repository's key: %d %s", a.Status, a.Body)
		}
	}
	return w
}

// must sends a request to the protocol replica and fails the test unless it answers one of the statuses.
func (w *actsWorld) must(method, path string, body any, want []int, hdr ...string) testenv.Answer {
	w.t.Helper()
	a := w.st.Do(w.pkgs, method, path, w.admin, body, hdr...)
	for _, s := range want {
		if a.Status == s {
			return a
		}
	}
	w.t.Fatalf("%s %s: %d %s", method, path, a.Status, a.Body)
	return a
}

// deleteItem removes a file or a folder with its subtree through the items API, as the repository's screen does.
func (w *actsWorld) deleteItem(repo, path string) {
	w.t.Helper()
	w.must("DELETE", "/api/v1/projects/"+testenv.DefaultProject+"/repositories/"+repo+"/items?path="+url.QueryEscape(path), nil,
		[]int{http.StatusNoContent})
}

func digestOf(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

// pushImage pushes an image of tiny layers by its tag as docker push does: each blob in one request, then the manifest.
func (w *actsWorld) pushImage(name, tag string, layers int) string {
	w.t.Helper()
	v2 := "/v2/" + testenv.DefaultProject + "/" + actsOCI + "/" + name
	config := []byte(`{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":[]}}`)
	var ls []map[string]any
	blobs := [][]byte{config}
	for i := range layers {
		b := []byte(fmt.Sprintf("layer %d of %s:%s", i, name, tag))
		blobs = append(blobs, b)
		ls = append(ls, map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar", "digest": digestOf(b), "size": len(b)})
	}
	for _, b := range blobs {
		w.must("POST", v2+"/blobs/uploads/?digest="+digestOf(b), b, []int{http.StatusCreated}, "Content-Type", "application/octet-stream")
	}
	manifest, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": digestOf(config), "size": len(config)},
		"layers": ls,
	})
	w.must("PUT", v2+"/manifests/"+tag, manifest, []int{http.StatusCreated}, "Content-Type", "application/vnd.oci.image.manifest.v1+json")
	return digestOf(manifest)
}

// publishNPM publishes name@version as npm publish does: the document with the tarball attached.
func (w *actsWorld) publishNPM(name, version string) {
	w.t.Helper()
	pj, _ := json.Marshal(map[string]any{"name": name, "version": version})
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	_ = tw.WriteHeader(&tar.Header{Name: "package/package.json", Mode: 0o644, Size: int64(len(pj)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(pj)
	_ = tw.Close()
	_ = zw.Close()
	tgz := buf.Bytes()
	s512, s1 := sha512.Sum512(tgz), sha1.Sum(tgz) //nolint:gosec // see the import
	file := name + "-" + version + ".tgz"
	doc := map[string]any{
		"_id": name, "name": name, "dist-tags": map[string]string{"latest": version},
		"versions": map[string]any{version: map[string]any{
			"name": name, "version": version, "_id": name + "@" + version,
			"dist": map[string]any{
				"integrity": "sha512-" + base64.StdEncoding.EncodeToString(s512[:]), "shasum": hex.EncodeToString(s1[:]),
				"tarball": "http://x/" + name + "/-/" + file,
			},
		}},
		"access":       nil,
		"_attachments": map[string]any{file: map[string]any{"content_type": "application/octet-stream", "data": base64.StdEncoding.EncodeToString(tgz), "length": len(tgz)}},
	}
	w.must("PUT", "/api/npm/"+testenv.DefaultProject+"/"+actsNPM+"/"+name, doc, []int{http.StatusCreated})
}

// pushNuGet pushes a package as dotnet nuget push does.
func (w *actsWorld) pushNuGet(id, version string) {
	w.t.Helper()
	nuspec := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://schemas.microsoft.com/packaging/2013/05/nuspec.xsd"><metadata><id>%s</id><version>%s</version><authors>Acme</authors><description>Widgets</description></metadata></package>`, id, version)
	var pkg bytes.Buffer
	zw := zip.NewWriter(&pkg)
	for _, f := range [][2]string{{id + ".nuspec", nuspec}, {"lib/net8.0/" + id + ".dll", "MZ" + version}} {
		fw, _ := zw.Create(f[0])
		_, _ = fw.Write([]byte(f[1]))
	}
	_ = zw.Close()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="package"; filename="package"`)
	h.Set("Content-Type", "application/octet-stream")
	part, _ := mw.CreatePart(h)
	_, _ = part.Write(pkg.Bytes())
	_ = mw.Close()
	w.must("PUT", "/api/nuget/"+testenv.DefaultProject+"/"+actsNuGet+"/v3/package", buf.Bytes(), []int{http.StatusCreated}, "Content-Type", mw.FormDataContentType())
}

// publishAnsible publishes acme.tools at a version as ansible-galaxy collection publish does and waits for its import.
func (w *actsWorld) publishAnsible(version string) {
	w.t.Helper()
	archive := collectiontest.Build(collectiontest.Spec{
		Namespace: "acme", Name: "tools", Version: version,
		Files: map[string]string{"README.md": "# acme.tools\n", "meta/runtime.yml": "---\nrequires_ansible: '>=2.14.0'\n"},
	})
	sum := sha256.Sum256(archive)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("sha256", hex.EncodeToString(sum[:]))
	fw, _ := mw.CreateFormFile("file", "acme-tools.tar.gz")
	_, _ = fw.Write(archive)
	_ = mw.Close()
	w.must("POST", "/api/ansible/"+testenv.DefaultProject+"/"+actsAnsible+"/v3/artifacts/collections/", buf.Bytes(), []int{http.StatusAccepted},
		"Content-Type", mw.FormDataContentType())
	testenv.WaitFor(w.t, "the collection's import", 30*time.Second, func() bool {
		return w.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'package.published' AND repository = $1`,
			testenv.DefaultProject+"/"+actsAnsible) > 0
	})
}

// uploadFiles uploads files into a folder one after another, as the screen's upload dialog does. The dialog's content route serves a
// session alone; a token's upload goes through /files/, which commits through the same files service.
func (w *actsWorld) uploadFiles(folder string, names ...string) {
	w.t.Helper()
	for _, n := range names {
		w.must("PUT", "/files/"+testenv.DefaultProject+"/"+actsGeneric+"/"+folder+"/"+n, []byte("bytes of "+n),
			[]int{http.StatusCreated}, "Content-Type", "application/octet-stream")
	}
}

// actsRows reads the rows and the events of a repository on the replica of the role api: the events by action, and the rows as
// "<act> <unit> <version> <items>" (a single event as "event <action>").
func (w *actsWorld) actsRows(repo string) (map[string]int, []string, []map[string]any) {
	w.t.Helper()
	q := "?limit=1000&repository=" + url.QueryEscape(testenv.DefaultProject+"/"+repo)
	ev := w.st.Do(w.api, "GET", "/api/v1/audit/events"+q, w.admin, nil)
	rw := w.st.Do(w.api, "GET", "/api/v1/audit/rows"+q, w.admin, nil)
	if ev.Status != http.StatusOK || rw.Status != http.StatusOK {
		w.t.Fatalf("the audit reads on the role api: %d %s / %d %s", ev.Status, ev.Body, rw.Status, rw.Body)
	}
	counts := map[string]int{}
	for _, it := range ev.JSON(w.t)["items"].([]any) {
		counts[it.(map[string]any)["action"].(string)]++
	}
	var shapes []string
	var groups []map[string]any
	for _, it := range rw.JSON(w.t)["items"].([]any) {
		r := it.(map[string]any)
		g, _ := r["group"].(map[string]any)
		if g == nil {
			shapes = append(shapes, "event "+r["event"].(map[string]any)["action"].(string))
			continue
		}
		v, _ := g["version"].(string)
		if n := g["versions"].(float64); n > 1 {
			v = fmt.Sprintf("%d versions", int(n))
		}
		shapes = append(shapes, fmt.Sprintf("%s %s %s %d", g["act"], g["unit"], v, int(g["items"].(float64))))
		groups = append(groups, g)
	}
	sort.Strings(shapes)
	return counts, shapes, groups
}

// TestR_OPS_056_APushAndADeleteAreOneRowInEveryFormat measures what a push and a delete write in every format, through the protocol
// of a replica of the role all, and reads the audit log's rows on a replica of the role api: every push, publish and upload is one row
// beside one row of its allowed writes, every delete one row; the events stay one per file and per version. The numbers it pins are
// the measurement of R-OPS-056's table. Integration tier, about 4 s with two replicas (their start besides); payloads of a few hundred bytes.
func TestR_OPS_056_APushAndADeleteAreOneRowInEveryFormat(t *testing.T) {
	w := newActsWorld(t)
	type step struct {
		name, repo string
		act        func()
		events     map[string]int
		rows       []string
	}
	steps := []step{
		{
			"R-OPS-056 a docker push of an image of three tiny layers is one row of a push and one of its writes", actsOCI,
			func() { w.pushImage("acme/app", "1.4.0", 3) },
			ev("artifact.deployed", 6, "authz.write.allowed", 6, "package.published", 1),
			[]string{"push acme/app 1.4.0 7", "writes acme/app  6"},
		},
		{
			"R-OPS-056 the delete of the image's manifest is one row of a delete", actsOCI,
			func() {
				d := w.must("HEAD", "/v2/"+testenv.DefaultProject+"/"+actsOCI+"/acme/app/manifests/1.4.0", nil, []int{http.StatusOK})
				w.must("DELETE", "/v2/"+testenv.DefaultProject+"/"+actsOCI+"/acme/app/manifests/"+d.Header.Get("Docker-Content-Digest"), nil,
					[]int{http.StatusAccepted})
			},
			ev("artifact.deleted", 2, "oci.manifest.deleted", 1, "authz.write.allowed", 1),
			[]string{"delete acme/app 1.4.0 3"},
		},
		{
			"R-OPS-056 an npm publish is one row of a publish", actsNPM, func() { w.publishNPM("left-pad", "1.0.0") },
			ev("artifact.deployed", 1, "npm.publish", 1, "authz.write.allowed", 2),
			[]string{"publish left-pad@1.0.0 1.0.0 2", "writes left-pad  2"},
		},
		{
			"R-OPS-056 the delete of an npm package of three versions from its screen is one row naming the three", actsNPM,
			func() {
				w.publishNPM("left-pad", "1.1.0")
				w.publishNPM("left-pad", "2.0.0")
				w.deleteItem(actsNPM, "left-pad")
			},
			ev("artifact.deployed", 2, "npm.publish", 2, "artifact.deleted", 3, "npm.unpublish", 3, "authz.write.allowed", 5),
			[]string{"delete left-pad 3 versions 6"},
		},
		{
			"R-OPS-056 a dotnet nuget push is one row of a push", actsNuGet, func() { w.pushNuGet("Acme.Widgets", "1.2.3") },
			ev("artifact.deployed", 2, "package.published", 1, "authz.write.allowed", 2),
			[]string{"push acme.widgets@1.2.3 1.2.3 3", "writes acme.widgets  2"},
		},
		{
			"R-OPS-056 a dotnet nuget delete is one row of a delete", actsNuGet,
			func() {
				w.must("DELETE", "/api/nuget/"+testenv.DefaultProject+"/"+actsNuGet+"/v3/package/Acme.Widgets/1.2.3", nil, []int{http.StatusNoContent})
			},
			ev("artifact.deleted", 2, "package.deleted", 1, "authz.write.allowed", 3),
			[]string{"delete acme.widgets 1.2.3 3"},
		},
		{
			"R-OPS-056 a Debian upload is one row of a publish", actsDeb,
			func() {
				d, err := debgen.Build(debgen.Hello("hello", "2.10-3", "amd64"))
				if err != nil {
					t.Fatal(err)
				}
				w.must("PUT", "/api/deb/"+testenv.DefaultProject+"/"+actsDeb+"/upload/hello_2.10-3_amd64.deb?distribution=stable", d.Bytes,
					[]int{http.StatusCreated}, "Content-Type", "application/vnd.debian.binary-package")
			},
			ev("artifact.deployed", 1, "package.published", 1, "authz.write.allowed", 1),
			[]string{"publish hello@2.10-3 2.10-3 2", "writes hello  1"},
		},
		{
			"R-OPS-056 the delete of a Debian package's file is one row of a delete", actsDeb,
			func() {
				w.deleteItem(actsDeb, w.st.Text(`SELECT i.path FROM items i JOIN repositories r ON r.id = i.repo_id WHERE r.key = $1 AND i.path LIKE '%.deb'`, actsDeb))
			},
			ev("artifact.deleted", 1, "deb.package.deleted", 1, "authz.write.allowed", 1),
			[]string{"delete hello 2.10-3 2"},
		},
		{
			"R-OPS-056 an Ansible Galaxy publish is one row of a publish", actsAnsible, func() { w.publishAnsible("1.4.0") },
			ev("artifact.deployed", 1, "package.published", 1, "authz.write.allowed", 1),
			[]string{"publish acme.tools@1.4.0 1.4.0 2", "writes acme.tools  1"},
		},
		{
			"R-OPS-056 the delete of an Ansible Galaxy version is one row of a delete", actsAnsible,
			func() { w.deleteItem(actsAnsible, "collections/acme/tools/acme-tools-1.4.0.tar.gz") },
			ev("artifact.deleted", 1, "ansible.version.deleted", 1, "authz.write.allowed", 1),
			[]string{"delete acme.tools 1.4.0 2"},
		},
		{
			"R-OPS-056 three files uploaded into a folder from the screen are one row of an upload", actsGeneric,
			func() { w.uploadFiles("releases/1.0", "app.zip", "app.zip.sha256", "NOTES.md") },
			ev("artifact.deployed", 3, "authz.write.allowed", 3),
			[]string{"upload releases/1.0/  3", "writes releases/1.0/  3"},
		},
		{
			"R-OPS-056 the delete of the folder is one row of a delete", actsGeneric, func() { w.deleteItem(actsGeneric, "releases") },
			ev("artifact.deleted", 3, "authz.write.allowed", 1),
			[]string{"delete releases/  3"},
		},
	}
	before := map[string][]string{}
	beforeCounts := map[string]map[string]int{}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			if _, ok := before[s.repo]; !ok {
				beforeCounts[s.repo], before[s.repo], _ = w.actsRows(s.repo)
			}
			s.act()
			counts, shapes, _ := w.actsRows(s.repo)
			added := map[string]int{}
			for k, v := range counts {
				if d := v - beforeCounts[s.repo][k]; d > 0 {
					added[k] = d
				}
			}
			newRows := without(shapes, before[s.repo])
			t.Logf("%s: events %v; new rows %q", s.repo, added, newRows)
			delete(added, "repository.claims_changed") // the first version of a name claims it (R-REPO-023)
			if fmt.Sprint(added) != fmt.Sprint(s.events) {
				t.Errorf("the act wrote %v, want %v", added, s.events)
			}
			for _, want := range s.rows {
				if !hasRow(newRows, want) {
					t.Errorf("no new row %q among %q", want, newRows)
				}
			}
			for _, r := range newRows {
				// The claim of a namespace by the first version of a name is a change of its own (R-REPO-023), a row of its own.
				if strings.HasPrefix(r, "event ") && r != "event repository.claims_changed" {
					t.Errorf("a single event's row %q: every event of a push and a delete joins a row", r)
				}
			}
			beforeCounts[s.repo], before[s.repo] = counts, shapes
		})
	}

	t.Run("R-OPS-056 a row's events are every event of its act, and the rows are read under audit:read alone", func(t *testing.T) {
		q := "?limit=1000&repository=" + url.QueryEscape(testenv.DefaultProject+"/"+actsOCI)
		rows := w.st.Do(w.api, "GET", "/api/v1/audit/rows"+q, w.admin, nil).JSON(t)["items"].([]any)
		for _, it := range rows {
			r := it.(map[string]any)
			g, _ := r["group"].(map[string]any)
			if g == nil || g["act"] != "push" {
				continue
			}
			id := r["event"].(map[string]any)["id"].(string)
			a := w.st.Do(w.api, "GET", "/api/v1/audit/rows/"+id+"/events?limit=1000", w.admin, nil)
			if a.Status != http.StatusOK || len(a.JSON(t)["items"].([]any)) != int(g["items"].(float64)) {
				t.Errorf("the push's events: %d %s, want %v", a.Status, a.Body, g["items"])
			}
			for _, p := range []string{"/api/v1/audit/rows" + q, "/api/v1/audit/rows/" + id + "/events", "/api/v1/audit/events" + q} {
				if a := w.st.Do(w.api, "GET", p, w.read, nil); a.Status != http.StatusForbidden {
					t.Errorf("a reader of the repository without audit:read on %s: %d", p, a.Status)
				}
			}
		}
	})
}

// ev is a count of events by action, written as action, count, action, count, ...
func ev(kv ...any) map[string]int {
	out := map[string]int{}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1].(int)
	}
	return out
}

// without is the rows of now that were not among before (each counted once).
func without(now, before []string) []string {
	left := map[string]int{}
	for _, b := range before {
		left[b]++
	}
	var out []string
	for _, n := range now {
		if left[n] > 0 {
			left[n]--
			continue
		}
		out = append(out, n)
	}
	return out
}

// hasRow says whether a row's shape is want ("<act> <unit> <version> <items>"). The allowed writes of a delete join those of the act a
// moment before it on the same package by the same caller (within the pause, H-2265), so a delete's step names no row of writes.
func hasRow(rows []string, want string) bool {
	for _, r := range rows {
		if r == want {
			return true
		}
	}
	return false
}
