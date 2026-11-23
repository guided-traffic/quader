//go:build integration

package backup

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/backup/create"
	"github.com/guided-traffic/quader/internal/backup/format"
	"github.com/guided-traffic/quader/internal/backup/restore"
	"github.com/guided-traffic/quader/internal/backup/retention"
	"github.com/guided-traffic/quader/internal/backup/target"
	"github.com/guided-traffic/quader/internal/backup/verify"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	storagekeys "github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/blob/reconcile"
	"github.com/guided-traffic/quader/internal/blob/s3"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/outbound"
	"github.com/guided-traffic/quader/internal/secrets"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// sealedInstall is an installation whose blobs lie on an encrypted connection archive (keyed names) on a prefix of the stack's blob
// bucket, beside the plain connection default, with the key ring that seals its storage keys and a real canary.
type sealedInstall struct {
	*install
	ring    *secrets.KeyRing
	ringDir string
	storage config.Storage
	archive config.StorageConnection
	plain   config.StorageConnection
}

func newSealedInstall(t *testing.T) *sealedInstall {
	t.Helper()
	ctx := context.Background()
	in := &sealedInstall{install: newInstall(t), ringDir: t.TempDir()}
	key := filepath.Join(in.ringDir, "2026-10.key")
	if err := os.WriteFile(key, []byte(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x2a}, 32))), 0o600); err != nil {
		t.Fatal(err)
	}
	var err error
	if in.ring, err = secrets.Load(config.Secrets{EncryptionKeyFiles: []string{key}, ActiveKeyID: "2026-10"}); err != nil {
		t.Fatal(err)
	}
	canary, err := in.ring.SealCanary("2026-10")
	if err != nil {
		t.Fatal(err)
	}
	in.exec(`UPDATE quader.secret_key_canary SET ciphertext = $1 WHERE key_id = '2026-10'`, canary)
	in.plain = connectionOf(in.s3cfg)
	in.archive = connectionOf(in.s3cfg)
	pre := in.prefix + "-archive"
	in.archive.ID, in.archive.Prefix, in.archive.Encrypted = "archive", &pre, true
	in.archive = in.archive.WithDefaults()
	testenv.RemoveObjectsAtCleanup(t, in.s3cfg.Bucket, pre)
	in.storage = blobtest.Storage(t, t.TempDir())
	in.storage.Connections = []config.StorageConnection{in.archive, in.plain}
	in.storage.Upload.BlobWaitTimeout = 60 * time.Second
	h, err := storagekeys.New(storagekeys.Options{DB: in.web, Ring: in.ring, Log: quiet})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	a, d := blob.NewConnection(in.archive, in.client), blob.NewConnection(in.plain, in.client)
	conns, err := blob.NewConnections(a, d)
	if err != nil {
		t.Fatal(err)
	}
	if in.store, err = blob.New(in.storage, conns, in.web, blob.WithLogger(quiet), blob.WithWaitPoll(25*time.Millisecond), blob.WithStorageKeys(h)); err != nil {
		t.Fatal(err)
	}
	if err := in.store.RecordConnections(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.Ensure(ctx, storagekeys.Connection{ID: "archive", KeyedNames: true, Prefix: pre, Objects: a.Objects()}); err != nil {
		t.Fatal(err)
	}
	return in
}

// sources gives a run the two connections, the archive draining or not.
func (in *sealedInstall) sources(drain bool) func(*create.Deps) {
	return func(d *create.Deps) {
		facts := d.BlobState.(*backup.PGFacts)
		ep := config.NormalizeEndpoint(in.s3cfg.Endpoint)
		d.SourceConnections = []format.ConnectionRef{
			{ID: "archive", Endpoint: in.s3cfg.Endpoint, Bucket: in.s3cfg.Bucket, Prefix: *in.archive.Prefix},
			{ID: config.DefaultConnectionID, Endpoint: in.s3cfg.Endpoint, Bucket: in.s3cfg.Bucket, Prefix: in.s3cfg.Prefix},
		}
		d.Staying = []string{config.DefaultConnectionID}
		if !drain {
			d.Staying = append(d.Staying, "archive")
		}
		d.Source = backup.NewStoreSource(facts, []backup.SourceConnection{
			{ID: "archive", Drain: drain, Objects: in.client, Endpoint: ep, Bucket: in.s3cfg.Bucket, ServerSide: true, Encrypted: true},
			{ID: config.DefaultConnectionID, Objects: in.client, Endpoint: ep, Bucket: in.s3cfg.Bucket, ServerSide: true},
		})
	}
}

// pruneWith adds the pruning phase of retention with keep_last n to a run.
func pruneWith(r *rig, n int, then func(*create.Deps)) func(*create.Deps) {
	return func(d *create.Deps) {
		then(d)
		d.Prune = &retention.Stage{
			Store: r.tgt, Layout: format.Layout{Prefix: r.cfg.Target.Prefix}, Policy: retention.Policy{KeepLast: n},
			Facts: d.Facts, Audit: d.Events.(retention.Audit), Log: quiet,
		}
	}
}

// emptyDatabase is a database with the schema quader and no relation, a restore's destination (R-BAK-020 check 4).
func emptyDatabase(t *testing.T) config.Database {
	t.Helper()
	name := "quader_bak_dst_" + runID()
	maint := dialDB(t, "postgres")
	if _, err := maint.Exec(context.Background(), "CREATE DATABASE "+name+" ENCODING 'UTF8' TEMPLATE template0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = maint.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)") })
	c := dialDB(t, name)
	if _, err := c.Exec(context.Background(), "CREATE SCHEMA quader"); err != nil {
		t.Fatal(err)
	}
	return pgConfig(t, t.TempDir(), name)
}

// restoreInto restores the backup id into an empty database whose first connection is the encrypted connection conn on a prefix of
// its own, and returns the database's configuration and the connection's element.
func (in *sealedInstall) restoreInto(t *testing.T, r *rig, id string, conn string) (config.Database, config.StorageConnection) {
	t.Helper()
	ctx := context.Background()
	dbCfg := emptyDatabase(t)
	el := connectionOf(in.s3cfg)
	pre := "it-bak-dst/" + runID()
	el.ID, el.Prefix, el.Encrypted = conn, &pre, true
	el = el.WithDefaults()
	testenv.RemoveObjectsAtCleanup(t, in.s3cfg.Bucket, pre)
	cc := config.ClientConfig(0, el, in.storage.ObjectStore)
	client, err := s3.NewFromFactory(ctx, cc, outbound.New(config.Outbound{TLS: config.OutboundTLS{UseSystemRoots: true}}))
	if err != nil {
		t.Fatal(err)
	}
	session, err := postgres.OpenRestoreSession(ctx, dbCfg, postgres.MigrateOptions{
		Instance: "restore-pod", Version: "0.1.0", Log: quiet,
		Migrate: config.Migrate{AdvisoryLockWait: 20 * time.Second, DDLLockTimeout: 2 * time.Second, DDLLockRetries: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	tgt := r.tgt.(*s3.Target)
	cfg := r.cfg
	cfg.StallTimeout = 5 * time.Minute
	res := restore.New(restore.Deps{
		Session: session, Target: tgt, Layout: format.Layout{Prefix: cfg.Target.Prefix}, TargetEndpoint: tgt.Endpoint(), TargetBucket: tgt.Bucket(),
		Connections: []restore.Connection{{
			ID: conn, Bucket: el.Bucket, Prefix: pre, Store: s3.NewBlobSink(client, cc, int64(cfg.Copy.PartSize)), Encrypted: true, KeyedNames: true,
			Writer: blob.NewConnection(el, client),
		}},
		Keys: in.ring, Journal: func(context.Context, restore.JournalEnv) (restore.Journal, error) { return testJournal{}, nil },
		Config: cfg, PartSize: 16 << 20, Log: quiet, Pod: "restore-pod",
	})
	if _, err := res.Restore(ctx, backup.RestoreOptions{ID: format.ID(id)}); err != nil {
		t.Fatalf("restore into %s: %v", conn, err)
	}
	return dbCfg, el
}

// downloads opens every blob through a replica's store over a restored database and its encrypted connection.
func (in *sealedInstall) downloads(t *testing.T, dbCfg config.Database, el config.StorageConnection, blobs map[blob.Digest][]byte) {
	t.Helper()
	ctx := context.Background()
	db, err := postgres.Open(ctx, dbCfg, "it-bak-dst-web", "web", quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h, err := storagekeys.New(storagekeys.Options{DB: db, Ring: in.ring, Log: quiet})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.Load(ctx, el.ID); err != nil {
		t.Fatal(err)
	}
	storage := in.storage
	storage.Connections = []config.StorageConnection{el}
	conns, err := blob.NewConnections(blob.NewConnection(el, in.client))
	if err != nil {
		t.Fatal(err)
	}
	s, err := blob.New(storage, conns, db, blob.WithLogger(quiet), blob.WithStorageKeys(h))
	if err != nil {
		t.Fatal(err)
	}
	for d, data := range blobs {
		rc, err := s.Open(ctx, d, int64(len(data)), blob.OpenOptions{})
		if err != nil {
			t.Fatalf("%s on %s: %v", d.Hex(), el.ID, err)
		}
		got, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("%s on %s reads %d other bytes (%v)", d.Hex(), el.ID, len(got), err)
		}
	}
}

// poolObjects lists the pool objects of a target by name.
func poolObjects(t *testing.T, r *rig) map[string]target.Object {
	t.Helper()
	out := map[string]target.Object{}
	l := format.Layout{Prefix: r.cfg.Target.Prefix}
	if err := r.tgt.List(context.Background(), l.PoolPrefix(), func(o target.Object) error {
		full, err := r.tgt.Head(context.Background(), o.Key)
		out[o.Key[strings.LastIndex(o.Key, "/")+1:]] = full
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestR_BAK_031_EncryptedConnectionsBackedUpAndRestored is R-BAK-031's and R-BAK-032's case: an installation with an encrypted
// connection holding three blobs of 1 KiB on the stack's store is backed up into a prefix of the backup bucket; verify --deep runs
// without and with the key ring; the backup is restored into an empty database whose first connection has the source's encrypted
// connection id and into one whose first connection is another encrypted id; then the encrypted connection drains, the next backup
// copies the blobs again from the plain connection, and a run with keep_last: 1 leaves no retained backup naming its key.
func TestR_BAK_031_EncryptedConnectionsBackedUpAndRestored(t *testing.T) {
	ctx := context.Background()
	in := newSealedInstall(t)
	blobs := map[blob.Digest][]byte{}
	for i := range 3 {
		data := bytes.Repeat([]byte{byte('a' + i)}, 1024)
		d, err := in.upload(ctx, in.repo, "sealed-"+string(rune('a'+i)), data)
		if err != nil {
			t.Fatal(err)
		}
		blobs[d] = data
	}
	if n := in.number(`SELECT count(*) FROM quader.blob_copies WHERE connection_id = 'archive' AND storage_key_id LIKE 'sk-archive-1-%'`); n != 3 {
		t.Fatalf("%d encrypted copies, want 3", n)
	}
	r := in.newRig(t)
	r.mutate = in.sources(false)
	first, err := in.run(ctx, r, backup.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	m := manifestOf(t, r.tgt, r.cfg, first.BackupID)
	var keyID string

	t.Run("R-BAK-031 R-BAK-032 the pool keeps each encrypted copy as stored, with its record, under the digest and its key's identifier", func(t *testing.T) {
		pool := poolObjects(t, r)
		if len(pool) != 3 {
			t.Fatalf("pool objects %v", pool)
		}
		for name, o := range pool {
			hex, id, ok := strings.Cut(name, ".")
			if !ok || !strings.HasPrefix(id, "sk-archive-1-") || o.Metadata[blobcrypt.MetaKey] != id || blobcrypt.CheckRecordForm(o.Metadata) != nil {
				t.Fatalf("pool object %s with %v", name, o.Metadata)
			}
			keyID = id
			rc, _, err := r.tgt.Get(ctx, o.Key)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(rc)
			_ = rc.Close()
			d, _ := format.ParseDigest(hex)
			if bytes.Contains(body, blobs[blob.Digest(d)][:16]) {
				t.Fatalf("%s holds plaintext", name)
			}
		}
		sk := m.StorageKeys
		if sk == nil || len(sk.Pool) != 1 || sk.Pool[0].KeyID != keyID || sk.Pool[0].Objects != 3 || len(sk.Sealed) != 2 {
			t.Fatalf("storageKeys = %+v", sk)
		}
		validateManifest(t, mustEncode(t, m))
	})

	t.Run("R-BAK-031 Q-BAK-66 verify --deep counts the encrypted pool objects as not authenticated; with --key-ring it reads them, and a flipped bit is digest_mismatch", func(t *testing.T) {
		v := &verify.Verifier{Store: r.tgt, Layout: format.Layout{Prefix: r.cfg.Target.Prefix}, Log: quiet}
		rep, err := v.Verify(ctx, backup.VerifyOptions{Latest: true, Deep: true})
		if err != nil || rep.NotAuthenticated != 3 {
			t.Fatalf("without the key ring: %+v, %v", rep, err)
		}
		if rep, err = v.Verify(ctx, backup.VerifyOptions{Latest: true, Deep: true, KeyRing: in.ring}); err != nil || rep.NotAuthenticated != 0 || len(rep.Findings) != 0 {
			t.Fatalf("with the key ring: %+v, %v", rep, err)
		}
		var victim target.Object
		for _, o := range poolObjects(t, r) {
			victim = o
			break
		}
		rc, _, _ := r.tgt.Get(ctx, victim.Key)
		good, _ := io.ReadAll(rc)
		_ = rc.Close()
		bad := bytes.Clone(good)
		bad[40] ^= 1
		if _, err := r.tgt.Put(ctx, victim.Key, bad, target.PutOptions{Metadata: victim.Metadata}); err != nil {
			t.Fatal(err)
		}
		rep, err = v.Verify(ctx, backup.VerifyOptions{Latest: true, Deep: true, KeyRing: in.ring})
		if backup.ExitCode(err) != backup.ExitFindings || len(rep.Findings) != 1 || rep.Findings[0].Kind != backup.FindingDigestMismatch {
			t.Fatalf("a flipped bit: %+v, %v", rep, err)
		}
		if _, err := r.tgt.Put(ctx, victim.Key, good, target.PutOptions{Metadata: victim.Metadata}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("R-BAK-031 a restore into the source's encrypted connection id writes the objects unchanged, one into another encrypted id writes new bodies; both download", func(t *testing.T) {
		source := map[string][]byte{}
		for _, key := range listKeysOf(t, in.client, *in.archive.Prefix+"/keyed/") {
			rc, _, err := in.client.Get(ctx, key, nil)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(rc)
			_ = rc.Close()
			source[key[strings.LastIndex(key, "/")+1:]] = b
		}
		same, sameEl := in.restoreInto(t, r, first.BackupID, "archive")
		other, otherEl := in.restoreInto(t, r, first.BackupID, "vault")
		for _, key := range listKeysOf(t, in.client, *sameEl.Prefix+"/keyed/") {
			rc, _, _ := in.client.Get(ctx, key, nil)
			b, _ := io.ReadAll(rc)
			_ = rc.Close()
			if !bytes.Equal(b, source[key[strings.LastIndex(key, "/")+1:]]) {
				t.Fatalf("%s is not the source's object", key)
			}
		}
		vault := listKeysOf(t, in.client, *otherEl.Prefix+"/keyed/")
		if len(vault) != 3 {
			t.Fatalf("vault holds %v", vault)
		}
		for _, key := range vault {
			rc, info, _ := in.client.Get(ctx, key, nil)
			b, _ := io.ReadAll(rc)
			_ = rc.Close()
			for _, s := range source {
				if bytes.Equal(b, s) {
					t.Fatalf("%s carries a source object's ciphertext", key)
				}
			}
			if !strings.HasPrefix(info.Metadata[blobcrypt.MetaKey], "sk-vault-1-") {
				t.Fatalf("%s is wrapped under %s", key, info.Metadata[blobcrypt.MetaKey])
			}
		}
		in.downloads(t, same, sameEl, blobs)
		in.downloads(t, other, otherEl, blobs)
		var n int64
		db := dialDBCfg(t, other)
		if err := db.QueryRow(ctx, `SELECT count(*) FROM quader.blob_copies WHERE connection_id = 'vault' AND storage_key_id LIKE 'sk-vault-1-%' AND object_key LIKE '%/keyed/%'`).Scan(&n); err != nil || n != 3 {
			t.Fatalf("%d copy rows of vault with their storage key (%v)", n, err)
		}
	})

	t.Run("R-BAK-032 once the encrypted connection drains, the next backup copies the blobs from the plain one; keep_last 1 leaves no retained backup naming its key", func(t *testing.T) {
		rec, err := reconcile.New(reconcile.Options{Store: in.store, DB: in.web, Storage: in.storage, Log: quiet, StaleAfter: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		if res, err := rec.Run(ctx, nil); err != nil || res.Copied != 3 {
			t.Fatalf("the reconciliation: %+v, %v", res, err)
		}
		r.mutate = pruneWith(r, 5, in.sources(true))
		second, err := in.run(ctx, r, backup.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if second.Repooled != 3 || second.BlobsCopied != 3 {
			t.Fatalf("second %+v", second)
		}
		var older *backup.Retained
		for i := range second.Retained {
			if second.Retained[i].BackupID == first.BackupID {
				older = &second.Retained[i]
			}
		}
		if older == nil || len(older.PoolKeyIDs) != 1 || older.PoolKeyIDs[0] != keyID || len(older.SealedKeyIDs) != 2 {
			t.Fatalf("the retained backups %+v", second.Retained)
		}
		if m2 := manifestOf(t, r.tgt, r.cfg, second.BackupID); len(m2.StorageKeys.Pool) != 0 {
			t.Fatalf("the second backup's pool names %+v", m2.StorageKeys.Pool)
		}
		r.mutate = pruneWith(r, 1, in.sources(true))
		third, err := in.run(ctx, r, backup.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(third.Retained) != 1 || len(third.Retained[0].PoolKeyIDs) != 0 || third.Retained[0].PoolKeyIDs == nil {
			t.Fatalf("the retained backups after keep_last 1: %+v", third.Retained)
		}
		for name := range poolObjects(t, r) {
			if strings.Contains(name, keyID) {
				t.Fatalf("the pool keeps %s", name)
			}
		}
	})
}

// listKeysOf lists the keys below prefix on the stack's blob bucket.
func listKeysOf(t *testing.T, c *s3.Client, prefix string) []string {
	t.Helper()
	var out []string
	token := ""
	for {
		page, err := c.List(context.Background(), prefix, blob.ListOptions{Token: token})
		if err != nil {
			t.Fatal(err)
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

// dialDBCfg connects to the database of a configuration with the stack's superuser.
func dialDBCfg(t *testing.T, cfg config.Database) *pgx.Conn {
	t.Helper()
	return dialDB(t, cfg.Name)
}
