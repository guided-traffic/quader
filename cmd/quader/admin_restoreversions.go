package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/restoreversions"
	"github.com/guided-traffic/quader/internal/blob/verify"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/journal/replay"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/outbound"
)

// storageRestoreVersionsCmd is quader admin storage restore-versions (R-HA-025 step 5, backup-and-restore.md R-BAK-030): after a
// point-in-time restore of the database it finds the copies whose object is missing, on each connection or the one --connection
// names, and removes the delete markers younger than t from their keys, so that the version before the marker is current
// again. It needs a credential that may list and delete object versions (s3:ListBucketVersions, s3:DeleteObjectVersion), which
// the runtime credential deliberately lacks (R-HA-023): the operator passes it with --credentials-file, to this command only,
// as one element per connection of the run in the journal reader's file (Q-STOR-268, backup-and-restore.md Q-BAK-73). It exits 5
// when a blob still has no copy with its object afterwards (with --connection: a copy on that connection), 75 when the database
// or a store could not be reached or a check was not answered.
func storageRestoreVersionsCmd(args []string, s streams) error {
	c, asJSON := newBackupFlags("admin storage restore-versions", s)
	before := c.fs.String("before", "", "t, the time the platform restore went to (RFC 3339): delete markers created after it are removed")
	creds := c.fs.String("credentials-file", "", "a file of one credential per connection that may list and delete object versions, each used on its connection; empty = each connection's own credential")
	conn := c.fs.String("connection", "", "restore only the copies on this connection; every connection without it")
	dry := c.fs.Bool("dry-run", false, "list the blobs that would be restored and remove nothing")
	conc := c.fs.Int("concurrency", verify.DefaultConcurrency, "simultaneous existence checks")
	_, flags, names, err := parseBackup(c, args, 0)
	if err != nil {
		return backupExit(err)
	}
	t, perr := time.Parse(time.RFC3339, *before)
	if *before == "" || perr != nil || *conc < 1 {
		return usageError{storageUsage}
	}
	env, err := openPostRestoreEnv(c, s, flags, names)
	if err != nil {
		return backupExit(err)
	}
	env.json = *asJSON
	ctx, stop := env.context()
	defer stop()
	cfg := env.cfg
	db, err := postgres.Open(ctx, cfg.Database, env.instance, meta.RoleWeb, env.log)
	if err != nil {
		if postgres.IsUnavailable(err) {
			return backup.Fail(backup.ExitTemporary, backup.CodeTemporary, err)
		}
		return backupExit(err)
	}
	defer db.Close()
	override, err := restoreVersionsCredentials(cfg.Storage, *creds)
	if err != nil {
		return backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
	}
	clients, err := connectionClients(ctx, cfg.Storage, outbound.New(cfg.Proxy.Outbound), override)
	if err != nil {
		return backupExit(err)
	}
	conns := make([]restoreversions.Conn, 0, len(clients))
	for _, cl := range clients {
		conns = append(conns, restoreversions.Conn{ID: cl.id, Store: cl.client})
	}
	rep, err := restoreversions.Run(ctx, restoreversions.Options{
		DB: verify.PGSource{DB: db}, Connections: conns, Connection: *conn, Before: t, DryRun: *dry, Concurrency: *conc, Log: env.log,
	})
	if errors.Is(err, verify.ErrUnknownConnection) {
		return backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
	}
	if rep != nil {
		if env.json {
			if jerr := env.printJSON(rep); jerr != nil {
				return jerr
			}
		} else if werr := writeRestoreVersionsReport(s.stdout, rep); werr != nil {
			return werr
		}
		if rep.MarkersRemoved > 0 {
			if aerr := recordVersionsRestored(ctx, env, db, rep); aerr != nil {
				return backupExit(aerr)
			}
		}
	}
	if err != nil {
		if blob.IsConfig(err) {
			return backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
		}
		return backupExit(err)
	}
	switch {
	case rep.Lost > 0:
		return backup.Fail(backup.ExitFindings, backup.CodeFindings,
			fmt.Errorf("%d findings remain: %d copies still have no object and %d another size; %d items are affected", rep.Lost, rep.Remaining, rep.SizeMismatch, rep.ItemsAffected))
	case rep.Unchecked > 0:
		return backup.Fail(backup.ExitTemporary, backup.CodeTemporary, fmt.Errorf("%d copies could not be checked: the object store did not answer", rep.Unchecked))
	}
	return nil
}

// restoreVersionsCredentials reads the credentials file of --credentials-file, the journal reader's file of one element per
// connection (backup-and-restore.md Q-BAK-73), and returns the credential it names for each connection of the configuration. As
// for a journal reader, a connection the file names no element for keeps its own credential, which lists no object versions
// (R-HA-023): a run that needs them there ends with the store's refusal for that connection. An empty path is each connection's
// own credential.
func restoreVersionsCredentials(st config.Storage, path string) (map[string]config.StorageS3Credentials, error) {
	if path == "" {
		return nil, nil
	}
	file, err := replay.LoadCredentials(path)
	if err != nil {
		return nil, err
	}
	out := map[string]config.StorageS3Credentials{}
	for _, el := range st.Connections {
		id := el.WithDefaults().ID
		if c, ok := file.For(id); ok {
			out[id] = c
		}
	}
	return out, nil
}

// recordVersionsRestored writes system.storage.versions_restored for a run that removed delete markers.
func recordVersionsRestored(ctx context.Context, env *backupEnv, db *postgres.DB, rep *restoreversions.Report) error {
	rec, err := audit.New(audit.Options{DB: db, Replica: env.instance, Config: env.cfg.Audit, Log: env.log, AppLog: env.log})
	if err != nil {
		return err
	}
	details, err := json.Marshal(map[string]any{
		"before": rep.Before.UTC().Format(time.RFC3339), "markersRemoved": rep.MarkersRemoved, "restored": rep.Restored, "remaining": rep.Remaining,
	})
	if err != nil {
		return err
	}
	return db.InTx(ctx, func(tx meta.Tx) error {
		return rec.RecordTx(ctx, tx, audit.Event{
			Type: audit.EventStorageVersionsRestored, Outcome: audit.Success, Actor: audit.Actor{Kind: "system", Name: "admin"}, Details: details,
		})
	})
}

// writeRestoreVersionsReport prints one line per copy that had no object, and the totals last (the line the end-to-end tests
// read).
func writeRestoreVersionsReport(w io.Writer, rep *restoreversions.Report) error {
	for _, e := range rep.Entries {
		var detail string
		switch e.Outcome {
		case restoreversions.OutcomeRestored:
			detail = fmt.Sprintf("restored, %d delete markers removed", e.MarkersRemoved)
		case restoreversions.OutcomeWouldRestore:
			detail = fmt.Sprintf("would be restored, %d delete markers would be removed", e.MarkersRemoved)
		case restoreversions.OutcomeNoDeleteMarker:
			detail = "no delete marker younger than t: the object is not missing because of a deletion after t"
		case restoreversions.OutcomeNoVersion:
			detail = "no object version below its delete markers"
		default:
			detail = "still without object after removing its delete markers"
		}
		items := ""
		if e.ItemCount > 0 {
			items = fmt.Sprintf(" (%d items)", e.ItemCount)
		}
		if _, err := fmt.Fprintf(w, "copy %s %s: %s%s\n", e.Connection, e.SHA256, detail, items); err != nil {
			return err
		}
	}
	mode := ""
	if rep.DryRun {
		mode = " (dry run)"
	}
	_, err := fmt.Fprintf(w, "storage restore-versions before %s%s: %d blobs checked, %d without object, %d restored, %d delete markers removed, %d still without object, %d of another size, %d not checked\n",
		rep.Before.UTC().Format(time.RFC3339), mode, rep.Checked, rep.Missing, rep.Restored, rep.MarkersRemoved, rep.Remaining, rep.SizeMismatch, rep.Unchecked)
	return err
}
