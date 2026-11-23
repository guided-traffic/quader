package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/secrets"
)

const storageKeysUsage = `usage: quader admin storage keys export [--connection <id>] [--config <file>]
       quader admin storage keys import <file> [--other-installation] [--json] [--config <file>]
       quader admin storage keys new-generation --connection <id> [--json] [--config <file>]
       quader admin storage keys destroy --connection <id> [--confirm <token>] [--dry-run] [--json] [--config <file>]

export          print the rows of storage_keys, sealed as they lie, as one JSON document; keep it beside the escrow copy of the key ring.
                It opens nothing and needs no key ring (R-STOR-085); a destroyed key is listed without a sealed value (R-STOR-103)
import          open every key of an export with the configured key ring, then add the rows the database lacks and keep the equal ones
                (R-STOR-085); exit 2, writing nothing, when the ring lacks a root key, a key does not open, a key exists with another
                value or the file is of another installation (accepted with --other-installation). A key of a connection whose keys
                were destroyed is skipped and reported: nothing brings it back (R-STOR-103)
new-generation  draw generation n+1 of a connection's storage key and retire generation n; new objects are wrapped under it once the
                replicas learn it, and the older generations keep opening what they wrapped (R-STOR-083). Export again afterwards
destroy         destroy every storage key and the name key of a retired connection, so that what its provider still holds becomes
                unreadable (R-STOR-097 to R-STOR-100). Without --confirm a dry run: it checks, reports and prints the confirmation; with
                the confirmation it destroys. Exit 5 while something still holds the keys (a live replica, the connection's state, a
                copy, a queue row, an upload session, a retained backup), also in a dry run. It cannot be undone

exit codes: 0 done, 1 failed, 2 usage, or a file, key ring, database or confirmation that does not fit, 5 destroy refused by a holder,
75 temporary (the database could not be reached)`

// exportReminder is printed after every change of the rows (R-STOR-085, R-STOR-090).
const exportReminder = "export the storage keys again and keep the export beside the key ring's escrow copy: quader admin storage keys export"

// storageKeysCmd dispatches quader admin storage keys <command>.
func storageKeysCmd(args []string, s streams) error {
	if len(args) >= 1 {
		switch args[0] {
		case "export":
			return storageKeysExportCmd(args[1:], s)
		case "import":
			return storageKeysImportCmd(args[1:], s)
		case "new-generation":
			return storageKeysGenerationCmd(args[1:], s)
		case "destroy":
			return storageKeysDestroyCmd(args[1:], s)
		}
	}
	return usageError{storageKeysUsage}
}

// keysDeps are what the commands of the storage keys work with once the database is open; the unit tests give them the simulated
// database of internal/blob/keys/keystest.
type keysDeps struct {
	db           meta.Runner
	rows         keys.Rows
	installation string
	// instance is the pod the command runs in, which an export records (storage_key_exports, R-STOR-103).
	instance string
	ring     keys.Sealer
	rec      audit.Recorder
	// notify announces the change area storage_keys for a connection in the writing transaction (R-STOR-083).
	notify func(tx meta.Tx, connection string) error
	// notifyDestroyed announces a destruction with the payload's version 2 (R-STOR-101).
	notifyDestroyed func(tx meta.Tx, connection string, ids []string) error
	random          io.Reader
	now             func() time.Time
}

// keysEnv opens what the commands need: the configuration, the database with the runtime role (where admin storage verify runs),
// the installation's id, the audit recorder, the change log's writer and, with ring, the configured key ring.
type keysEnv struct {
	env   *backupEnv
	deps  keysDeps
	close func()
}

func openKeysEnv(c *commonFlags, s streams, flags, names map[string]string, withRing bool) (*keysEnv, context.Context, context.CancelFunc, error) {
	env, err := openPostRestoreEnv(c, s, flags, names)
	if err != nil {
		return nil, nil, nil, err
	}
	cfg := env.cfg
	deps := keysDeps{random: rand.Reader, now: time.Now}
	if withRing {
		ring, err := secrets.Load(cfg.Secrets)
		if err != nil {
			return nil, nil, nil, backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
		}
		deps.ring = ring
	}
	ctx, stop := env.context()
	db, err := postgres.Open(ctx, cfg.Database, env.instance, meta.RoleWeb, env.log)
	if err != nil {
		stop()
		if postgres.IsUnavailable(err) {
			return nil, nil, nil, backup.Fail(backup.ExitTemporary, backup.CodeTemporary, err)
		}
		return nil, nil, nil, err
	}
	facts, err := db.SystemFacts(ctx)
	if err != nil {
		db.Close()
		stop()
		return nil, nil, nil, err
	}
	rec, err := audit.New(audit.Options{DB: db, Replica: env.instance, Config: cfg.Audit, Log: env.log, AppLog: env.log})
	if err != nil {
		db.Close()
		stop()
		return nil, nil, nil, err
	}
	// The change log writes the area's row so that a running replica learns of the change; the command holds no replica and polls
	// nothing (as admin post-restore does).
	changes, err := cluster.NewChanges(cluster.ChangesOptions{DB: db.ControlPool(), Config: cfg.Cluster, WatchdogTimeout: cfg.Health.WatchdogTimeout, Log: env.log})
	if err != nil {
		db.Close()
		stop()
		return nil, nil, nil, err
	}
	deps.db, deps.installation, deps.instance, deps.rec = db, facts.InstallationID, env.instance, rec
	deps.notify = func(tx meta.Tx, connection string) error {
		return changes.Notify(tx, cluster.AreaStorageKeys, 1, map[string]string{"connection": connection})
	}
	deps.notifyDestroyed = func(tx meta.Tx, connection string, ids []string) error {
		return changes.Notify(tx, cluster.AreaStorageKeys, keys.DestroyedChangeVersion, keys.DestroyedChange(connection, ids))
	}
	return &keysEnv{env: env, deps: deps, close: db.Close}, ctx, stop, nil
}

// adminEvent writes a transactional audit event of actor system:admin.
func adminEvent(ctx context.Context, rec audit.Recorder, tx meta.Tx, typ string, details any) error {
	data, err := json.Marshal(details)
	if err != nil {
		return err
	}
	return rec.RecordTx(ctx, tx, audit.Event{Type: typ, Outcome: audit.Success, Actor: audit.Actor{Kind: "system", Name: "admin"}, Details: data})
}

// storageKeysExportCmd is quader admin storage keys export (R-STOR-085): one JSON document of the rows of storage_keys on standard
// output, sealed as they lie.
func storageKeysExportCmd(args []string, s streams) error {
	c, _ := newBackupFlags("admin storage keys export", s)
	conn := c.fs.String("connection", "", "export the keys of this connection only")
	pos, flags, names, err := parseBackup(c, args, 0)
	if err != nil || len(pos) != 0 {
		return keysUsage(err)
	}
	ke, ctx, stop, err := openKeysEnv(c, s, flags, names, false)
	if err != nil {
		return backupExit(err)
	}
	defer stop()
	defer ke.close()
	return backupExit(runKeysExport(ctx, ke.deps, *conn, s.stdout))
}

// runKeysExport reads the rows, records the export and writes system.storage.keys_exported in the same transaction and then prints
// the document.
func runKeysExport(ctx context.Context, d keysDeps, connection string, out io.Writer) error {
	doc, err := keys.Export(ctx, d.db, d.rows, d.installation, d.instance, connection, d.now(), func(ctx context.Context, tx meta.Tx, doc keys.Document) error {
		return adminEvent(ctx, d.rec, tx, audit.EventStorageKeysExported, map[string]any{
			"connection": connection, "keys": len(doc.Keys), "rootKeyIds": doc.RootKeyIDs, "keyIds": doc.KeyIDs(),
		})
	})
	if errors.Is(err, keys.ErrNoKeys) {
		return backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
	}
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// storageKeysImportCmd is quader admin storage keys import <file> (R-STOR-085).
func storageKeysImportCmd(args []string, s streams) error {
	c, asJSON := newBackupFlags("admin storage keys import", s)
	other := c.fs.Bool("other-installation", false, "accept an export of another installation, for a recovery into a new installation")
	pos, flags, names, err := parseBackup(c, args, 1)
	if err != nil || len(pos) != 1 {
		return keysUsage(err)
	}
	f, err := os.Open(pos[0]) //nolint:gosec // G703: the operator names the export file on the command line
	if err != nil {
		return backup.Fail(backup.ExitUsage, backup.CodeConfig, fmt.Errorf("the export %s cannot be read: %w", pos[0], err))
	}
	doc, err := keys.ReadDocument(f)
	_ = f.Close()
	if err != nil {
		return backupExit(importExit(err))
	}
	ke, ctx, stop, err := openKeysEnv(c, s, flags, names, true)
	if err != nil {
		return backupExit(err)
	}
	defer stop()
	defer ke.close()
	return backupExit(runKeysImport(ctx, ke.deps, doc, *other, *asJSON, s.stdout))
}

// runKeysImport imports a document read from a file and prints what it did.
func runKeysImport(ctx context.Context, d keysDeps, doc keys.Document, other, asJSON bool, out io.Writer) error {
	rep, err := keys.Import(ctx, d.db, d.rows, d.ring, doc, keys.ImportOptions{Installation: d.installation, OtherInstallation: other},
		func(ctx context.Context, tx meta.Tx, rep keys.ImportReport) error {
			for _, c := range rep.Connections {
				if err := d.notify(tx, c); err != nil {
					return err
				}
			}
			return adminEvent(ctx, d.rec, tx, audit.EventStorageKeysImported, rep)
		})
	if err != nil {
		return importExit(err)
	}
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	_, err = fmt.Fprintf(out, "storage keys imported for %s: %d added, %d already present and equal; %s\n",
		strings.Join(rep.Connections, ", "), rep.Inserted, rep.Kept, exportReminder)
	if err == nil && len(rep.Destroyed) > 0 {
		_, err = fmt.Fprintf(out, "skipped as destroyed (their connection's keys were destroyed; nothing brings them back): %s\n", strings.Join(rep.Destroyed, ", "))
	}
	if err == nil && len(rep.StillSealed) > 0 {
		_, err = fmt.Fprintf(out, "listed as destroyed in the file and sealed in this database, left as they are (an import never destroys a key; quader admin storage keys destroy does): %s\n",
			strings.Join(rep.StillSealed, ", "))
	}
	return err
}

// importExit makes a refusal of the import exit 2 with its sentence.
func importExit(err error) error {
	var ie *keys.ImportError
	if errors.As(err, &ie) {
		return backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
	}
	return err
}

// storageKeysGenerationCmd is quader admin storage keys new-generation --connection <id> (R-STOR-083).
func storageKeysGenerationCmd(args []string, s streams) error {
	c, asJSON := newBackupFlags("admin storage keys new-generation", s)
	conn := c.fs.String("connection", "", "the encrypted connection whose storage key gets a new generation")
	pos, flags, names, err := parseBackup(c, args, 0)
	if err != nil || len(pos) != 0 || *conn == "" {
		return keysUsage(err)
	}
	ke, ctx, stop, err := openKeysEnv(c, s, flags, names, true)
	if err != nil {
		return backupExit(err)
	}
	defer stop()
	defer ke.close()
	return backupExit(runKeysGeneration(ctx, ke.deps, *conn, *asJSON, s.stdout))
}

// runKeysGeneration draws the generation with its audit event and its change in one transaction and prints the reminder to export.
func runKeysGeneration(ctx context.Context, d keysDeps, connection string, asJSON bool, out io.Writer) error {
	g, err := keys.NewGeneration(ctx, d.db, d.rows, d.ring, d.random, connection, func(ctx context.Context, tx meta.Tx, g keys.Generation) error {
		if err := d.notify(tx, g.Connection); err != nil {
			return err
		}
		return adminEvent(ctx, d.rec, tx, audit.EventStorageKeyGenerationAdded, g)
	})
	switch {
	case errors.Is(err, keys.ErrNoStorageKey), keys.IsRefusal(err):
		return backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
	case err != nil:
		return err
	}
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(g)
	}
	_, err = fmt.Fprintf(out, "connection %s: storage key %s (generation %d) is active, %s is retired and keeps opening what it wrapped; %s\n",
		g.Connection, g.KeyID, g.Generation, g.RetiredKeyID, exportReminder)
	return err
}

// keysUsage is the usage of the storage keys' commands, or the error of the flags (a help request passes through).
func keysUsage(err error) error {
	var ue usageError
	if err != nil && !errors.As(err, &ue) {
		return backupExit(err)
	}
	return usageError{storageKeysUsage}
}
