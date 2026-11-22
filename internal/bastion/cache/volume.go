package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// staleSpool is how old a leftover of an interrupted fill must be before it is removed at start.
const staleSpool = time.Hour

// volumeStore keeps the layout in a local directory (R-BAS-030). Fills are spooled to <dir>/tmp, on the same
// filesystem, and renamed to their content address after verification, so a reader never sees a partial object.
type volumeStore struct {
	root string
	tmp  string
	now  func() time.Time
}

var _ store = (*volumeStore)(nil)

func newVolumeStore(dir string, now func() time.Time) (*volumeStore, error) {
	if dir == "" {
		return nil, errors.New("cache: the volume directory is empty")
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	v := &volumeStore{root: root, tmp: filepath.Join(root, "tmp"), now: now}
	if err := os.MkdirAll(v.tmp, 0o750); err != nil {
		return nil, fmt.Errorf("cache: prepare the volume %s: %w", root, err)
	}
	removeStale(v.tmp, now())
	return v, nil
}

// prepareSpool returns <tmpRoot>/quader-bastion-cache, the spool of fills in the pod's temporary directory (os.TempDir()
// for an empty tmpRoot), created and swept of the leftovers of fills that never finished.
func prepareSpool(tmpRoot string, now time.Time) (string, error) {
	if tmpRoot == "" {
		tmpRoot = os.TempDir()
	}
	tmp := filepath.Join(tmpRoot, "quader-bastion-cache")
	if err := os.MkdirAll(tmp, 0o750); err != nil {
		return "", fmt.Errorf("cache: prepare the spool directory %s: %w", tmp, err)
	}
	removeStale(tmp, now)
	return tmp, nil
}

// removeStale deletes spooled files of fills that never finished.
func removeStale(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && !e.IsDir() && now.Sub(info.ModTime()) > staleSpool {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func (v *volumeStore) tmpDir() string { return v.tmp }

func (v *volumeStore) path(key string) string { return filepath.Join(v.root, filepath.FromSlash(key)) }

func mapFS(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	return err
}

func (v *volumeStore) putFile(ctx context.Context, key, name string, _ int64, _ []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dest := v.path(key)
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return err
	}
	return os.Rename(name, dest)
}

func (v *volumeStore) putBytes(ctx context.Context, key string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.CreateTemp(v.tmp, "put-*")
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	if err := v.putFile(ctx, key, f.Name(), int64(len(data)), nil); err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	return nil
}

// putStream writes the sealed object to a file of <dir>/tmp and renames it into place, so a reader never sees a partial
// object; the file holds ciphertext only.
func (v *volumeStore) putStream(ctx context.Context, key string, rs io.ReadSeeker, size int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.CreateTemp(v.tmp, "seal-*")
	if err != nil {
		return err
	}
	n, err := io.Copy(f, rs)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n != size {
		err = fmt.Errorf("cache: the sealed object has %d bytes, not %d", n, size)
	}
	if err == nil {
		err = v.putFile(ctx, key, f.Name(), size, nil)
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}
	return err
}

type fileReader struct {
	io.Reader
	f *os.File
}

func (r fileReader) Close() error { return r.f.Close() }

func (v *volumeStore) get(ctx context.Context, key string, offset, length int64) (io.ReadCloser, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	f, err := os.Open(v.path(key))
	if err != nil {
		return nil, 0, mapFS(err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	if offset == 0 && length < 0 {
		return f, info.Size(), nil
	}
	n := info.Size() - offset
	if length >= 0 {
		n = min(n, length)
	}
	return fileReader{Reader: io.NewSectionReader(f, offset, max(n, 0)), f: f}, info.Size(), nil
}

func (v *volumeStore) stat(ctx context.Context, key string) (int64, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return 0, time.Time{}, err
	}
	info, err := os.Stat(v.path(key))
	if err != nil {
		return 0, time.Time{}, mapFS(err)
	}
	if !info.Mode().IsRegular() {
		return 0, time.Time{}, ErrNotFound
	}
	return info.Size(), info.ModTime(), nil
}

func (v *volumeStore) list(ctx context.Context, prefix string, fn func(string, int64, time.Time) error) error {
	if prefix == "" || strings.HasSuffix(prefix, "/") {
		return v.walk(ctx, v.path(strings.TrimSuffix(prefix, "/")), fn)
	}
	// A prefix that is no directory (the trees bc-…, R-BAS-061): the entries of its directory whose names start with it.
	dir, stem := path.Split(prefix)
	entries, err := os.ReadDir(v.path(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), stem) {
			if err := v.walk(ctx, v.path(dir+e.Name()), fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// walk calls fn for every regular file below top, top itself included when it is one.
func (v *volumeStore) walk(ctx context.Context, top string, fn func(string, int64, time.Time) error) error {
	err := filepath.WalkDir(top, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil // vanished, or the directory was never created
			}
			return err
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil // deleted while listing
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(v.root, p)
		if err != nil {
			return err
		}
		return fn(filepath.ToSlash(rel), info.Size(), info.ModTime())
	})
	return err
}

func (v *volumeStore) remove(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(v.path(key)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (v *volumeStore) touch(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, now := v.path(key), v.now()
	err := os.Chtimes(p, now, now)
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0o640) //nolint:gosec // p is built from a validated key below the volume
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chtimes(p, now, now)
}
