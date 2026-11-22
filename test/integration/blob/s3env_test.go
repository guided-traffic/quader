//go:build integration

// Package blob tests the blob store against the integration stack: the
// object-store conformance check (it needs only the QUADER_TEST_S3_* variables, so
// the nightly MinIO and AWS legs run it with `-run Conformance` and no
// PostgreSQL or Valkey), and the commit protocol and garbage collection against
// PostgreSQL and the S3 store.
//
// Variables read (testenv):
//
//	QUADER_TEST_S3_ENDPOINT, _REGION, _ACCESS_KEY, _SECRET_KEY, _BUCKET_BLOBS, _FORCE_PATH_STYLE
//
// and optionally, for stores with other settings (R-STOR-030):
//
//	QUADER_TEST_S3_PAYLOAD_CHECKSUM   sha256 | md5 | none      (default sha256)
//	QUADER_TEST_S3_REQUEST_CHECKSUM   when_supported | when_required
//	QUADER_TEST_S3_RESPONSE_CHECKSUM  when_supported | when_required
//	QUADER_TEST_S3_BATCH_DELETE       true | false             (default true)
//
// The database tests also read QUADER_TEST_PG_*; nothing here reads
// QUADER_TEST_VALKEY_*: the blob store needs no Valkey (R-STOR-046).
package blob

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/blob/s3"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/outbound"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// runID separates the keys of this test run from every other run in the shared
// bucket.
func runID(tb testing.TB) string {
	tb.Helper()
	var b [5]byte
	if _, err := rand.Read(b[:]); err != nil {
		tb.Fatal(err)
	}
	return hex.EncodeToString(b[:])
}

func optional(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// s3Env is the object store under test: one connection, default.
type s3Env struct {
	conn    config.StorageConnection // the element of the connection
	cfg     config.StorageS3         // its client's configuration
	factory *outbound.Factory
	http    *http.Client
	raw     blob.ObjectStore // the client, without a breaker
	prefix  string
}

// newS3 builds the client for the shared bucket under a prefix of its own and
// removes everything below that prefix when the test ends.
func newS3(tb testing.TB, name string) *s3Env {
	tb.Helper()
	dir := tb.TempDir()
	writeSecret := func(file, v string) string {
		p := filepath.Join(dir, file)
		if err := os.WriteFile(p, []byte(v+"\n"), 0o600); err != nil {
			tb.Fatal(err)
		}
		return p
	}
	prefix := "it-blob/" + runID(tb) + "/" + name
	batch := optional("QUADER_TEST_S3_BATCH_DELETE", "true") == "true"
	conn := config.StorageConnection{
		Endpoint: testenv.Get(tb, "QUADER_TEST_S3_ENDPOINT"), Region: testenv.Get(tb, "QUADER_TEST_S3_REGION"),
		Bucket: testenv.Get(tb, "QUADER_TEST_S3_BUCKET_BLOBS"), Prefix: &prefix,
		ForcePathStyle:             testenv.Get(tb, "QUADER_TEST_S3_FORCE_PATH_STYLE") == "true",
		CAFile:                     testenv.CAFile(tb), // the gateway is HTTPS-only, signed by the stack's CA
		PayloadChecksum:            optional("QUADER_TEST_S3_PAYLOAD_CHECKSUM", "sha256"),
		RequestChecksumCalculation: optional("QUADER_TEST_S3_REQUEST_CHECKSUM", "when_supported"),
		ResponseChecksumValidation: optional("QUADER_TEST_S3_RESPONSE_CHECKSUM", "when_supported"),
		BatchDelete:                &batch,
		CredentialsSource:          "static",
		AccessKeyIDFile:            writeSecret("access-key", testenv.Get(tb, "QUADER_TEST_S3_ACCESS_KEY")),
		SecretAccessKeyFile:        writeSecret("secret-key", testenv.Get(tb, "QUADER_TEST_S3_SECRET_KEY")),
	}.WithDefaults()
	cfg := config.ClientConfig(0, conn, blobtest.Storage(tb, dir).ObjectStore)
	f := outbound.New(config.Outbound{TLS: config.OutboundTLS{UseSystemRoots: true}})
	c, err := s3.NewFromFactory(context.Background(), cfg, f)
	if err != nil {
		tb.Fatalf("object store client: %v", err)
	}
	testenv.RemoveObjectsAtCleanup(tb, cfg.Bucket, cfg.Prefix)
	e := &s3Env{conn: conn, cfg: cfg, factory: f, http: c.HTTPClient(), raw: c, prefix: cfg.Prefix}
	return e
}

// keys lists every object below the test's prefix.
func (e *s3Env) keys(tb testing.TB) []string {
	tb.Helper()
	var out []string
	token := ""
	for {
		page, err := e.raw.List(context.Background(), e.prefix+"/", blob.ListOptions{Token: token})
		if err != nil {
			tb.Fatal(err)
		}
		for _, o := range page.Objects {
			out = append(out, o.Key)
		}
		if page.Next == "" {
			return out
		}
		token = page.Next
	}
}

// storage returns the storage configuration for a Store on this S3 env.
func (e *s3Env) storage(tb testing.TB) config.Storage {
	tb.Helper()
	cfg := blobtest.Storage(tb, tb.TempDir())
	cfg.Connections = []config.StorageConnection{e.conn}
	cfg.Upload.BlobWaitTimeout = 60 * time.Second
	return cfg
}

// connections is the one connection of a store of this env, reached through objects, recorded in the database of p (the
// copy rows refer to it, R-STOR-052).
func (e *s3Env) connections(tb testing.TB, cfg config.Storage, objects blob.ObjectStore) *blob.Connections {
	tb.Helper()
	conns, err := blob.NewConnections(blob.NewConnection(cfg.Connections[0], objects))
	if err != nil {
		tb.Fatal(err)
	}
	return conns
}

// newStore builds a store on this env's connection over the database and records the connection.
func (e *s3Env) newStore(tb testing.TB, cfg config.Storage, objects blob.ObjectStore, db blob.DB, opts ...blob.Option) *blob.Store {
	tb.Helper()
	s, err := blob.New(cfg, e.connections(tb, cfg, objects), db, opts...)
	if err != nil {
		tb.Fatal(err)
	}
	if err := s.RecordConnections(context.Background()); err != nil {
		tb.Fatalf("record the connections: %v", err)
	}
	return s
}

// collector builds a collector on this env's connection.
func collector(cfg config.Storage, objects blob.ObjectStore, db blob.DB, opts ...blob.CollectorOption) *blob.Collector {
	c := cfg.Connections[0]
	return blob.NewCollector(cfg.GC, c.KeyPrefix(), objects, db, append([]blob.CollectorOption{blob.WithBatchDelete(c.BatchDeletes())}, opts...)...)
}

// newClient builds a client for another configuration of the same store.
func newClient(tb testing.TB, cfg config.StorageS3) (blob.ObjectStore, error) {
	tb.Helper()
	f := outbound.New(config.Outbound{TLS: config.OutboundTLS{UseSystemRoots: true}})
	return s3.NewFromFactory(context.Background(), cfg, f)
}
