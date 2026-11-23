package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta"
)

// quader admin storage keys destroy (storage.md R-STOR-097 to R-STOR-103, Q-STOR-404, Q-STOR-405, Q-STOR-410): the one command of the
// product that destroys something on purpose. A dry run by default; --confirm with the value the dry run printed destroys every key of
// a retired connection in one transaction with its change and its journaled audit event. It runs with the runtime database role and
// without the key ring, because it opens nothing.

// destroyOptions are what the command takes from its flags and its configuration.
type destroyOptions struct {
	connection string
	confirm    string
	dryRun     bool
	asJSON     bool
	// configured are the ids of the command's own storage.connections.
	configured []string
	staleAfter time.Duration
	backup     keys.BackupTarget
	// auditDays is audit.retention_days, which bounds what the audit trail says of the files whose data key was handed to a Bastion
	// (0 keeps every event).
	auditDays int
}

// storageKeysDestroyCmd is quader admin storage keys destroy --connection <id> [--confirm <token>] [--dry-run] [--json].
func storageKeysDestroyCmd(args []string, s streams) error {
	c, asJSON := newBackupFlags("admin storage keys destroy", s)
	conn := c.fs.String("connection", "", "the retired connection whose storage keys and name key are destroyed")
	confirm := c.fs.String("confirm", "", "the confirmation the dry run printed; without it the command checks, reports and changes nothing")
	dry := c.fs.Bool("dry-run", false, "with --confirm: compare the confirmation and run every check, and destroy nothing")
	pos, flags, names, err := parseBackup(c, args, 0)
	if err != nil || len(pos) != 0 || *conn == "" {
		return keysUsage(err)
	}
	ke, ctx, stop, err := openKeysEnv(c, s, flags, names, false)
	if err != nil {
		return backupExit(err)
	}
	defer stop()
	defer ke.close()
	cfg := ke.env.cfg
	o := destroyOptions{
		connection: *conn, confirm: *confirm, dryRun: *dry, asJSON: *asJSON, configured: storageConnectionIDs(cfg),
		staleAfter: cfg.System.HeartbeatStaleAfter, backup: keys.BackupTarget{Bucket: cfg.Backup.Target.Bucket, Prefix: cfg.Backup.Target.Prefix},
		auditDays: cfg.Audit.RetentionDays,
	}
	return backupExit(runKeysDestroy(ctx, ke.deps, o, s.stdout))
}

// storageConnectionIDs are the ids of a configuration's storage.connections, in list order.
func storageConnectionIDs(cfg *config.Config) []string {
	out := make([]string, 0, len(cfg.Storage.Connections))
	for _, el := range cfg.Storage.Connections {
		out = append(out, el.WithDefaults().ID)
	}
	return out
}

// runKeysDestroy runs the destruction and prints its report; the exit codes are R-STOR-099's: 2 for a wrong call, 5 while a holder
// refuses (in a dry run as well), 75 when the database could not be reached.
func runKeysDestroy(ctx context.Context, d keysDeps, o destroyOptions, out io.Writer) error {
	rep, err := keys.Destroy(ctx, d.db, d.rows, keys.DestroyOptions{
		Connection: o.connection, Installation: d.installation, Confirm: o.confirm, DryRun: o.dryRun, Configured: o.configured,
		StaleAfter: o.staleAfter, Backup: o.backup, Now: d.now,
	}, func(ctx context.Context, tx meta.Tx, rep keys.Destruction) error {
		if err := d.notifyDestroyed(tx, rep.Connection, rep.Destroyed); err != nil {
			return err
		}
		return destroyedEvent(ctx, d.rec, tx, rep)
	})
	switch {
	case errors.Is(err, keys.ErrNoKeys), errors.Is(err, keys.ErrConfigured), errors.Is(err, keys.ErrConfirmMismatch):
		return backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
	case errors.Is(err, keys.ErrHeld):
		if perr := printDestruction(out, rep, o, d.now()); perr != nil {
			return perr
		}
		return backup.Fail(backup.ExitFindings, backup.CodePrecondition, fmt.Errorf("%w (connection %s): the report names each holder", err, o.connection))
	case err != nil:
		return err
	}
	return printDestruction(out, rep, o, d.now())
}

// destroyedEvent writes system.storage.keys_destroyed with its journal entry, in the destroying transaction (R-STOR-099 step 6).
func destroyedEvent(ctx context.Context, rec audit.Recorder, tx meta.Tx, rep keys.Destruction) error {
	type listing struct {
		RunID    string     `json:"runId"`
		ListedAt *time.Time `json:"listedAt"`
		Target   string     `json:"target"`
	}
	var l *listing
	if b := rep.Remaining.Backups; b.RunID != "" {
		l = &listing{RunID: b.RunID, ListedAt: b.ListedAt, Target: b.Target}
	}
	details, err := json.Marshal(map[string]any{
		"connection": rep.Connection, "keyIds": rep.Destroyed, "exports": rep.Remaining.Exports, "backupListing": l,
		"retainedHoldingSealed": rep.Remaining.Backups.Count,
	})
	if err != nil {
		return err
	}
	entry, err := json.Marshal(keys.DestroyedEntry{Connection: rep.Connection, KeyIDs: rep.Destroyed, DestroyedAt: *rep.DestroyedAt})
	if err != nil {
		return err
	}
	return rec.RecordTx(ctx, tx, audit.Event{
		Type: audit.EventStorageKeysDestroyed, Outcome: audit.Success, Actor: audit.Actor{Kind: "system", Name: "admin"},
		Details: details, Journal: entry,
	})
}

// printDestruction prints the report as JSON or as the text of R-STOR-099; now dates the audit trail's bound.
func printDestruction(out io.Writer, rep keys.Destruction, o destroyOptions, now time.Time) error {
	if o.asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	_, err := io.WriteString(out, destructionText(rep, o, now))
	return err
}

// destructionText is the report in the operator's words (R-STOR-099).
func destructionText(rep keys.Destruction, o destroyOptions, now time.Time) string {
	var b strings.Builder
	c := rep.Connection
	if rep.State == keys.StateDestroyed {
		fmt.Fprintf(&b, "connection %s: its storage keys were destroyed on %s; nothing to do\n", c, day(rep.DestroyedAt))
		return b.String()
	}
	b.WriteString(stateLine(rep))
	b.WriteString("keys: " + keyList(rep.Keys) + "\n")
	if len(rep.Refusals) == 0 {
		b.WriteString("refused by: none\n")
	} else {
		b.WriteString("refused by:\n")
		for _, r := range rep.Refusals {
			fmt.Fprintf(&b, "  %s: %s\n", r.Holder, r.Say)
		}
	}
	fmt.Fprintf(&b, "still able to open what %s's provider keeps, with the key ring:\n", c)
	b.WriteString("  " + exportsLine(rep.Remaining.Exports) + "\n")
	b.WriteString("  " + backupsLine(rep.Remaining.Backups) + "\n")
	b.WriteString("  " + rep.Remaining.Database + "\n")
	b.WriteString("  " + handedLine(rep.Remaining.Handed, o.auditDays, now) + "\n")
	switch {
	case len(rep.Refusals) > 0:
		b.WriteString("refused: nothing was destroyed; run the command again once the holders above are gone\n")
	case len(rep.Destroyed) > 0:
		fmt.Fprintf(&b, "destroyed %d keys of connection %s; what %s's bucket still holds stays there as ciphertext; export the storage keys again: quader admin storage keys export\n",
			len(rep.Destroyed), c, c)
	case o.confirm != "":
		fmt.Fprintf(&b, "dry run: the confirmation matches and every check passes; nothing was destroyed. To destroy, run: quader admin storage keys destroy --connection %s --confirm %s\n", c, rep.Confirm)
	default:
		fmt.Fprintf(&b, "dry run: nothing was destroyed. To destroy, run: quader admin storage keys destroy --connection %s --confirm %s\n", c, rep.Confirm)
	}
	return b.String()
}

// stateLine is the first line: the connection and its state.
func stateLine(rep keys.Destruction) string {
	c := rep.Connection
	switch rep.State {
	case keys.StateRetired:
		drained := ""
		if rep.DrainedAt != nil {
			drained = " (drained " + day(rep.DrainedAt) + ")"
		}
		return fmt.Sprintf("connection %s: retired on %s%s\n", c, day(rep.RetiredAt), drained)
	case "unrecorded":
		return fmt.Sprintf("connection %s: unrecorded (storage_connections has no row of it, as after a restore of a backup)\n", c)
	}
	return fmt.Sprintf("connection %s: %s\n", c, rep.State)
}

// keyList names the keys as "sk-… (generation 1, retired), nk-… (name key)".
func keyList(list []keys.DestroyKey) string {
	parts := make([]string, 0, len(list))
	for _, k := range list {
		switch {
		case k.Kind == keys.KindName && k.State == keys.StateDestroyed:
			parts = append(parts, k.KeyID+" (name key, destroyed)")
		case k.Kind == keys.KindName:
			parts = append(parts, k.KeyID+" (name key)")
		default:
			parts = append(parts, fmt.Sprintf("%s (generation %d, %s)", k.KeyID, k.Generation, k.State))
		}
	}
	return strings.Join(parts, ", ")
}

func exportsLine(e keys.ExportsHeld) string {
	if e.Count == 0 {
		return "exports made before: none recorded"
	}
	return fmt.Sprintf("exports made before: %d, the last on %s; destroy those files", e.Count, e.LastAt.UTC().Format(time.RFC3339))
}

func backupsLine(b keys.BackupsHeld) string {
	const lead = "retained backups whose database holds these keys sealed: "
	switch {
	case !b.Recorded:
		return lead + "none, no backup recorded in this database"
	case b.RunID == "":
		return lead + "not known, no retention listing recorded"
	case b.Count == nil:
		return fmt.Sprintf("%snot known on %s (the listing of run %s at %s records no sealedKeyIds)", lead, b.Target, b.RunID, b.ListedAt.UTC().Format(time.RFC3339))
	}
	s := fmt.Sprintf("%s%d on %s (listed by run %s at %s)", lead, *b.Count, b.Target, b.RunID, b.ListedAt.UTC().Format(time.RFC3339))
	if b.LeaveNotBefore != nil {
		s += "; they leave retention not before " + day(b.LeaveNotBefore)
	}
	return s
}

// handedLine names the files whose data key was handed to a Bastion and the handovers, with the bound of the audit trail they are
// counted from (Q-STOR-475): the day audit.retention_days before now, or every event when the retention is 0.
func handedLine(h keys.HandedHeld, auditDays int, now time.Time) string {
	const lead = "files whose data key was handed to a Bastion: "
	bound := "the audit log keeps every event"
	if auditDays > 0 {
		bound = "the audit log goes back to " + now.UTC().AddDate(0, 0, -auditDays).Format(time.DateOnly)
	}
	if h.Handovers == 0 {
		return lead + "none recorded (" + bound + ")"
	}
	handovers := "handovers"
	if h.Handovers == 1 {
		handovers = "handover"
	}
	return fmt.Sprintf("%s%d (%d %s; %s)", lead, h.Files, h.Handovers, handovers, bound)
}

func day(t *time.Time) string {
	if t == nil {
		return "an unknown day"
	}
	return t.UTC().Format(time.DateOnly)
}
