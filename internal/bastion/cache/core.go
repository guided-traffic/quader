package cache

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/syncwrite"
)

// store is the keyed object storage a [Backend] is built on: the volume (a directory) and the bucket (S3 with
// five operations). Keys are the names of a [layout] and are built only from validated parts. A missing key is
// [ErrNotFound] everywhere.
type store interface {
	// tmpDir is an existing directory for spooled fills; on the volume it is on the same filesystem as the
	// objects, so that a fill is committed by a rename.
	tmpDir() string
	// putFile stores the spooled file at path under key and removes it from the spool when it can (volume); the
	// caller removes what is left. sum is the raw SHA-256 of the bytes.
	putFile(ctx context.Context, key, path string, size int64, sum []byte) error
	putBytes(ctx context.Context, key string, b []byte) error
	// putStream stores the size bytes of rs under key, the sealed objects of R-BAS-061. rs yields the same bytes
	// after a seek to its start, so the bucket reads it once for the payload checksum and once to send it.
	putStream(ctx context.Context, key string, rs io.ReadSeeker, size int64) error
	// get opens the object, or length bytes from offset (length -1 is to the end), and returns the size of the
	// whole object, or -1 when the store did not say.
	get(ctx context.Context, key string, offset, length int64) (io.ReadCloser, int64, error)
	stat(ctx context.Context, key string) (size int64, modified time.Time, err error)
	// list calls fn for every object below prefix; fn may return an error to stop. A prefix that ends in a slash
	// is a directory of the layout; one that does not names every object whose name starts with it.
	list(ctx context.Context, prefix string, fn func(key string, size int64, modified time.Time) error) error
	remove(ctx context.Context, key string) error
	// touch sets the modification time of key to now, creating an empty object when it does not exist.
	touch(ctx context.Context, key string) error
}

// maxEntryBytes bounds the read of an entry record, maxSpoolBytes the read of a spool file.
const (
	maxEntryBytes = 1 << 20
	maxSpoolBytes = 256 << 20
)

// backend implements [Backend] over a [store]. It counts the errors of the store in
// quader_bastion_cache_errors_total{op} (R-BAS-030); a cancelled context, a missing key and a refused object are
// not errors.
type backend struct {
	s         store
	now       func() time.Time
	count     func(op string)
	lastSpool atomic.Int64
	// lay names the objects: R-BAS-030's layout, or R-BAS-061's below the tree of the cache key. seal is the
	// cache key, nil for a cache without one.
	lay  layout
	seal *sealer
	// spoolDir is where fills and puts are spooled before they are committed: the store's tmpDir, or for a
	// volume with a cache key the pod's temporary directory, so that the cache volume holds no plaintext
	// (Q-BAS-214).
	spoolDir string
	// onRefused counts and logs a file of the audit spool that the stored format refused and that was deleted.
	onRefused func(key string, err error)
}

var _ Backend = (*backend)(nil)

// sealWith makes the backend keep R-BAS-061's layout under the cache key s, spooling its fills to spoolDir.
func (b *backend) sealWith(s *sealer, spoolDir string) {
	b.seal, b.lay, b.spoolDir = s, newSealedLayout(s), spoolDir
}

func (b *backend) fail(op string, err error) error {
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalid) || errors.Is(err, context.Canceled) ||
		errors.Is(err, ErrCorrupt) || errors.Is(err, ErrBadSignature) {
		return err
	}
	if b.count != nil {
		b.count(op)
	}
	return err
}

// Check implements [Backend]: it writes, reads and deletes a probe object, with a cache key one of the stored
// format bound to its name, so that the first check also proves the key seals and opens.
func (b *backend) Check(ctx context.Context) error {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	key := b.lay.probe(hex.EncodeToString(id[:]))
	payload := []byte("quader-bastion-cache-probe " + hex.EncodeToString(id[:]))
	put, read := b.s.putBytes, func(ctx context.Context, key string) ([]byte, error) { return readAll(ctx, b.s, key, 1<<10) }
	if b.seal != nil {
		put, read = b.putSealedBytes, func(ctx context.Context, key string) ([]byte, error) { return b.readSealedBytes(ctx, key, 1<<10) }
	}
	if err := put(ctx, key, payload); err != nil {
		return fmt.Errorf("cache check: write: %w", err)
	}
	got, err := read(ctx, key)
	if err != nil {
		_ = b.s.remove(ctx, key)
		return fmt.Errorf("cache check: read: %w", err)
	}
	if !bytes.Equal(got, payload) {
		_ = b.s.remove(ctx, key)
		return errors.New("cache check: the probe read back differs from what was written")
	}
	if err := b.s.remove(ctx, key); err != nil {
		return fmt.Errorf("cache check: delete: %w", err)
	}
	return nil
}

func readAll(ctx context.Context, s store, key string, limit int64) ([]byte, error) {
	rc, _, err := s.get(ctx, key, 0, -1)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return readLimited(rc, key, limit)
}

// readLimited reads r to its end, refusing more than limit bytes.
func readLimited(r io.Reader, key string, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("cache: %s is larger than %d bytes", key, limit)
	}
	return data, nil
}

// spool copies r into a file of the spool directory and returns its path and digest. It reads at most size+1
// bytes, so an oversize source is told from an exact one.
func (b *backend) spool(r io.Reader, size int64) (name string, sum [32]byte, n int64, err error) {
	f, err := os.CreateTemp(b.spoolDir, "fill-*")
	if err != nil {
		return "", sum, 0, err
	}
	h := sha256.New()
	n, err = syncwrite.Copy(f, io.TeeReader(io.LimitReader(r, size+1), h), syncwrite.Every) // flushed every 32 MiB (Q-STOR-486)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", sum, n, err
	}
	h.Sum(sum[:0])
	return f.Name(), sum, n, nil
}

// PutObject implements [Backend]. It spools the bytes, checks them against sha256 and size once more (the
// caller has verified them, a cheap second check keeps a wrong object from ever being committed), then commits.
func (b *backend) PutObject(ctx context.Context, sha string, size int64, r io.Reader) error {
	if _, err := b.lay.object(areaOf(ctx), sha); err != nil {
		return err
	}
	if size < 0 {
		return fmt.Errorf("%w: size %d", ErrInvalid, size)
	}
	name, sum, n, err := b.spool(r, size)
	if err != nil {
		return b.fail(metrics.OpPut, err)
	}
	defer func() { _ = os.Remove(name) }()
	if n != size || hex.EncodeToString(sum[:]) != sha {
		return fmt.Errorf("%w: read %d bytes with SHA-256 %s, expected %d bytes with %s", ErrMismatch, n, hex.EncodeToString(sum[:]), size, sha)
	}
	return b.commitFile(ctx, sha, size, name, sum[:])
}

// commitFile stores a spooled, verified file under its content address: as it is, or with a cache key as an
// object of the stored format under a data key of its own (R-BAS-061).
func (b *backend) commitFile(ctx context.Context, sha string, size int64, name string, sum []byte) error {
	key, err := b.lay.object(areaOf(ctx), sha)
	if err != nil {
		return err
	}
	if b.seal != nil {
		return b.fail(metrics.OpPut, b.putSealedFile(ctx, key, name, size, sum))
	}
	return b.fail(metrics.OpPut, b.s.putFile(ctx, key, name, size, sum))
}

// GetObject implements [Backend]. With a cache key it returns the plaintext of the object, every segment
// authenticated before its bytes leave; a refusal is [ErrCorrupt], before the first byte or from Read.
func (b *backend) GetObject(ctx context.Context, sha string, offset, length int64) (io.ReadCloser, error) {
	key, err := b.lay.object(areaOf(ctx), sha)
	if err != nil {
		return nil, err
	}
	if offset < 0 || length < -1 {
		return nil, fmt.Errorf("%w: range %d,%d", ErrInvalid, offset, length)
	}
	if b.seal != nil {
		rc, err := b.getSealed(ctx, key, sha, offset, length)
		return rc, b.fail(metrics.OpGet, err)
	}
	rc, _, err := b.s.get(ctx, key, offset, length)
	return rc, b.fail(metrics.OpGet, err)
}

// HeadObject implements [Backend]. With a cache key the size is the plaintext's, from the stored size; a stored
// size that no plaintext size gives is [ErrCorrupt].
func (b *backend) HeadObject(ctx context.Context, sha string) (ObjectInfo, error) {
	key, err := b.lay.object(areaOf(ctx), sha)
	if err != nil {
		return ObjectInfo{}, err
	}
	size, mod, err := b.s.stat(ctx, key)
	if err != nil {
		return ObjectInfo{}, b.fail(metrics.OpGet, err)
	}
	if b.seal != nil {
		if size, err = b.plainSize(size); err != nil {
			return ObjectInfo{}, err
		}
	}
	return ObjectInfo{Size: size, LastAccess: mod}, nil
}

// DeleteObject implements [Backend]: the bytes and their marker.
func (b *backend) DeleteObject(ctx context.Context, sha string) error {
	obj, err := b.lay.object(areaOf(ctx), sha)
	if err != nil {
		return err
	}
	mk, err := b.lay.marker(areaOf(ctx), sha)
	if err != nil {
		return err
	}
	return b.removeNamed(ctx, obj, mk)
}

// removeNamed removes an object and its marker.
func (b *backend) removeNamed(ctx context.Context, obj, marker string) error {
	if err := b.s.remove(ctx, obj); err != nil {
		return b.fail(metrics.OpDelete, err)
	}
	return b.fail(metrics.OpDelete, b.s.remove(ctx, marker))
}

// PutEntry implements [Backend]. With a cache key the record is an object of the stored format, bound to the
// entry's name (Q-BAS-213).
func (b *backend) PutEntry(ctx context.Context, keyHash string, record []byte) error {
	key, err := b.lay.entry(keyHash)
	if err != nil {
		return err
	}
	if b.seal != nil {
		return b.fail(metrics.OpPut, b.putSealedBytes(ctx, key, record))
	}
	return b.fail(metrics.OpPut, b.s.putBytes(ctx, key, record))
}

// GetEntry implements [Backend]. With a cache key an entry the stored format refuses, or one bound to another
// name, is [ErrBadSignature]: the caller deletes it as an entry that fails its signature.
func (b *backend) GetEntry(ctx context.Context, keyHash string) ([]byte, error) {
	key, err := b.lay.entry(keyHash)
	if err != nil {
		return nil, err
	}
	if b.seal != nil {
		data, err := b.readSealedBytes(ctx, key, maxEntryBytes)
		if err != nil && refused(err) {
			err = fmt.Errorf("%w: %w", ErrBadSignature, err)
		}
		return data, b.fail(metrics.OpGet, err)
	}
	data, err := readAll(ctx, b.s, key, maxEntryBytes)
	return data, b.fail(metrics.OpGet, err)
}

// DeleteEntry implements [Backend].
func (b *backend) DeleteEntry(ctx context.Context, keyHash string) error {
	key, err := b.lay.entry(keyHash)
	if err != nil {
		return err
	}
	return b.fail(metrics.OpDelete, b.s.remove(ctx, key))
}

// Touch implements [Backend]: the marker of the content address in the area of ctx ([WithArea]).
func (b *backend) Touch(ctx context.Context, sha string) error {
	key, err := b.lay.marker(areaOf(ctx), sha)
	if err != nil {
		return err
	}
	return b.fail(metrics.OpPut, b.s.touch(ctx, key))
}

// Usage implements [Backend]. With a cache key the bytes are the stored ones, which are what the store holds.
func (b *backend) Usage(ctx context.Context) (Usage, error) {
	var u Usage
	for _, a := range []struct {
		dir string
		sum *int64
	}{{b.lay.areaDir(EntryArtifact), &u.Artifacts}, {b.lay.areaDir(EntryDocument), &u.Documents}} {
		err := b.s.list(ctx, a.dir, func(key string, size int64, _ time.Time) error {
			if validHash(base(key)) {
				*a.sum += size
			}
			return nil
		})
		if err != nil {
			return Usage{}, b.fail(metrics.OpList, err)
		}
	}
	return u, nil
}

type held struct {
	base string
	size int64
	last time.Time
}

// Sweep implements [Backend] (R-BAS-031, Q-BAS-43): two listings, the area's bytes and the access markers; when
// the area holds more than high bytes it deletes by last access, oldest first, until at most target remain. The
// last access of an object is the later of its marker and its own time, so a fill counts as an access. Objects
// and markers are matched by their base name, which is the content address in the plain layout and the keyed
// name with a cache key.
func (b *backend) Sweep(ctx context.Context, area string, high, target int64) (int, error) {
	kind, err := areaKind(area)
	if err != nil {
		return 0, err
	}
	var items []held
	var total int64
	err = b.s.list(ctx, b.lay.areaDir(kind), func(key string, size int64, mod time.Time) error {
		if name := base(key); validHash(name) {
			items = append(items, held{base: name, size: size, last: mod})
			total += size
		}
		return nil
	})
	if err != nil {
		return 0, b.fail(metrics.OpList, err)
	}
	if total <= high {
		return 0, nil
	}
	marks := map[string]time.Time{}
	err = b.s.list(ctx, b.lay.markerDir(), func(key string, _ int64, mod time.Time) error {
		if name := base(key); validHash(name) {
			marks[name] = mod
		}
		return nil
	})
	if err != nil {
		return 0, b.fail(metrics.OpList, err)
	}
	for i := range items {
		if m, ok := marks[items[i].base]; ok && m.After(items[i].last) {
			items[i].last = m
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].last.Equal(items[j].last) {
			return items[i].last.Before(items[j].last)
		}
		return items[i].base < items[j].base
	})
	var evicted int
	var errs []error
	for _, it := range items {
		if total <= target {
			break
		}
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if err := b.removeNamed(ctx, b.lay.objectOf(kind, it.base), b.lay.markerOf(it.base)); err != nil {
			errs = append(errs, err)
			continue
		}
		total -= it.size
		evicted++
	}
	return evicted, errors.Join(errs...)
}

// putConfiguration stores the configuration record (R-BAS-093), with a cache key as an object of the stored format bound to
// its name.
func (b *backend) putConfiguration(ctx context.Context, record []byte) error {
	key := b.lay.configuration()
	if b.seal != nil {
		return b.fail(metrics.OpPut, b.putSealedBytes(ctx, key, record))
	}
	return b.fail(metrics.OpPut, b.s.putBytes(ctx, key, record))
}

// getConfiguration reads the configuration record. With a cache key a record the stored format refuses, or one bound to
// another name, is [ErrBadSignature].
func (b *backend) getConfiguration(ctx context.Context) ([]byte, error) {
	key := b.lay.configuration()
	if b.seal != nil {
		data, err := b.readSealedBytes(ctx, key, maxEntryBytes)
		if err != nil && refused(err) {
			err = fmt.Errorf("%w: %w", ErrBadSignature, err)
		}
		return data, b.fail(metrics.OpGet, err)
	}
	data, err := readAll(ctx, b.s, key, maxEntryBytes)
	return data, b.fail(metrics.OpGet, err)
}

// SpoolWrite implements [Backend]: audit-spool/<replica>/<unix ms>.jsonl, or with a cache key an object of the
// stored format at s/<replica>/<unix ms> bound to its name. The name is the time of the write, raised by one
// millisecond where two writes of this process fall in the same one.
func (b *backend) SpoolWrite(ctx context.Context, replicaName string, jsonl []byte) error {
	for {
		last := b.lastSpool.Load()
		ms := max(b.now().UnixMilli(), last+1)
		if !b.lastSpool.CompareAndSwap(last, ms) {
			continue
		}
		key, err := b.lay.spool(replicaName, b.lay.spoolName(ms))
		if err != nil {
			return err
		}
		if b.seal != nil {
			return b.fail(metrics.OpSpool, b.putSealedBytes(ctx, key, jsonl))
		}
		return b.fail(metrics.OpSpool, b.s.putBytes(ctx, key, jsonl))
	}
}

// SpoolList implements [Backend], oldest first.
func (b *backend) SpoolList(ctx context.Context) ([]SpoolFile, error) {
	var out []SpoolFile
	dir := b.lay.spoolDir()
	err := b.s.list(ctx, dir, func(key string, size int64, _ time.Time) error {
		parts := strings.Split(strings.TrimPrefix(key, dir), "/")
		if len(parts) != 2 {
			return nil
		}
		if _, err := b.lay.spool(parts[0], parts[1]); err == nil {
			out = append(out, SpoolFile{Replica: parts[0], Name: parts[1], Size: size})
		}
		return nil
	})
	if err != nil {
		return nil, b.fail(metrics.OpList, err)
	}
	sort.Slice(out, func(i, j int) bool {
		a, c := spoolMs(out[i].Name), spoolMs(out[j].Name)
		if a != c {
			return a < c
		}
		return out[i].Replica < out[j].Replica
	})
	return out, nil
}

func spoolMs(name string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSuffix(path.Base(name), ".jsonl"), 10, 64)
	return n
}

// SpoolRead implements [Backend]. With a cache key a file the stored format refuses, or one bound to another
// name, is deleted, counted as corrupt and read as [ErrNotFound]: whoever wrote it had no key.
func (b *backend) SpoolRead(ctx context.Context, f SpoolFile) ([]byte, error) {
	key, err := b.lay.spool(f.Replica, f.Name)
	if err != nil {
		return nil, err
	}
	if b.seal == nil {
		data, err := readAll(ctx, b.s, key, maxSpoolBytes)
		return data, b.fail(metrics.OpSpool, err)
	}
	data, err := b.readSealedBytes(ctx, key, maxSpoolBytes)
	if err != nil && refused(err) {
		_ = b.s.remove(ctx, key)
		if b.onRefused != nil {
			b.onRefused(key, err)
		}
		return nil, ErrNotFound
	}
	return data, b.fail(metrics.OpSpool, err)
}

// SpoolDelete implements [Backend].
func (b *backend) SpoolDelete(ctx context.Context, f SpoolFile) error {
	key, err := b.lay.spool(f.Replica, f.Name)
	if err != nil {
		return err
	}
	return b.fail(metrics.OpSpool, b.s.remove(ctx, key))
}
