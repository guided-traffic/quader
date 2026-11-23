//go:build integration

package blob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	storagekeys "github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/blob/reconcile"
	"github.com/guided-traffic/quader/internal/blob/scrub"
	"github.com/guided-traffic/quader/internal/blob/summary"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta"
)

// encStore is a store over the connections primary (plain), archive and, with a third store, vault (both encrypted with keyed
// names), recorded in p's database, the holder h holding the keys of each encrypted connection; mutate changes the configuration.
func encStore(t *testing.T, p *pgEnv, h *storagekeys.Holder, mutate func(*config.Storage), stores ...*blobtest.Store) (*blob.Store, config.Storage) {
	t.Helper()
	ctx := context.Background()
	cfg := blobtest.Storage(t, t.TempDir())
	cfg.Upload.BlobWaitTimeout = 10 * time.Second
	cfg.GC.GracePeriod, cfg.GC.DeleteSettle, cfg.GC.HandoverDelay = 0, time.Second, 4*time.Second // the sweep's settle wait in a second
	ids := []string{"primary", "archive", "vault"}
	cfg.Connections = nil
	for i := range stores {
		pre := "quader"
		cfg.Connections = append(cfg.Connections, config.StorageConnection{ID: ids[i], Bucket: "bucket-" + ids[i], Prefix: &pre, Encrypted: i > 0}.WithDefaults())
	}
	if mutate != nil {
		mutate(&cfg)
	}
	var list []*blob.Connection
	for i, s := range stores {
		c := blob.NewConnection(cfg.Connections[i], s)
		c.SetCapabilities(blob.Capabilities{PayloadChecksumEnforced: true, Metadata: true})
		list = append(list, c)
	}
	conns, err := blob.NewConnections(list...)
	if err != nil {
		t.Fatal(err)
	}
	s, err := blob.New(cfg, conns, p.db, blob.WithLogger(quiet), blob.WithWaitPoll(25*time.Millisecond), blob.WithStorageKeys(h))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordConnections(ctx); err != nil {
		t.Fatal(err)
	}
	for _, c := range conns.List() {
		if c.Encrypted() {
			if err := h.Ensure(ctx, storagekeys.Connection{ID: c.ID(), KeyedNames: c.KeyedNames(), Drain: c.Drain(), Prefix: c.Prefix(), Objects: c.Objects()}); err != nil {
				t.Fatal(err)
			}
		}
	}
	return s, cfg
}

func readAllOf(t *testing.T, s *blob.Store, d blob.Digest, data []byte) {
	t.Helper()
	rc, err := s.Open(context.Background(), d, int64(len(data)), blob.OpenOptions{})
	if err != nil {
		t.Fatalf("%s: %v", d.Hex(), err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("%s reads %d other bytes (%v)", d.Hex(), len(got), err)
	}
}

// TestR_STOR_087_TheJobsOnAnEncryptedConnection is R-STOR-087's case against PostgreSQL of the stack with in-memory stores, one
// database, ten blobs of 1 KiB: the reconciliation fills a new encrypted connection from a plain one, the scrub finds a changed copy
// and replaces it, the orphan sweep reads the records of the objects it cannot place, the plain connection drains, and a second
// encrypted connection is filled from the first with new bytes.
func TestR_STOR_087_TheJobsOnAnEncryptedConnection(t *testing.T) {
	p := newPG(t)
	ctx := context.Background()
	h := newHolder(t, p, rootRing(t, "k1", map[string]byte{"k1": 1}))
	a, b := blobtest.New(), blobtest.New()
	s, cfg := encStore(t, p, h, nil, a, b)
	data := map[blob.Digest][]byte{}
	var digests []blob.Digest
	for i := range 10 {
		v := memData(1024, byte(30+i))
		d := commit(t, s, v) // on primary (min_copies 1), its gap on archive queued
		data[d], digests = v, append(digests, d)
	}
	stored, _ := blobcrypt.StoredSize(1024)

	t.Run("R-STOR-087 one reconciliation run copies the ten blobs onto the encrypted connection: records, keyed names, verified rows of S(L) and the storage key, which the storage summary counts", func(t *testing.T) {
		if res := reconcileRun(t, newReconciler(t, p, s, cfg, nil)); res.Copied != 10 || res.Streamed != 10 || res.Failed != 0 {
			t.Fatalf("run %+v", res)
		}
		keyed := b.Keys("quader/keyed/")
		if len(keyed) != 10 || len(b.Keys("quader/sha256/")) != 0 {
			t.Fatalf("%d objects below keyed/, %d below sha256/", len(keyed), len(b.Keys("quader/sha256/")))
		}
		for _, k := range keyed {
			body, _ := b.Data(k)
			if blobcrypt.CheckRecordForm(b.MetadataOf(k)) != nil || int64(len(body)) != stored {
				t.Fatalf("%s carries no record or %d bytes", k, len(body))
			}
			for _, v := range data {
				if bytes.Contains(body, v[:16]) {
					t.Fatalf("%s holds plaintext", k)
				}
			}
		}
		if n := p.count(t, "blob_copies WHERE connection_id = 'archive' AND state = 'present' AND stored_size = $1 AND storage_key_id LIKE 'sk-archive-1-%' AND verified_at IS NOT NULL AND object_key LIKE 'quader/keyed/%'", stored); n != 10 {
			t.Fatalf("%d verified rows of the encrypted copies, want 10", n)
		}
		sum, err := summary.Compute(ctx, p.db, summary.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := summary.Save(ctx, p.db, sum); err != nil {
			t.Fatal(err)
		}
		views, err := summary.List(ctx, p.db, 5*time.Minute, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range views {
			if v.ID != "archive" {
				continue
			}
			var copies int64
			for _, n := range v.Figures.KeyCopies {
				copies += n
			}
			if v.Figures.PresentCopies != 10 || v.Figures.PresentBytes != 10*stored || copies != 10 {
				t.Fatalf("the summary of archive: %+v", v.Figures)
			}
		}
	})

	t.Run("R-STOR-087 the scrub marks an encrypted copy with a changed segment corrupt and replaces it from the plain connection under a new data key", func(t *testing.T) {
		d := digests[2]
		var obj string
		if err := p.admin.QueryRow(ctx, `SELECT object_key FROM quader.blob_copies WHERE sha256 = $1 AND connection_id = 'archive'`, d.Bytes()).Scan(&obj); err != nil {
			t.Fatal(err)
		}
		before, _ := b.Data(obj)
		changed := bytes.Clone(before)
		changed[100] ^= 1
		md := b.MetadataOf(obj)
		b.Set(obj, changed, time.Now())
		b.SetMetadata(obj, md)
		p.exec(t, `UPDATE quader.blob_copies SET verified_at = now() - interval '40 days' WHERE connection_id = 'archive' AND sha256 = $1`, d.Bytes())
		res, err := scrubber(t, p, s).Run(ctx, scrub.RunOptions{Connection: "archive"})
		if err != nil {
			t.Fatal(err)
		}
		if r := res.Connections[0]; r.Corrupt != 1 || r.Repaired != 1 || r.UnknownKey != 0 {
			t.Fatalf("archive = %+v", r)
		}
		after, _ := b.Data(obj)
		if bytes.Equal(after, before) || bytes.Equal(after, changed) {
			t.Fatal("the repair did not write new bytes")
		}
		if st, verified := copyState(t, p, d, "archive"); st != blob.CopyPresent || !verified {
			t.Fatalf("the repaired copy is %s, verified %v", st, verified)
		}
		readAllOf(t, s, d, data[d])
	})

	t.Run("R-STOR-087 Q-STOR-314 the orphan sweep adopts an object of a live blob without a copy row, reports one whose record names an unknown key, and deletes an orphan after min_age", func(t *testing.T) {
		adopted := digests[4]
		p.exec(t, `DELETE FROM quader.blob_copies WHERE sha256 = $1 AND connection_id = 'archive'`, adopted.Bytes())
		archive, _ := s.Connections().Get("archive")
		// another installation's object below keyed/: a record under a storage key this database does not hold
		foreign, other := blobtest.NewKeys(), blobtest.New()
		foreign.Draw("archive", 1)
		pre := "quader"
		stranger := blob.NewConnection(config.StorageConnection{ID: "archive", Bucket: "bucket-archive", Prefix: &pre, Encrypted: true}, other)
		stranger.UseKeys(foreign)
		alien := memData(1024, 90)
		ad := blob.Digest(sha256.Sum256(alien))
		fw, err := stranger.WriteSealedStream(ctx, ad, 1024, bytes.NewReader(alien), 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := other.Data(fw.Key)
		b.Set(fw.Key, body, time.Now())
		b.SetMetadata(fw.Key, other.MetadataOf(fw.Key))
		// an orphan: Quader's object of a blob that has no row
		lost := memData(1024, 91)
		ld := blob.Digest(sha256.Sum256(lost))
		lw, err := archive.WriteSealedStream(ctx, ld, 1024, bytes.NewReader(lost), 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		c := blob.NewCollector(cfg, s.Connections(), p.db, blob.WithCollectorLogger(quiet),
			blob.WithCollectorClock(func() time.Time { return time.Now().Add(cfg.Orphans.MinAge + time.Hour) }))
		res, err := c.Sweep(ctx, blob.SweepOptions{Connection: "archive"})
		if err != nil {
			t.Fatal(err)
		}
		r := res.Connections[0]
		if r.Adopted != 1 || r.OrphansDeleted != 1 || r.ForeignKeys != 1 || r.CopiesMarked != 0 {
			t.Fatalf("sweep %+v", r)
		}
		if n := p.count(t, "blob_copies WHERE sha256 = $1 AND connection_id = 'archive' AND state = 'present' AND verified_at IS NULL AND stored_size = $2 AND storage_key_id LIKE 'sk-archive-1-%'", adopted.Bytes(), stored); n != 1 {
			t.Fatal("the adopted copy is not present, unverified, of S(L) with its storage key")
		}
		if _, ok := b.Data(fw.Key); !ok {
			t.Fatal("the sweep deleted an object whose record it cannot open")
		}
		if _, ok := b.Data(lw.Key); ok {
			t.Fatal("the orphan older than min_age remains")
		}
		if err := b.Delete(ctx, fw.Key); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("R-STOR-087 after drain: true on the plain connection and a run every blob downloads from the encrypted one", func(t *testing.T) {
		if _, err := p.admin.Exec(ctx, liveReplica, `{"connections":[{"id":"primary","circuit":"closed","drain":true},{"id":"archive","circuit":"closed","drain":false}]}`); err != nil {
			t.Fatal(err)
		}
		defer p.exec(t, `DELETE FROM quader.replica`)
		// the adopted copy is not verified; the scrub would read it first, and the drain keeps the plain copy until it did
		p.exec(t, `UPDATE quader.blob_copies SET verified_at = now() WHERE connection_id = 'archive'`)
		sd, cfgD := encStore(t, p, h, func(c *config.Storage) { c.Connections[0].Drain = true }, a, b)
		res := reconcileRun(t, newReconciler(t, p, sd, cfgD, func(o *reconcile.Options) {
			o.Drained = func(context.Context, meta.Tx, string, int64) error { return nil }
		}))
		if res.CopiesDeleted != 10 || len(a.Keys("quader/sha256/")) != 0 {
			t.Fatalf("run %+v, %d objects left on the plain connection", res, len(a.Keys("quader/sha256/")))
		}
		for d, v := range data {
			readAllOf(t, sd, d, v)
		}
	})

	t.Run("R-STOR-087 Q-STOR-308 a second encrypted connection is filled from the first: new bodies under its own key, both read back", func(t *testing.T) {
		c := blobtest.New()
		sv, cfgV := encStore(t, p, h, func(c *config.Storage) { c.Connections[0].Drain = true }, a, b, c)
		if res := reconcileRun(t, newReconciler(t, p, sv, cfgV, nil)); res.Copied != 10 {
			t.Fatalf("run %+v", res)
		}
		for _, k := range c.Keys("quader/keyed/") {
			vb, _ := c.Data(k)
			for _, ak := range b.Keys("quader/keyed/") {
				if ab, _ := b.Data(ak); bytes.Equal(ab, vb) {
					t.Fatalf("%s carries the ciphertext of %s", k, ak)
				}
			}
			if !strings.HasPrefix(c.MetadataOf(k)[blobcrypt.MetaKey], "sk-vault-1-") {
				t.Fatalf("%s is wrapped under %s", k, c.MetadataOf(k)[blobcrypt.MetaKey])
			}
		}
		if len(c.Keys("quader/keyed/")) != 10 {
			t.Fatalf("%d objects on vault", len(c.Keys("quader/keyed/")))
		}
		for d, v := range data {
			readAllOf(t, sv, d, v)
		}
	})
}
