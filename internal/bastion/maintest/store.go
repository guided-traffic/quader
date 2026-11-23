package maintest

import (
	"encoding/pem"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

// Store is an object store for the tests of the handover per blob (R-BAS-074, R-BAS-075): an httptest TLS server that
// answers a GET of an object's path with the bytes a test put there, records every request, and can be told to refuse, to
// lie about a length, to redirect, to cut a body or to hold it after some bytes. It knows nothing of signatures and of the
// stored format: a link is the object's URL with a query that stands for a signature, and a test seals an encrypted
// object itself and puts its stored bytes. The bytes are the main installation's objects as its store holds them; the
// Bastion reads them with its link client and no credential.
type Store struct {
	srv *httptest.Server
	t   testing.TB

	mu       sync.Mutex
	objects  map[string][]byte
	status   map[string]int
	length   map[string]int64
	cut      map[string]int
	holds    map[string]*storeHold
	redirect map[string]string
	reqs     []StoreRequest
}

// StoreRequest is a request the store received.
type StoreRequest struct {
	Method   string
	Path     string
	RawQuery string
	Header   http.Header
}

type storeHold struct {
	after   int
	release chan struct{}
	reached chan struct{}
	once    sync.Once
}

// NewStore starts the store. It is closed with the test.
func NewStore(t testing.TB) *Store {
	t.Helper()
	s := &Store{
		t: t, objects: map[string][]byte{}, status: map[string]int{}, length: map[string]int64{}, cut: map[string]int{},
		holds: map[string]*storeHold{}, redirect: map[string]string{},
	}
	s.srv = httptest.NewUnstartedServer(http.HandlerFunc(s.serve))
	// A refused handshake and a cut body are what some tests want; the server's own log lines about them are noise.
	s.srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	s.srv.StartTLS()
	t.Cleanup(s.srv.Close)
	return s
}

// URL is the store's origin (https, 127.0.0.1).
func (s *Store) URL() string { return s.srv.URL }

// CAFile writes the store's certificate as a PEM bundle into dir and returns its path.
func (s *Store) CAFile(dir string) string {
	s.t.Helper()
	path := filepath.Join(dir, "store-ca.pem")
	if err := os.WriteFile(path, s.PEM(), 0o600); err != nil {
		s.t.Fatal(err)
	}
	return path
}

// PEM is the store's certificate in PEM, for a bundle that trusts the store and something else.
func (s *Store) PEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.srv.Certificate().Raw})
}

// Put stores b under path ("/quader-blobs/sha256/…").
func (s *Store) Put(path string, b []byte) {
	s.mu.Lock()
	s.objects[path] = append([]byte(nil), b...)
	s.mu.Unlock()
}

// Link is a link for a GET of path: the object's URL with a query that stands for a signature.
func (s *Store) Link(path string) string {
	return s.srv.URL + path + "?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Expires=60&X-Amz-Signature=" + strconv.Itoa(len(path))
}

// SetStatus makes a GET of path answer status with no body; zero removes it.
func (s *Store) SetStatus(path string, status int) { s.lock(func() { s.status[path] = status }) }

// SetLength makes a GET of path announce n bytes in Content-Length whatever it holds; a negative n removes it.
func (s *Store) SetLength(path string, n int64) { s.lock(func() { s.length[path] = n }) }

// CutAfter makes a GET of path break the connection after n bytes of the body; a negative n removes it.
func (s *Store) CutAfter(path string, n int) { s.lock(func() { s.cut[path] = n }) }

// Redirect makes a GET of path answer 302 to to.
func (s *Store) Redirect(path, to string) { s.lock(func() { s.redirect[path] = to }) }

// HoldAfter makes the next GET of path send n bytes of the body and wait until release is called (or the request ends).
// reached is closed once the n bytes are written.
func (s *Store) HoldAfter(path string, n int) (release func(), reached <-chan struct{}) {
	h := &storeHold{after: n, release: make(chan struct{}), reached: make(chan struct{})}
	s.lock(func() { s.holds[path] = h })
	return func() { h.once.Do(func() { close(h.release) }) }, h.reached
}

// Requests returns the requests received so far.
func (s *Store) Requests() []StoreRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]StoreRequest(nil), s.reqs...)
}

func (s *Store) lock(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f()
}

func (s *Store) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.reqs = append(s.reqs, StoreRequest{Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery, Header: r.Header.Clone()})
	body, ok := s.objects[r.URL.Path]
	status, to := s.status[r.URL.Path], s.redirect[r.URL.Path]
	length, lie := s.length[r.URL.Path]
	cut, cutting := s.cut[r.URL.Path]
	hold := s.holds[r.URL.Path]
	delete(s.holds, r.URL.Path)
	s.mu.Unlock()
	switch {
	case to != "":
		http.Redirect(w, r, to, http.StatusFound)
		return
	case status != 0:
		w.WriteHeader(status)
		return
	case !ok:
		http.NotFound(w, r)
		return
	}
	n := int64(len(body))
	if lie && length >= 0 {
		n = length
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(n, 10))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	if hold != nil && hold.after < len(body) {
		_, _ = w.Write(body[:hold.after])
		_ = http.NewResponseController(w).Flush()
		close(hold.reached)
		select {
		case <-hold.release:
		case <-r.Context().Done():
			return
		}
		body = body[hold.after:]
		if cutting {
			cut -= hold.after
		}
	}
	if cutting && cut >= 0 && cut < len(body) {
		_, _ = w.Write(body[:cut])
		_ = http.NewResponseController(w).Flush()
		panic(http.ErrAbortHandler) // the connection breaks off: the client sees a body that ends early
	}
	_, _ = w.Write(body)
}
