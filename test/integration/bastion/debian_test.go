//go:build integration

package bastion

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/openpgp"
	"github.com/guided-traffic/quader/test/debstub/debgen"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// Debian through a Bastion (bastion.md R-BAS-055): apt's requests replayed by Go's client through an in-process Bastion against a local
// repository of the main installation that builds its own indexes (the role all).

const debLocal = testenv.DefaultProject + "/apt"

// debWorld is the world with the local Debian repository web/apt, read by acme-full, with its first signing key.
func debWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t, worldOptions{
		Role: "all",
		Env:  []string{"QUADER_DEB_INDEX_WAIT_MAX=2s", "QUADER_DEB_INDEX_DELAY=1s"},
		Seed: func(w *world) {
			w.st.Repository(debLocal, "local", "deb", "{}")
			w.st.Policy("deb-read", []string{"read"}, []string{debLocal}, testenv.Person{ID: w.full.pid, Name: "acme-full", Kind: "service_account"})
		},
	})
	keys := "/api/v1/projects/" + testenv.DefaultProject + "/repositories/apt/deb/signing-keys"
	w.mustAPI("POST", keys, 201, map[string]any{"generate": true})
	if a := w.api("POST", keys+"/promote", w.admin, nil); a.Status/100 != 2 {
		t.Fatalf("promote the first key: %d %s", a.Status, a.Body)
	}
	// The promotion is a content entry of the feed (Q-BAS-175); it belongs to the set-up, older than any Bastion's history, as the
	// claims of the first publishes do (setUpOlderThanTheFeed).
	w.st.Exec(`DELETE FROM audit_event WHERE action LIKE 'deb.signing_key.%'`)
	return w
}

// uploadDeb uploads a package of about 1 KiB into the suite stable and waits until the suite's index holds it (R-DEB-014).
func (w *world) uploadDeb(name, version string) {
	w.t.Helper()
	d, err := debgen.Build(debgen.Hello(name, version, "amd64"))
	if err != nil {
		w.t.Fatal(err)
	}
	a := w.api("PUT", "/api/deb/"+debLocal+"/upload/"+d.Spec.Filename()+"?distribution=stable&component=main", w.admin, d.Bytes,
		"Content-Type", "application/vnd.debian.binary-package")
	if a.Status != http.StatusCreated {
		w.t.Fatalf("upload %s: %d %s", name, a.Status, a.Body)
	}
	var up struct {
		Index []struct {
			Distribution string `json:"distribution"`
			ChangeSeq    int64  `json:"changeSeq"`
		} `json:"index"`
	}
	if err := json.Unmarshal(a.Body, &up); err != nil || len(up.Index) == 0 {
		w.t.Fatalf("upload answer: %v %s", err, a.Body)
	}
	seq := up.Index[0].ChangeSeq
	path := "/api/v1/projects/" + testenv.DefaultProject + "/repositories/apt/deb/index?waitFor=stable:" + strconv.FormatInt(seq, 10)
	testWait(w.t, "the index of stable", func() bool {
		r := w.api("GET", path, w.admin, nil)
		var st struct {
			Suites []struct {
				Distribution string `json:"distribution"`
				PublishedSeq int64  `json:"publishedSeq"`
			} `json:"suites"`
		}
		_ = json.Unmarshal(r.Body, &st)
		for _, s := range st.Suites {
			if s.Distribution == "stable" && s.PublishedSeq >= seq {
				return true
			}
		}
		return false
	})
	w.setUpOlderThanTheFeed()
}

// byHashPath is the by-hash path of an index the release names.
func byHashPath(t *testing.T, release []byte, index string) string {
	t.Helper()
	in := false
	for _, l := range strings.Split(string(release), "\n") {
		switch {
		case strings.HasPrefix(l, "SHA256:"):
			in = true
		case in && strings.HasPrefix(l, " "):
			if f := strings.Fields(l); len(f) == 3 && f[2] == index {
				return "dists/stable/" + strings.TrimSuffix(index, "Packages.gz") + "by-hash/SHA256/" + f[0]
			}
		default:
			in = false
		}
	}
	t.Fatalf("the release names no %s:\n%s", index, release)
	return ""
}

// TestR_BAS_055_DebianThroughABastion: with a token without package patterns, the InRelease, the by-hash Packages.gz it names and the
// pool file are served through a Bastion cold and warm; the InRelease verifies with signing-key.asc read through the Bastion; after a
// second upload and build the first generation's by-hash file still answers 200; after the package file is deleted at the main
// installation, a content entry ends the service of the cached pool file within the feed's bound. About 6 s, packages of 1 KiB.
func TestR_BAS_055_DebianThroughABastion(t *testing.T) {
	w := debWorld(t)
	w.uploadDeb("hello", "1.0-1")
	b := w.newBastion(t, bastionOptions{Settings: relaxed}).wait()
	r := b.reps[0]
	o := callOpts{token: w.full.token}
	base := "/api/deb/" + debLocal + "/"
	get := func(p string) reply { return r.do(t, "GET", base+p, o) }

	in := get("dists/stable/InRelease")
	if in.Status != http.StatusOK {
		t.Fatalf("InRelease: %d %s", in.Status, in.Body)
	}
	key := get("signing-key.asc")
	certs, err := openpgp.ParseCertificates(key.Body, time.Now(), openpgp.DefaultLimits())
	if err != nil {
		t.Fatalf("signing-key.asc through the Bastion: %d %v", key.Status, err)
	}
	release, _, err := openpgp.VerifyClear(in.Body, certs, time.Now(), openpgp.DefaultLimits())
	if err != nil {
		t.Fatalf("the InRelease served through the Bastion does not verify: %v", err)
	}
	byHash := byHashPath(t, release, "main/binary-amd64/Packages.gz")
	pkgs := get(byHash)
	if pkgs.Status != http.StatusOK {
		t.Fatalf("%s: %d", byHash, pkgs.Status)
	}
	zr, err := gzip.NewReader(bytes.NewReader(pkgs.Body))
	if err != nil {
		t.Fatal(err)
	}
	index, _ := io.ReadAll(zr)
	var pool string
	for _, l := range strings.Split(string(index), "\n") {
		if f, ok := strings.CutPrefix(l, "Filename: "); ok {
			pool = f
		}
	}
	if pool == "" || get(pool).Status != http.StatusOK {
		t.Fatalf("the pool file %q", pool)
	}

	m := w.front.Mark()
	for _, p := range []string{"dists/stable/InRelease", byHash, pool} {
		if got := get(p); got.Status != http.StatusOK {
			t.Fatalf("warm %s: %d", p, got.Status)
		}
	}
	if c := w.front.Cost(m); c != 0 {
		for _, x := range w.front.Since(m) {
			t.Logf("%s %s %s %d inm=%q", x.Kind, x.Method, x.Path, x.Status, x.Header.Get("If-None-Match"))
		}
		t.Errorf("the warm reads cost the main installation %d requests", c)
	}

	w.uploadDeb("world", "1.0-1")
	if got := get(byHash); got.Status != http.StatusOK {
		t.Errorf("the first generation's by-hash file after a second build: %d", got.Status)
	}
	if a := w.api("GET", base+byHash, w.full.token, nil); a.Status != http.StatusOK {
		t.Errorf("the first generation's by-hash file at the main installation: %d", a.Status)
	}

	if a := w.api("DELETE", base+pool, w.admin, nil); a.Status != http.StatusNoContent {
		t.Fatalf("delete the package file: %d %s", a.Status, a.Body)
	}
	start := time.Now()
	testWait(t, "the deleted package file refused through the Bastion", func() bool { return get(pool).Status == http.StatusNotFound })
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("refused after %v, beyond the bound of 5 s", d)
	}
}
