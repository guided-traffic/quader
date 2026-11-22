//go:build integration

package backup

import (
	"context"
	"testing"

	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/backup/format"
	"github.com/guided-traffic/quader/internal/blob/s3"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/outbound"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_BAK_021_ABackupOfTheCreatePipelineRestoresIntoAFreshInstallation is the round trip: the installation of the restore
// tests (seeded files, packages, jobs, sealed secrets) is backed up by `create` - not by the restore fixtures' hand-made exporter
// - and the backup is restored into an empty database and blob prefix. The data, the blobs and the sequences come back.
func TestR_BAK_021_ABackupOfTheCreatePipelineRestoresIntoAFreshInstallation(t *testing.T) {
	src := newSource(t, 25)
	loaded, err := config.Load(config.Options{Environ: append(src.stack.Environ(), "QUADER_BACKUP_TARGET_BUCKET="+testenv.Get(t, "QUADER_TEST_S3_BUCKET_BACKUPS")), Command: config.CommandBackup})
	if err != nil {
		t.Fatal(err)
	}
	cfg := loaded.Config
	factory := outbound.New(cfg.Proxy.Outbound)
	client, err := s3.NewFromFactory(context.Background(), firstS3(cfg.Storage), factory)
	if err != nil {
		t.Fatal(err)
	}
	in := &install{t: t, dbName: src.stack.Database(), dbCfg: cfg.Database, s3cfg: firstS3(cfg.Storage), obj: client, client: client, prefix: firstS3(cfg.Storage).Prefix}
	in.dbCfg.Preflight.AllowSuperuser = true

	// A target of its own, so that the backup under test is the pipeline's and not the fixture's.
	tc := targetConfig(t, testenv.Get(t, "QUADER_TEST_S3_BUCKET_BACKUPS"), "none")
	tgt := newTarget(t, tc)
	cleanup(t, tgt, tc.Target.Prefix+"/")
	r := &rig{cfg: in.backupConfig(t), tgt: tgt}
	r.cfg.Target = tc.Target
	// The fixture put its blobs into the pool only; the source's blob store holds them as in a running installation, each with
	// its copy row on the connection default, which the backup reads its copies from (R-BAK-030).
	s3cfg := firstS3(cfg.Storage)
	src.stack.Exec(`INSERT INTO storage_connections (id, bucket, prefix) VALUES ('default', $1, $2) ON CONFLICT DO NOTHING`, s3cfg.Bucket, s3cfg.Prefix)
	for d, content := range src.seed.blobs {
		src.stack.PutObject(t, d[:], content)
		h := d.Hex()
		src.stack.Exec(`INSERT INTO blob_copies (sha256, connection_id, object_key, stored_size, state) VALUES ($1, 'default', $2, $3, 'present')`,
			d[:], s3cfg.Prefix+"/sha256/"+h[0:2]+"/"+h[2:4]+"/"+h, int64(len(content)))
	}
	sourcePrint := fingerprint(src.stack, exportedTables(src))

	res, err := in.run(context.Background(), r, backup.CreateOptions{NoPrune: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	m := manifestOf(t, tgt, r.cfg, res.BackupID)
	validateManifest(t, mustEncode(t, m))
	src.layout, src.tgtCfg, src.target, src.id, src.m = format.Layout{Prefix: tc.Target.Prefix}, tc, tgt, m.BackupID, m

	dst := src.destination()
	rep, err := dst.restore(backup.RestoreOptions{SkipRevocationJournal: true})
	if err != nil {
		t.Fatalf("restore of a backup written by create: %v", err)
	}
	for _, tf := range m.Tables {
		if rep.Tables[tf.Name] != tf.Rows {
			t.Errorf("table %s: the restore loaded %d rows, the manifest says %d", tf.Name, rep.Tables[tf.Name], tf.Rows)
		}
	}
	restored := fingerprint(dst.stack, nil)
	for _, part := range []string{"itemsHash=", "blobsHash=", "pvHash="} {
		if pick(restored, part) != pick(sourcePrint, part) || pick(restored, part) == "" {
			t.Errorf("%s differs: restored %s, source %s", part, pick(restored, part), pick(sourcePrint, part))
		}
	}
	for d, content := range src.seed.blobs {
		if got := dst.blobObject(d); string(got) != string(content) {
			t.Fatalf("object %s = %q, want %q", d.Hex(), got, content)
		}
	}
}
