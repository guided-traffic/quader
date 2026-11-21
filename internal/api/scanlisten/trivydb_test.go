package scanlisten

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/vuln/imagescan"
)

// trivySource is the database in use and the layers its repository holds.
type trivySource struct {
	listing imagescan.DatabaseListing
	layers  map[string][]byte
	// serve, when set, is served under the digest in use in place of its layer (a tampered store).
	serve []byte
}

func (s *trivySource) InUse(context.Context) (imagescan.DatabaseListing, error) {
	return s.listing, nil
}

func (s *trivySource) Open(_ context.Context, digest string) (io.ReadCloser, int64, error) {
	b, ok := s.layers[digest]
	if digest != s.listing.Digest || !ok {
		return nil, 0, ErrNoTrivyDatabase
	}
	if s.serve != nil {
		b = s.serve
	}
	return io.NopCloser(bytes.NewReader(b)), int64(len(b)), nil
}

func imageScannerCert() httpserver.Principal {
	return httpserver.Principal{Kind: httpserver.KindSystem, Name: registrytoken.ScanSubject, Credential: httpserver.Credential{Method: httpserver.MethodClientCertificate}}
}

// R-VULN-049, Q-VULN-206: the listing names the database in use, its layer's size and its build time; the layer route serves the layer
// of that digest whole with its length and Repr-Digest; another digest, a layer the repository does not hold and another principal are
// 404 (the stage refuses a token, another kind's certificate and no certificate with 401 before a handler runs).
func TestR_VULN_049_TheDatabaseRoutesServeTheDatabaseInUse(t *testing.T) {
	layer := []byte("the layer of the database: bytes stand for the 142.6 MB")
	sum := sha256.Sum256(layer)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	built := time.Date(2026, 11, 21, 7, 2, 51, 0, time.UTC)
	src := &trivySource{layers: map[string][]byte{digest: layer}}
	mux := http.NewServeMux()
	RegisterTrivyDatabase(mux, TrivyOptions{Source: src, Log: slog.New(slog.DiscardHandler)})
	do := func(path string, p httpserver.Principal) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r = r.WithContext(httpserver.WithPrincipal(r.Context(), p))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}

	w := do(registrytoken.TrivyDatabasePath, imageScannerCert())
	if w.Code != 200 || w.Body.String() != `{"digest":""}` {
		t.Fatalf("before the first database: %d %s", w.Code, w.Body.String())
	}
	src.listing = imagescan.DatabaseListing{Digest: digest, Size: int64(len(layer)), BuiltAt: &built}
	w = do(registrytoken.TrivyDatabasePath, imageScannerCert())
	var got imagescan.DatabaseListing
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != 200 || got.Digest != digest || got.Size != int64(len(layer)) ||
		got.BuiltAt == nil || !got.BuiltAt.Equal(built) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("the listing: %d %s", w.Code, w.Body.String())
	}

	w = do(registrytoken.TrivyDatabasePath+"/"+digest, imageScannerCert())
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), layer) || w.Header().Get("Content-Length") != "55" ||
		w.Header().Get("Repr-Digest") != "sha-256=:"+base64.StdEncoding.EncodeToString(sum[:])+":" ||
		w.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("the layer: %d %v %q", w.Code, w.Header(), w.Body.String())
	}

	other := "sha256:" + hex.EncodeToString(make([]byte, 32))
	for name, c := range map[string]struct {
		path string
		p    httpserver.Principal
	}{
		"another digest":              {registrytoken.TrivyDatabasePath + "/" + other, imageScannerCert()},
		"no digest":                   {registrytoken.TrivyDatabasePath + "/latest", imageScannerCert()},
		"the malware scanner":         {registrytoken.TrivyDatabasePath + "/" + digest, certified()},
		"an image's scan token":       {registrytoken.TrivyDatabasePath + "/" + digest, httpserver.Principal{Kind: httpserver.KindSystem, Name: registrytoken.ScanSubject, Credential: httpserver.Credential{Method: httpserver.MethodBearer, Registry: &httpserver.RegistryGrant{}}}},
		"the listing to the malware":  {registrytoken.TrivyDatabasePath, certified()},
		"no principal of the scanner": {registrytoken.TrivyDatabasePath, httpserver.Principal{}},
	} {
		if w := do(c.path, c.p); w.Code != http.StatusNotFound {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	src.layers = map[string][]byte{}
	if w := do(registrytoken.TrivyDatabasePath+"/"+digest, imageScannerCert()); w.Code != http.StatusNotFound {
		t.Errorf("a layer the repository no longer holds: %d", w.Code)
	}

	t.Run("stored bytes that are not the digest abort the answer", func(t *testing.T) {
		src.layers = map[string][]byte{digest: layer}
		src.serve = append([]byte(nil), layer...)
		src.serve[0] ^= 0xff
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mux.ServeHTTP(w, r.WithContext(httpserver.WithPrincipal(r.Context(), imageScannerCert())))
		}))
		defer srv.Close()
		res, err := http.Get(srv.URL + registrytoken.TrivyDatabasePath + "/" + digest)
		if err != nil {
			return // the connection was aborted before the status line: nothing complete was answered
		}
		defer res.Body.Close()
		if _, err := io.ReadAll(res.Body); err == nil {
			t.Fatal("a body that is not the digest was answered complete")
		}
	})
}
