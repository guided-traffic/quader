package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/secrets"
)

// destroyFixture is the keys of connection archive (two generations and the name key) in the simulated database, retired without a
// holder, with the commands' dependencies and the destructions' announcements.
type destroyFixture struct {
	*keysFixture
	ring      *secrets.KeyRing
	destroyed [][]string
}

func newDestroyFixture(t *testing.T) *destroyFixture {
	t.Helper()
	ring := keysRing(t, "k1", map[string]byte{"k1": 1})
	f := &destroyFixture{keysFixture: newKeysFixture(ring, installation), ring: ring}
	f.populate(t, ring)
	f.deps.instance = "quader-web-0"
	f.deps.notifyDestroyed = func(_ meta.Tx, c string, ids []string) error {
		f.destroyed = append(f.destroyed, append([]string{c}, ids...))
		return nil
	}
	f.db.Held = keys.Holders{Recorded: true, DrainedAt: time.Date(2027, 3, 1, 9, 0, 0, 0, time.UTC), RetiredAt: time.Date(2027, 3, 2, 9, 0, 0, 0, time.UTC)}
	return f
}

// run is the command once its configuration is loaded: the output and the error it exits with.
func (f *destroyFixture) run(confirm string, change func(*destroyOptions)) (string, error) {
	o := destroyOptions{connection: "archive", confirm: confirm, staleAfter: 30 * time.Second, auditDays: 365}
	if change != nil {
		change(&o)
	}
	var out bytes.Buffer
	err := runKeysDestroy(context.Background(), f.deps, o, &out)
	return out.String(), err
}

func (f *destroyFixture) ids() []string {
	var out []string
	for _, r := range f.db.Rows() {
		out = append(out, r.KeyID)
	}
	return out
}

func TestR_STOR_099_TheDestroyCommand(t *testing.T) {
	t.Run("R-STOR-099 without --confirm the command prints the report and the confirmation, changes nothing and exits 0", func(t *testing.T) {
		f := newDestroyFixture(t)
		f.deps.now = func() time.Time { return time.Date(2027, 3, 20, 23, 30, 0, 0, time.UTC) }
		before, events := f.db.Rows(), len(f.ev.list)
		out, err := f.run("", nil)
		want := keys.Confirmation(installation, "archive", f.ids())
		for _, line := range []string{
			"connection archive: retired on 2027-03-02 (drained 2027-03-01)\n",
			"keys: sk-archive-1-", " (generation 1, retired), sk-archive-2-", " (generation 2, active), nk-archive-",
			"refused by: none\n",
			"still able to open what archive's provider keeps, with the key ring:\n",
			"  exports made before: none recorded\n",
			"  retained backups whose database holds these keys sealed: none, no backup recorded in this database\n",
			"  PostgreSQL: old row versions, the write-ahead log and any point-in-time archive until they turn over\n",
			"  files whose data key was handed to a Bastion: none recorded (the audit log goes back to 2026-03-20)\n",
			"dry run: nothing was destroyed. To destroy, run: quader admin storage keys destroy --connection archive --confirm " + want + "\n",
		} {
			if !strings.Contains(out, line) {
				t.Errorf("the report lacks %q:\n%s", line, out)
			}
		}
		if err != nil || len(f.ev.list) != events || len(f.destroyed) != 0 || len(f.db.Rows()) != len(before) || f.db.Rows()[0] != before[0] {
			t.Fatalf("exit %d: %v; a dry run changed something", exitCode(err), err)
		}
	})
	t.Run("R-STOR-099 Q-STOR-475 the dry run names the files whose data key was handed and the handovers, with the audit log's bound", func(t *testing.T) {
		f := newDestroyFixture(t)
		f.deps.now = func() time.Time { return time.Date(2027, 3, 20, 23, 30, 0, 0, time.UTC) }
		f.db.Held.HandedFiles, f.db.Held.Handovers = 2, 3
		for days, want := range map[int]string{
			365: "  files whose data key was handed to a Bastion: 2 (3 handovers; the audit log goes back to 2026-03-20)\n",
			0:   "  files whose data key was handed to a Bastion: 2 (3 handovers; the audit log keeps every event)\n",
		} {
			out, err := f.run("", func(o *destroyOptions) { o.auditDays = days })
			if err != nil || !strings.Contains(out, want) {
				t.Errorf("retention %d: %v, the report lacks %q:\n%s", days, err, want, out)
			}
		}
		out, err := f.run("", func(o *destroyOptions) { o.asJSON = true })
		if err != nil || !strings.Contains(out, `"handed": {`) || !strings.Contains(out, `"files": 2,`) || !strings.Contains(out, `"handovers": 3`) {
			t.Errorf("--json: %v\n%s", err, out)
		}
	})
	t.Run("R-STOR-099 a confirmation computed for another installation, or before a generation was added, exits 2", func(t *testing.T) {
		f := newDestroyFixture(t)
		other := keys.Confirmation("0b7c3f5e-2222-4000-8000-000000000002", "archive", f.ids())
		before := keys.Confirmation(installation, "archive", f.ids())
		if err := runKeysGeneration(context.Background(), f.deps, "archive", false, io.Discard); err != nil {
			t.Fatal(err)
		}
		events := len(f.ev.list)
		for _, token := range []string{other, before} {
			if _, err := f.run(token, nil); exitCode(err) != 2 || !strings.Contains(err.Error(), "--confirm "+token+" does not match the keys of connection archive now") {
				t.Errorf("%s: exit %d: %v", token, exitCode(err), err)
			}
		}
		if len(f.ev.list) != events || len(f.destroyed) != 0 {
			t.Fatal("a mismatch was audited or announced")
		}
	})
	t.Run("R-STOR-099 the right confirmation destroys every key in one transaction with one journaled event and one change; --json holds no sealed value", func(t *testing.T) {
		f := newDestroyFixture(t)
		rows := f.db.Rows()
		out, err := f.run(keys.Confirmation(installation, "archive", f.ids()), func(o *destroyOptions) { o.asJSON = true })
		if err != nil {
			t.Fatal(err)
		}
		var rep keys.Destruction
		if err := json.Unmarshal([]byte(out), &rep); err != nil || len(rep.Destroyed) != 3 || rep.DryRun || rep.State != keys.StateRetired {
			t.Fatalf("%v: %s", err, out)
		}
		for _, r := range f.db.Rows() {
			if r.State != keys.StateDestroyed || r.Sealed != "" {
				t.Errorf("row %+v", r)
			}
		}
		last := f.ev.list[len(f.ev.list)-1]
		var entry keys.DestroyedEntry
		if last.Type != audit.EventStorageKeysDestroyed || last.Actor.Name != "admin" || json.Unmarshal(last.Journal, &entry) != nil ||
			entry.Connection != "archive" || len(entry.KeyIDs) != 3 || entry.DestroyedAt.IsZero() {
			t.Fatalf("event %+v, entry %s", last, last.Journal)
		}
		if !strings.Contains(string(last.Details), `"keyIds":[`) || !strings.Contains(string(last.Details), `"backupListing":null`) {
			t.Errorf("details %s", last.Details)
		}
		if len(f.destroyed) != 1 || f.destroyed[0][0] != "archive" || len(f.destroyed[0]) != 4 {
			t.Errorf("changes %v", f.destroyed)
		}
		if showsKey(t, f.ring, rows, out+string(last.Details)+string(last.Journal)) {
			t.Fatal("the output or the event shows a key or a sealed value")
		}
		text, err := f.run("", nil)
		if err != nil || !strings.HasPrefix(text, "connection archive: its storage keys were destroyed on ") || !strings.HasSuffix(text, "; nothing to do\n") {
			t.Fatalf("a second run: %v %q", err, text)
		}
	})
	t.Run("R-STOR-099 Q-STOR-405 a holder's refusal exits 5 and prints the report with the refusal, in a dry run as well", func(t *testing.T) {
		f := newDestroyFixture(t)
		f.db.Held.Copies = 1
		out, err := f.run("", nil)
		if exitCode(err) != 5 || !strings.Contains(out, "refused by:\n  copies: 1 copy rows lie on connection archive") ||
			!strings.Contains(out, "refused: nothing was destroyed") || strings.Contains(out, "--confirm") {
			t.Fatalf("exit %d: %v\n%s", exitCode(err), err, out)
		}
	})
	t.Run("R-STOR-099 a connection of the command's own configuration, and one without keys, exit 2", func(t *testing.T) {
		f := newDestroyFixture(t)
		if _, err := f.run("", func(o *destroyOptions) { o.configured = []string{"archive"} }); exitCode(err) != 2 {
			t.Errorf("configured: exit %d: %v", exitCode(err), err)
		}
		if _, err := f.run("", func(o *destroyOptions) { o.connection = "nothing" }); exitCode(err) != 2 {
			t.Errorf("no keys: exit %d: %v", exitCode(err), err)
		}
		s := streams{stdin: strings.NewReader(""), stdout: io.Discard, stderr: io.Discard}
		var u usageError
		if err := adminCmd([]string{"storage", "keys", "destroy"}, s); !errors.As(err, &u) || !strings.Contains(u.Error(), "quader admin storage keys destroy --connection <id>") {
			t.Errorf("without --connection: %v", err)
		}
	})
}

func TestR_STOR_103_ExportAndImportKnowDestroyedKeys(t *testing.T) {
	f := newDestroyFixture(t)
	var older bytes.Buffer
	if err := runKeysExport(context.Background(), f.deps, "", &older); err != nil {
		t.Fatal(err)
	}
	h, err := keys.New(keys.Options{DB: f.db, Rows: f.db, Ring: f.ring})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.Ensure(context.Background(), keys.Connection{ID: "keep", Prefix: "keep", Objects: blobtest.New()}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.run(keys.Confirmation(installation, "archive", f.archiveIDs()), nil); err != nil {
		t.Fatal(err)
	}
	t.Run("R-STOR-103 an export after a destruction lists the destroyed rows without sealed, and every export is a row of storage_key_exports", func(t *testing.T) {
		var out bytes.Buffer
		if err := runKeysExport(context.Background(), f.deps, "", &out); err != nil {
			t.Fatal(err)
		}
		var doc keys.Document
		if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		destroyed := 0
		for _, k := range doc.Keys {
			if k.Connection == "archive" {
				destroyed++
				if k.State != keys.StateDestroyed || k.Sealed != "" || k.DestroyedAt == nil || k.RetiredAt == nil {
					t.Errorf("entry %+v", k)
				}
			}
		}
		if destroyed != 3 || strings.Count(out.String(), `"sealed"`) != 1 || len(doc.RootKeyIDs) != 1 {
			t.Fatalf("export %s", out.String())
		}
		exports := f.db.Exports()
		if len(exports) != 2 || exports[1].Instance != "quader-web-0" || len(exports[1].KeyIDs) != 4 {
			t.Fatalf("storage_key_exports %+v", exports)
		}
		last := f.ev.list[len(f.ev.list)-1]
		if last.Type != audit.EventStorageKeysExported || !strings.Contains(string(last.Details), `"keyIds":["nk-archive-`) {
			t.Errorf("event %s", last.Details)
		}
		if _, err := keys.ReadDocument(bytes.NewReader(out.Bytes())); err != nil {
			t.Errorf("the import does not read the export of destroyed rows: %v", err)
		}
	})
	t.Run("R-STOR-103 an import of an older export inserts nothing for the destroyed connection, reports its identifiers, and imports the other connections' missing rows", func(t *testing.T) {
		dst := newKeysFixture(f.ring, installation)
		for _, r := range f.db.Rows() {
			if r.Connection == "archive" {
				dst.db.Put(r) // the destroyed rows; the database lacks the other connection
			}
		}
		var newer bytes.Buffer
		if err := runKeysExport(context.Background(), f.deps, "keep", &newer); err != nil {
			t.Fatal(err)
		}
		merged := mergeExports(t, older.Bytes(), newer.Bytes())
		msg, err := dst.importFile(merged, false)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(msg, "1 added") || !strings.Contains(msg, "skipped as destroyed (their connection's keys were destroyed; nothing brings them back): ") {
			t.Fatalf("printed %q", msg)
		}
		for _, r := range dst.db.Rows() {
			if r.Connection == "archive" && (r.State != keys.StateDestroyed || r.Sealed != "") {
				t.Errorf("the import brought %s back: %+v", r.KeyID, r)
			}
		}
		if ev := dst.ev.list[len(dst.ev.list)-1]; !strings.Contains(string(ev.Details), `"destroyed":["nk-archive-`) {
			t.Errorf("event %s", ev.Details)
		}
	})
}

func (f *destroyFixture) archiveIDs() []string {
	var out []string
	for _, r := range f.db.Rows() {
		if r.Connection == "archive" {
			out = append(out, r.KeyID)
		}
	}
	return out
}

// mergeExports joins the keys of two exports of one installation into one document.
func mergeExports(t *testing.T, a, b []byte) []byte {
	t.Helper()
	var x, y keys.Document
	if err := errors.Join(json.Unmarshal(a, &x), json.Unmarshal(b, &y)); err != nil {
		t.Fatal(err)
	}
	x.Keys = append(x.Keys, y.Keys...)
	out, err := json.Marshal(x)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
