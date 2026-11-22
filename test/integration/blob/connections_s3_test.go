//go:build integration

package blob

import (
	"bytes"
	"context"
	"io"
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

// Two connections on two real buckets of one endpoint (storage.md R-STOR-063): the stack's SeaweedFS, the buckets
// QUADER_TEST_S3_BUCKET_BLOBS and QUADER_TEST_S3_BUCKET_BACKUPS under a prefix of the run, three objects of 1 KiB. What only a
// real store shows: the commit's routing, a server-side copy across buckets and a streamed one, the metadata a copy keeps, and
// a read that falls over and the copy the reconciliation makes back.

// bucketPair is the two connections and their clients.
type bucketPair struct {
	cfg     config.Storage
	clients map[string]*s3.Client
	conns   *blob.Connections
}

func newBucketPair(tb testing.TB) *bucketPair {
	tb.Helper()
	dir := tb.TempDir()
	secret := func(file, v string) string {
		p := filepath.Join(dir, file)
		if err := os.WriteFile(p, []byte(v+"\n"), 0o600); err != nil {
			tb.Fatal(err)
		}
		return p
	}
	prefix := "it-blob/" + runID(tb) + "/pair"
	cfg := blobtest.Storage(tb, tb.TempDir())
	cfg.Upload.BlobWaitTimeout = 60 * time.Second
	cfg.Connections = nil
	for _, el := range []struct{ id, bucket string }{
		{"primary", testenv.Get(tb, "QUADER_TEST_S3_BUCKET_BLOBS")},
		{"secondary", testenv.Get(tb, "QUADER_TEST_S3_BUCKET_BACKUPS")},
	} {
		cfg.Connections = append(cfg.Connections, config.StorageConnection{
			ID: el.id, Endpoint: testenv.Get(tb, "QUADER_TEST_S3_ENDPOINT"), Region: testenv.Get(tb, "QUADER_TEST_S3_REGION"),
			Bucket: el.bucket, Prefix: &prefix, ForcePathStyle: testenv.Get(tb, "QUADER_TEST_S3_FORCE_PATH_STYLE") == "true",
			CAFile:              testenv.CAFile(tb),
			PayloadChecksum:     optional("QUADER_TEST_S3_PAYLOAD_CHECKSUM", "sha256"),
			CredentialsSource:   "static",
			AccessKeyIDFile:     secret("access-key", testenv.Get(tb, "QUADER_TEST_S3_ACCESS_KEY")),
			SecretAccessKeyFile: secret("secret-key", testenv.Get(tb, "QUADER_TEST_S3_SECRET_KEY")),
		}.WithDefaults())
		testenv.RemoveObjectsAtCleanup(tb, el.bucket, prefix)
	}
	f := outbound.New(config.Outbound{TLS: config.OutboundTLS{UseSystemRoots: true}})
	bp := &bucketPair{cfg: cfg, clients: map[string]*s3.Client{}}
	var list []*blob.Connection
	for i, el := range cfg.Connections {
		c, err := s3.NewFromFactory(context.Background(), config.ClientConfig(i, el, cfg.ObjectStore), f)
		if err != nil {
			tb.Fatal(err)
		}
		bp.clients[el.ID] = c
		list = append(list, blob.NewConnection(el, blob.NewGuard(c, cfg.ObjectStore.CircuitBreaker, blob.WithGuardConnection(el.ID))))
	}
	conns, err := blob.NewConnections(list...)
	if err != nil {
		tb.Fatal(err)
	}
	bp.conns = conns
	return bp
}

// store builds a store over the pair, records the connections and runs their start checks, which record what each supports.
func (bp *bucketPair) store(tb testing.TB, p *pgEnv) *blob.Store {
	tb.Helper()
	s, err := blob.New(bp.cfg, bp.conns, p.db, blob.WithLogger(quiet))
	if err != nil {
		tb.Fatal(err)
	}
	if err := s.RecordConnections(context.Background()); err != nil {
		tb.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := blob.AwaitConnections(ctx, bp.conns, blob.StartCheckOptions{Replica: "it-pair", Record: s.RecordCapabilities, Log: quiet}); err != nil {
		tb.Fatal(err)
	}
	return s
}

// object reads an object through a connection's client, nil when it is not there.
func (bp *bucketPair) object(tb testing.TB, conn, key string) []byte {
	tb.Helper()
	rc, _, err := bp.clients[conn].Get(context.Background(), key, nil)
	if err != nil {
		return nil
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		tb.Fatal(err)
	}
	return b
}

func TestR_STOR_056_TwoRealBuckets(t *testing.T) {
	ctx := context.Background()
	p := newPG(t)
	bp := newBucketPair(t)
	s := bp.store(t, p)
	primary, _ := bp.conns.Get("primary")
	secondary, _ := bp.conns.Get("secondary")
	if !secondary.Capabilities().ServerSideCopyFrom["primary"] {
		t.Fatalf("the start check found no server-side copy from primary into secondary: %+v", secondary.Capabilities())
	}
	auto := bp.cfg
	never := bp.cfg
	never.Reconcile.ServerSide = "never"

	first := memData(1024, 1)
	d1 := commit(t, s, first)
	t.Run("R-STOR-053 an upload is written to the first bucket and queued for the second", func(t *testing.T) {
		if !bytes.Equal(bp.object(t, "primary", primary.ObjectKey(d1)), first) || bp.object(t, "secondary", secondary.ObjectKey(d1)) != nil {
			t.Fatal("the object does not lie on the first bucket only")
		}
		if p.scalarInt(t, `SELECT count(*) FROM blob_copy_queue WHERE sha256 = $1 AND connection_id = 'secondary'`, d1.Bytes()) != 1 {
			t.Fatal("no queue row for the second connection")
		}
	})
	t.Run("R-STOR-056 a copy between two buckets of one endpoint is server-side; with server_side never it is streamed; both read back as the blob", func(t *testing.T) {
		if res := reconcileRun(t, newReconciler(t, p, s, auto, nil)); res.ServerSide != 1 || res.Streamed != 0 {
			t.Fatalf("server_side auto: %+v", res)
		}
		if !bytes.Equal(bp.object(t, "secondary", secondary.ObjectKey(d1)), first) {
			t.Fatal("the server-side copy does not read back as the blob")
		}
		if p.scalarInt(t, `SELECT count(*) FROM blob_copies WHERE sha256 = $1 AND connection_id = 'secondary' AND state = 'present' AND verified_at IS NULL`, d1.Bytes()) != 1 {
			t.Fatal("the server-side copy is not present and unverified")
		}
		second := memData(1024, 2)
		d2 := commit(t, s, second)
		if res := reconcileRun(t, newReconciler(t, p, s, never, nil)); res.Streamed != 1 || res.ServerSide != 0 {
			t.Fatalf("server_side never: %+v", res)
		}
		if !bytes.Equal(bp.object(t, "secondary", secondary.ObjectKey(d2)), second) {
			t.Fatal("the streamed copy does not read back as the blob")
		}
	})
	t.Run("R-STOR-062 a server-side copy across buckets keeps the object's metadata", func(t *testing.T) {
		src, dst := primary.Prefix()+"/_probe/meta-src", secondary.Prefix()+"/_probe/meta-dst"
		body := []byte("metadata")
		if err := bp.clients["primary"].Put(ctx, src, bytes.NewReader(body), int64(len(body)), blob.Checksum{}, blob.Metadata{"quader-probe": "kept"}); err != nil {
			t.Fatal(err)
		}
		_, bucket := bp.clients["primary"].Location()
		if err := bp.clients["secondary"].CopyFrom(ctx, bucket, src, dst, int64(len(body)), nil); err != nil {
			t.Fatal(err)
		}
		info, err := bp.clients["secondary"].Head(ctx, dst)
		if err != nil || info.Metadata["quader-probe"] != "kept" {
			t.Fatalf("metadata after the copy: %v (%v)", info.Metadata, err)
		}
	})
	t.Run("R-STOR-055 R-STOR-056 a read falls over when the first bucket lost the object, and the reconciliation copies it back", func(t *testing.T) {
		third := memData(1024, 3)
		d3 := commit(t, s, third)
		reconcileRun(t, newReconciler(t, p, s, auto, nil))
		if err := bp.clients["primary"].Delete(ctx, primary.ObjectKey(d3)); err != nil {
			t.Fatal(err)
		}
		rc, err := s.Open(ctx, d3, int64(len(third)), blob.OpenOptions{})
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil || !bytes.Equal(got, third) {
			t.Fatalf("the read did not fall over to the second bucket: %d bytes (%v)", len(got), err)
		}
		if p.scalarInt(t, `SELECT count(*) FROM blob_copy_queue WHERE sha256 = $1 AND connection_id = 'primary'`, d3.Bytes()) != 1 {
			t.Fatal("the missing copy is not queued for its repair")
		}
		if res := reconcileRun(t, newReconciler(t, p, s, auto, nil)); res.Copied != 1 {
			t.Fatalf("the repair: %+v", res)
		}
		if !bytes.Equal(bp.object(t, "primary", primary.ObjectKey(d3)), third) {
			t.Fatal("the object is not back on the first bucket")
		}
		if p.scalarInt(t, `SELECT count(*) FROM blob_copies WHERE sha256 = $1 AND connection_id = 'primary' AND state = 'present'`, d3.Bytes()) != 1 {
			t.Fatal("the first bucket's copy is not present again")
		}
	})
}
