package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/blob/offline"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/secrets"
)

// offlineFixture is an encrypted connection archive with keyed names in an in-memory store holding three blobs of 1 KiB, the key
// ring that sealed its export, the export, and every secret byte the fixture made, so that no output may show one (R-STOR-089).
type offlineFixture struct {
	store   *blobtest.Store
	ringDir string
	export  string
	element string
	blobs   [][]byte
	names   []string
	secrets [][]byte
	sealed  []string
	deps    offlineDeps
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func newOfflineFixture(t *testing.T) *offlineFixture {
	t.Helper()
	f := &offlineFixture{store: blobtest.New(), ringDir: t.TempDir()}
	root, skRaw, nkRaw := randomBytes(t, 32), randomBytes(t, 32), randomBytes(t, 32)
	f.secrets = append(f.secrets, root, skRaw, nkRaw)
	file := filepath.Join(f.ringDir, "2026-12.key")
	if err := os.WriteFile(file, []byte(base64.StdEncoding.EncodeToString(root)), 0o600); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.Load(config.Secrets{EncryptionKeyFiles: []string{file}, ActiveKeyID: "2026-12"})
	if err != nil {
		t.Fatal(err)
	}
	sk, err := blobcrypt.NewStorageKey("sk-archive-1-0a1b2c3d", skRaw)
	if err != nil {
		t.Fatal(err)
	}
	nk, err := blobcrypt.KeyFromBytes(nkRaw)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	doc := keys.Document{Format: keys.DocumentFormat, InstallationID: "0b7c3f5e-0000-4000-8000-000000000001", ExportedAt: created, RootKeyIDs: []string{"2026-12"}}
	for _, k := range []struct {
		id  string
		raw []byte
	}{{sk.ID, skRaw}, {"nk-archive-71be04d9", nkRaw}} {
		s, err := ring.Seal(keys.Column(), secrets.Row(k.id), k.raw)
		if err != nil {
			t.Fatal(err)
		}
		f.sealed = append(f.sealed, s)
		id, _ := keys.ParseID(k.id)
		doc.Keys = append(doc.Keys, keys.DocumentKey{KeyID: k.id, Connection: id.Connection, Kind: id.Kind, Generation: id.Generation, State: keys.StateActive, CreatedAt: created, Sealed: s})
	}
	b, _ := json.Marshal(doc)
	f.export = filepath.Join(t.TempDir(), "export.json")
	f.element = filepath.Join(t.TempDir(), "archive.yaml")
	for p, data := range map[string][]byte{f.export: b, f.element: []byte("id: archive\nbucket: quader-archive\nencrypted: true\n")} {
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		plain := randomBytes(t, 1024)
		d := sha256.Sum256(plain)
		dek, _ := blobcrypt.NewKey(rand.Reader)
		dekRaw, _ := dek.Bytes()
		f.secrets = append(f.secrets, dekRaw)
		seed, _ := blobcrypt.NewSeed(rand.Reader)
		sec, err := blobcrypt.NewSection(dek, seed, bytes.NewReader(plain), 0, int64(len(plain)), &blobcrypt.Final{Size: int64(len(plain)), Digest: d})
		if err != nil {
			t.Fatal(err)
		}
		rec, err := blobcrypt.Wrap(sk, blobcrypt.DataKey{Key: dek, Digest: d, Size: int64(len(plain))}, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		name, _ := blobcrypt.KeyedName(nk, "quader", d)
		if err := f.store.Put(context.Background(), name, sec, sec.Size(), blob.Checksum{}, rec.Metadata()); err != nil {
			t.Fatal(err)
		}
		f.blobs, f.names = append(f.blobs, plain), append(f.names, name)
	}
	info := offline.Info{Connection: "archive", Prefix: "quader", Encrypted: true, KeyedNames: true}
	f.deps = offlineDeps{
		openElement: func(_ context.Context, file string) (blob.ObjectStore, offline.Info, error) {
			if file != f.element {
				return nil, offline.Info{}, fmt.Errorf("unexpected element %s", file)
			}
			return f.store, info, nil
		},
		now: func() time.Time { return created },
	}
	return f
}

// run runs a command as the binary does and returns its outputs and exit code.
func (f *offlineFixture) run(args ...string) (string, string, int) {
	var out, errb bytes.Buffer
	err := runStorageOffline(args, streams{stdout: &out, stderr: &errb}, f.deps)
	code := 0
	if err != nil {
		fmt.Fprintf(&errb, "quader: %v\n", err)
		var u usageError
		if errors.As(err, &u) {
			code = 2
		} else {
			code = exitCode(err)
		}
	}
	return out.String(), errb.String(), code
}

// showsSecret reports whether text holds any of the secrets, whole or as a window of 8 bytes, in hex or base64, or a sealed value.
func (f *offlineFixture) showsSecret(text string) string {
	for _, s := range f.sealed {
		if strings.Contains(text, s) || strings.Contains(text, strings.SplitN(s, ":", 3)[2]) {
			return "a sealed value"
		}
	}
	for i, k := range f.secrets {
		forms := []string{base64.StdEncoding.EncodeToString(k), base64.RawURLEncoding.EncodeToString(k)}
		for j := 0; j+8 <= len(k); j++ {
			forms = append(forms, hex.EncodeToString(k[j:j+8]))
		}
		for _, form := range forms {
			if strings.Contains(strings.ToLower(text), strings.ToLower(form)) {
				return fmt.Sprintf("secret %d as %s", i, form)
			}
		}
	}
	return ""
}

func TestR_STOR_089_QuaderAdminStorageOffline(t *testing.T) {
	f := newOfflineFixture(t)
	var outputs strings.Builder
	run := func(args ...string) (string, string, int) {
		o, e, c := f.run(args...)
		outputs.WriteString(o + e)
		return o, e, c
	}
	keysArgs := []string{"--keys", f.export, "--key-ring", f.ringDir}
	bucket := append([]string{"--connection-file", f.element}, keysArgs...)
	digest := func(i int) string { d := sha256.Sum256(f.blobs[i]); return hex.EncodeToString(d[:]) }

	t.Run("R-STOR-089 list prints three ok lines with their digests and the storage key", func(t *testing.T) {
		out, errOut, code := run(append([]string{"list"}, bucket...)...)
		if code != 0 || strings.Count(out, "\nok ")+boolInt(strings.HasPrefix(out, "ok ")) != 3 || !strings.Contains(out, "3 objects, 3 ok, 0 plain, 0 refused") {
			t.Fatalf("exit %d\n%s%s", code, out, errOut)
		}
		for i := range f.blobs {
			if !strings.Contains(out, "ok "+f.names[i]+" stored=1120 size=1024 sha256="+digest(i)+" key=sk-archive-1-0a1b2c3d\n") {
				t.Errorf("no line of blob %d in\n%s", i, out)
			}
		}
	})

	t.Run("R-STOR-089 get of a digest writes its bytes and refuses to write over an existing file", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "blob.bin")
		stdout, errOut, code := run(append([]string{"get", digest(1), "--out", out}, bucket...)...)
		b, err := os.ReadFile(out)
		if code != 0 || err != nil || !bytes.Equal(b, f.blobs[1]) || !strings.HasPrefix(stdout, "wrote "+out+": 1024 bytes, sha256 "+digest(1)) {
			t.Fatalf("exit %d, %v\n%s%s", code, err, stdout, errOut)
		}
		if _, errOut, code := run(append([]string{"get", f.names[0], "--out", out}, bucket...)...); code != 2 || !strings.Contains(errOut, "exists") {
			t.Errorf("over an existing file: exit %d\n%s", code, errOut)
		}
		if b, _ := os.ReadFile(out); !bytes.Equal(b, f.blobs[1]) {
			t.Error("the existing file was changed")
		}
	})

	t.Run("R-STOR-089 pull copies the objects with their records, and verify --dir exits 0", func(t *testing.T) {
		to := filepath.Join(t.TempDir(), "copy")
		if out, errOut, code := run("pull", "--connection-file", f.element, "--to", to); code != 0 || !strings.Contains(out, "3 objects, 3360 bytes") {
			t.Fatalf("pull: exit %d\n%s%s", code, out, errOut)
		}
		if _, err := os.Stat(filepath.Join(to, filepath.FromSlash(f.names[2])+offline.RecordSuffix)); err != nil {
			t.Fatalf("no record file: %v", err)
		}
		out, errOut, code := run(append([]string{"verify", "--dir", to, "--json"}, keysArgs...)...)
		var doc struct {
			Source  offline.Info     `json:"source"`
			Objects []offline.Result `json:"objects"`
			Totals  offline.Totals   `json:"totals"`
		}
		if code != 0 || json.Unmarshal([]byte(out), &doc) != nil || doc.Totals.OK != 3 || len(doc.Objects) != 3 || doc.Source.Connection != "archive" {
			t.Fatalf("verify --dir: exit %d\n%s%s", code, out, errOut)
		}
	})

	t.Run("R-STOR-089 an object with a flipped bit makes verify exit 5 and get of it write nothing", func(t *testing.T) {
		info, err := f.store.Head(context.Background(), f.names[2])
		if err != nil {
			t.Fatal(err)
		}
		stored, _ := f.store.Data(f.names[2])
		stored[blobcrypt.NonceSize+100] ^= 0x10
		if err := f.store.Put(context.Background(), f.names[2], bytes.NewReader(stored), int64(len(stored)), blob.Checksum{}, info.Metadata); err != nil {
			t.Fatal(err)
		}
		out, errOut, code := run(append([]string{"verify"}, bucket...)...)
		if code != 5 || !strings.Contains(out, "segment "+f.names[2]) || strings.Count(out, "\nok ")+boolInt(strings.HasPrefix(out, "ok ")) != 2 {
			t.Errorf("verify: exit %d\n%s%s", code, out, errOut)
		}
		dir := t.TempDir()
		target := filepath.Join(dir, "broken.bin")
		if out, errOut, code := run(append([]string{"get", digest(2), "--out", target}, bucket...)...); code != 5 {
			t.Errorf("get: exit %d\n%s%s", code, out, errOut)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("get of a refused object left %v", entries)
		}
	})

	t.Run("R-STOR-089 a key ring without the export's root key exits 2 naming it", func(t *testing.T) {
		other := t.TempDir()
		root := randomBytes(t, 32)
		f.secrets = append(f.secrets, root)
		if err := os.WriteFile(filepath.Join(other, "2027-01.key"), []byte(base64.StdEncoding.EncodeToString(root)), 0o600); err != nil {
			t.Fatal(err)
		}
		_, errOut, code := run("list", "--connection-file", f.element, "--keys", f.export, "--key-ring", other)
		if code != 2 || !strings.Contains(errOut, "lacks the root key 2026-12") {
			t.Errorf("exit %d\n%s", code, errOut)
		}
	})

	t.Run("R-STOR-089 a command without its files, with both sources or with a second positional argument prints the usage", func(t *testing.T) {
		for _, args := range [][]string{
			{},
			{"frobnicate"},
			{"list", "--connection-file", f.element},
			{"list", "--keys", f.export, "--key-ring", f.ringDir},
			append([]string{"list", "--dir", "x"}, bucket...), append([]string{"get", "--out", "x"}, bucket...),
			append([]string{"get", "a", "b", "--out", "x"}, bucket...),
			{"pull", "--connection-file", f.element},
		} {
			if _, errOut, code := run(args...); code != 2 || !strings.Contains(errOut, "quader admin storage offline list") {
				t.Errorf("%v: exit %d\n%s", args, code, errOut)
			}
		}
	})

	t.Run("R-STOR-089 no output of any command holds a key, a part of one or a sealed value", func(t *testing.T) {
		if what := f.showsSecret(outputs.String()); what != "" {
			t.Errorf("an output holds %s", what)
		}
	})
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
