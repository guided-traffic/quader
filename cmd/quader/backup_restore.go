package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/guided-traffic/quader/internal/app"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/backup/format"
	"github.com/guided-traffic/quader/internal/backup/restore"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/s3"
	"github.com/guided-traffic/quader/internal/buildinfo"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/journal/replay"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/outbound"
	"github.com/guided-traffic/quader/internal/secrets"
)

// backupRestoreCmd is quader backup restore <id> (R-BAK-020, R-BAK-021). Owned by the restore work package: it replaces
// newRestorer.
func backupRestoreCmd(args []string, s streams) error {
	c, asJSON := newBackupFlags("restore", s)
	check := c.fs.Bool("check", false, "run the preconditions of R-BAK-020 and change nothing")
	skip := c.fs.Bool("skip-revocation-journal", false, "restore without replaying the revocation journal (R-BAK-024)")
	journalCreds := c.fs.String("journal-credentials-file", "", "credentials that read the source's revocation journal when the blob-store credentials cannot")
	identity := c.fs.String("identity-file", "", "age identity of a client-side encrypted backup (R-BAK-027)")
	pos, flags, names, err := parseBackup(c, args, 1)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usageError{backupUsage}
	}
	id, err := format.ParseID(pos[0])
	if err != nil {
		return usageError{err.Error()}
	}
	env, err := openBackupEnv(c, s, flags, names)
	if err != nil {
		return err
	}
	env.json = *asJSON
	restorer, err := newRestorer(env)
	if err != nil {
		return err
	}
	ctx, stop := env.context()
	defer stop()
	rep, err := restorer.Restore(ctx, backup.RestoreOptions{
		ID: id, Check: *check, SkipRevocationJournal: *skip, JournalCredentialsFile: *journalCreds, IdentityFile: *identity,
	})
	if err != nil {
		return err
	}
	if env.json {
		return env.printJSON(rep)
	}
	if rep.AlreadyRestored {
		_, err = fmt.Fprintf(s.stdout, "backup %s is already restored into this database; nothing to do\n", rep.BackupID)
		return err
	}
	if rep.Checked {
		_, err = fmt.Fprintf(s.stdout, "restore of %s: every precondition holds\n", rep.BackupID)
		return err
	}
	_, err = fmt.Fprintf(s.stdout, "restored %s (snapshot %s), journal %s\n", rep.BackupID, rep.SnapshotAt.UTC().Format("2006-01-02T15:04:05Z"), rep.Journal)
	return err
}

// newRestorer builds the Restorer of R-BAK-021: the database session (it takes the migration lock when the restore starts and
// holds it until the command ends), the backup target with its own credential, every connection of the new installation with
// its runtime credential (R-BAK-030), and the key ring.
func newRestorer(env *backupEnv) (backup.Restorer, error) {
	cfg := env.cfg
	ctx := context.Background()
	out := outbound.New(cfg.Proxy.Outbound)
	if err := out.Err(); err != nil {
		return nil, backup.Fail(backup.ExitUsage, backup.CodeConfig, fmt.Errorf("outbound configuration: %w", err))
	}
	tgt, err := s3.NewTarget(ctx, cfg.Backup, out)
	if err != nil {
		return nil, backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
	}
	conns, err := restoreConnections(ctx, cfg, out)
	if err != nil {
		return nil, backup.Fail(backup.ExitUsage, backup.CodeConfig, err) // the error names the connection and its key
	}
	ring, err := secrets.Load(cfg.Secrets)
	if err != nil {
		return nil, backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
	}
	session, err := postgres.OpenRestoreSession(ctx, cfg.Database, postgres.MigrateOptions{
		Instance: env.instance, Migrate: cfg.Migrate, Version: buildinfo.Get().Version, Log: env.log,
	})
	if err != nil {
		if postgres.IsUnavailable(err) {
			return nil, backup.Fail(backup.ExitTemporary, backup.CodeTemporary, err)
		}
		return nil, err
	}
	return restore.New(restore.Deps{
		Session: session, Target: tgt, Layout: format.Layout{Prefix: cfg.Backup.Target.Prefix},
		TargetEndpoint: tgt.Endpoint(), TargetBucket: tgt.Bucket(),
		Connections: conns, Keys: ring,
		Items: app.RestoreItems(cfg, env.log), Journal: restoreJournal(cfg, out, env.log), Config: cfg.Backup, Log: env.log, Out: env.stdout, Pod: env.instance,
	}), nil
}

// restoreConnections is every connection of storage.connections as the restore writes, checks and probes it (R-BAK-030), with
// its runtime credential, the blob writer of R-BAK-015, and the bucket and prefix its files resolve to.
func restoreConnections(ctx context.Context, cfg *config.Config, out *outbound.Factory) ([]restore.Connection, error) {
	conns, err := config.ResolveConnections(cfg.Storage.Connections, nil)
	if err != nil {
		return nil, err
	}
	res := make([]restore.Connection, 0, len(conns))
	for i, el := range conns {
		cc := config.ClientConfig(i, el, cfg.Storage.ObjectStore)
		client, err := s3.NewFromFactory(ctx, cc, out)
		if err != nil {
			return nil, err
		}
		c := restore.Connection{
			ID: el.ID, Bucket: el.Bucket, Prefix: el.KeyPrefix(), Drain: el.Drain, Store: s3.NewBlobSink(client, cc, int64(cfg.Backup.Copy.PartSize)),
			Encrypted: el.Encrypted, KeyedNames: el.UsesKeyedNames(),
		}
		if el.Encrypted {
			// an encrypted connection is written through its own writer with the keys the restore opens from the backup or draws
			// (backup-and-restore.md R-BAK-031)
			c.Writer = blob.NewConnection(el, client)
		}
		res = append(res, c)
	}
	return res, nil
}

// restoreJournal builds the revocation journal of a source for the restore (R-BAK-020 check 8, R-BAK-024): the reader of the
// journal on every connection of the source (R-BAK-030), each with the blob-store credentials or with its element of
// --journal-credentials-file, and the replayer over the restore's session.
func restoreJournal(cfg *config.Config, out *outbound.Factory, log *slog.Logger) func(context.Context, restore.JournalEnv) (restore.Journal, error) {
	return func(ctx context.Context, e restore.JournalEnv) (restore.Journal, error) {
		locs, err := sourceLocations(cfg, e.Sources)
		if err != nil {
			return nil, err
		}
		reader, err := replay.OpenJournal(ctx, locs, out, e.CredentialsFile)
		if err != nil {
			return nil, err
		}
		return journalSource{reader: reader, Replayer: replay.New(replay.Options{DB: e.Runner, Reader: reader, Recorder: e.Recorder, AdminBinding: authz.HasAdminBinding, Log: log})}, nil
	}
}

// sourceLocations points a client at the journal of each source connection: the client configuration of the restoring
// installation's connection of the same id (its credential, CA and settings), or of its first connection when none has that id,
// with the source's bucket and prefix and, where the source names one, its endpoint.
func sourceLocations(cfg *config.Config, sources []format.ConnectionRef) ([]replay.Location, error) {
	conns, err := config.ResolveConnections(cfg.Storage.Connections, nil)
	if err != nil {
		return nil, err
	}
	if len(conns) == 0 {
		return nil, errors.New("storage.connections: no connection is configured (R-STOR-050)")
	}
	base := map[string]config.StorageS3{}
	for i, el := range conns {
		base[el.ID] = config.ClientConfig(i, el, cfg.Storage.ObjectStore)
	}
	first := base[conns[0].ID]
	locs := make([]replay.Location, 0, len(sources))
	for _, s := range sources {
		cc, ok := base[s.ID]
		if !ok {
			cc = first
		}
		cc.Bucket, cc.Prefix = s.Bucket, s.Prefix
		if s.Endpoint != "" {
			cc.Endpoint = s.Endpoint
		}
		locs = append(locs, replay.Location{Connection: s.ID, Client: cc})
	}
	return locs, nil
}

// journalSource is the journal of the source: the check lists it, the replay applies it.
type journalSource struct {
	reader *replay.Reader
	journal.Replayer
}

// Reachable lists the journal prefix for the entries of today and later, which needs the same permission as a replay.
func (j journalSource) Reachable(ctx context.Context) error {
	_, _, err := j.reader.Entries(ctx, time.Now())
	return err
}
