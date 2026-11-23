package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/backup/create"
	"github.com/guided-traffic/quader/internal/backup/format"
	"github.com/guided-traffic/quader/internal/backup/retention"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/s3"
	"github.com/guided-traffic/quader/internal/buildinfo"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/observability"
	"github.com/guided-traffic/quader/internal/outbound"
)

// backupCreateCmd is quader backup create (R-BAK-009).
func backupCreateCmd(args []string, s streams) error {
	c, asJSON := newBackupFlags("create", s)
	trigger := c.fs.String("trigger", string(backup.TriggerManual), "who started the run: schedule or manual; the chart's CronJob passes schedule")
	exclude := c.fs.Bool("exclude-remote-caches", false, "leave out blobs that only remote-repository caches reference (R-BAK-008)")
	noPrune := c.fs.Bool("no-prune", false, "skip the retention and pool sweep of the pruning phase")
	adopt := c.fs.Bool("adopt-target", false, "adopt a target that names the installation this one was restored from (R-BAK-002)")
	_, flags, names, err := parseBackup(c, args, 0)
	if err != nil {
		return err
	}
	if *trigger != string(backup.TriggerSchedule) && *trigger != string(backup.TriggerManual) {
		return usageError{backupUsage}
	}
	env, err := openBackupEnv(c, s, flags, names)
	if err != nil {
		return err
	}
	env.json = *asJSON
	creator, err := newCreator(env)
	if err != nil {
		return err
	}
	ctx, stop := env.context()
	defer stop()
	res, err := creator.Create(ctx, backup.CreateOptions{
		Trigger: backup.Trigger(*trigger), ExcludeRemoteCaches: *exclude, NoPrune: *noPrune, AdoptTarget: *adopt,
	})
	if err != nil {
		return err
	}
	if env.json {
		return env.printJSON(res)
	}
	_, err = fmt.Fprintf(s.stdout, "backup %s %s: %d blobs copied (%d bytes), %d damaged\n", res.BackupID, res.Outcome, res.BlobsCopied, res.BytesCopied, res.DamagedBlobs)
	return err
}

// newCreator builds the Creator of a backup run. The connections open when the run starts, with the run's context, so a
// SIGTERM during the connect ends the command.
func newCreator(e *backupEnv) (backup.Creator, error) { return &commandCreator{env: e}, nil }

// commandCreator opens the database, the blob store and the target of one run and runs the create pipeline on them.
type commandCreator struct{ env *backupEnv }

// Create implements backup.Creator.
func (c *commandCreator) Create(ctx context.Context, opts backup.CreateOptions) (*backup.Result, error) {
	e := c.env
	cfg := e.cfg
	db, err := postgres.Open(ctx, cfg.Database, e.instance, meta.RoleBackup, e.log)
	if err != nil {
		return nil, mapOpenError(err)
	}
	defer db.Close()
	metrics := observability.NewMetrics(meta.RoleBackup)
	sources, refs, err := backupSources(ctx, cfg, outbound.New(cfg.Proxy.Outbound), blob.NewMetrics(metrics), e.log)
	if err != nil {
		return nil, backup.Fail(backup.ExitUsage, backup.CodeConfig, err) // the error names the connection and its key
	}
	tgt, layout, err := openTarget(ctx, e)
	if err != nil {
		return nil, err
	}
	events := backup.NewPGEvents(db, e.instance, e.log)
	stopOps := serveBackupMetrics(cfg.Server.OpsListen, metrics, e.log)
	defer stopOps()
	facts := backup.NewPGFacts(db, cfg.Database.Schema)
	store := backup.NewPGStore(db)
	b := cfg.Backup
	stage := &retention.Stage{
		DeleteTimeout: b.FinalizeTimeout,
		Store:         tgt, Layout: layout, Policy: retention.Policy{KeepLast: b.Retention.KeepLast, KeepWithin: b.Retention.KeepWithin},
		SweepInterval: b.Retention.SweepInterval, Facts: facts, Audit: events,
		Clock: retention.RunSweepClock{Runs: store}, Log: e.log,
	}
	info := buildinfo.Get()
	var staying []string
	for _, s := range sources {
		if !s.Drain {
			staying = append(staying, s.ID)
		}
	}
	pipeline, err := create.New(create.Deps{
		Cfg:               cfg.Backup,
		SourceConnections: refs,
		Staying:           staying,
		Instance:          e.instance, JobName: jobName(e.environ), Version: info.Version, Revision: info.Commit, BackupRole: cfg.Database.BackupRole,
		Store: store, Prune: stage, Facts: facts, Grants: facts, Pins: facts, BlobState: facts,
		Snapshot: backup.NewPGSnapshotter(db),
		Source:   backup.NewStoreSource(facts, sources),
		Target:   tgt, Events: events,
		Metrics: backup.NewMetrics(metrics), Log: e.log,
	})
	if err != nil {
		return nil, err
	}
	return pipeline.Create(ctx, opts)
}

// backupSources builds the backup's reader of every connection of storage.connections (R-BAK-030): a client per connection with
// the read credential of its element of backup.source.connections, or with the connection's own credential when it has none,
// behind a guard of its own, so a connection that does not answer opens its circuit and its copies are passed over fast; and the
// locations the manifest records for the journal of a restore.
func backupSources(ctx context.Context, cfg *config.Config, out *outbound.Factory, bm *blob.Metrics, log *slog.Logger) ([]backup.SourceConnection, []format.ConnectionRef, error) {
	conns, err := config.ResolveConnections(cfg.Storage.Connections, nil)
	if err != nil {
		return nil, nil, err
	}
	if len(conns) == 0 {
		return nil, nil, errors.New("storage.connections: no connection is configured (R-STOR-050)")
	}
	read := map[string]config.StorageS3Credentials{}
	for _, c := range cfg.Backup.Source.Connections {
		read[c.Connection] = c.Credentials()
	}
	t := cfg.Backup.Target
	target := config.ComparableEndpoint(t.Endpoint, t.Region)
	var sources []backup.SourceConnection
	var refs []format.ConnectionRef
	for i, el := range conns {
		cc := config.ClientConfig(i, el, cfg.Storage.ObjectStore)
		if creds, ok := read[el.ID]; ok {
			cc.Credentials = creds
		}
		client, err := s3.NewFromFactory(ctx, cc, out)
		if err != nil {
			return nil, nil, err
		}
		guard := blob.NewGuard(client, cfg.Storage.ObjectStore.CircuitBreaker, blob.WithGuardMetrics(bm), blob.WithGuardConnection(el.ID),
			blob.WithGuardLogger(log, cc.Credentials.Source))
		endpoint := config.NormalizeEndpoint(el.Endpoint)
		// an encrypted connection's copies go into the pool as stored, with their records; the pod opens nothing (R-BAK-031)
		sources = append(sources, backup.SourceConnection{
			ID: el.ID, Drain: el.Drain, Objects: guard, Endpoint: endpoint, Bucket: el.Bucket,
			ServerSide: config.ComparableEndpoint(el.Endpoint, el.Region) == target, Encrypted: el.Encrypted,
		})
		refs = append(refs, format.ConnectionRef{ID: el.ID, Endpoint: endpoint, Bucket: el.Bucket, Prefix: el.KeyPrefix()})
	}
	return sources, refs, nil
}

// mapOpenError gives a failure to open the database the exit code of R-BAK-013: a standby is a usage error, an unreachable
// database is temporary.
func mapOpenError(err error) error {
	switch {
	case errors.Is(err, postgres.ErrBackupStandby):
		return backup.Fail(backup.ExitUsage, backup.CodeStandby, err)
	case postgres.IsState(err, "42501"):
		return backup.Failf(backup.ExitUsage, backup.CodeBackupRoleGrant,
			"the database role of the backup lacks privileges (%v); quader migrate grants them to database.backup_role (R-BAK-015)", err)
	case postgres.IsUnavailable(err):
		return backup.Fail(backup.ExitTemporary, backup.CodeTemporary, err)
	}
	return err
}

// serveBackupMetrics exports the counters of the run on the operations port for as long as it lasts, for clusters that scrape
// Jobs (R-BAK-014). A port that cannot be bound is logged and the run goes on: the counters are not the backup.
func serveBackupMetrics(addr string, m *observability.Metrics, log *slog.Logger) func() {
	if addr == "" {
		return func() {}
	}
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", addr)
	if err != nil {
		log.Warn("the backup run does not export metrics", "addr", addr, "err", err)
		return func() {}
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", m.Handler())
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return func() { _ = srv.Close() }
}

// jobName is the Kubernetes Job the pod belongs to: the chart sets QUADER_JOB_NAME from the pod's job-name label. Empty outside a
// Job (R-BAK-010).
func jobName(environ []string) string {
	for _, kv := range environ {
		if v, ok := strings.CutPrefix(kv, "QUADER_JOB_NAME="); ok {
			return v
		}
	}
	return ""
}
