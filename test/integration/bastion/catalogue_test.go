//go:build integration

package bastion

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// The public catalogue's builder at the main installation (R-BAS-104 to R-BAS-108; W-35's package P6), against the role all of the
// integration stack, which runs the job bastion-catalogue: a publish reaches the change list within the debounce, every document
// names nothing of the main installation, a lowered level deletes what fell out, and the web interface switched off answers 404
// and deletes the catalogue. Two npm packages and a NuGet package of bytes; about 5 s beside the world's set-up.

// mirrorURL is a second entry of server.additional_base_urls: another Bastion's host, which no document of partners may name.
const mirrorURL = "https://mirror.example.com"

// catalogueAnswer is one call of a catalogue route with the Bastion token.
func (w *world) catalogue(method, path string, body any) testenv.Answer {
	w.t.Helper()
	return w.api(method, path, w.bst, body, iface.HeaderInterface, strconv.Itoa(iface.InterfaceVersion))
}

// changes reads the whole change list after the cursor.
func (w *world) changes(after int64) iface.CatalogueChanges {
	w.t.Helper()
	all := iface.CatalogueChanges{Cursor: after}
	for {
		a := w.catalogue("GET", iface.PathCatalogue+"?after="+strconv.FormatInt(all.Cursor, 10), nil)
		var c iface.CatalogueChanges
		if a.Status != http.StatusOK || iface.DecodeStrict(bytes.NewReader(a.Body), &c) != nil || c.Validate(all.Cursor) != nil {
			w.t.Fatalf("the change list after %d: %d %s", all.Cursor, a.Status, a.Body)
		}
		all.Entries, all.Cursor, all.Reset = append(all.Entries, c.Entries...), c.Cursor, all.Reset || c.Reset
		if !c.More {
			return all
		}
	}
}

// documents reads every live document of the list through the batch read and answers its raw bytes by path, verified.
func (w *world) documents(list iface.CatalogueChanges) map[string][]byte {
	w.t.Helper()
	var paths []string
	for _, e := range list.Entries {
		if !e.Deleted {
			paths = append(paths, e.Path)
		}
	}
	out := map[string][]byte{}
	for len(paths) > 0 {
		n := min(len(paths), 500)
		a := w.catalogue("POST", iface.PathCatalogueDocuments, iface.CatalogueBatch{Paths: paths[:n]})
		if a.Status != http.StatusOK {
			w.t.Fatalf("the batch: %d %s", a.Status, a.Body)
		}
		r := bufio.NewReader(bytes.NewReader(a.Body))
		read := 0
		for {
			f, gz, err := iface.ReadCatalogueFrame(r, 16<<20)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				w.t.Fatal(err)
			}
			read++
			if f.Missing {
				continue
			}
			zr, err := gzip.NewReader(bytes.NewReader(gz))
			if err != nil {
				w.t.Fatal(err)
			}
			raw, err := io.ReadAll(zr)
			sum := sha256.Sum256(raw)
			if err != nil || hex.EncodeToString(sum[:]) != f.SHA256 || int64(len(raw)) != f.Size {
				w.t.Fatalf("%s does not verify: %v", f.Path, err)
			}
			out[f.Path] = raw
		}
		paths = paths[read:]
	}
	return out
}

// readmes counts the README documents of the catalogue's live list and the versions that announce one (R-BAS-124).
func (w *world) readmes() (docs, announced int) {
	w.t.Helper()
	list := w.changes(0)
	held := w.documents(list)
	for p, raw := range held {
		switch {
		case strings.HasSuffix(p, "/readme"):
			docs++
		case strings.HasPrefix(p, "packages/") && strings.Count(p, "/") == 3 && bytes.Contains(raw, []byte(`"hasReadme":true`)):
			announced++
		}
	}
	return docs, announced
}

// documentationFollowsTheWikiAndTheSwitch (R-BAS-124): with docs on and a public wiki the npm READMEs are in the catalogue and their
// versions announce them; a wiki lowered to internal takes them out and the versions announce none, and so does docs off for every
// format, at the next run.
func (w *world) documentationFollowsTheWikiAndTheSwitch(docs *atomic.Bool) {
	w.t.Helper()
	start := time.Now()
	before, announced := w.readmes()
	if before < 2 || announced < 2 {
		w.t.Fatalf("docs on: %d READMEs, %d versions announce one", before, announced)
	}
	wiki := func(level string) {
		for _, repo := range []string{npmAll, npmLocal, npmRemote} {
			w.mustAPI("PATCH", "/api/v1/projects/"+testenv.DefaultProject+"/repositories/"+repo[len(testenv.DefaultProject)+1:], 200,
				map[string]any{"visibility": map[string]any{"wiki": level}}, "If-Match", "*", "Content-Type", "application/merge-patch+json")
		}
	}
	wiki("internal")
	testenv.WaitFor(w.t, "the npm READMEs gone with an internal wiki", 30*time.Second, func() bool {
		n, a := w.readmes()
		return n < before && a < announced
	})
	wiki("public")
	testenv.WaitFor(w.t, "the npm READMEs back", 30*time.Second, func() bool { n, _ := w.readmes(); return n == before })
	docs.Store(false)
	w.mustAPI("POST", "/api/v1/system/tasks/bastion-catalogue/run", 202, nil)
	testenv.WaitFor(w.t, "no README with docs off", 30*time.Second, func() bool { n, a := w.readmes(); return n == 0 && a == 0 })
	docs.Store(true)
	w.t.Logf("the documentation followed the wiki and the switch in %s", time.Since(start).Round(100*time.Millisecond))
}

// TestR_BAS_104_TheCatalogueFollowsPublishesLevelsAndTheSwitch: the catalogue of a registration whose web interface is on holds
// the public packages within the debounce of their publish, every document is free of the main installation's base URL, of
// another Bastion's and of the management API's path, a level lowered to private deletes what fell out, and the web interface
// switched off answers 404 at once and deletes the catalogue at the next run.
func TestR_BAS_104_TheCatalogueFollowsPublishesLevelsAndTheSwitch(t *testing.T) {
	var gui, docsOn atomic.Bool
	gui.Store(true)
	docsOn.Store(true)
	w := newWorld(t, worldOptions{
		Role: "all", Egress: []string{"127.0.0.1/32"},
		Env: []string{`QUADER_SERVER_ADDITIONAL_BASE_URLS=["` + baseURL + `","` + mirrorURL + `"]`},
		Switches: func(string) iface.Switches {
			return iface.Switches{GUI: gui.Load(), Docs: docsOn.Load()}
		},
	})
	// The handshake of the Bastion, from its egress address with its host forwarded, reports its base URL, which the documents are
	// rendered on (R-BAS-104).
	if a := w.api("GET", iface.PathHandshake, w.bst, nil, iface.HeaderInterface, strconv.Itoa(iface.InterfaceVersion),
		"X-Forwarded-Proto", "https", "X-Forwarded-Host", baseHost); a.Status != http.StatusOK {
		t.Fatalf("handshake: %d %s", a.Status, a.Body)
	}
	if got := w.st.Text(`SELECT base_url FROM bastion_contact ORDER BY contact_at DESC LIMIT 1`); got != baseURL {
		t.Fatalf("the reported base URL is %q", got)
	}
	w.anonymousPolicy()
	w.publishNPM("@acme/sdk", "1.0.0")
	w.publishNPM("plain-lib", "1.0.0")
	w.publishNuGet("Acme.Sdk", "1.0.0")

	// A publish reaches the change list within the debounce and a run.
	start := time.Now()
	var list iface.CatalogueChanges
	testenv.WaitFor(t, "the published packages in the catalogue", 30*time.Second, func() bool {
		list = w.changes(0)
		n, state := 0, false
		for _, e := range list.Entries {
			if strings.HasPrefix(e.Path, "packages/") && strings.HasSuffix(e.Path, "/versions/1.0.0") {
				n++
			}
			state = state || e.Path == "state"
		}
		return n == 3 && state
	})
	t.Logf("the three versions arrived %s after the publishes", time.Since(start).Round(100*time.Millisecond))

	// Every document names nothing of the main installation (B.13): no base URL of it, no other Bastion's, no /api/v1/.
	docs := w.documents(list)
	if len(docs) < 20 {
		t.Fatalf("only %d documents", len(docs))
	}
	for p, raw := range docs {
		for _, bad := range []string{"://" + mainHost, mirrorURL, "/api/v1/", "sa:publisher"} {
			if bytes.Contains(raw, []byte(bad)) {
				t.Errorf("%s names %q: %.300s", p, bad, raw)
			}
		}
	}
	var setup []byte
	for p, raw := range docs {
		if strings.HasPrefix(p, "setup/"+testenv.DefaultProject+"/npm-local/npm/") {
			setup = raw
		}
	}
	if !bytes.Contains(setup, []byte(baseHost)) {
		t.Errorf("the set-up of a version does not name the Bastion: %s", setup)
	}
	w.documentationFollowsTheWikiAndTheSwitch(&docsOn)

	// A level lowered to private: what fell out is deleted.
	cursor := list.Cursor
	w.mustAPI("PUT", "/api/v1/projects/"+testenv.DefaultProject+"/visibility", 200, map[string]any{"level": "private"}, "If-Match", "*")
	testenv.WaitFor(t, "the private packages deleted from the catalogue", 30*time.Second, func() bool {
		after := w.changes(cursor)
		live := w.changes(0)
		for _, e := range live.Entries {
			if strings.HasPrefix(e.Path, "packages/") {
				return false
			}
		}
		return len(after.Entries) > 0
	})

	// The web interface switched off: 404 at once, the catalogue deleted at the next run.
	gui.Store(false)
	if a := w.catalogue("GET", iface.PathCatalogue+"?after=0", nil); a.Status != http.StatusNotFound {
		t.Fatalf("gui off: %d %s", a.Status, a.Body)
	}
	w.mustAPI("POST", "/api/v1/system/tasks/bastion-catalogue/run", 202, nil)
	testenv.WaitFor(t, "the catalogue deleted", 30*time.Second, func() bool {
		return w.st.Text(`SELECT count(*)::text FROM bastion_catalogue`) == "0" && w.st.Text(`SELECT count(*)::text FROM bastion_catalogue_state`) == "0"
	})

	// Switched on again, the catalogue starts above every number of the one before: the cursor the Bastion kept answers reset
	// (R-BAS-108), as after a restore, which brings back no catalogue.
	gui.Store(true)
	w.mustAPI("POST", "/api/v1/system/tasks/bastion-catalogue/run", 202, nil)
	testenv.WaitFor(t, "the catalogue built again", 30*time.Second, func() bool {
		return w.st.Text(`SELECT count(*)::text FROM bastion_catalogue_state WHERE checked_at IS NOT NULL`) == "1"
	})
	if c := w.changes(cursor); !c.Reset {
		t.Fatalf("the cursor %d of the dropped catalogue does not answer reset: %+v", cursor, c)
	}
}
