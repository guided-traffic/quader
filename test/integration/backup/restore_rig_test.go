//go:build integration

package backup

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"strconv"
	"testing"
	"testing/fstest"

	"github.com/guided-traffic/quader/internal/app"
	"github.com/guided-traffic/quader/internal/backup/format"
	"github.com/guided-traffic/quader/internal/backup/restore"
	"github.com/guided-traffic/quader/internal/backup/target"
	"github.com/guided-traffic/quader/internal/blob/s3"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/outbound"
	"github.com/guided-traffic/quader/internal/secrets"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// source is an installation with seeded state and the backup taken of it, in a prefix of the stack's backup bucket.
type source struct {
	t      *testing.T
	stack  *testenv.Stack
	seed   *seeded
	layout format.Layout
	tgtCfg config.Backup
	target *s3.Target
	id     format.ID
	m      *format.Manifest
}

func newSource(t *testing.T, files int) *source { return newSourceRows(t, files, 0) }

// newSourceRows is newSource with bulkAudit extra audit events, to make the load of audit_event slow.
func newSourceRows(t *testing.T, files, bulkAudit int) *source {
	t.Helper()
	src := testenv.NewStack(t, testenv.StackOptions{Name: "bksrc"})
	ring, _ := keyRingOf(t, src, "2026-10", 42)
	seed := seedSource(t, src, files, bulkAudit, ring)
	cfg := targetConfig(t, testenv.Get(t, "QUADER_TEST_S3_BUCKET_BACKUPS"), "none")
	tgt := newTarget(t, cfg)
	cleanup(t, tgt, cfg.Target.Prefix+"/")
	layout := format.Layout{Prefix: cfg.Target.Prefix}
	m := exportBackup(t, src, tgt, layout, seed.blobs, exportOptions{})
	return &source{t: t, stack: src, seed: seed, layout: layout, tgtCfg: cfg, target: tgt, id: m.BackupID, m: m}
}

// rewriteManifest stores the edited manifest again.
func (s *source) rewriteManifest() {
	s.t.Helper()
	b, err := s.m.Encode()
	if err != nil {
		s.t.Fatal(err)
	}
	if _, err := s.target.Put(context.Background(), s.layout.Manifest(s.id), b, target.PutOptions{}); err != nil {
		s.t.Fatal(err)
	}
}

// destination is an installation whose database is empty (schema created, no relation) with its own blob prefix.
type destination struct {
	t       *testing.T
	stack   *testenv.Stack
	src     *source
	environ []string
	cfg     *config.Config
}

func (s *source) destination(env ...string) *destination {
	s.t.Helper()
	dst := testenv.NewStack(s.t, testenv.StackOptions{Name: "bkdst"})
	dst.Exec("DROP SCHEMA quader CASCADE")
	dst.Exec("CREATE SCHEMA quader")
	return s.destinationOn(dst, env...)
}

// destinationOn restores into the database of an existing installation.
func (s *source) destinationOn(dst *testenv.Stack, env ...string) *destination {
	s.t.Helper()
	d := &destination{t: s.t, stack: dst, src: s}
	t := s.tgtCfg.Target
	d.environ = append(dst.Environ(),
		"QUADER_BACKUP_TARGET_ENDPOINT="+t.Endpoint, "QUADER_BACKUP_TARGET_REGION="+t.Region, "QUADER_BACKUP_TARGET_BUCKET="+t.Bucket,
		"QUADER_BACKUP_TARGET_PREFIX="+t.Prefix, "QUADER_BACKUP_TARGET_FORCE_PATH_STYLE=true", "QUADER_BACKUP_TARGET_CA_FILE="+t.CAFile,
		"QUADER_BACKUP_TARGET_CREDENTIALS_SOURCE=static",
		"QUADER_BACKUP_TARGET_CREDENTIALS_ACCESS_KEY_ID_FILE="+t.Credentials.AccessKeyIDFile,
		"QUADER_BACKUP_TARGET_CREDENTIALS_SECRET_ACCESS_KEY_FILE="+t.Credentials.SecretAccessKeyFile,
		"QUADER_BACKUP_TARGET_SSE_MODE=none", "QUADER_BACKUP_DATABASE_MIGRATION_WAIT=2s", "QUADER_BACKUP_STALL_TIMEOUT=5m",
		"QUADER_BACKUP_COPY_CONCURRENCY=4", "QUADER_BACKUP_TARGET_ALLOW_SAME_BUCKET=false")
	d.environ = append(d.environ, env...)
	loaded, err := config.Load(config.Options{Environ: d.environ, Command: config.CommandBackup})
	if err != nil {
		s.t.Fatalf("config: %v", err)
	}
	d.cfg = loaded.Config
	return d
}

// testJournal is a revocation journal that lists and replays nothing.
type testJournal struct{ reach error }

func (j testJournal) Reachable(context.Context) error { return j.reach }
func (testJournal) Replay(context.Context, journal.ReplayOptions) (*journal.Summary, error) {
	return &journal.Summary{}, nil
}

// restorerOptions vary a restore of the rig.
type restorerOptions struct {
	hook  func(string)
	steps fs.FS
	ring  *secrets.KeyRing
	jrn   restore.Journal
	// journalFn builds the journal from what the restore tells it (the real replay of cmd/quader), instead of the fixed jrn.
	journalFn func(context.Context, restore.JournalEnv) (restore.Journal, error)
	// targetEndpoint replaces the target's endpoint in the copy source, so that the blob store sees another store.
	targetEndpoint string
}

// restorer builds the Restorer the way cmd/quader does.
func (d *destination) restorer(o restorerOptions) (*restore.Restorer, *bytes.Buffer) {
	d.t.Helper()
	ctx := context.Background()
	cfg := d.cfg
	f := outbound.New(cfg.Proxy.Outbound)
	tgt, err := s3.NewTarget(ctx, cfg.Backup, f)
	if err != nil {
		d.t.Fatal(err)
	}
	client, err := s3.NewFromFactory(ctx, firstS3(cfg.Storage), f)
	if err != nil {
		d.t.Fatal(err)
	}
	ring := o.ring
	if ring == nil {
		if ring, err = secrets.Load(cfg.Secrets); err != nil {
			d.t.Fatal(err)
		}
	}
	session, err := postgres.OpenRestoreSession(ctx, cfg.Database, postgres.MigrateOptions{
		Instance: "restore-pod", Migrate: cfg.Migrate, Version: "0.1.0", Log: testenv.Quiet(), Steps: o.steps,
	})
	if err != nil {
		d.t.Fatal(err)
	}
	endpoint := tgt.Endpoint()
	if o.targetEndpoint != "" {
		endpoint = o.targetEndpoint
	}
	var out bytes.Buffer
	jrn := o.jrn
	if jrn == nil {
		jrn = testJournal{}
	}
	journalFn := func(context.Context, restore.JournalEnv) (restore.Journal, error) { return jrn, nil }
	if o.journalFn != nil {
		journalFn = o.journalFn
	}
	return restore.New(restore.Deps{
		Session: session, Target: tgt, Layout: d.src.layout, TargetEndpoint: endpoint, TargetBucket: tgt.Bucket(),
		Blobs: s3.NewBlobSink(client, firstS3(cfg.Storage), int64(cfg.Backup.Copy.PartSize)), Keys: ring,
		Items:   app.RestoreItems(cfg, testenv.Quiet()),
		Journal: journalFn,
		Config:  cfg.Backup, Log: testenv.Quiet(), Out: &out, Pod: "restore-pod", Hook: o.hook,
	}), &out
}

// blobClient is the destination's blob store, for the assertions on its objects.
func (d *destination) blobClient() *s3.Client {
	d.t.Helper()
	c, err := s3.NewFromFactory(context.Background(), firstS3(d.cfg.Storage), outbound.New(d.cfg.Proxy.Outbound))
	if err != nil {
		d.t.Fatal(err)
	}
	return c
}

// blobObject reads the object of a digest from the destination's blob store, nil when it is absent.
func (d *destination) blobObject(dg format.Digest) []byte {
	d.t.Helper()
	h := dg.Hex()
	key := firstS3(d.cfg.Storage).Prefix + "/sha256/" + h[0:2] + "/" + h[2:4] + "/" + h
	rc, _, err := d.blobClient().Get(context.Background(), key, nil)
	if err != nil {
		return nil
	}
	defer func() { _ = rc.Close() }()
	b, _ := io.ReadAll(rc)
	return b
}

// relations is the number of relations the destination's schema holds.
func (d *destination) relations() int64 {
	return d.stack.Number("SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'quader' AND c.relkind IN ('r','p','v','S')")
}

// editedSteps is the embedded migrations with step n changed by a trailing comment: the binary of a later build in which a
// migration file was edited (R-BAK-023).
func editedSteps(t *testing.T, n int) fs.FS {
	t.Helper()
	emb := postgres.EmbeddedFS()
	out := fstest.MapFS{}
	entries, err := fs.ReadDir(emb, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := fs.ReadFile(emb, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if len(e.Name()) > 5 && e.Name()[:5] == leftPad(n) {
			b = append(b, []byte("\n-- edited after the backup was taken\n")...)
		}
		out[e.Name()] = &fstest.MapFile{Data: b}
	}
	return out
}

func leftPad(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 5 {
		s = "0" + s
	}
	return s
}
