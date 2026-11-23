package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	storagekeys "github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/blob/s3"
	"github.com/guided-traffic/quader/internal/blob/verify"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/outbound"
	"github.com/guided-traffic/quader/internal/secrets"
)

const storageUsage = `usage: quader admin storage verify [--connection <id>] [--since <RFC 3339 time>] [--concurrency N] [--max-items N] [--json] [--config <file>]
       quader admin storage restore-versions --before <RFC 3339 time> [--connection <id>] [--credentials-file <path>] [--dry-run] [--concurrency N] [--json] [--config <file>]
       quader admin storage check [--connection <id>] [--json] [--config <file>]
       quader admin storage forget-connection <id> [--json] [--config <file>]
       quader admin storage keys export|import|new-generation|destroy ... (quader admin storage keys for its usage)
       quader admin storage offline list|get|verify|pull ... (quader admin storage offline for its usage)

verify             ask the store of each connection, or of the one named, for the object of every present copy of a live blob; list the missing copies and the items of each blob none of whose copies is there (R-STOR-058, R-HA-024, R-HA-025)
restore-versions   remove the delete markers younger than t from the keys of the copies that have no object, on each connection or the one named, so that a point-in-time restore of the database finds its objects again (R-HA-025 step 5)
check              run the conformance check and the capabilities on every object-store connection, or the one named (R-STOR-035); exit 5 when a check that is not optional failed
forget-connection  give up a connection that is gone for good: its copy and queue rows go, its id is retired, no object is deleted (R-STOR-051); exit 2 while a live replica has it
keys               export, import and draw a new generation of the storage keys of the encrypted connections, and destroy the keys of a retired connection (R-STOR-083, R-STOR-085, R-STOR-099)
offline            list, read and verify the objects of an encrypted connection's bucket with the export of the storage keys and the key ring, without a database (R-STOR-089)

exit codes: 0 no blob is lost (missing copies are listed; the reconciliation and the scrub repair them), 1 failed, 2 usage or configuration, 5 a blob is lost, or with --connection a copy on that connection is missing or of another size, 75 temporary (the database or a store could not be reached or answered)`

// storageCmd dispatches quader admin storage <command>.
func storageCmd(args []string, s streams) error {
	if len(args) >= 1 {
		switch args[0] {
		case "verify":
			return storageVerifyCmd(args[1:], s)
		case "restore-versions":
			return storageRestoreVersionsCmd(args[1:], s)
		case "check":
			return storageCheckCmd(args[1:], s)
		case "forget-connection":
			return storageForgetCmd(args[1:], s)
		case "keys":
			return storageKeysCmd(args[1:], s)
		case "offline":
			return storageOfflineCmd(args[1:], s)
		}
	}
	return usageError{storageUsage}
}

// connectionClient is the client of one connection of the configuration, as an operator command reaches it.
type connectionClient struct {
	id     string
	client *s3.Client
}

// connectionClients builds a client for every connection of the configuration, in list order, with its files read: with each
// connection's runtime credential, or with its credential in creds when creds names one (the credentials file of
// restore-versions, one element per connection, Q-STOR-268). A connection whose client cannot be built is a configuration error
// naming it.
func connectionClients(ctx context.Context, st config.Storage, out *outbound.Factory, creds map[string]config.StorageS3Credentials) ([]connectionClient, error) {
	resolved, err := config.ResolveConnections(st.Connections, nil)
	if err != nil {
		return nil, backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
	}
	list := make([]connectionClient, 0, len(resolved))
	for i, el := range resolved {
		cfg := config.ClientConfig(i, el, st.ObjectStore)
		if c, ok := creds[el.ID]; ok {
			cfg.Credentials = c
		}
		c, err := s3.NewFromFactory(ctx, cfg, out)
		if err != nil {
			// the error names the connection and its key: storage.connections[i] (id): ca_file
			return nil, backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
		}
		list = append(list, connectionClient{id: el.ID, client: c})
	}
	return list, nil
}

// verifyConnections is the connections of a verification.
func verifyConnections(clients []connectionClient) []verify.Conn {
	out := make([]verify.Conn, 0, len(clients))
	for _, c := range clients {
		out = append(out, verify.Conn{ID: c.id, Objects: c.client})
	}
	return out
}

// recordKeys gives every encrypted connection of a verification the lookup of the storage keys its records name (R-STOR-087): a
// holder over the database and the configured key ring, which opens the connections' rows of storage_keys and destroys the keys when
// the returned function is called. Without an encrypted connection it loads no key ring.
func recordKeys(ctx context.Context, cfg *config.Config, db *postgres.DB, conns []verify.Conn, log *slog.Logger) (func(), error) {
	resolved, err := config.ResolveConnections(cfg.Storage.Connections, nil)
	if err != nil {
		return nil, backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
	}
	encrypted := map[string]bool{}
	for _, el := range resolved {
		encrypted[el.ID] = el.Encrypted
	}
	need := false
	for _, c := range conns {
		need = need || encrypted[c.ID]
	}
	if !need {
		return func() {}, nil
	}
	ring, err := secrets.Load(cfg.Secrets)
	if err != nil {
		return nil, backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
	}
	h, err := storagekeys.New(storagekeys.Options{DB: db, Ring: ring, Log: log})
	if err != nil {
		return nil, err
	}
	for i := range conns {
		id := conns[i].ID
		if !encrypted[id] {
			continue
		}
		if err := h.Load(ctx, id); err != nil {
			h.Close()
			if storagekeys.IsRefusal(err) {
				return nil, backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
			}
			return nil, err
		}
		conns[i].Keys = func(ctx context.Context) blobcrypt.KeyLookup { return h.Lookup(ctx, id) }
	}
	return h.Close, nil
}

// storageVerifyCmd is quader admin storage verify (R-STOR-058, R-HA-024, R-HA-025 step 4): it asks the store of each
// connection, or of the one named, for the object of every present copy of a live blob, prints one line per missing copy and
// one per item that references a blob none of whose copies is there. It runs with the runtime database role and each
// connection's runtime credential and changes nothing: it needs no write permission, and it leaves the marks of the scrub and
// the reads (R-STOR-027, R-STOR-055) alone.
func storageVerifyCmd(args []string, s streams) error {
	c, asJSON := newBackupFlags("admin storage verify", s)
	since := c.fs.String("since", "", "only the blobs created or changed at or after this time (RFC 3339); every blob without it")
	conn := c.fs.String("connection", "", "check only the copies on this connection; every connection without it")
	conc := c.fs.Int("concurrency", verify.DefaultConcurrency, "simultaneous existence checks")
	maxItems := c.fs.Int("max-items", verify.DefaultMaxItems, "items listed per lost blob")
	_, flags, names, err := parseBackup(c, args, 0)
	if err != nil {
		return backupExit(err)
	}
	var cut time.Time
	if *since != "" {
		cut, err = time.Parse(time.RFC3339, *since)
		if err != nil {
			return usageError{storageUsage}
		}
	}
	if *conc < 1 || *maxItems < 1 {
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
	clients, err := connectionClients(ctx, cfg.Storage, outbound.New(cfg.Proxy.Outbound), nil)
	if err != nil {
		return backupExit(err)
	}
	conns := verifyConnections(clients)
	closeKeys, err := recordKeys(ctx, cfg, db, conns, env.log)
	if err != nil {
		return backupExit(err)
	}
	defer closeKeys()
	rep, err := verify.Run(ctx, verify.Options{
		DB: verify.PGSource{DB: db}, Connections: conns, Connection: *conn, Since: cut,
		Concurrency: *conc, MaxItems: *maxItems, Log: env.log,
	})
	if errors.Is(err, verify.ErrUnknownConnection) {
		return backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
	}
	if rep != nil {
		if env.json {
			if jerr := env.printJSON(rep); jerr != nil {
				return jerr
			}
		} else if werr := writeVerifyReport(s.stdout, rep); werr != nil {
			return werr
		}
	}
	return verifyExit(rep, err, *conn)
}

// verifyExit is the exit status of a verification (R-STOR-058, backup-and-restore.md R-BAK-013): 5 for a lost blob or, with
// --connection, a finding on that connection; 2 when a store refused the checks of a connection; 75 when a check was not
// answered; 0 otherwise.
func verifyExit(rep *verify.Report, err error, conn string) error {
	switch {
	case rep != nil && rep.Lost > 0:
		return backup.Fail(backup.ExitFindings, backup.CodeFindings,
			fmt.Errorf("%d blobs have no copy with their object (%d of them a copy of another size); %d items are affected", rep.Lost, rep.SizeMismatch, rep.ItemsAffected))
	case rep != nil && conn != "" && rep.ConnectionFindings(conn) > 0:
		return backup.Fail(backup.ExitFindings, backup.CodeFindings,
			fmt.Errorf("%d copies on connection %s are missing or of another size", rep.ConnectionFindings(conn), conn))
	case err != nil && blob.IsConfig(err):
		return backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
	case err != nil:
		return backupExit(err)
	case len(rep.Unchecked) > 0:
		return backup.Fail(backup.ExitTemporary, backup.CodeTemporary, fmt.Errorf("%d copies could not be checked: the object store did not answer", len(rep.Unchecked)))
	}
	return nil
}

// writeVerifyReport prints the report as text: one line per missing copy, one per item of a lost blob, a line per lost blob
// that no item references, one per copy that could not be checked, the totals per connection, and the totals last (the line
// the end-to-end tests read).
func writeVerifyReport(w io.Writer, rep *verify.Report) error {
	for _, cf := range rep.Copies {
		if _, err := fmt.Fprintf(w, "copy %s %s: %s\n", cf.Connection, cf.SHA256, copyDetail(cf)); err != nil {
			return err
		}
	}
	for _, f := range rep.Findings {
		if err := writeLost(w, f); err != nil {
			return err
		}
	}
	for _, u := range rep.Unchecked {
		if _, err := fmt.Fprintf(w, "copy %s %s: not checked: %s\n", u.Connection, u.SHA256, u.Error); err != nil {
			return err
		}
	}
	for _, c := range rep.Connections {
		ended := ""
		if c.Error != "" {
			ended = ", ended: " + c.Error
		}
		if c.Record > 0 {
			ended = fmt.Sprintf(", %d with a record that does not open (R-STOR-087)", c.Record) + ended
		}
		if _, err := fmt.Fprintf(w, "connection %s: %d copies checked, %d missing, %d of another size, %d not checked%s\n",
			c.Connection, c.Checked, c.Missing, c.SizeMismatch, c.Unchecked, ended); err != nil {
			return err
		}
	}
	since := ""
	if !rep.Since.IsZero() {
		since = " since " + rep.Since.UTC().Format(time.RFC3339)
	}
	_, err := fmt.Fprintf(w, "storage verify%s: %d blobs checked, %d without object, %d of another size, %d not checked, %d items affected\n",
		since, rep.Checked, rep.Missing, rep.SizeMismatch, len(rep.Unchecked), rep.ItemsAffected)
	return err
}

func copyDetail(cf verify.CopyFinding) string {
	switch cf.Kind {
	case verify.KindSizeMismatch:
		return fmt.Sprintf("object has %d bytes, the row says %d", cf.ObjectSize, cf.StoredSize)
	case verify.KindRecord:
		return "the record of the encrypted object does not open: " + cf.Reason
	}
	return "object missing"
}

// writeLost prints the lines of one lost blob: one per item, the number of the rest, or one line when no item references it.
func writeLost(w io.Writer, f verify.Finding) error {
	detail := "no copy has its object"
	switch f.Kind {
	case verify.KindSizeMismatch:
		detail = "no copy has its object of its size"
	case verify.KindRecord:
		detail = "no copy has its object with a record that opens"
	}
	if len(f.Items) == 0 {
		_, err := fmt.Fprintf(w, "blob %s: %s (no item references it)\n", f.SHA256, detail)
		return err
	}
	for _, it := range f.Items {
		trashed := ""
		if it.Trashed {
			trashed = " (trashed)"
		}
		if _, err := fmt.Fprintf(w, "item %s/%s:%s%s: %s, blob %s\n", it.Project, it.Repository, it.Path, trashed, detail, f.SHA256); err != nil {
			return err
		}
	}
	if more := f.ItemCount - int64(len(f.Items)); more > 0 {
		if _, err := fmt.Fprintf(w, "blob %s: and %d more items\n", f.SHA256, more); err != nil {
			return err
		}
	}
	return nil
}
