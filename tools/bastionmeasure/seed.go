package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // npm's dist.shasum is a SHA-1
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/formats/ansible/collection/collectiontest"
	"github.com/guided-traffic/quader/test/debstub/debgen"
)

// The content of the measurement, all generated: what each command of the table fetches.
const (
	npmPackages    = 475 // the lockfile of R-LIM-041's measurement locks 475 packages
	nugetPackages  = 200 // "a project of about 200 packages"
	fortyLayers    = 40
	bigLayers      = 8
	bigLayerBytes  = 128 << 20 // eight layers of 128 MiB: an image of 1 GiB
	debTools       = 5         // apt-get install of five packages that share one library
	galaxyDeps     = 3         // a collection with three dependencies
	genericBytes   = 1 << 20   // the generic file curl fetches
	floodFileBytes = 4 << 20   // the overflow attack's distinct artifacts
)

type seeder struct {
	adm     *client
	project string
	host    string // the Bastion's origin the lockfile and the snippets name, https://<host>:<port>
	arch    string // the platform of the client containers (arm64 on the measuring machine)
	out     string // where the client files go: the lockfile, the project, the configuration snippets
	floodN  int
}

func seed(args []string) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	mainURL := fs.String("main", "", "the main installation's base URL")
	ca := fs.String("ca", "", "CA file")
	tokenFile := fs.String("token-file", "", "an administrator's token")
	s := &seeder{}
	fs.StringVar(&s.project, "project", "e4", "the project of the measurement")
	fs.StringVar(&s.host, "bastion", "", "the Bastion's origin as the clients name it")
	fs.StringVar(&s.arch, "arch", "arm64", "the clients' platform")
	fs.StringVar(&s.out, "out", "", "the directory for the client files")
	fs.IntVar(&s.floodN, "flood", 768, "distinct artifacts of 4 MiB for the overflow attack (768 are 3 GiB)")
	only := fs.String("only", "", "comma-separated parts: repos,reader,npm,nuget,oci,deb,galaxy,generic,flood (all when empty)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tok, err := readToken(*tokenFile)
	if err != nil {
		return err
	}
	if s.adm, err = newClient(*mainURL, *ca, tok, 10*time.Minute); err != nil {
		return err
	}
	if err := os.MkdirAll(s.out, 0o750); err != nil {
		return err
	}
	parts := []struct {
		name string
		run  func() error
	}{
		{"repos", s.repos},
		{"reader", s.reader},
		{"npm", s.npm},
		{"nuget", s.nuget},
		{"oci", s.oci},
		{"deb", s.deb},
		{"galaxy", s.galaxy},
		{"generic", s.generic},
		{"flood", s.flood},
	}
	for _, p := range parts {
		if *only != "" && !strings.Contains(","+*only+",", ","+p.name+",") {
			continue
		}
		start := time.Now()
		if err := p.run(); err != nil {
			return fmt.Errorf("%s: %w", p.name, err)
		}
		fmt.Printf("seed %s: %s\n", p.name, time.Since(start).Round(time.Millisecond))
	}
	return nil
}

var created = []int{http.StatusCreated, http.StatusConflict}

func (s *seeder) repoKeys() map[string]string {
	return map[string]string{
		"npm-local": "npm", "nuget-local": "nuget", "docker-local": "oci", "apt": "deb", "galaxy": "ansible",
		"files": "generic", "flood": "generic",
	}
}

// repos creates the project and its local repositories, every one readable by anyone (the anonymous runs), and the Debian
// repository's signing key.
func (s *seeder) repos() error {
	if _, err := s.adm.must("project", "POST", "/api/v1/projects", jsonBody(map[string]any{"key": s.project, "displayName": "W34-E4"}),
		created, "Content-Type", "application/json"); err != nil {
		return err
	}
	if _, err := s.adm.must("project public", "PUT", "/api/v1/projects/"+s.project+"/visibility", jsonBody(map[string]any{"level": "public"}),
		[]int{200}, "Content-Type", "application/json", "If-Match", "*"); err != nil {
		return err
	}
	for key, format := range s.repoKeys() {
		if _, err := s.adm.must("repository "+key, "POST", "/api/v1/projects/"+s.project+"/repositories",
			jsonBody(map[string]any{"key": key, "class": "local", "format": format}), created, "Content-Type", "application/json"); err != nil {
			return err
		}
		if _, err := s.adm.must("repository public "+key, "PATCH", "/api/v1/projects/"+s.project+"/repositories/"+key,
			jsonBody(map[string]any{"visibility": map[string]any{"artifacts": "public", "wiki": "public"}}), []int{200},
			"Content-Type", "application/merge-patch+json", "If-Match", "*"); err != nil {
			return err
		}
	}
	keys := "/api/v1/projects/" + s.project + "/repositories/apt/deb/signing-keys"
	a, err := s.adm.must("signing keys", "GET", keys, nil, []int{200})
	if err != nil {
		return err
	}
	if !strings.Contains(string(a.Body), `"state":"active"`) {
		if _, err := s.adm.must("generate a key", "POST", keys, jsonBody(map[string]any{"generate": true}), []int{201},
			"Content-Type", "application/json"); err != nil {
			return err
		}
		if _, err := s.adm.must("promote the key", "POST", keys+"/promote", nil, []int{200, 201, 204}); err != nil {
			return err
		}
	}
	return nil
}

// reader creates the service account e4-reader, which reads the project's repositories, and writes a token of it to
// <out>/reader-token: the credential of the signed-in runs, a reader as a CI job or a developer is.
func (s *seeder) reader() error {
	if _, err := s.adm.must("service account", "POST", "/api/v1/service-accounts",
		jsonBody(map[string]any{"name": "e4-reader", "description": "W34-E4 measurement"}), created, "Content-Type", "application/json"); err != nil {
		return err
	}
	policy := map[string]any{
		"name": "e4-reader", "description": "W34-E4 measurement",
		"resources": map[string]any{"repositories": []string{s.project + "/*"}},
		"entries":   []map[string]any{{"principal": "sa:e4-reader", "actions": []string{"read"}}},
	}
	if a, err := s.adm.do("POST", "/api/v1/permission-policies", jsonBody(policy), "Content-Type", "application/json"); err != nil {
		return err
	} else if a.Status != 201 && a.Status != 409 {
		return fmt.Errorf("policy: %d %s", a.Status, a.Body)
	}
	tokFile := filepath.Join(s.out, "reader-token")
	if b, err := os.ReadFile(tokFile); err == nil && len(b) > 0 { //nolint:gosec // our own file
		return nil
	}
	a, err := s.adm.must("token", "POST", "/api/v1/service-accounts/e4-reader/technical-tokens",
		jsonBody(map[string]any{
			"name": fmt.Sprintf("e4-%d", time.Now().Unix()), "description": "W34-E4", "ttlSeconds": 30 * 86400,
			"scope": map[string]any{"inherit": true},
		}), []int{201}, "Content-Type", "application/json")
	if err != nil {
		return err
	}
	var c struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(a.Body, &c); err != nil {
		return fmt.Errorf("token answer: %w", err)
	}
	if c.Token == "" {
		return fmt.Errorf("token answer without a token: %.200s", a.Body)
	}
	return os.WriteFile(tokFile, []byte(c.Token+"\n"), 0o600)
}

// parallel runs f(0..n-1) on w workers and returns the first error.
func parallel(n, w int, f func(i int) error) error {
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		first error
	)
	work := make(chan int)
	for range w {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				if err := f(i); err != nil {
					mu.Lock()
					if first == nil {
						first = err
					}
					mu.Unlock()
				}
			}
		}()
	}
	for i := range n {
		work <- i
	}
	close(work)
	wg.Wait()
	return first
}

// npm publishes 475 packages of one tarball each and writes the lockfile that locks them all at the Bastion's address.
func (s *seeder) npm() error {
	type locked struct{ name, integrity string }
	locks := make([]locked, npmPackages)
	err := parallel(npmPackages, 8, func(i int) error {
		name := fmt.Sprintf("e4pkg-%03d", i+1)
		tgz := npmTarball(name, "1.0.0")
		s512 := sha512.Sum512(tgz)
		s1 := sha1.Sum(tgz) //nolint:gosec // see the import
		integrity := "sha512-" + base64.StdEncoding.EncodeToString(s512[:])
		file := name + "-1.0.0.tgz"
		doc := map[string]any{
			"_id": name, "name": name, "dist-tags": map[string]string{"latest": "1.0.0"}, "access": nil,
			"versions": map[string]any{"1.0.0": map[string]any{
				"name": name, "version": "1.0.0", "_id": name + "@1.0.0",
				"dist": map[string]any{"integrity": integrity, "shasum": hex.EncodeToString(s1[:]), "tarball": "http://x/" + name + "/-/" + file},
			}},
			"_attachments": map[string]any{file: map[string]any{
				"content_type": "application/octet-stream",
				"data":         base64.StdEncoding.EncodeToString(tgz), "length": len(tgz),
			}},
		}
		locks[i] = locked{name, integrity}
		_, err := s.adm.must("publish "+name, "PUT", "/api/npm/"+s.project+"/npm-local/"+name, jsonBody(doc), []int{200, 201, 409},
			"Content-Type", "application/json")
		return err
	})
	if err != nil {
		return err
	}
	deps := map[string]string{}
	pkgs := map[string]any{}
	for _, l := range locks {
		deps[l.name] = "1.0.0"
		pkgs["node_modules/"+l.name] = map[string]any{
			"version":   "1.0.0",
			"resolved":  s.host + "/api/npm/" + s.project + "/npm-local/" + l.name + "/-/" + l.name + "-1.0.0.tgz",
			"integrity": l.integrity,
		}
	}
	pkgs[""] = map[string]any{"name": "e4-app", "version": "1.0.0", "dependencies": deps}
	dir := filepath.Join(s.out, "npm")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "package.json"), map[string]any{"name": "e4-app", "version": "1.0.0", "private": true, "dependencies": deps}); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, "package-lock.json"), map[string]any{
		"name": "e4-app", "version": "1.0.0", "lockfileVersion": 3, "requires": true, "packages": pkgs,
	})
}

func npmTarball(name, version string) []byte {
	pj, _ := json.Marshal(map[string]any{"name": name, "version": version, "main": "index.js"})
	return tgz(map[string]string{
		"package/package.json": string(pj),
		"package/index.js":     fmt.Sprintf("module.exports = %q;\n", name+"@"+version),
		"package/README.md":    "# " + name + "\n",
	}, []string{"package/package.json", "package/index.js", "package/README.md"})
}

func tgz(files map[string]string, order []string) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, n := range order {
		body := files[n]
		_ = tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg, ModTime: time.Unix(1_700_000_000, 0)})
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = zw.Close()
	return buf.Bytes()
}

// nuget pushes 200 packages and writes a project that references them all, with a nuget.config of the Bastion's feed.
func (s *seeder) nuget() error {
	ids := make([]string, nugetPackages)
	err := parallel(nugetPackages, 8, func(i int) error {
		id := fmt.Sprintf("E4.Pkg%03d", i+1)
		ids[i] = id
		body, ct := nugetForm(nupkg(id, "1.0.0"))
		_, err := s.adm.must("push "+id, "PUT", "/api/nuget/"+s.project+"/nuget-local/v3/package", body, []int{201, 409}, "Content-Type", ct)
		return err
	})
	if err != nil {
		return err
	}
	dir := filepath.Join(s.out, "nuget")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	var refs strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&refs, "    <PackageReference Include=%q Version=\"1.0.0\" />\n", id)
	}
	proj := "<Project Sdk=\"Microsoft.NET.Sdk\">\n  <PropertyGroup>\n    <TargetFramework>net10.0</TargetFramework>\n" +
		"    <NuGetAudit>false</NuGetAudit>\n  </PropertyGroup>\n  <ItemGroup>\n" + refs.String() + "  </ItemGroup>\n</Project>\n"
	return os.WriteFile(filepath.Join(dir, "e4.csproj"), []byte(proj), 0o600)
}

func nupkg(id, version string) []byte {
	nuspec := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://schemas.microsoft.com/packaging/2013/05/nuspec.xsd"><metadata><id>%s</id><version>%s</version><authors>e4</authors><description>W34-E4</description></metadata></package>`, id, version)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range [][2]string{{id + ".nuspec", nuspec}, {"lib/net10.0/_._", ""}} {
		w, _ := zw.Create(f[0])
		_, _ = w.Write([]byte(f[1]))
	}
	_ = zw.Close()
	return buf.Bytes()
}

func nugetForm(pkg []byte) ([]byte, string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="package"; filename="package"`)
	h.Set("Content-Type", "application/octet-stream")
	part, _ := mw.CreatePart(h)
	_, _ = part.Write(pkg)
	_ = mw.Close()
	return buf.Bytes(), mw.FormDataContentType()
}

// oci pushes three images to docker-local: small (one layer of 4 KiB), forty (40 layers of 1 to 2 KiB) and big (eight layers
// of 128 MiB of random bytes, 1 GiB), each tagged 1, for the clients' platform.
func (s *seeder) oci() error {
	if err := s.pushImage("small", 1, func(int) int { return 4 << 10 }); err != nil {
		return err
	}
	if err := s.pushImage("forty", fortyLayers, func(i int) int { return 1024 + 25*i }); err != nil {
		return err
	}
	return s.pushImage("big", bigLayers, func(int) int { return bigLayerBytes })
}

type descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

func (s *seeder) pushImage(name string, layers int, size func(i int) int) error {
	repo := "/v2/" + s.project + "/docker-local/" + name
	descs := make([]descriptor, layers)
	diffIDs := make([]string, layers)
	err := parallel(layers, 4, func(i int) error {
		content := make([]byte, size(i))
		_, _ = rand.Read(content)
		var tb bytes.Buffer
		tw := tar.NewWriter(&tb)
		_ = tw.WriteHeader(&tar.Header{Name: fmt.Sprintf("layer-%02d.bin", i), Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg, ModTime: time.Unix(1_700_000_000, 0)})
		_, _ = tw.Write(content)
		_ = tw.Close()
		diff := sha256.Sum256(tb.Bytes())
		diffIDs[i] = "sha256:" + hex.EncodeToString(diff[:])
		var gz bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&gz, gzip.BestSpeed)
		_, _ = zw.Write(tb.Bytes())
		_ = zw.Close()
		d, err := s.pushBlob(repo, gz.Bytes())
		descs[i] = descriptor{MediaType: "application/vnd.oci.image.layer.v1.tar+gzip", Digest: d, Size: int64(gz.Len())}
		return err
	})
	if err != nil {
		return err
	}
	cfg, _ := json.Marshal(map[string]any{
		"architecture": s.arch, "os": "linux", "created": "2026-11-26T00:00:00Z",
		"config": map[string]any{"Cmd": []string{"/bin/true"}},
		"rootfs": map[string]any{"type": "layers", "diff_ids": diffIDs},
	})
	cd, err := s.pushBlob(repo, cfg)
	if err != nil {
		return err
	}
	man, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config": descriptor{MediaType: "application/vnd.oci.image.config.v1+json", Digest: cd, Size: int64(len(cfg))},
		"layers": descs,
	})
	_, err = s.adm.must("manifest "+name, "PUT", repo+"/manifests/1", man, []int{201}, "Content-Type", "application/vnd.oci.image.manifest.v1+json")
	return err
}

func (s *seeder) pushBlob(repo string, b []byte) (string, error) {
	sum := sha256.Sum256(b)
	d := "sha256:" + hex.EncodeToString(sum[:])
	if a, err := s.adm.do("HEAD", repo+"/blobs/"+d, nil); err == nil && a.Status == 200 {
		return d, nil
	}
	a, err := s.adm.must("upload", "POST", repo+"/blobs/uploads/", nil, []int{202})
	if err != nil {
		return "", err
	}
	loc, err := url.Parse(a.Header.Get("Location"))
	if err != nil {
		return "", err
	}
	q := loc.Query()
	q.Set("digest", d)
	loc.RawQuery = q.Encode()
	p := loc.Path + "?" + loc.RawQuery
	_, err = s.adm.must("blob", "PUT", p, b, []int{201}, "Content-Type", "application/octet-stream")
	return d, err
}

// deb uploads e4-lib and five tools that depend on it into the suite stable of apt.
func (s *seeder) deb() error {
	specs := []debgen.Spec{debgen.Hello("e4-lib", "1.0", s.arch)}
	for i := 1; i <= debTools; i++ {
		sp := debgen.Hello(fmt.Sprintf("e4-tool%d", i), "1.0", s.arch)
		sp.Depends = "e4-lib (>= 1.0)"
		specs = append(specs, sp)
	}
	for _, sp := range specs {
		d, err := debgen.Build(sp)
		if err != nil {
			return err
		}
		if _, err := s.adm.must("upload "+sp.Package, "PUT", "/api/deb/"+s.project+"/apt/upload/"+sp.Filename()+"?distribution=stable&component=main",
			d.Bytes, []int{201, 409}, "Content-Type", "application/vnd.debian.binary-package"); err != nil {
			return err
		}
	}
	return nil
}

// galaxy publishes e4.dep1 to e4.dep3 and e4.app, which depends on the three.
func (s *seeder) galaxy() error {
	root := "/api/ansible/" + s.project + "/galaxy/"
	deps := map[string]string{}
	var specs []collectiontest.Spec
	for i := 1; i <= galaxyDeps; i++ {
		n := "dep" + strconv.Itoa(i)
		deps["e4."+n] = ">=1.0.0"
		specs = append(specs, collectiontest.Spec{
			Namespace: "e4", Name: n, Version: "1.0.0",
			Files: map[string]string{"README.md": "# e4." + n + "\n", "plugins/modules/" + n + ".py": "# module\n"},
		})
	}
	specs = append(specs, collectiontest.Spec{
		Namespace: "e4", Name: "app", Version: "1.0.0", Dependencies: deps,
		Files: map[string]string{"README.md": "# e4.app\n", "plugins/modules/app.py": "# module\n"},
	})
	for _, sp := range specs {
		arch := collectiontest.Build(sp)
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		_ = mw.WriteField("sha256", collectiontest.Sum(arch))
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, sp.FileName()))
		h.Set("Content-Type", "application/octet-stream")
		part, _ := mw.CreatePart(h)
		_, _ = part.Write(arch)
		_ = mw.Close()
		a, err := s.adm.must("publish "+sp.Name, "POST", root+"v3/artifacts/collections/", buf.Bytes(), []int{202, 409}, "Content-Type", mw.FormDataContentType())
		if err != nil {
			return err
		}
		if a.Status == 202 {
			var task struct {
				Task string `json:"task"`
			}
			_ = json.Unmarshal(a.Body, &task)
			if err := s.waitTask(root, task.Task); err != nil {
				return fmt.Errorf("import %s: %w", sp.Name, err)
			}
		}
	}
	return nil
}

func (s *seeder) waitTask(apiRoot, task string) error {
	if task == "" {
		return nil
	}
	u, err := url.Parse(task)
	if err != nil {
		return err
	}
	p := u.Path
	if !strings.HasPrefix(p, "/") {
		p = apiRoot + p
	}
	for range 120 {
		a, err := s.adm.do("GET", p, nil)
		if err != nil {
			return err
		}
		var st struct {
			State string `json:"state"`
		}
		_ = json.Unmarshal(a.Body, &st)
		switch st.State {
		case "completed":
			return nil
		case "failed":
			return fmt.Errorf("%s", a.Body)
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("task %s did not finish", task)
}

// generic uploads the file curl fetches: 1 MiB of random bytes.
func (s *seeder) generic() error {
	b := make([]byte, genericBytes)
	_, _ = rand.Read(b)
	_, err := s.adm.must("file", "PUT", "/files/"+s.project+"/files/e4/file-1mib.bin", b, []int{200, 201, 409}, "Content-Type", "application/octet-stream")
	return err
}

// flood uploads the overflow attack's distinct artifacts: floodN files of 4 MiB of random bytes in flood/f/<n>.bin.
func (s *seeder) flood() error {
	return parallel(s.floodN, 8, func(i int) error {
		b := make([]byte, floodFileBytes)
		_, _ = rand.Read(b)
		_, err := s.adm.must("flood file", "PUT", fmt.Sprintf("/files/%s/flood/f/%04d.bin", s.project, i), b, []int{200, 201, 409},
			"Content-Type", "application/octet-stream")
		return err
	})
}

func jsonBody(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func writeJSON(file string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, append(b, '\n'), 0o600)
}
