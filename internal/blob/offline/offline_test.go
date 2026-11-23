package offline_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/blob/offline"
)

// The cases of the offline tool's reader that the command's test (cmd/quader) does not show: the export's destroyed keys, the names
// it refuses, plain and pool objects, and the directory's bounds (storage.md R-STOR-089, R-STOR-103). Objects are written with the
// format's writer, kilobytes each.

func list(t *testing.T, r *offline.Reader) (map[string]offline.Result, offline.Totals) {
	t.Helper()
	return collect(t, func(emit func(offline.Result) error) (offline.Totals, error) {
		return r.List(context.Background(), emit)
	})
}

func verify(t *testing.T, r *offline.Reader) (map[string]offline.Result, offline.Totals) {
	t.Helper()
	return collect(t, func(emit func(offline.Result) error) (offline.Totals, error) {
		return r.Verify(context.Background(), 2, emit)
	})
}

func TestR_STOR_089_TheOfflineReader(t *testing.T) {
	ctx := context.Background()
	archive := offline.Info{Connection: "archive", Prefix: "quader", Encrypted: true, KeyedNames: true}

	t.Run("R-STOR-089 R-STOR-103 a record of a key the export lists as destroyed is destroyed_key, one of a key it lacks unknown_key, one of a generation it holds ok", func(t *testing.T) {
		ringDir, ring := keyRing(t, "2026-12")
		gen1, gen2, gen3, name := randomKey(), randomKey(), randomKey(), randomKey()
		nk, _ := blobcrypt.KeyFromBytes(name)
		s := blobtest.New()
		a, b, c := content(100, 1), content(200, 2), content(300, 3)
		na, nb, nc := keyedName(t, nk, sha256.Sum256(a)), keyedName(t, nk, sha256.Sum256(b)), keyedName(t, nk, sha256.Sum256(c))
		putSealed(t, s, na, storageKey(t, "sk-archive-1-0a1b2c3d", gen1), a)
		putSealed(t, s, nb, storageKey(t, "sk-archive-2-1b2c3d4e", gen2), b)
		putSealed(t, s, nc, storageKey(t, "sk-archive-3-2c3d4e5f", gen3), c)
		export := writeExport(t, ring, entry{id: "sk-archive-1-0a1b2c3d"}, entry{id: "sk-archive-2-1b2c3d4e", raw: gen2}, entry{id: "nk-archive-71be04d9", raw: name})
		ks, err := offline.LoadKeys(export, ringDir)
		if err != nil {
			t.Fatal(err)
		}
		defer ks.Close()
		got, tot := list(t, offline.NewReader(offline.Bucket(s, archive), ks))
		if got[na].Status != offline.StatusDestroyedKey || got[na].KeyID != "sk-archive-1-0a1b2c3d" || got[nb].Status != offline.StatusOK ||
			got[nc].Status != string(blobcrypt.ReasonUnknownKey) || tot.Findings != 2 || tot.OK != 1 {
			t.Errorf("list = %+v, totals %+v", got, tot)
		}
		// An export made before the destruction still lists the key sealed and opens the object (H-1370).
		before := writeExport(t, ring, entry{id: "sk-archive-1-0a1b2c3d", raw: gen1}, entry{id: "nk-archive-71be04d9", raw: name})
		ks2, err := offline.LoadKeys(before, ringDir)
		if err != nil {
			t.Fatal(err)
		}
		defer ks2.Close()
		if got, _ := verify(t, offline.NewReader(offline.Bucket(s, archive), ks2)); got[na].Status != offline.StatusOK {
			t.Errorf("with the export made before: %+v", got[na])
		}
	})

	t.Run("R-STOR-089 an object at another keyed name, or at a name that is not of the layout, is name_mismatch, and pull leaves the foreign name out", func(t *testing.T) {
		ringDir, ring := keyRing(t, "2026-12")
		gen1, name, other := randomKey(), randomKey(), randomKey()
		onk, _ := blobcrypt.KeyFromBytes(other)
		sk := storageKey(t, "sk-archive-1-0a1b2c3d", gen1)
		s := blobtest.New()
		a := content(64, 4)
		moved := keyedName(t, onk, sha256.Sum256(a))
		putSealed(t, s, moved, sk, a)
		s.Set("quader/keyed/zz/zz/not-a-name", []byte("x"), time.Now())
		export := writeExport(t, ring, entry{id: "sk-archive-1-0a1b2c3d", raw: gen1}, entry{id: "nk-archive-71be04d9", raw: name})
		ks, err := offline.LoadKeys(export, ringDir)
		if err != nil {
			t.Fatal(err)
		}
		defer ks.Close()
		got, _ := list(t, offline.NewReader(offline.Bucket(s, archive), ks))
		if got[moved].Status != offline.StatusNameMismatch || got[moved].SHA256 != hexOf(sha256.Sum256(a)) || got["quader/keyed/zz/zz/not-a-name"].Status != offline.StatusNameMismatch {
			t.Errorf("list = %+v", got)
		}
		to := filepath.Join(t.TempDir(), "pull")
		pt, err := offline.Pull(ctx, offline.Bucket(s, archive), to, time.Now(), func(string, int64) error { return nil })
		if err != nil || pt.Objects != 1 || pt.Skipped != 1 {
			t.Fatalf("pull = %+v, %v", pt, err)
		}
		if _, err := os.Stat(filepath.Join(to, "quader", "keyed", "zz")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the foreign name was written: %v", err)
		}
	})

	t.Run("R-STOR-089 a plain connection's object is plain and verified against the digest of its name; a changed byte is digest; on an encrypted connection an object without record is format", func(t *testing.T) {
		ringDir, ring := keyRing(t, "2026-12")
		export := writeExport(t, ring)
		ks, err := offline.LoadKeys(export, ringDir)
		if err != nil {
			t.Fatal(err)
		}
		defer ks.Close()
		s := blobtest.New()
		a, b := content(70000, 5), content(10, 6)
		na, nb := blob.Key("quader", sha256.Sum256(a)), blob.Key("quader", sha256.Sum256(b))
		s.Set(na, a, time.Now())
		changed := append([]byte(nil), b...)
		changed[3] ^= 1
		s.Set(nb, changed, time.Now())
		plain := offline.Info{Connection: "default", Prefix: "quader"}
		got, tot := verify(t, offline.NewReader(offline.Bucket(s, plain), ks))
		if got[na].Status != offline.StatusPlain || *got[na].Size != 70000 || got[nb].Status != string(blobcrypt.ReasonDigest) || tot.Plain != 1 || tot.Findings != 1 {
			t.Errorf("plain = %+v %+v", got, tot)
		}
		unkeyed := offline.Info{Connection: "archive", Prefix: "quader", Encrypted: true}
		if got, _ := list(t, offline.NewReader(offline.Bucket(s, unkeyed), ks)); got[na].Status != string(blobcrypt.ReasonFormat) {
			t.Errorf("encrypted without keyed names: %+v", got[na])
		}
	})

	t.Run("R-STOR-089 a backup's pool object with its key in the name is ok, a plain one plain, and one whose name names another key name_mismatch; get by digest finds the pool object", func(t *testing.T) {
		ringDir, ring := keyRing(t, "2026-12")
		gen1 := randomKey()
		sk := storageKey(t, "sk-archive-1-0a1b2c3d", gen1)
		s := blobtest.New()
		a, b, c := content(500, 7), content(600, 8), content(700, 9)
		pool := func(d [32]byte, suffix string) string {
			h := hexOf(d)
			return "quader/pool/sha256/" + h[0:2] + "/" + h[2:4] + "/" + h + suffix
		}
		pa, pb, pc := pool(sha256.Sum256(a), ".sk-archive-1-0a1b2c3d"), pool(sha256.Sum256(b), ""), pool(sha256.Sum256(c), ".sk-archive-2-0a1b2c3d")
		putSealed(t, s, pa, sk, a)
		s.Set(pb, b, time.Now())
		putSealed(t, s, pc, sk, c)
		export := writeExport(t, ring, entry{id: "sk-archive-1-0a1b2c3d", raw: gen1})
		ks, err := offline.LoadKeys(export, ringDir)
		if err != nil {
			t.Fatal(err)
		}
		defer ks.Close()
		target := offline.Info{Connection: "backup", Prefix: "quader"}
		r := offline.NewReader(offline.Bucket(s, target), ks)
		got, _ := verify(t, r)
		if got[pa].Status != offline.StatusOK || got[pb].Status != offline.StatusPlain || got[pc].Status != offline.StatusNameMismatch {
			t.Errorf("pool = %+v", got)
		}
		name, err := r.Locate(ctx, hexOf(sha256.Sum256(a)))
		if err != nil || name != pa {
			t.Errorf("locate = %q, %v", name, err)
		}
		if _, err := r.Locate(ctx, hexOf(sha256.Sum256([]byte("absent")))); !errors.Is(err, blob.ErrNotFound) {
			t.Errorf("locate of an absent blob = %v", err)
		}
	})

	t.Run("R-STOR-089 a directory is read only below itself: a name that leaves it is refused, a missing record file reads as no metadata, and a pull refuses a directory that is not empty", func(t *testing.T) {
		ringDir, ring := keyRing(t, "2026-12")
		gen1, name := randomKey(), randomKey()
		nk, _ := blobcrypt.KeyFromBytes(name)
		s := blobtest.New()
		a := content(1000, 10)
		na := keyedName(t, nk, sha256.Sum256(a))
		putSealed(t, s, na, storageKey(t, "sk-archive-1-0a1b2c3d", gen1), a)
		to := filepath.Join(t.TempDir(), "pull")
		if _, err := offline.Pull(ctx, offline.Bucket(s, archive), to, time.Now(), func(string, int64) error { return nil }); err != nil {
			t.Fatal(err)
		}
		if _, err := offline.Pull(ctx, offline.Bucket(s, archive), to, time.Now(), func(string, int64) error { return nil }); !offline.IsRefusal(err) {
			t.Errorf("a second pull into the same directory = %v", err)
		}
		src, err := offline.Dir(to)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"../outside", "quader/../../outside", "/etc/passwd", offline.ManifestFile} {
			if _, _, err := src.Head(ctx, bad); !offline.IsRefusal(err) {
				t.Errorf("Head(%q) = %v", bad, err)
			}
		}
		export := writeExport(t, ring, entry{id: "sk-archive-1-0a1b2c3d", raw: gen1}, entry{id: "nk-archive-71be04d9", raw: name})
		ks, err := offline.LoadKeys(export, ringDir)
		if err != nil {
			t.Fatal(err)
		}
		defer ks.Close()
		r := offline.NewReader(src, ks)
		if _, err := r.Locate(ctx, "quader/keyed/../../../outside"); !offline.IsRefusal(err) {
			t.Errorf("locate of a name that leaves the directory = %v", err)
		}
		if err := os.Remove(filepath.Join(to, filepath.FromSlash(na)+offline.RecordSuffix)); err != nil {
			t.Fatal(err)
		}
		if got, _ := list(t, r); got[na].Status != string(blobcrypt.ReasonFormat) {
			t.Errorf("without its record file: %+v", got[na])
		}
	})

	t.Run("R-STOR-089 an export that was changed, that lists a destroyed key with a sealed value, or that is no export is refused before anything is read", func(t *testing.T) {
		ringDir, ring := keyRing(t, "2026-12")
		good := writeExport(t, ring, entry{id: "sk-archive-1-0a1b2c3d", raw: randomKey()})
		raw, err := os.ReadFile(good)
		if err != nil {
			t.Fatal(err)
		}
		for name, body := range map[string]string{
			"moved":     replaceOnce(t, string(raw), `"connection":"archive"`, `"connection":"other"`),
			"destroyed": replaceOnce(t, string(raw), `"state":"active"`, `"state":"destroyed","destroyedAt":"2026-11-23T00:00:00Z"`),
			"cut":       string(raw[:len(raw)/2]),
			"other":     `{"format":"something-else","keys":[]}`,
		} {
			p := filepath.Join(t.TempDir(), name+".json")
			if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := offline.LoadKeys(p, ringDir); !offline.IsRefusal(err) {
				t.Errorf("%s: %v", name, err)
			}
		}
	})
}

func TestR_STOR_089_TheConnectionElement(t *testing.T) {
	write := func(body string) string {
		p := filepath.Join(t.TempDir(), "element.yaml")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Run("R-STOR-089 an element is read with the binary's rules and defaults: keyed names are on with encrypted, the prefix is quader", func(t *testing.T) {
		_, info, err := offline.OpenElement(context.Background(), write("id: archive\nbucket: quader-archive\nendpoint: https://objects.example\nencrypted: true\n"))
		if err != nil || info != (offline.Info{Connection: "archive", Prefix: "quader", Encrypted: true, KeyedNames: true}) {
			t.Fatalf("info = %+v, %v", info, err)
		}
	})
	t.Run("R-STOR-089 an element the binary refuses, a key it does not know, or a file that is no map is refused naming what is wrong", func(t *testing.T) {
		for body, want := range map[string]string{
			"bucket: b\nkeyed_names: false\n": "keyed_names",
			"bucket: b\nflavour: x\n":         "flavour",
			"- bucket: b\n":                   "not a connection element",
		} {
			_, _, err := offline.OpenElement(context.Background(), write(body))
			if !offline.IsRefusal(err) || !strings.Contains(err.Error(), want) {
				t.Errorf("%q: %v", body, err)
			}
		}
	})
}

func hexOf(d [32]byte) string { return hex.EncodeToString(d[:]) }

func replaceOnce(t *testing.T, s, old, repl string) string {
	t.Helper()
	i := strings.Index(s, old)
	if i < 0 {
		t.Fatalf("%q not in %s", old, s)
	}
	return s[:i] + repl + s[i+len(old):]
}
