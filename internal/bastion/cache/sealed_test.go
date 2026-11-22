package cache

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/config"
)

// The tests of R-BAS-061: the cache encrypted in the stored format once its chart gives it a key. Every test runs on a
// temporary volume with objects of bytes to kilobytes; the format itself is tested in internal/blob/blobcrypt.

// keyK is the cache key 00 01 … 1f of the known answers; keyK2 is 20 21 … 3f.
func keyK() []byte  { return seq(0) }
func keyK2() []byte { return seq(32) }

func seq(from int) []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(from + i)
	}
	return b
}

// The identifiers of K and K2, computed for this test by a script on Python's hmac and hashlib (HKDF written out by RFC 5869),
// independent of this package's code.
const (
	idK  = "bc-2fc6dc08f8147dd4"
	idK2 = "bc-784eac046c1653c9"
)

func writeKeyFile(t *testing.T, content []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// sealedRig opens a cache on the volume dir through New, with the key file keyFile ("" for none): the path a replica takes.
// The caches of one test share clk.
func sealedRig(t *testing.T, dir, keyFile string, clk *clock, mod ...func(*config.BastionCache)) *rig {
	t.Helper()
	cfg := testConfig(dir)
	cfg.EncryptionKeyFile = keyFile
	for _, m := range mod {
		m(&cfg)
	}
	m, err := metrics.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	c, err := New(context.Background(), cfg, testSigner(t), Options{Metrics: m, Now: clk.Now, TempDir: tmp})
	if err != nil {
		t.Fatal(err)
	}
	return &rig{Cache: c, t: t, dir: dir, clock: clk, m: m, cfg: cfg, podTmp: tmp}
}

// path is where the volume keeps the object of a layout name.
func (r *rig) path(name string, err error) string {
	r.t.Helper()
	if err != nil {
		r.t.Fatal(err)
	}
	return filepath.Join(r.dir, filepath.FromSlash(name))
}

func (r *rig) objectFile(kind EntryKind, sha string) string {
	return r.path(r.back.lay.object(kind, sha))
}

func (r *rig) entryFile(cacheKey string) string {
	return r.path(r.back.lay.entry(KeyHash(cacheKey)))
}

// storeFiles returns every file of the volume by its name below the volume, without the volume's own tmp/, which must be
// empty once nothing is in progress.
func storeFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "tmp/") {
			t.Errorf("%s is left in the volume's tmp", rel)
			return nil
		}
		b, err := os.ReadFile(p)
		out[rel] = b
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// sharedRun returns the offset in hay of the first 16-byte run of needle, or -1.
func sharedRun(hay, needle []byte) int {
	for i := 0; i+16 <= len(needle); i++ {
		if j := bytes.Index(hay, needle[i:i+16]); j >= 0 {
			return j
		}
	}
	return -1
}

func flipByte(t *testing.T, p string, at int) {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	b[at] ^= 0x01
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func gone(p string) bool {
	_, err := os.Stat(p)
	return errors.Is(err, fs.ErrNotExist)
}

func filesBelow(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

// read is a hit: look up, open verified, read to the end.
func (r *rig) read(cacheKey string) ([]byte, error) {
	ctx := context.Background()
	e, _, err := r.LookupEntry(ctx, cacheKey)
	if err != nil {
		return nil, err
	}
	rc, err := r.Open(ctx, cacheKey, e)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

func (r *rig) keep(kind EntryKind, cacheKey string, data []byte) Entry {
	r.t.Helper()
	r.fillObject(kind, data)
	e := r.entry(kind, data)
	if err := r.StoreEntry(context.Background(), cacheKey, e); err != nil {
		r.t.Fatal(err)
	}
	return e
}

const (
	tarballKey  = "/api/npm/web/npm-all/left-pad/-/left-pad-1.3.0.tgz"
	documentKey = "/api/npm/web/npm-all/left-pad"
)

var spoolEvents = []byte(`{"events":[{"tokenId":"tok-1","principalId":"alice","path":"/api/npm/web/npm-all/left-pad/-/left-pad-1.3.0.tgz"}]}` + "\n")

// TestR_BAS_061_WithAKeyNoObjectHoldsAPlainRunOrANameOfWhatTheCacheKeeps: every family of the store (content, documents,
// entries, access markers, the audit spool) lies below the key's tree; no object holds a 16-byte run of a tarball, a
// document, an entry or a spooled event, and no name holds a content address, the hash of a cache key or a path. The fill
// in progress is spooled on the pod's temporary volume, not on the cache volume (Q-BAS-214).
func TestR_BAS_061_WithAKeyNoObjectHoldsAPlainRunOrANameOfWhatTheCacheKeeps(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "volume")
	r := sealedRig(t, dir, writeKeyFile(t, keyK()), newClock())
	ctx := context.Background()
	if err := r.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	art := payload(1, 512)
	doc := []byte(`{"name":"left-pad","versions":{"1.3.0":{"dist":{"tarball":"https://packages.example.com/left-pad-1.3.0.tgz"}}}}`)

	f, err := r.NewFill(EntryArtifact)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(art); err != nil {
		t.Fatal(err)
	}
	if n := filesBelow(t, filepath.Join(r.podTmp, "quader-bastion-cache")); n != 1 {
		t.Fatalf("the fill in progress is not spooled on the pod's temporary volume: %d files", n)
	}
	if n := filesBelow(t, filepath.Join(dir, "tmp")); n != 0 {
		t.Fatalf("the fill in progress lies on the cache volume: %d files", n)
	}
	if err := f.Commit(ctx, digest(art)); err != nil {
		t.Fatal(err)
	}
	ea := r.entry(EntryArtifact, art)
	ea.Header = http.Header{"Content-Type": {"application/octet-stream"}, "Etag": {`"left-pad-1.3.0"`}}
	if err := r.StoreEntry(ctx, tarballKey, ea); err != nil {
		t.Fatal(err)
	}
	ed := r.keep(EntryDocument, documentKey, doc)
	r.Touch(ctx, ea)
	r.Touch(ctx, ed)
	if err := r.Backend().SpoolWrite(ctx, "bastion-0", spoolEvents); err != nil {
		t.Fatal(err)
	}
	recA, _ := json.Marshal(ea)
	recD, _ := json.Marshal(ed)
	plain := map[string][]byte{"tarball": art, "document": doc, "artifact entry": recA, "document entry": recD, "spooled event": spoolEvents}
	names := []string{digest(art), digest(doc), KeyHash(tarballKey), KeyHash(documentKey), "left-pad", "alice", "npm-all"}

	families := map[string]int{}
	for p, b := range storeFiles(t, dir) {
		if !strings.HasPrefix(p, idK+"/") {
			t.Errorf("%s lies outside the tree of the key %s", p, idK)
			continue
		}
		families[strings.Split(p, "/")[1]]++
		for what, s := range plain {
			if at := sharedRun(b, s); at >= 0 {
				t.Errorf("%s holds a 16-byte run of the %s at offset %d", p, what, at)
			}
		}
		for _, n := range names {
			if strings.Contains(p, n) {
				t.Errorf("the name %s holds %q", p, n)
			}
		}
	}
	if want := map[string]int{"a": 1, "d": 1, "e": 2, "x": 2, "s": 1}; fmt.Sprint(families) != fmt.Sprint(want) {
		t.Errorf("objects by family %v, want %v (the probe of the first check is deleted)", families, want)
	}
	for key, want := range map[string][]byte{tarballKey: art, documentKey: doc} {
		if got, err := r.read(key); err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s reads back: %d bytes, %v", key, len(got), err)
		}
	}
	files, err := r.Backend().SpoolList(ctx)
	if err != nil || len(files) != 1 {
		t.Fatalf("spool: %v %v", files, err)
	}
	if got, err := r.Backend().SpoolRead(ctx, files[0]); err != nil || !bytes.Equal(got, spoolEvents) {
		t.Fatalf("the spooled events read back: %q %v", got, err)
	}
}

// TestR_BAS_061_AChangedByteIsRefusedBeforeAByteIsServed: one flipped bit in the body or in the record of each family's
// object is refused before a byte of it leaves; the object goes (and the entry of content), and the next read is a miss.
func TestR_BAS_061_AChangedByteIsRefusedBeforeAByteIsServed(t *testing.T) {
	ctx := context.Background()
	recordByte := len("quader-blob-v1") + 1 + len(idK) + 40 // inside the wrap
	for name, kind := range map[string]EntryKind{"content": EntryArtifact, "documents": EntryDocument} {
		for where, at := range map[string]int{"body": int(frameSize(idK)) + 100, "record": recordByte} {
			t.Run(name+" "+where, func(t *testing.T) {
				r := sealedRig(t, filepath.Join(t.TempDir(), "volume"), writeKeyFile(t, keyK()), newClock())
				e := r.keep(kind, "/k", payload(3, 150))
				obj := r.objectFile(kind, e.SHA256)
				flipByte(t, obj, at)
				served, err := r.read("/k")
				if !errors.Is(err, ErrCorrupt) || len(served) != 0 {
					t.Fatalf("a changed %s served %d bytes, %v", where, len(served), err)
				}
				if !gone(obj) || !gone(r.entryFile("/k")) || val(t, r.m.CacheCorrupt) != 1 {
					t.Errorf("object gone %v, entry gone %v, corrupt %v", gone(obj), gone(r.entryFile("/k")), val(t, r.m.CacheCorrupt))
				}
				if _, _, err := r.LookupEntry(ctx, "/k"); !errors.Is(err, ErrNotFound) {
					t.Errorf("the next read: %v", err)
				}
			})
		}
	}
	t.Run("entries", func(t *testing.T) {
		r := sealedRig(t, filepath.Join(t.TempDir(), "volume"), writeKeyFile(t, keyK()), newClock())
		r.keep(EntryArtifact, "/k", payload(4, 512))
		flipByte(t, r.entryFile("/k"), int(frameSize(idK))+30)
		if _, _, err := r.LookupEntry(ctx, "/k"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("a changed entry: %v", err)
		}
		if !gone(r.entryFile("/k")) || evictions(t, r.m, metrics.EvictStale) != 1 {
			t.Errorf("entry gone %v, stale %v", gone(r.entryFile("/k")), evictions(t, r.m, metrics.EvictStale))
		}
	})
	t.Run("audit spool", func(t *testing.T) {
		r := sealedRig(t, filepath.Join(t.TempDir(), "volume"), writeKeyFile(t, keyK()), newClock())
		if err := r.Backend().SpoolWrite(ctx, "bastion-0", spoolEvents); err != nil {
			t.Fatal(err)
		}
		files, _ := r.Backend().SpoolList(ctx)
		p := r.path(r.back.lay.spool(files[0].Replica, files[0].Name))
		flipByte(t, p, recordByte)
		if got, err := r.Backend().SpoolRead(ctx, files[0]); !errors.Is(err, ErrNotFound) || got != nil {
			t.Fatalf("a changed spool file: %q %v", got, err)
		}
		if !gone(p) || val(t, r.m.CacheCorrupt) != 1 {
			t.Errorf("spool file gone %v, corrupt %v", gone(p), val(t, r.m.CacheCorrupt))
		}
	})
}

// TestR_BAS_061_AnObjectOfAnotherNameFamilyOrKeyIsRefusedAndDeleted: an entry moved to another cache key's name, the bytes of
// another content address, an entry put into the audit spool, and the same content written under another key are each
// refused when read and deleted. The backend refuses the bytes of another content address before it hands out a reader
// (step 4 of R-STOR-077), which the verifying reader's hold-back would otherwise hide for an object this small.
func TestR_BAS_061_AnObjectOfAnotherNameFamilyOrKeyIsRefusedAndDeleted(t *testing.T) {
	ctx := context.Background()
	clk := newClock()
	r := sealedRig(t, filepath.Join(t.TempDir(), "volume"), writeKeyFile(t, keyK()), clk)
	a, b := payload(5, 512), payload(6, 512)
	ea := r.keep(EntryArtifact, "/a", a)
	eb := r.keep(EntryArtifact, "/b", b)

	copyFile(t, r.entryFile("/a"), r.entryFile("/b"))
	if _, _, err := r.LookupEntry(ctx, "/b"); !errors.Is(err, ErrNotFound) || !gone(r.entryFile("/b")) {
		t.Fatalf("an entry moved to another cache key: %v", err)
	}

	copyFile(t, r.objectFile(EntryArtifact, ea.SHA256), r.objectFile(EntryArtifact, eb.SHA256))
	if rc, err := r.Backend().GetObject(ctx, eb.SHA256, 0, -1); !errors.Is(err, ErrCorrupt) {
		if rc != nil {
			_ = rc.Close()
		}
		t.Fatalf("the backend handed out the bytes of another content address: %v", err)
	}
	if err := r.StoreEntry(ctx, "/b", eb); err != nil {
		t.Fatal(err)
	}
	if got, err := r.read("/b"); !errors.Is(err, ErrCorrupt) || len(got) != 0 || !gone(r.objectFile(EntryArtifact, eb.SHA256)) {
		t.Fatalf("the bytes of another content address: %d bytes, %v", len(got), err)
	}

	spool := r.path(r.back.lay.spool("bastion-0", "1"))
	copyFile(t, r.entryFile("/a"), spool)
	if got, err := r.Backend().SpoolRead(ctx, SpoolFile{Replica: "bastion-0", Name: "1"}); !errors.Is(err, ErrNotFound) || got != nil || !gone(spool) {
		t.Fatalf("an entry in the audit spool: %q %v", got, err)
	}

	other := sealedRig(t, filepath.Join(t.TempDir(), "volume"), writeKeyFile(t, keyK2()), clk)
	other.fillObject(EntryArtifact, a)
	copyFile(t, other.objectFile(EntryArtifact, ea.SHA256), r.objectFile(EntryArtifact, ea.SHA256))
	if got, err := r.read("/a"); !errors.Is(err, ErrCorrupt) || len(got) != 0 || !gone(r.objectFile(EntryArtifact, ea.SHA256)) {
		t.Fatalf("the object of another key: %d bytes, %v", len(got), err)
	}
}

// TestR_BAS_061_AHitAfterARestartWithTheSameKeyFileIsServed: a second cache over the same volume with the same key file, here
// in its base64 form, serves the artifact, the document and the spooled events, and its sweep leaves its own tree alone.
func TestR_BAS_061_AHitAfterARestartWithTheSameKeyFileIsServed(t *testing.T) {
	ctx := context.Background()
	dir, clk := filepath.Join(t.TempDir(), "volume"), newClock()
	keyFile := writeKeyFile(t, []byte(base64.StdEncoding.EncodeToString(keyK())+"\n"))
	roomy := func(c *config.BastionCache) { c.MaxBytes, c.DocumentsMaxBytes = 4000, 1000 } // nothing above a limit
	r := sealedRig(t, dir, keyFile, clk, roomy)
	art, doc := payload(7, 512), payload(8, 150)
	r.keep(EntryArtifact, tarballKey, art)
	r.keep(EntryDocument, documentKey, doc)
	if err := r.Backend().SpoolWrite(ctx, "bastion-0", spoolEvents); err != nil {
		t.Fatal(err)
	}
	before := storeFiles(t, dir)

	again := sealedRig(t, dir, keyFile, clk, roomy)
	if err := again.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string][]byte{tarballKey: art, documentKey: doc} {
		if got, err := again.read(key); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s after the restart: %d bytes, %v", key, len(got), err)
		}
	}
	files, err := again.Backend().SpoolList(ctx)
	if err != nil || len(files) != 1 {
		t.Fatalf("spool: %v %v", files, err)
	}
	if got, err := again.Backend().SpoolRead(ctx, files[0]); err != nil || !bytes.Equal(got, spoolEvents) {
		t.Fatalf("spooled events after the restart: %q %v", got, err)
	}
	if err := again.SweepOnce(ctx, nil); err != nil {
		t.Fatal(err)
	}
	after := storeFiles(t, dir)
	for p := range before {
		if _, ok := after[p]; !ok {
			t.Errorf("the sweep deleted %s of its own tree", p)
		}
	}
}

// TestR_BAS_061_ABastionWithAnotherKeyStartsMissesSweepsTheOldTreeAndFillsAgain: a lost or changed key costs the cache and
// nothing else. Another key starts, misses, its sweep deletes the old tree, and it fills again; a cache without a key keeps
// R-BAS-030's layout and deletes the key's tree; a key again deletes the plain layout. After the first hour a replica stops
// looking once a sweep found nothing.
func TestR_BAS_061_ABastionWithAnotherKeyStartsMissesSweepsTheOldTreeAndFillsAgain(t *testing.T) {
	ctx := context.Background()
	dir, clk := filepath.Join(t.TempDir(), "volume"), newClock()
	art := payload(9, 512)
	old := sealedRig(t, dir, writeKeyFile(t, keyK()), clk)
	e := old.keep(EntryArtifact, tarballKey, art)
	old.Touch(ctx, e)
	if err := old.Backend().SpoolWrite(ctx, "bastion-0", spoolEvents); err != nil {
		t.Fatal(err)
	}

	stranger := filepath.Join(dir, "bc-not-a-tree", "x") // not a name of a key's tree: no sweep touches it
	copyFile(t, writeKeyFile(t, []byte("x")), stranger)
	r := sealedRig(t, dir, writeKeyFile(t, keyK2()), clk)
	if err := r.Ready(ctx); err != nil {
		t.Fatalf("a Bastion with another key does not start: %v", err)
	}
	if _, err := r.read(tarballKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the first read with another key: %v", err)
	}
	if err := r.SweepOnce(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if n := filesBelow(t, filepath.Join(dir, idK)); n != 0 {
		t.Fatalf("%d objects of the old key's tree survive the sweep", n)
	}
	r.keep(EntryArtifact, tarballKey, art)
	if got, err := r.read(tarballKey); err != nil || !bytes.Equal(got, art) {
		t.Fatalf("filled again: %v", err)
	}

	plain := sealedRig(t, dir, "", clk)
	plain.keep(EntryArtifact, tarballKey, art)
	if gone(filepath.Join(dir, "sha256", e.SHA256[0:2], e.SHA256[2:4], e.SHA256)) {
		t.Fatal("without a key the layout is not R-BAS-030's")
	}
	if err := plain.SweepOnce(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if n := filesBelow(t, filepath.Join(dir, idK2)); n != 0 {
		t.Fatalf("a cache without a key left %d objects of a key's tree", n)
	}
	if got, err := plain.read(tarballKey); err != nil || !bytes.Equal(got, art) {
		t.Fatalf("the plain cache's own hit: %v", err)
	}

	again := sealedRig(t, dir, writeKeyFile(t, keyK()), clk)
	for i := range 2 { // the first finds the plain layout, the second nothing
		if err := again.SweepOnce(ctx, nil); err != nil {
			t.Fatal(err)
		}
		if i == 0 && again.foreignFound.Load() != true {
			t.Fatal("the sweep that deleted the plain layout did not say so")
		}
	}
	for _, fam := range []string{"sha256", "documents", "entries", "access", "audit-spool"} {
		if n := filesBelow(t, filepath.Join(dir, fam)); n != 0 {
			t.Errorf("%d objects of the plain layout's %s survive the sweep", n, fam)
		}
	}
	clk.Advance(2 * time.Hour)
	late := filepath.Join(dir, idK2, "a", "00", "00", strings.Repeat("0", 64))
	copyFile(t, writeKeyFile(t, []byte("x")), late)
	if err := again.SweepOnce(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if gone(late) {
		t.Error("a sweep after the first hour, after one that found nothing, still looked for other layouts")
	}
	if gone(stranger) {
		t.Error("a sweep deleted a name below the prefix that is no tree of a key")
	}
}

// TestR_BAS_061_TheKeyFileAndWhatDerivesFromIt: the key ring's file rules (raw, base64 with and without padding, white space
// around it), the refusals, which name nothing of the file, the identifier and the keyed names against the known answers,
// and a sealer that renders its identifier only.
func TestR_BAS_061_TheKeyFileAndWhatDerivesFromIt(t *testing.T) {
	k := keyK()
	std := base64.StdEncoding.EncodeToString(k)
	for name, content := range map[string][]byte{
		"raw":            k,
		"base64":         []byte(std + "\n"),
		"base64 no pads": []byte("  " + strings.TrimRight(std, "=") + "\r\n"),
	} {
		s, err := openSealer(writeKeyFile(t, content))
		if err != nil || s.id != idK {
			t.Errorf("%s: %v %v", name, s, err)
		}
	}
	short := base64.StdEncoding.EncodeToString(k[:31])
	for name, content := range map[string][]byte{
		"31 raw bytes": k[:31], "33 raw bytes": append(append([]byte{}, k...), 0), "base64 of 31 bytes": []byte(short),
		"44 characters of no base64": []byte(strings.Repeat("!", 44)), "empty": nil,
	} {
		_, err := openSealer(writeKeyFile(t, content))
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		msg := err.Error()
		if strings.Contains(msg, std[:20]) || strings.Contains(msg, short[:20]) || strings.Contains(msg, hex.EncodeToString(k[:8])) || !strings.Contains(msg, "R-ARCH-021") {
			t.Errorf("%s: the refusal %q names the file's content or not the rule", name, msg)
		}
	}
	if _, err := openSealer(filepath.Join(t.TempDir(), "absent")); err == nil || !strings.Contains(err.Error(), "secrets.cacheKey") {
		t.Errorf("a missing file: %v", err)
	}
	if s, err := openSealer(""); s != nil || err != nil {
		t.Errorf("no file is no key: %v %v", s, err)
	}

	s, err := newSealer(k)
	if err != nil {
		t.Fatal(err)
	}
	a := digestBytes([]byte("a"))
	for what, c := range map[string]struct{ got, want string }{
		"artifact": {s.name(nameArtifact, a), "febf36063854f44f704163b3ee6102bff78ae752cd16548a3df3f1ff3cf7c239"},
		"document": {s.name(nameDocument, a), "3e8e1d587ffcd444f0b60f69372830aad174c8e151e0ab0d611555e17df92738"},
		"entry":    {s.name(nameEntry, digestBytes([]byte(documentKey))), "f3101860b90d37e55b6502bbf64e73bcb22533ca76d529e3dc58cf62bb1eafa6"},
	} {
		if c.got != c.want {
			t.Errorf("the keyed name of the %s is %s, want %s", what, c.got, c.want)
		}
	}
	s2, _ := newSealer(keyK2())
	if s2.id != idK2 {
		t.Errorf("the identifier of K2 is %s, want %s", s2.id, idK2)
	}
	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("x", "key", s)
	for _, out := range []string{fmt.Sprint(s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s), buf.String(), fmt.Sprintf("%v", s.storage)} {
		if !strings.Contains(out, idK) || strings.Contains(out, hex.EncodeToString(s.names[:4])) || strings.Contains(out, "[") {
			t.Errorf("a sealer renders %q", out)
		}
	}
}

func digestBytes(b []byte) []byte {
	d, _ := hex.DecodeString(digest(b))
	return d
}

// TestR_BAS_061_ARangeIsAuthenticatedPerSegmentAndDamageOutsideItsWindowIsNotSeen: a range of a sealed artifact of two
// segments reads only its window's segments; a changed byte in the second segment refuses a range that reaches it, deletes
// the object, and is not seen by a range of the first segment (H-1300).
func TestR_BAS_061_ARangeIsAuthenticatedPerSegmentAndDamageOutsideItsWindowIsNotSeen(t *testing.T) {
	ctx := context.Background()
	r := sealedRig(t, filepath.Join(t.TempDir(), "volume"), writeKeyFile(t, keyK()), newClock(), func(c *config.BastionCache) {
		c.MaxObjectBytes, c.MaxBytes, c.DocumentsMaxBytes = 128*kib, 256*kib, kib
	})
	data := payload(10, 70*kib)
	r.fillObject(EntryArtifact, data)
	e := r.entry(EntryArtifact, data)
	for _, c := range []struct{ off, n int64 }{{65530, 20}, {100, -1}, {70*kib - 10, 50}, {0, 1}, {70 * kib, 5}} {
		rc, err := r.OpenRange(ctx, e, c.off, c.n)
		if err != nil {
			t.Fatalf("range %d,%d: %v", c.off, c.n, err)
		}
		end := int64(len(data))
		if c.n >= 0 {
			end = min(end, c.off+c.n)
		}
		want := data[min(c.off, int64(len(data))):end]
		if got := readAllT(t, rc); !bytes.Equal(got, want) {
			t.Errorf("range %d,%d: %d bytes, want %d", c.off, c.n, len(got), len(want))
		}
	}
	obj := r.objectFile(EntryArtifact, e.SHA256)
	flipByte(t, obj, int(frameSize(idK))+65564+100)
	rc, err := r.OpenRange(ctx, e, 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	if got := readAllT(t, rc); !bytes.Equal(got, data[10:30]) {
		t.Fatal("a range of the first segment did not read its window")
	}
	rc, err = r.OpenRange(ctx, e, 66000, 10)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if !errors.Is(err, ErrCorrupt) || len(got) != 0 || !gone(obj) {
		t.Fatalf("a range of the changed segment: %d bytes, %v, object gone %v", len(got), err, gone(obj))
	}
}

// TestR_BAS_061_TheSweepEvictsSealedObjectsByLastAccessWithTheirMarkers: the size sweep of R-BAS-031 on the sealed layout
// counts the stored bytes, orders by the keyed markers and deletes objects with their markers. Four objects of 100 bytes are
// stored as 378 bytes each (a frame of 182 and S(100) = 196): 1512 bytes against an artifact limit of 1200.
func TestR_BAS_061_TheSweepEvictsSealedObjectsByLastAccessWithTheirMarkers(t *testing.T) {
	ctx := context.Background()
	r := sealedRig(t, filepath.Join(t.TempDir(), "volume"), writeKeyFile(t, keyK()), newClock(), func(c *config.BastionCache) {
		c.MaxBytes, c.DocumentsMaxBytes = 1400, 200
	})
	base := r.clock.Now().Add(-time.Hour)
	var es []Entry
	for i := range 4 {
		e := r.keep(EntryArtifact, fmt.Sprintf("/obj/%d", i), payload(uint64(20+i), 100))
		r.setTime(r.objectFile(EntryArtifact, e.SHA256), base.Add(time.Duration(i)*time.Minute))
		es = append(es, e)
	}
	r.Touch(ctx, es[0])
	if err := r.SweepOnce(ctx, nil); err != nil {
		t.Fatal(err)
	}
	var kept []int
	for i, e := range es {
		if !gone(r.objectFile(EntryArtifact, e.SHA256)) {
			kept = append(kept, i)
		}
	}
	if fmt.Sprint(kept) != "[0 3]" || evictions(t, r.m, metrics.EvictSize) != 2 {
		t.Fatalf("kept %v, %v evictions; want [0 3] and 2", kept, evictions(t, r.m, metrics.EvictSize))
	}
	if got := val(t, r.m.CacheBytes.WithLabelValues(metrics.AreaArtifacts)); got != 756 {
		t.Errorf("quader_bastion_cache_bytes{artifacts} = %v, want 756", got)
	}
	if gone(r.path(r.back.lay.marker(EntryArtifact, es[0].SHA256))) {
		t.Error("the marker of the touched object went")
	}
}

// TestR_BAS_061_ConcurrentFillsReadsAndSweepsOfASealedCacheAreRaceFree: the sealed paths under the race detector.
func TestR_BAS_061_ConcurrentFillsReadsAndSweepsOfASealedCacheAreRaceFree(t *testing.T) {
	r := sealedRig(t, filepath.Join(t.TempDir(), "volume"), writeKeyFile(t, keyK()), newClock(), func(c *config.BastionCache) {
		c.MaxBytes, c.DocumentsMaxBytes = 6000, 1000
	})
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var served []string
	for g := range 4 {
		wg.Go(func() {
			for i := range 8 {
				data := payload(uint64(g*100+i), 200+i)
				key := "/k/" + digest(data)
				f, err := r.NewFill(EntryArtifact)
				if err != nil {
					t.Error(err)
					return
				}
				_, _ = f.Write(data)
				if err := f.Commit(ctx, digest(data)); err != nil {
					t.Error(err)
					return
				}
				if err := r.StoreEntry(ctx, key, r.entry(EntryArtifact, data)); err != nil {
					t.Error(err)
				}
				if got, err := r.read(key); err == nil {
					if !bytes.Equal(got, data) {
						t.Errorf("%s served other bytes", key)
					}
					mu.Lock()
					served = append(served, key)
					mu.Unlock()
				}
				if i%4 == 0 {
					_ = r.SweepOnce(ctx, nil)
				}
			}
		})
	}
	wg.Wait()
	sort.Strings(served)
	if len(served) == 0 {
		t.Error("nothing was served")
	}
}
