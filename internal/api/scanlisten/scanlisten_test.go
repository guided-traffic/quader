package scanlisten

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/malware"
	"github.com/guided-traffic/quader/internal/malware/scanread"
	"github.com/guided-traffic/quader/internal/qname"
)

var npmLocal = qname.Repo{Project: "web", Key: "npm-local"}

// fakeStore is the blob store and the holdings of a test: blobs by digest, and the repositories that hold each.
type fakeStore struct {
	blobs   map[blob.Digest][]byte
	holders map[blob.Digest][]qname.Repo
	missing map[blob.Digest]bool
	opened  int
}

func (f *fakeStore) Held(_ context.Context, repo qname.Repo, d blob.Digest) (scanread.Held, bool, error) {
	for i, h := range f.holders[d] {
		if h == repo {
			return scanread.Held{RepoID: int64(i + 1), Size: int64(len(f.blobs[d]))}, true, nil
		}
	}
	return scanread.Held{}, false, nil
}

func (f *fakeStore) Open(_ context.Context, d blob.Digest, size int64, _ blob.OpenOptions) (io.ReadCloser, error) {
	f.opened++
	b, ok := f.blobs[d]
	if !ok || f.missing[d] {
		return nil, blob.ErrNotFound
	}
	if int64(len(b)) != size {
		return nil, errors.New("size")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (f *fakeStore) put(b []byte, holders ...qname.Repo) blob.Digest {
	d := blob.Digest(sha256.Sum256(b))
	f.blobs[d] = b
	f.holders[d] = holders
	return d
}

type fakeRow struct{ db malware.Database }

func (f fakeRow) Database(context.Context) (malware.Database, error) { return f.db, nil }

type env struct {
	st  *fakeStore
	row *fakeRow
	mux *http.ServeMux
}

func newEnv() *env {
	e := &env{st: &fakeStore{blobs: map[blob.Digest][]byte{}, holders: map[blob.Digest][]qname.Repo{}, missing: map[blob.Digest]bool{}}, row: &fakeRow{}}
	e.mux = http.NewServeMux()
	Register(e.mux, Options{Holdings: e.st, Blobs: e.st, Database: scanread.RowSource{Row: e.row, Blobs: e.st}, Log: slog.New(slog.DiscardHandler)})
	return e
}

// do sends a request as the stage of the scan listener hands it on: with the principal p.
func (e *env) do(method, path string, p httpserver.Principal) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r = r.WithContext(httpserver.WithPrincipal(r.Context(), p))
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, r)
	return w
}

func blobReader(repo qname.Repo, d blob.Digest) httpserver.Principal {
	return httpserver.Principal{
		Kind: httpserver.KindSystem, Name: registrytoken.BlobSubject,
		Credential: httpserver.Credential{Method: httpserver.MethodBearer, ScanID: "sid", ScanBlob: &httpserver.ScanBlob{Repo: repo, Digest: "sha256:" + d.Hex()}},
	}
}

func certified() httpserver.Principal {
	return httpserver.Principal{Kind: httpserver.KindSystem, Name: registrytoken.BlobSubject, Credential: httpserver.Credential{Method: httpserver.MethodClientCertificate}}
}

// R-MAL-006, Q-AUTHZ-340: the blob route serves the token's blob whole, with its length and digest, while the token's repository holds
// it, and answers 404 for every other case without saying which; HEAD answers the headers.
func TestR_MAL_006_TheBlobRouteServesTheTokensBlobWhileItsRepositoryHoldsIt(t *testing.T) {
	e := newEnv()
	content := []byte("a kilobyte would do; eight bytes do too")
	d := e.st.put(content, npmLocal)
	path := registrytoken.BlobPath("sha256:" + d.Hex())

	w := e.do("GET", path, blobReader(npmLocal, d))
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), content) {
		t.Fatalf("GET: %d %q", w.Code, w.Body.String())
	}
	h := w.Header()
	if h.Get("Content-Type") != "application/octet-stream" || h.Get("Content-Length") != "39" ||
		h.Get("Repr-Digest") != "sha-256=:"+base64.StdEncoding.EncodeToString(d[:])+":" || h.Get("Accept-Ranges") != "none" ||
		h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers %v", h)
	}
	t.Run("HEAD answers the headers without reading the blob", func(t *testing.T) {
		before := e.st.opened
		w := e.do("HEAD", path, blobReader(npmLocal, d))
		if w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Length") != "39" || e.st.opened != before {
			t.Fatalf("HEAD: %d %v, opened %d", w.Code, w.Header(), e.st.opened-before)
		}
	})
	t.Run("a Range is ignored: the scanner reads the whole blob", func(t *testing.T) {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Range", "bytes=0-3")
		r = r.WithContext(httpserver.WithPrincipal(r.Context(), blobReader(npmLocal, d)))
		w := httptest.NewRecorder()
		e.mux.ServeHTTP(w, r)
		if w.Code != 200 || w.Body.Len() != len(content) {
			t.Fatalf("%d %d", w.Code, w.Body.Len())
		}
	})
	t.Run("404 without saying why", func(t *testing.T) {
		gone := e.st.put([]byte("object gone"), npmLocal)
		e.st.missing[gone] = true
		other := e.st.put([]byte("in another repository"), qname.Repo{Project: "web", Key: "files-internal"})
		for name, c := range map[string]struct {
			path string
			p    httpserver.Principal
		}{
			"a repository that does not hold it": {registrytoken.BlobPath("sha256:" + other.Hex()), blobReader(npmLocal, other)},
			"a blob whose object is missing":     {registrytoken.BlobPath("sha256:" + gone.Hex()), blobReader(npmLocal, gone)},
			"a token for another digest":         {path, blobReader(npmLocal, other)},
			"no blob token":                      {path, certified()},
			"another system subject":             {path, httpserver.Principal{Kind: httpserver.KindSystem, Name: registrytoken.ScanSubject}},
		} {
			w := e.do("GET", c.path, c.p)
			if w.Code != 404 || bytes.Contains(w.Body.Bytes(), []byte("in another repository")) {
				t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
			}
		}
	})
}

// R-MAL-006, Q-MAL-27: the database routes list the version in use and serve a file of it or of the previous version by the digest the
// row names; a version or a name the row does not name, and a file whose bytes are gone, answer 404.
func TestR_MAL_006_TheDatabaseRoutesServeTheVersionsTheRowNames(t *testing.T) {
	e := newEnv()
	mainCvd, daily, oldDaily := []byte("ClamAV-VDB:main"), []byte("ClamAV-VDB:daily-2"), []byte("ClamAV-VDB:daily-1")
	file := func(name string, b []byte) malware.DatabaseFile {
		d := e.st.put(b)
		return malware.DatabaseFile{Name: name, SHA256: hex.EncodeToString(d[:]), Size: int64(len(b))}
	}
	built := time.Date(2026, 11, 20, 6, 0, 0, 0, time.UTC)
	e.row.db = malware.Database{
		Version: "main-62.daily-2.bytecode-339", Files: []malware.DatabaseFile{file("main.cvd", mainCvd), file("daily.cld", daily)}, BuiltAt: built,
		Previous: &malware.DatabaseVersion{Version: "main-62.daily-1.bytecode-339", Files: []malware.DatabaseFile{file("main.cvd", mainCvd), file("daily.cld", oldDaily)}},
	}

	w := e.do("GET", registrytoken.DatabasePath, certified())
	var got Database
	if err := json.Unmarshal(w.Body.Bytes(), &got); w.Code != 200 || err != nil {
		t.Fatalf("%d %s %v", w.Code, w.Body.String(), err)
	}
	if got.Version != e.row.db.Version || len(got.Files) != 2 || got.Files[1] != e.row.db.Files[1] {
		t.Fatalf("listing %+v", got)
	}
	for name, c := range map[string]struct {
		path string
		want []byte
	}{
		"a file of the version in use":   {registrytoken.DatabasePath + "/main-62.daily-2.bytecode-339/daily.cld", daily},
		"a file of the previous version": {registrytoken.DatabasePath + "/main-62.daily-1.bytecode-339/daily.cld", oldDaily},
	} {
		w := e.do("GET", c.path, certified())
		sum := sha256.Sum256(c.want)
		if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), c.want) || w.Header().Get("Repr-Digest") != "sha-256=:"+base64.StdEncoding.EncodeToString(sum[:])+":" {
			t.Errorf("%s: %d %q %v", name, w.Code, w.Body.String(), w.Header())
		}
	}
	gone := file("bytecode.cvd", []byte("ClamAV-VDB:bytecode"))
	e.st.missing[mustDigest(t, gone.SHA256)] = true
	e.row.db.Files = append(e.row.db.Files, gone)
	for name, path := range map[string]string{
		"a version the row does not name":  registrytoken.DatabasePath + "/main-61.daily-1.bytecode-338/main.cvd",
		"a name the version does not list": registrytoken.DatabasePath + "/main-62.daily-2.bytecode-339/freshclam.dat",
		"a file whose bytes are gone":      registrytoken.DatabasePath + "/main-62.daily-2.bytecode-339/bytecode.cvd",
	} {
		if w := e.do("GET", path, certified()); w.Code != 404 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	t.Run("before the first version the listing is empty", func(t *testing.T) {
		e.row.db = malware.Database{}
		w := e.do("GET", registrytoken.DatabasePath, certified())
		if w.Code != 200 || w.Body.String() != `{"version":"","files":[]}` {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("another principal is answered 404", func(t *testing.T) {
		if w := e.do("GET", registrytoken.DatabasePath, httpserver.Principal{Kind: httpserver.KindSystem, Name: registrytoken.ScanSubject}); w.Code != 404 {
			t.Fatalf("%d", w.Code)
		}
	})
}

func mustDigest(t *testing.T, h string) blob.Digest {
	t.Helper()
	d, ok := blob.ParseDigest(h)
	if !ok {
		t.Fatal(h)
	}
	return d
}
