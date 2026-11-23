package offline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// PullTotals count what a pull copied.
type PullTotals struct {
	Objects int   `json:"objects"`
	Bytes   int64 `json:"bytes"`
	// Skipped are the objects below the families whose name is not of the layout: they are not written to the directory.
	Skipped int `json:"skipped"`
}

// Pull copies every object below keyed/, sha256/ and pool/sha256/ of the source's prefix into the directory dir, as stored, with
// each object's metadata beside it as <name>.record.json (R-STOR-089 pull, H-1303), and the source's facts in quader-pull.json. It
// needs no key and opens nothing. dir must be empty or not exist; every file is created new (mode 0600, directories 0700), so a pull
// never writes over a file. An object whose name is not of the layout is skipped and counted, so no name leads out of dir. emit is
// called after each object.
func Pull(ctx context.Context, src Source, dir string, now time.Time, emit func(name string, size int64) error) (PullTotals, error) {
	var t PullTotals
	if err := emptyDir(dir); err != nil {
		return t, err
	}
	m, err := json.MarshalIndent(manifest{Format: manifestFormat, PulledAt: now.UTC().Truncate(time.Second), Info: src.Info()}, "", "  ")
	if err != nil {
		return t, err
	}
	if err := writeNew(filepath.Join(dir, ManifestFile), func(w io.Writer) (int64, error) {
		n, err := w.Write(append(m, '\n'))
		return int64(n), err
	}); err != nil {
		return t, err
	}
	prefix := src.Info().Prefix
	for _, f := range families() {
		err := src.Walk(ctx, base(prefix)+f.root, func(o Object) error {
			if _, ok := parseName(prefix, o.Name); !ok || !filepath.IsLocal(filepath.FromSlash(o.Name)) {
				t.Skipped++
				return nil
			}
			n, err := pullOne(ctx, src, dir, o.Name)
			if err != nil {
				return err
			}
			t.Objects++
			t.Bytes += n
			return emit(o.Name, n)
		})
		if err != nil {
			return t, err
		}
	}
	return t, nil
}

// pullOne writes one object and its record file.
func pullOne(ctx context.Context, src Source, dir, name string) (int64, error) {
	obj, md, err := src.Head(ctx, name)
	if err != nil {
		return 0, err
	}
	body, err := src.Open(ctx, name)
	if err != nil {
		return 0, err
	}
	defer func() { _ = body.Close() }()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return 0, err
	}
	var n int64
	if err := writeNew(p, func(w io.Writer) (int64, error) {
		var err error
		n, err = io.Copy(w, body)
		return n, err
	}); err != nil {
		return 0, err
	}
	if n != obj.Size {
		return 0, fmt.Errorf("the object %s has %d bytes and %d were read: it changed during the pull; pull again into an empty directory", name, obj.Size, n)
	}
	if md == nil {
		md = map[string]string{}
	}
	rec, err := json.MarshalIndent(md, "", "  ")
	if err != nil {
		return 0, err
	}
	return n, writeNew(p+RecordSuffix, func(w io.Writer) (int64, error) {
		k, err := w.Write(append(rec, '\n'))
		return int64(k), err
	})
}

// emptyDir makes dir, or accepts it when it is an empty directory.
func emptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return os.MkdirAll(dir, 0o700)
	case err != nil:
		return refuse("--to %s cannot be read: %v", dir, err)
	case len(entries) > 0:
		return refuse("--to %s is not empty; a pull writes into an empty or new directory, so that nothing of another pull is mixed in", dir)
	}
	return nil
}

// writeNew creates the file p, which must not exist, writes it with fill and closes it; a failure removes it.
func writeNew(p string, fill func(io.Writer) (int64, error)) error {
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // G304: a path below the directory the operator named, from a name of the layout
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return refuse("%s exists; the offline tool writes no file over another", p)
		}
		return err
	}
	_, err = fill(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(p)
	}
	return err
}
