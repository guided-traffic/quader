package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/blob/keys/keystest"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/secrets"
)

const installation = "0b7c3f5e-1111-4000-8000-000000000001"

// events records the transactional audit events of a command.
type events struct{ list []audit.Event }

func (e *events) RecordTx(_ context.Context, _ meta.Tx, ev audit.Event) error {
	e.list = append(e.list, ev)
	return nil
}
func (e *events) Record(context.Context, audit.Event) {}

// keysRing loads a key ring of the root keys ids, each 32 bytes of its fill byte.
func keysRing(t *testing.T, active string, ids map[string]byte) *secrets.KeyRing {
	t.Helper()
	dir := t.TempDir()
	var files []string
	for id, fill := range ids {
		p := filepath.Join(dir, id+".key")
		if err := os.WriteFile(p, []byte(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32))), 0o600); err != nil {
			t.Fatal(err)
		}
		files = append(files, p)
	}
	r, err := secrets.Load(config.Secrets{EncryptionKeyFiles: files, ActiveKeyID: active})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// keysFixture is a database of the simulation, with the commands' dependencies over it.
type keysFixture struct {
	db       *keystest.DB
	ev       *events
	notified []string
	deps     keysDeps
}

func newKeysFixture(ring *secrets.KeyRing, inst string) *keysFixture {
	f := &keysFixture{db: keystest.New(), ev: &events{}}
	f.deps = keysDeps{
		db: f.db, rows: f.db, installation: inst, ring: ring, rec: f.ev, now: time.Now,
		notify: func(_ meta.Tx, c string) error { f.notified = append(f.notified, c); return nil },
	}
	return f
}

// populate gives the database of f the keys of an encrypted connection archive with keyed names and a second generation.
func (f *keysFixture) populate(t *testing.T, ring *secrets.KeyRing) {
	t.Helper()
	h, err := keys.New(keys.Options{DB: f.db, Rows: f.db, Ring: ring})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.Ensure(context.Background(), keys.Connection{ID: "archive", KeyedNames: true, Prefix: "quader", Objects: blobtest.New()}); err != nil {
		t.Fatal(err)
	}
	if err := runKeysGeneration(context.Background(), f.deps, "archive", false, io.Discard); err != nil {
		t.Fatal(err)
	}
}

// importFile is what quader admin storage keys import does with a file once its configuration is loaded.
func (f *keysFixture) importFile(data []byte, other bool) (string, error) {
	doc, err := keys.ReadDocument(bytes.NewReader(data))
	if err != nil {
		return "", importExit(err)
	}
	var out bytes.Buffer
	err = runKeysImport(context.Background(), f.deps, doc, other, false, &out)
	return out.String(), err
}

// exitCode is the code an error exits with: 1 for an error without one.
func exitCode(err error) int {
	var ee *backup.ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	return 1
}

// showsKey reports whether text holds a byte of a key of rows, opened with ring, or a sealed value.
func showsKey(t *testing.T, ring *secrets.KeyRing, rows []keys.Row, text string) bool {
	t.Helper()
	for _, r := range rows {
		if strings.Contains(text, r.Sealed) || strings.Contains(text, strings.SplitN(r.Sealed, ":", 3)[2]) {
			return true
		}
		raw, err := ring.Open(keys.Column(), secrets.Row(r.KeyID), r.Sealed)
		if err != nil {
			continue
		}
		for _, form := range []string{hex.EncodeToString(raw), base64.StdEncoding.EncodeToString(raw), base64.RawURLEncoding.EncodeToString(raw)} {
			if strings.Contains(text, form) {
				return true
			}
		}
	}
	return false
}

func TestR_STOR_085_TheExportAndTheImportRoundTrip(t *testing.T) {
	ring := keysRing(t, "k1", map[string]byte{"k1": 1})
	src := newKeysFixture(ring, installation)
	src.populate(t, ring)
	var out bytes.Buffer
	if err := runKeysExport(context.Background(), src.deps, "", &out); err != nil {
		t.Fatal(err)
	}
	exported := out.Bytes()
	t.Run("R-STOR-085 export prints the rows sealed as they lie, names the installation and the root keys, and is audited", func(t *testing.T) {
		var doc keys.Document
		if err := json.Unmarshal(exported, &doc); err != nil {
			t.Fatal(err)
		}
		rows := src.db.Rows()
		if doc.Format != keys.DocumentFormat || doc.InstallationID != installation || len(doc.RootKeyIDs) != 1 || doc.RootKeyIDs[0] != "k1" || len(doc.Keys) != len(rows) || len(rows) != 3 {
			t.Fatalf("document %s over %d rows", exported, len(rows))
		}
		for i, k := range doc.Keys {
			if k.KeyID != rows[i].KeyID || k.Sealed != rows[i].Sealed || k.State != rows[i].State || k.Generation != rows[i].Generation {
				t.Errorf("key %d: %+v, row %+v", i, k, rows[i])
			}
		}
		n := len(src.ev.list)
		if n == 0 || src.ev.list[n-1].Type != audit.EventStorageKeysExported || !strings.Contains(string(src.ev.list[n-1].Details), `"keys":3`) {
			t.Errorf("events %+v", src.ev.list)
		}
		for _, r := range rows {
			raw, _ := ring.Open(keys.Column(), secrets.Row(r.KeyID), r.Sealed)
			if bytes.Contains(exported, []byte(hex.EncodeToString(raw))) || bytes.Contains(exported, []byte(base64.StdEncoding.EncodeToString(raw))) {
				t.Fatal("the export holds an opened key")
			}
		}
	})
	t.Run("R-STOR-085 import of the export into an empty table restores the rows, and a holder opens the same keys", func(t *testing.T) {
		dst := newKeysFixture(ring, installation)
		msg, err := dst.importFile(exported, false)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(msg, "3 added, 0 already present") || !strings.Contains(msg, "quader admin storage keys export") {
			t.Errorf("printed %q", msg)
		}
		a, b := src.db.Rows(), dst.db.Rows()
		if len(a) != len(b) {
			t.Fatalf("%d rows imported, want %d", len(b), len(a))
		}
		for i := range a {
			if a[i].KeyID != b[i].KeyID || a[i].Sealed != b[i].Sealed || a[i].State != b[i].State {
				t.Errorf("row %d: %+v, want %+v", i, b[i], a[i])
			}
		}
		if len(dst.notified) != 1 || dst.notified[0] != "archive" || dst.ev.list[0].Type != audit.EventStorageKeysImported {
			t.Errorf("notified %v, events %+v", dst.notified, dst.ev.list)
		}
		ha, _ := keys.New(keys.Options{DB: src.db, Rows: src.db, Ring: ring})
		hb, _ := keys.New(keys.Options{DB: dst.db, Rows: dst.db, Ring: ring})
		defer ha.Close()
		defer hb.Close()
		if err := errors.Join(ha.Load(context.Background(), "archive"), hb.Load(context.Background(), "archive")); err != nil {
			t.Fatal(err)
		}
		ka, err1 := ha.Active(context.Background(), "archive")
		kb, err2 := hb.Active(context.Background(), "archive")
		if err1 != nil || err2 != nil || ka.ID != kb.ID || !ka.Key.Equal(kb.Key) {
			t.Fatalf("active keys %v %v (%v, %v)", ka, kb, err1, err2)
		}
		again, err := dst.importFile(exported, false)
		if err != nil || !strings.Contains(again, "0 added, 3 already present and equal") {
			t.Fatalf("a second import: %q %v", again, err)
		}
	})
	t.Run("R-STOR-085 a database restored to before generation 2 gets it back as its active key", func(t *testing.T) {
		dst := newKeysFixture(ring, installation)
		for _, r := range src.db.Rows() {
			if r.Generation == 2 {
				continue
			}
			if r.Kind == keys.KindStorage {
				r.State, r.RetiredAt = keys.StateActive, time.Time{}
			}
			dst.db.Put(r)
		}
		if _, err := dst.importFile(exported, false); err != nil {
			t.Fatal(err)
		}
		for _, r := range dst.db.Rows() {
			if want := map[int]string{1: keys.StateRetired, 2: keys.StateActive}[r.Generation]; r.Kind == keys.KindStorage && r.State != want {
				t.Errorf("generation %d is %s, want %s", r.Generation, r.State, want)
			}
		}
	})
	t.Run("R-STOR-085 the export of a connection without keys exits 2", func(t *testing.T) {
		err := runKeysExport(context.Background(), src.deps, "nothing", io.Discard)
		if exitCode(err) != 2 || !strings.Contains(err.Error(), "has no storage keys for this connection: nothing") {
			t.Fatalf("%v", err)
		}
	})
}

// changedSealed returns the envelope v1:<root key>:<base64> with one byte of its payload changed and encoded again, so the result is
// always a well-formed envelope of other bytes and meets the authentication check, not the check of the form. Flipping a bit of a
// character of the encoding instead gives a character outside the alphabet for some draws ('A' becomes '@', 'a' becomes '`').
func changedSealed(t *testing.T, sealed string) string {
	t.Helper()
	i := strings.LastIndex(sealed, ":")
	payload, err := base64.StdEncoding.DecodeString(sealed[i+1:])
	if i < 0 || err != nil || len(payload) == 0 {
		t.Fatalf("not an envelope of the form v1:<root key>:<base64>: %v", err)
	}
	payload[len(payload)-1] ^= 1
	return sealed[:i+1] + base64.StdEncoding.EncodeToString(payload)
}

func TestR_STOR_085_AnImportThatDoesNotFitExitsTwoAndWritesNothing(t *testing.T) {
	ring := keysRing(t, "k1", map[string]byte{"k1": 1})
	src := newKeysFixture(ring, installation)
	src.populate(t, ring)
	var out bytes.Buffer
	if err := runKeysExport(context.Background(), src.deps, "", &out); err != nil {
		t.Fatal(err)
	}
	exported := out.Bytes()
	var doc keys.Document
	if err := json.Unmarshal(exported, &doc); err != nil {
		t.Fatal(err)
	}
	edited := func(change func(*keys.Document)) []byte {
		var d keys.Document
		_ = json.Unmarshal(exported, &d)
		change(&d)
		b, _ := json.Marshal(d)
		return b
	}
	otherKey := func(id string) string {
		sealed, _ := ring.Seal(keys.Column(), secrets.Row(id), bytes.Repeat([]byte{0xee}, 32))
		return sealed
	}
	for _, c := range []struct {
		name  string
		ring  *secrets.KeyRing
		file  []byte
		other bool
		setup func(*keysFixture)
		want  string
	}{
		{
			name: "a ring that lacks the sealing key", ring: keysRing(t, "k2", map[string]byte{"k2": 2}), file: exported,
			want: `is sealed with the root key "k1", which this key ring lacks; configure the key ring that holds it`,
		},
		{
			name: "a ring whose key of that name is another key", ring: keysRing(t, "k1", map[string]byte{"k1": 3}), file: exported,
			want: `does not open with the root key "k1" of this key ring`,
		},
		{name: "a sealed value changed", file: edited(func(d *keys.Document) {
			d.Keys[0].Sealed = changedSealed(t, d.Keys[0].Sealed)
		}), want: `does not open with the root key "k1" of this key ring: the value was changed, or it was sealed for another identifier`},
		{
			name: "a sealed value moved to another key", file: edited(func(d *keys.Document) { d.Keys[0].Sealed, d.Keys[1].Sealed = d.Keys[1].Sealed, d.Keys[0].Sealed }),
			want: "sealed for another identifier",
		},
		{name: "a file cut short", file: exported[:len(exported)/2], want: "the file ends before the export does: it was cut short"},
		{name: "no export at all", file: []byte(`{"name": "something else"}`), want: "the file is not an export of the storage keys"},
		{name: "an export of another format", file: edited(func(d *keys.Document) { d.Format = "quader-storage-keys-v9" }), want: `the file is of the format "quader-storage-keys-v9"`},
		{
			name: "a row whose fields its identifier does not name", file: edited(func(d *keys.Document) { d.Keys[0].Connection = "default" }),
			want: "which its identifier does not name; the file was changed",
		},
		{
			name: "a file of another installation", file: edited(func(d *keys.Document) { d.InstallationID = "0b7c3f5e-2222-4000-8000-000000000002" }),
			want: "the file was exported by installation 0b7c3f5e-2222-4000-8000-000000000002, and this is installation " + installation + "; pass --other-installation",
		},
		{name: "a key that exists with another value", file: exported, setup: func(f *keysFixture) {
			r := doc.Keys[0]
			f.db.Put(keys.Row{
				KeyID: r.KeyID, Connection: r.Connection, Kind: r.Kind, Generation: r.Generation, State: r.State, Sealed: otherKey(r.KeyID),
				CreatedAt: r.CreatedAt, RetiredAt: deref(r.RetiredAt),
			})
		}, want: "exists in this database with another value"},
		{name: "another name key of the connection", file: exported, setup: func(f *keysFixture) {
			f.db.Put(keys.Row{KeyID: "nk-archive-0000ffff", Connection: "archive", Kind: keys.KindName, Generation: 1, State: keys.StateActive, Sealed: otherKey("nk-archive-0000ffff")})
		}, want: "connection archive holds nk-archive-0000ffff as its name key of generation 1"},
	} {
		t.Run("R-STOR-085 "+c.name, func(t *testing.T) {
			r := ring
			if c.ring != nil {
				r = c.ring
			}
			dst := newKeysFixture(r, installation)
			if c.setup != nil {
				c.setup(dst)
			}
			before := dst.db.Rows()
			_, err := dst.importFile(c.file, c.other)
			if exitCode(err) != 2 || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("exit %d: %v\nwant exit 2 with %q", exitCode(err), err, c.want)
			}
			if after := dst.db.Rows(); len(after) != len(before) {
				t.Errorf("%d rows written by a refused import", len(after)-len(before))
			}
			if len(dst.ev.list) != 0 || len(dst.notified) != 0 {
				t.Error("a refused import was audited or announced")
			}
			if showsKey(t, ring, append(src.db.Rows(), dst.db.Rows()...), err.Error()) {
				t.Errorf("the message shows a key or a sealed value: %v", err)
			}
		})
	}
	t.Run("R-STOR-085 a file of another installation is accepted with --other-installation", func(t *testing.T) {
		dst := newKeysFixture(ring, "0b7c3f5e-3333-4000-8000-000000000003")
		if _, err := dst.importFile(exported, true); err != nil {
			t.Fatal(err)
		}
		if len(dst.db.Rows()) != 3 || !strings.Contains(string(dst.ev.list[0].Details), `"otherInstallation":true`) {
			t.Fatalf("rows %d, event %s", len(dst.db.Rows()), dst.ev.list[0].Details)
		}
	})
}

func TestR_STOR_083_NewGenerationIsAuditedAndAnnounced(t *testing.T) {
	ring := keysRing(t, "k1", map[string]byte{"k1": 1})
	f := newKeysFixture(ring, installation)
	f.populate(t, ring)
	var out bytes.Buffer
	if err := runKeysGeneration(context.Background(), f.deps, "archive", false, &out); err != nil {
		t.Fatal(err)
	}
	t.Run("R-STOR-083 generation 3 is active, the event names the identifiers, the change area names the connection, the reminder to export is printed", func(t *testing.T) {
		var active string
		for _, r := range f.db.Rows() {
			if r.Kind == keys.KindStorage && r.State == keys.StateActive {
				active = r.KeyID
			}
		}
		if id, _ := keys.ParseID(active); id.Generation != 3 {
			t.Fatalf("active %s", active)
		}
		last := f.ev.list[len(f.ev.list)-1]
		if last.Type != audit.EventStorageKeyGenerationAdded || last.Actor.Name != "admin" || !strings.Contains(string(last.Details), active) {
			t.Errorf("event %+v %s", last, last.Details)
		}
		if len(f.notified) != 2 || f.notified[1] != "archive" {
			t.Errorf("notified %v", f.notified)
		}
		if !strings.Contains(out.String(), active) || !strings.Contains(out.String(), "export the storage keys again") {
			t.Errorf("printed %q", out.String())
		}
	})
	t.Run("R-STOR-083 a connection without a storage key exits 2", func(t *testing.T) {
		err := runKeysGeneration(context.Background(), f.deps, "plain", false, io.Discard)
		if exitCode(err) != 2 || !errors.Is(err, keys.ErrNoStorageKey) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("R-STOR-085 the commands refuse what is incomplete and are listed", func(t *testing.T) {
		s := streams{stdin: strings.NewReader(""), stdout: io.Discard, stderr: io.Discard}
		for _, args := range [][]string{
			{"storage", "keys"},
			{"storage", "keys", "frobnicate"},
			{"storage", "keys", "import"},
			{"storage", "keys", "new-generation"},
			{"storage", "keys", "export", "extra"},
		} {
			var u usageError
			if err := adminCmd(args, s); !errors.As(err, &u) || !strings.Contains(u.Error(), "quader admin storage keys export") {
				t.Errorf("%v: %v", args, err)
			}
		}
		if !strings.Contains(adminUsage, "storage keys") || !strings.Contains(storageUsage, "storage keys") {
			t.Error("the usages do not list storage keys")
		}
	})
}

func deref(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
