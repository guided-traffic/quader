//go:build integration

// Package storageverify tests `quader admin storage verify` (storage.md R-STOR-058, ha-and-helm.md R-HA-024, R-HA-025) against
// the integration stack: the blob, copy and item rows of an installation database and the objects in its prefixes of the S3
// store, read through the same code the command runs.
package storageverify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/blob/s3"
	"github.com/guided-traffic/quader/internal/blob/verify"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/outbound"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

func TestMain(m *testing.M) { os.Exit(testenv.Main(m)) }

// conn is a connection of the test: its id, its prefix and its client.
type conn struct {
	id, prefix string
	objects    blob.ObjectStore
}

// defaultConn is the stack's own prefix as the connection default.
func defaultConn(t *testing.T, s *testenv.Stack) conn {
	t.Helper()
	objects, prefix := s.ObjectClient(t)
	return conn{id: config.DefaultConnectionID, prefix: prefix, objects: objects}
}

// secondConn is a second connection on the stack's bucket below a prefix of its own.
func secondConn(t *testing.T, s *testenv.Stack) conn {
	t.Helper()
	el := testenv.Connection(t, t.TempDir(), s.ObjectPrefix()+"-secondary")
	el.ID = "secondary"
	c, err := s3.NewFromFactory(context.Background(), config.ClientConfig(1, el, blobtest.Storage(t, t.TempDir()).ObjectStore),
		outbound.New(config.Outbound{TLS: config.OutboundTLS{UseSystemRoots: true}}))
	if err != nil {
		t.Fatalf("object store client: %v", err)
	}
	return conn{id: el.ID, prefix: el.KeyPrefix(), objects: c}
}

// seedItem stores an object on each connection and the blob, copy and item rows that reference it, the way a committed upload
// and the reconciliation leave them, and returns the digest.
func seedItem(t *testing.T, s *testenv.Stack, repo, path string, data []byte, conns ...conn) []byte {
	t.Helper()
	sum := sha256.Sum256(data)
	s.Exec(`INSERT INTO blobs (sha256, sha1, md5, sha512, size) VALUES ($1, decode(repeat('00', 20), 'hex'), decode(repeat('00', 16), 'hex'), decode(repeat('00', 64), 'hex'), $2) ON CONFLICT DO NOTHING`,
		sum[:], len(data))
	for _, c := range conns {
		key := blob.Key(c.prefix, blob.Digest(sum))
		put(t, c, key, data)
		s.Exec(`INSERT INTO storage_connections (id, bucket, prefix) VALUES ($1, 'bucket', $2) ON CONFLICT DO NOTHING`, c.id, c.prefix)
		s.Exec(`INSERT INTO blob_copies (sha256, connection_id, object_key, stored_size, state) VALUES ($1, $2, $3, $4, 'present')`, sum[:], c.id, key, len(data))
	}
	s.Exec(`INSERT INTO items (repo_id, path, parent_path, kind, blob_sha256, size)
		SELECT r.id, $2::text, '', 'file', $3, $4 FROM repositories r WHERE r.key = $1`, repo, path, sum[:], len(data))
	return sum[:]
}

func put(t *testing.T, c conn, key string, data []byte) {
	t.Helper()
	if err := c.objects.Put(context.Background(), key, bytes.NewReader(data), int64(len(data)), blob.Checksum{}, nil); err != nil {
		t.Fatalf("put the object: %v", err)
	}
}

func remove(t *testing.T, c conn, digest []byte) {
	t.Helper()
	if err := c.objects.Delete(context.Background(), blob.Key(c.prefix, blob.Digest(digest))); err != nil {
		t.Fatalf("delete the object: %v", err)
	}
}

func run(t *testing.T, s *testenv.Stack, o verify.Options, conns ...conn) (*verify.Report, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := postgres.Open(ctx, s.DatabaseConfig(), "verify-it", meta.RoleWeb, testenv.Quiet())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)
	o.DB = verify.PGSource{DB: db}
	for _, c := range conns {
		o.Connections = append(o.Connections, verify.Conn{ID: c.id, Objects: c.objects})
	}
	return verify.Run(ctx, o)
}

// TestR_HA_024_StorageVerifyNamesTheItemsWhoseObjectIsGone: with objects deleted behind the installation's back, the report
// lists exactly the items that reference them (a trashed one included), and it is clean when every object is there. The table
// is read in pages smaller than itself.
func TestR_HA_024_StorageVerifyNamesTheItemsWhoseObjectIsGone(t *testing.T) {
	s := testenv.NewStack(t, testenv.StackOptions{Name: "verify"})
	s.Repository("npm-local", "local", "npm", "{}")
	d := defaultConn(t, s)
	var digests [][]byte
	for i := range 12 {
		digests = append(digests, seedItem(t, s, "npm-local", fmt.Sprintf("pkg-%02d/-/pkg-%02d-1.0.0.tgz", i, i), fmt.Appendf(nil, "tarball %d", i), d))
	}
	rep, err := run(t, s, verify.Options{PageSize: 5}, d)
	if err != nil || !rep.OK() || rep.Checked != 12 {
		t.Fatalf("clean store: rep = %+v, err = %v", rep, err)
	}

	s.DeleteObject(t, digests[2])
	s.DeleteObject(t, digests[9])
	s.Exec(`UPDATE items SET trashed_at = now() WHERE blob_sha256 = $1`, digests[9])
	rep, err = run(t, s, verify.Options{PageSize: 5}, d)
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK() || rep.Lost != 2 || rep.Missing != 2 || rep.SizeMismatch != 0 || rep.ItemsAffected != 2 || rep.Checked != 12 {
		t.Fatalf("rep = %+v, want 2 lost blobs and 2 affected items of 12 checked", rep)
	}
	seen := map[string]verify.Item{}
	for _, f := range rep.Findings {
		if f.Kind != verify.KindMissing || len(f.Items) != 1 {
			t.Fatalf("finding = %+v", f)
		}
		seen[f.Items[0].Path] = f.Items[0]
	}
	if it, ok := seen["pkg-02/-/pkg-02-1.0.0.tgz"]; !ok || it.Project != testenv.DefaultProject || it.Repository != "npm-local" || it.Trashed {
		t.Errorf("item of the first deleted object = %+v (found %v)", it, ok)
	}
	if it, ok := seen["pkg-09/-/pkg-09-1.0.0.tgz"]; !ok || !it.Trashed {
		t.Errorf("item of the second deleted object = %+v (found %v), want it reported as trashed", it, ok)
	}

	s.PutObject(t, digests[2], []byte("tarball 2"))
	s.PutObject(t, digests[9], []byte("tarball 9"))
	if rep, err = run(t, s, verify.Options{}, d); err != nil || !rep.OK() {
		t.Fatalf("after the objects are back: rep = %+v, err = %v", rep, err)
	}
}

// TestR_HA_025_StorageVerifySinceNarrowsTheRun: a cut-off after every row checks nothing, one before them checks all, and a blob
// of another size is reported as such.
func TestR_HA_025_StorageVerifySinceNarrowsTheRun(t *testing.T) {
	s := testenv.NewStack(t, testenv.StackOptions{Name: "verifysince"})
	s.Repository("npm-local", "local", "npm", "{}")
	d := defaultConn(t, s)
	old := seedItem(t, s, "npm-local", "old/-/old-1.0.0.tgz", []byte("old tarball"), d)
	s.Exec(`UPDATE blobs SET created_at = now() - interval '2 hours', state_changed_at = now() - interval '2 hours' WHERE sha256 = $1`, old)
	fresh := seedItem(t, s, "npm-local", "new/-/new-1.0.0.tgz", []byte("new tarball"), d)
	s.DeleteObject(t, old)
	s.PutObject(t, fresh, []byte("shorter"))

	rep, err := run(t, s, verify.Options{Since: time.Now().Add(-time.Hour)}, d)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Checked != 1 || rep.Missing != 0 || rep.SizeMismatch != 1 {
		t.Fatalf("since one hour: rep = %+v, want only the fresh blob, of another size", rep)
	}
	rep, err = run(t, s, verify.Options{Since: time.Now().Add(-3 * time.Hour)}, d)
	if err != nil || rep.Checked != 2 || rep.Missing != 1 || rep.SizeMismatch != 1 {
		t.Fatalf("since three hours: rep = %+v, err = %v", rep, err)
	}
}

// TestR_STOR_058_StorageVerifyChecksEveryCopyOnTwoConnections: with the object of one blob deleted on the first connection only,
// nothing is lost and the missing copy is listed; deleted on both, the blob is lost; with --connection default and the first
// object deleted, the copy is a finding of that connection.
func TestR_STOR_058_StorageVerifyChecksEveryCopyOnTwoConnections(t *testing.T) {
	s := testenv.NewStack(t, testenv.StackOptions{Name: "verifytwo"})
	s.Repository("npm-local", "local", "npm", "{}")
	first, second := defaultConn(t, s), secondConn(t, s)
	var digests [][]byte
	for i := range 3 {
		digests = append(digests, seedItem(t, s, "npm-local", fmt.Sprintf("two-%d.tgz", i), fmt.Appendf(nil, "two tarball %d", i), first, second))
	}
	remove(t, first, digests[0])
	rep, err := run(t, s, verify.Options{}, first, second)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Lost != 0 || len(rep.Copies) != 1 || rep.Copies[0].Connection != "default" || rep.ConnectionFindings("secondary") != 0 {
		t.Fatalf("one copy gone: rep = %+v", rep)
	}
	rep, err = run(t, s, verify.Options{Connection: "default"}, first, second)
	if err != nil || rep.Lost != 0 || rep.ConnectionFindings("default") != 1 {
		t.Fatalf("--connection default: rep = %+v, err = %v", rep, err)
	}
	remove(t, second, digests[0])
	rep, err = run(t, s, verify.Options{}, first, second)
	if err != nil || rep.Lost != 1 || rep.Findings[0].ItemCount != 1 || len(rep.Copies) != 2 {
		t.Fatalf("both copies gone: rep = %+v, err = %v", rep, err)
	}
}
