// Package tuftest builds a TUF repository with keys made in the test and serves it over TLS with net/http/httptest (signatures.md
// R-SIG-017): the unit tests of internal/signatures/shipped and the integration test of the task signature-trust-root refresh against
// it. The repository uses consistent snapshots as the public Sigstore TUF repository does, one Ed25519 key signs every role, and every
// role expires a week after it was signed, so that no test depends on the date. TrustedRoot makes a Sigstore trusted_root.json of keys
// made in the test, to serve as a target or to put into an anchor.
package tuftest

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"path"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sigstore/sigstore/pkg/signature"
	"github.com/theupdateframework/go-tuf/v2/metadata"
)

// lifetime is how long a role's metadata is valid after it was signed.
const lifetime = 7 * 24 * time.Hour

// Repo is a TUF repository served by a TLS test server. Its methods are safe for concurrent use with the server.
type Repo struct {
	// URL is the repository's base URL (the server's, no trailing slash).
	URL string

	t      testing.TB
	srv    *httptest.Server
	signer signature.Signer
	key    *metadata.Key

	mu      sync.Mutex
	files   map[string][]byte // the served path below URL -> the document
	root    []byte            // 1.root.json
	version int64             // the version of targets, snapshot and timestamp
	targets map[string]string // the target's name -> its served path
}

// New starts a repository whose first targets version holds targets (name -> document; a name may hold one directory, as
// registry.npmjs.org/keys.json does). The server stops when the test ends.
func New(t testing.TB, targets map[string][]byte) *Repo {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("tuftest: key: %v", err)
	}
	signer, err := signature.LoadSigner(priv, crypto.Hash(0))
	if err != nil {
		t.Fatalf("tuftest: signer: %v", err)
	}
	key, err := metadata.KeyFromPublicKey(priv.Public())
	if err != nil {
		t.Fatalf("tuftest: key: %v", err)
	}
	r := &Repo{t: t, signer: signer, key: key, files: map[string][]byte{}, targets: map[string]string{}}
	r.srv = httptest.NewTLSServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.srv.Close)
	r.URL = r.srv.URL
	r.signRoot()
	r.Publish(targets)
	return r
}

// Root returns the repository's root.json of version 1: the TUF root a client starts its chain from (shipped.RefreshOptions.Root).
func (r *Repo) Root() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]byte(nil), r.root...)
}

// Client returns an HTTP client that trusts the server's certificate.
func (r *Repo) Client() *http.Client { return r.srv.Client() }

// Certificate returns the server's TLS certificate, for a client that is built elsewhere.
func (r *Repo) Certificate() *x509.Certificate { return r.srv.Certificate() }

// Version returns the version of the targets, snapshot and timestamp metadata now served: one number, raised by each Publish.
func (r *Repo) Version() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.version
}

// Publish signs a new version of the targets, the snapshot and the timestamp that holds targets; a target left out is no longer
// listed.
func (r *Repo) Publish(targets map[string][]byte) {
	r.t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.version++
	now := time.Now().UTC()
	tm := metadata.Targets(now.Add(lifetime))
	tm.Signed.Version = r.version
	r.targets = map[string]string{}
	for name, doc := range targets {
		tf, err := metadata.TargetFile().FromBytes(name, doc, "sha256")
		if err != nil {
			r.t.Fatalf("tuftest: target %s: %v", name, err)
		}
		tm.Signed.Targets[name] = tf
		served := hashedPath(name, doc)
		r.targets[name] = served
		r.files[served] = append([]byte(nil), doc...)
	}
	r.put(strconv.FormatInt(r.version, 10)+".targets.json", sign(r.t, tm, r.signer))

	sm := metadata.Snapshot(now.Add(lifetime))
	sm.Signed.Version = r.version
	sm.Signed.Meta["targets.json"] = metadata.MetaFile(r.version)
	r.put(strconv.FormatInt(r.version, 10)+".snapshot.json", sign(r.t, sm, r.signer))

	ts := metadata.Timestamp(now.Add(lifetime))
	ts.Signed.Version = r.version
	ts.Signed.Meta["snapshot.json"] = metadata.MetaFile(r.version)
	r.put("timestamp.json", sign(r.t, ts, r.signer))
}

// Serve replaces the file of a listed target with doc without signing it: a target not signed by the chain, which a client must refuse.
func (r *Repo) Serve(name string, doc []byte) {
	r.t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	served, ok := r.targets[name]
	if !ok {
		r.t.Fatalf("tuftest: %s is no target of version %d", name, r.version)
	}
	r.files[served] = append([]byte(nil), doc...)
}

// signRoot makes root.json version 1, consistent snapshots, the one key in every role.
func (r *Repo) signRoot() {
	r.t.Helper()
	rm := metadata.Root(time.Now().UTC().Add(lifetime))
	for _, role := range []string{metadata.ROOT, metadata.TIMESTAMP, metadata.SNAPSHOT, metadata.TARGETS} {
		if err := rm.Signed.AddKey(r.key, role); err != nil {
			r.t.Fatalf("tuftest: root: %v", err)
		}
	}
	r.root = sign(r.t, rm, r.signer)
	r.put("1.root.json", r.root)
}

func (r *Repo) put(name string, doc []byte) { r.files[name] = doc }

func (r *Repo) serve(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	doc, ok := r.files[strings.TrimPrefix(req.URL.Path, "/")]
	r.mu.Unlock()
	if !ok || req.Method != http.MethodGet {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(doc)
}

// hashedPath is the path a target is served at under consistent snapshots: targets/<dir>/<sha256>.<name>.
func hashedPath(name string, doc []byte) string {
	sum := sha256.Sum256(doc)
	dir, file := path.Split(name)
	return path.Join("targets", dir, hex.EncodeToString(sum[:])+"."+file)
}

// signable is a TUF role's metadata.
type signable interface {
	Sign(signature.Signer) (*metadata.Signature, error)
	ToBytes(bool) ([]byte, error)
}

func sign(t testing.TB, m signable, s signature.Signer) []byte {
	t.Helper()
	if _, err := m.Sign(s); err != nil {
		t.Fatalf("tuftest: sign: %v", err)
	}
	b, err := m.ToBytes(true)
	if err != nil {
		t.Fatalf("tuftest: marshal: %v", err)
	}
	return b
}
