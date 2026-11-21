package imagescan

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeSpace is a budget: it holds what it was asked for and refuses with ErrNoSpace beyond it.
type fakeSpace struct {
	mu     sync.Mutex
	budget int64
	held   int64
}

func (f *fakeSpace) Hold(n int64) (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.held+n > f.budget {
		return nil, fmt.Errorf("%w: %d held of %d", ErrNoSpace, f.held, f.budget)
	}
	f.held += n
	var done sync.Once
	return func() {
		done.Do(func() {
			f.mu.Lock()
			f.held -= n
			f.mu.Unlock()
		})
	}, nil
}

func (f *fakeSpace) Held() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.held
}

func TestQ_VULN_145_TheRoomOfTheBlobsIsAdmittedBeforeTheFirstOneIsWritten(t *testing.T) {
	m := imageWith(tarOf(t, map[string]string{"etc/os-release": "alpine"}, true))
	var total int64
	for _, b := range m.blobs {
		total += int64(len(b))
	}
	t.Run("the admission sees the compressed size of the config and the layers", func(t *testing.T) {
		var got int64
		err := BuildLayout(context.Background(), m, ImageRef{}, t.TempDir(), lims(), func(n int64) error { got = n; return nil })
		if err != nil || got != total {
			t.Fatalf("%v: admitted %d, the blobs are %d", err, got, total)
		}
	})
	t.Run("a refusal ends the build before a file exists", func(t *testing.T) {
		dir := t.TempDir()
		refused := &ScanError{Code: CodeScannerBusy, Defer: time.Minute}
		err := BuildLayout(context.Background(), m, ImageRef{}, dir, lims(), func(int64) error { return refused })
		if !errors.Is(err, refused) {
			t.Fatalf("%v", err)
		}
		if ents, _ := os.ReadDir(dir); len(ents) != 0 {
			t.Fatalf("%d entries written before the admission", len(ents))
		}
	})
	t.Run("an image over the limit is refused as before and never asks for room", func(t *testing.T) {
		l := lims()
		l.MaxImageBytes = 10
		asked := false
		err := BuildLayout(context.Background(), m, ImageRef{}, t.TempDir(), l, func(int64) error { asked = true; return nil })
		wantScanError(t, err, CodeImageTooLarge)
		if asked {
			t.Fatal("asked for room for an image that is refused")
		}
	})
}

// dbArchive is a database archive with the two files, of the given sizes.
func dbArchive(t *testing.T, dbBytes int) []byte {
	t.Helper()
	return dbArchiveWith(t, map[string]string{
		dbMetaName: `{"Version":2,"UpdatedAt":"2026-11-08T12:51:20Z","NextUpdate":"2026-11-09T12:51:20Z"}`,
		dbFileName: string(bytes.Repeat([]byte("d"), dbBytes)),
	})
}

func dbArchiveWith(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"../escape", dbMetaName, dbFileName} {
		body, ok := files[name]
		if !ok {
			continue
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func digestOfArchive(archive []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(archive)) }

// R-VULN-035, R-VULN-049: the loader unpacks a layer with the code the worker used: the room of each file is held from its header
// before a byte is written, the bytes are the digest, metadata.json is of the schema this release reads, and nothing is kept otherwise.
func TestR_VULN_049_ADatabaseIsUnpackedOnlyWhenItsBytesAreItsDigest(t *testing.T) {
	ctx := context.Background()
	meta := int64(len(`{"Version":2,"UpdatedAt":"2026-11-08T12:51:20Z","NextUpdate":"2026-11-09T12:51:20Z"}`))
	t.Run("a good layer holds the sizes of its two files and reads as a complete database", func(t *testing.T) {
		a := dbArchive(t, 1000)
		sp := &fakeSpace{budget: 1 << 20}
		dir := t.TempDir()
		u, err := UnpackDatabase(ctx, bytes.NewReader(a), digestOfArchive(a), dir, sp.Hold)
		if err != nil {
			t.Fatal(err)
		}
		if u.Bytes != 1000+meta || sp.Held() != 1000+meta || u.Metadata.UpdatedAt.IsZero() {
			t.Fatalf("%+v, held %d", u, sp.Held())
		}
		if _, ok := DatabaseIn(dir); !ok {
			t.Fatal("the unpacked directory does not read as a complete database")
		}
		u.Release()
		u.Release()
		if sp.Held() != 0 {
			t.Fatalf("held %d after the release", sp.Held())
		}
	})
	t.Run("other bytes under the digest hold nothing", func(t *testing.T) {
		sp := &fakeSpace{budget: 1 << 20}
		_, err := UnpackDatabase(ctx, bytes.NewReader(dbArchive(t, 1001)), digestOfArchive(dbArchive(t, 1000)), t.TempDir(), sp.Hold)
		if !ErrLayerDigest(err) || sp.Held() != 0 {
			t.Fatalf("%v, held %d", err, sp.Held())
		}
	})
	t.Run("a budget without room refuses the file before a byte of it is written", func(t *testing.T) {
		a := dbArchive(t, 4000)
		sp := &fakeSpace{budget: 500}
		dir := t.TempDir()
		_, err := UnpackDatabase(ctx, bytes.NewReader(a), digestOfArchive(a), dir, sp.Hold)
		if !errors.Is(err, ErrNoSpace) || sp.Held() != 0 {
			t.Fatalf("%v, held %d", err, sp.Held())
		}
		if _, err := os.Stat(filepath.Join(dir, DatabaseDir, dbFileName)); err == nil {
			t.Fatal("the refused file was written")
		}
	})
	t.Run("an archive with an entry ../escape writes nothing outside its directory", func(t *testing.T) {
		a := dbArchiveWith(t, map[string]string{
			"../escape": "x", dbMetaName: `{"Version":2,"UpdatedAt":"2026-11-08T12:51:20Z"}`, dbFileName: "database",
		})
		root := t.TempDir()
		dir := filepath.Join(root, "unpack")
		if _, err := UnpackDatabase(ctx, bytes.NewReader(a), digestOfArchive(a), dir, (&fakeSpace{budget: 1 << 20}).Hold); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(root, "escape")); err == nil {
			t.Fatal("the entry ../escape was written")
		}
	})
	t.Run("an archive of another schema, without a build time or without a file is refused", func(t *testing.T) {
		for name, files := range map[string]map[string]string{
			"schema 1":      {dbMetaName: `{"Version":1,"UpdatedAt":"2026-11-08T12:51:20Z"}`, dbFileName: "d"},
			"no build time": {dbMetaName: `{"Version":2}`, dbFileName: "d"},
			"no trivy.db":   {dbMetaName: `{"Version":2,"UpdatedAt":"2026-11-08T12:51:20Z"}`},
		} {
			a := dbArchiveWith(t, files)
			sp := &fakeSpace{budget: 1 << 20}
			if _, err := UnpackDatabase(ctx, bytes.NewReader(a), digestOfArchive(a), t.TempDir(), sp.Hold); err == nil || sp.Held() != 0 {
				t.Errorf("%s: %v, held %d", name, err, sp.Held())
			}
		}
	})
	t.Run("a digest that is no sha256 decides no path", func(t *testing.T) {
		if _, err := UnpackDatabase(ctx, bytes.NewReader(nil), "sha256:../../etc", t.TempDir(), (&fakeSpace{budget: 1}).Hold); err == nil {
			t.Fatal("accepted")
		}
	})
}

// R-VULN-044, Q-VULN-171: the tag of the database's image is the schema version this release reads, and no setting.
func TestR_VULN_044_TheTagIsTheSchemaVersion(t *testing.T) {
	if DatabaseTag != fmt.Sprint(dbSchemaVersion) {
		t.Fatalf("tag %q, schema version %d", DatabaseTag, dbSchemaVersion)
	}
}
