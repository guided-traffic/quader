package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/s3"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/outbound"
)

// The commands of the object-store connections: the conformance check per connection (storage.md R-STOR-035) and
// forget-connection (R-STOR-051).

// connectionCheck is the report of the conformance check of one connection.
type connectionCheck struct {
	Connection string             `json:"connection"`
	Marker     string             `json:"marker"`
	Results    []blob.CheckResult `json:"results"`
}

// storageCheckCmd is quader admin storage check [--connection <id>] (R-STOR-035): the startup check with its capabilities and
// the conformance checks on each connection of the configuration, or on the one named, with each connection's own client and
// the others as the sources of the server-side copy between buckets of one endpoint. It reads the marker of each connection
// and writes none, opens no database and changes nothing outside <prefix>/_probe/. Overwrite and delete visibility are reported
// as strong or eventual, never as a failure; a capability the store lacks is a finding, not a failure. It exits 5 when a check
// that is not optional failed, 2 for a usage or configuration error.
func storageCheckCmd(args []string, s streams) error {
	c, asJSON := newBackupFlags("admin storage check", s)
	only := c.fs.String("connection", "", "the id of the one connection to check; every connection without it")
	_, flags, names, err := parseBackup(c, args, 0)
	if err != nil {
		return backupExit(err)
	}
	env, err := openPostRestoreEnv(c, s, flags, names)
	if err != nil {
		return backupExit(err)
	}
	ctx, stop := env.context()
	defer stop()
	cfg := env.cfg.Storage
	resolved, err := config.ResolveConnections(cfg.Connections, nil)
	if err != nil {
		return backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
	}
	if len(resolved) == 0 {
		return backup.Fail(backup.ExitUsage, backup.CodeConfig, errors.New("storage.connections: no connection is configured"))
	}
	f := outbound.New(env.cfg.Proxy.Outbound)
	clients := make([]*s3.Client, len(resolved))
	for i, el := range resolved {
		if clients[i], err = s3.NewFromFactory(ctx, config.ClientConfig(i, el, cfg.ObjectStore), f); err != nil {
			return backup.Fail(backup.ExitUsage, backup.CodeConfig, err) // the error names the connection and its key
		}
	}
	var reports []connectionCheck
	failed, found := 0, false
	for i, el := range resolved {
		if *only != "" && el.ID != *only {
			continue
		}
		found = true
		var peers []blob.ConformancePeer
		for j, other := range resolved {
			if j != i {
				peers = append(peers, blob.ConformancePeer{ID: other.ID, Prefix: other.KeyPrefix(), Store: clients[j]})
			}
		}
		alg := el.PayloadChecksum
		if alg == "none" {
			alg = ""
		}
		rep := connectionCheck{Connection: el.ID, Marker: readMarker(ctx, clients[i], el.KeyPrefix())}
		rep.Results = blob.RunConformance(ctx, clients[i], blob.ConformanceOptions{
			Prefix: el.KeyPrefix(), Replica: "check-" + env.instance, HTTP: clients[i].HTTPClient(), PayloadChecksum: alg, Peers: peers,
		})
		for _, r := range rep.Results {
			if !r.OK && !r.Optional {
				failed++
			}
		}
		reports = append(reports, rep)
	}
	if !found {
		return backup.Fail(backup.ExitUsage, backup.CodeConfig, fmt.Errorf("--connection %s: no connection of the configuration has this id", *only))
	}
	if *asJSON {
		if err := env.printJSON(reports); err != nil {
			return err
		}
	} else if err := writeCheckReport(s.stdout, reports); err != nil {
		return err
	}
	if failed > 0 {
		return backup.Fail(backup.ExitFindings, backup.CodeFindings, fmt.Errorf("%d checks failed", failed))
	}
	return nil
}

// readMarker reports the marker of a connection as the check finds it: absent, or the installation and the connection it names.
func readMarker(ctx context.Context, c blob.ObjectStore, prefix string) string {
	m, found, err := blob.ReadMarker(ctx, c, prefix)
	switch {
	case errors.Is(err, blob.ErrNotMarker):
		return "present, and no marker of a connection"
	case err != nil:
		return "not read: " + err.Error()
	case !found:
		return "absent: the first replica that starts with the connection writes it"
	}
	return fmt.Sprintf("connection %s of installation %s", m.ConnectionID, m.InstallationID)
}

// writeCheckReport prints one line per check of every connection and the marker, the failures marked.
func writeCheckReport(w io.Writer, reports []connectionCheck) error {
	for _, rep := range reports {
		if _, err := fmt.Fprintf(w, "connection %s: marker %s\n", rep.Connection, rep.Marker); err != nil {
			return err
		}
		for _, r := range rep.Results {
			state := "ok"
			switch {
			case !r.OK && (r.Optional || r.Capability):
				state = "absent"
			case !r.OK:
				state = "FAILED"
			}
			line := fmt.Sprintf("  %-6s %s", state, r.Name)
			if r.Note != "" {
				line += " (" + r.Note + ")"
			}
			if r.Error != "" {
				line += ": " + r.Error
			}
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
	}
	return nil
}

// storageForgetCmd is quader admin storage forget-connection <id> (R-STOR-051): it gives up a connection that is gone for
// good. It is refused (exit 2) while the command's own configuration or a live replica has the connection; otherwise it
// deletes the connection's copy and queue rows, lets the content state of every blob it held follow from the copies that remain,
// retires the id and writes system.storage.connection_forgotten. It deletes no object.
func storageForgetCmd(args []string, s streams) error {
	c, asJSON := newBackupFlags("admin storage forget-connection", s)
	pos, flags, names, err := parseBackup(c, args, 1)
	if err != nil {
		return backupExit(err)
	}
	if len(pos) != 1 {
		return usageError{storageUsage}
	}
	id := pos[0]
	env, err := openPostRestoreEnv(c, s, flags, names)
	if err != nil {
		return backupExit(err)
	}
	for _, el := range env.cfg.Storage.Connections {
		if el.WithDefaults().ID == id {
			return backup.Fail(backup.ExitUsage, backup.CodeConfig,
				fmt.Errorf("%w: %s is in this command's storage.connections; remove it from the configuration first", blob.ErrConnectionConfigured, id))
		}
	}
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
	rec, err := audit.New(audit.Options{DB: db, Replica: env.instance, Config: cfg.Audit, Log: env.log, AppLog: env.log})
	if err != nil {
		return backupExit(err)
	}
	rep, err := blob.ForgetConnection(ctx, db, id, cfg.System.HeartbeatStaleAfter, func(ctx context.Context, tx meta.Tx, conn string, rep blob.ForgetReport) error {
		details, err := json.Marshal(map[string]any{"connection": conn, "copies": rep.Copies, "queued": rep.Queued, "blobsMissing": rep.BlobsMissing})
		if err != nil {
			return err
		}
		return rec.RecordTx(ctx, tx, audit.Event{
			Type: audit.EventStorageConnectionForgotten, Outcome: audit.Success, Actor: audit.Actor{Kind: "system", Name: "admin"}, Details: details,
		})
	})
	switch {
	case errors.Is(err, blob.ErrConnectionConfigured), errors.Is(err, blob.ErrNoConnection):
		return backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
	case err != nil:
		return backupExit(err)
	}
	if *asJSON {
		return env.printJSON(rep)
	}
	_, err = fmt.Fprintf(s.stdout, "connection %s forgotten: %d copies and %d queued gaps removed, %d blobs without a present copy elsewhere; no object was deleted\n",
		id, rep.Copies, rep.Queued, rep.BlobsMissing)
	return err
}
