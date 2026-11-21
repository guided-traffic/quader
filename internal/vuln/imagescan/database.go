package imagescan

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// The bounds of the database archive: only its two files are taken, each within a size limit (R-VULN-035).
const (
	maxDBFileBytes  = int64(8) << 30
	maxMetaBytes    = int64(1) << 20
	dbFileName      = "trivy.db"
	dbMetaName      = "metadata.json"
	dbSchemaVersion = 2
)

// DBMetadata is the part of the archive's metadata.json that decides freshness.
type DBMetadata struct {
	Version    int       `json:"Version"`
	UpdatedAt  time.Time `json:"UpdatedAt"`
	NextUpdate time.Time `json:"NextUpdate"`
}

// ReadDBMetadata reads metadata.json from the archive and checks its schema version. The archive is read only as far as that file.
func ReadDBMetadata(archive io.Reader) (DBMetadata, error) {
	var meta DBMetadata
	err := walkArchive(archive, func(name string, _ int64, r io.Reader) (bool, error) {
		if name != dbMetaName {
			return false, nil
		}
		dec := json.NewDecoder(io.LimitReader(r, maxMetaBytes))
		if err := dec.Decode(&meta); err != nil {
			return false, fmt.Errorf("metadata.json: %w", err)
		}
		return true, nil
	})
	if err != nil {
		return DBMetadata{}, err
	}
	if meta.Version != dbSchemaVersion {
		return DBMetadata{}, fmt.Errorf("the database has schema version %d, this release reads %d", meta.Version, dbSchemaVersion)
	}
	if meta.UpdatedAt.IsZero() {
		return DBMetadata{}, errors.New("the database's metadata.json names no build time")
	}
	return meta, nil
}

// walkArchive reads a gzip tar and calls fn for each regular file at the root that the archive is expected to hold; fn returns true to stop.
// Any other kind of entry is skipped, never written anywhere, and a name with a directory in it is ignored, so that the archive decides
// nothing about a path (GHSA-mcj4-mphf-j9ff was a database archive that could write outside the cache directory).
func walkArchive(archive io.Reader, fn func(name string, size int64, r io.Reader) (bool, error)) error {
	zr, err := gzip.NewReader(archive)
	if err != nil {
		return fmt.Errorf("the database archive is not gzip: %w", err)
	}
	defer func() { _ = zr.Close() }()
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("the database archive: %w", err)
		}
		if h.Typeflag != tar.TypeReg || (h.Name != dbFileName && h.Name != dbMetaName) {
			continue
		}
		done, err := fn(h.Name, h.Size, tr)
		if err != nil || done {
			return err
		}
	}
}

// DatabaseTag is the tag of the database's image: the schema version of the database this release reads (R-VULN-044, Q-VULN-171). It
// is no setting, so that no configuration can point the analyser at a schema it cannot read; TestR_VULN_044_TheTagIsTheSchemaVersion
// holds it to dbSchemaVersion.
const DatabaseTag = "2"

// LayerMediaType is the media type of the database's layer in its image's manifest (R-VULN-044).
const LayerMediaType = "application/vnd.aquasec.trivy.db.layer.v1.tar+gzip"

// ErrLayerMissing says that the database's repository does not hold the layer of the database in use: a holder of delete removed it by
// hand, or the setting names another repository now (R-VULN-044). The scan acts as before the first database and wakes the task
// vuln-sync-trivy-db, which makes the repository hold it again or records why it cannot.
var ErrLayerMissing = errors.New("the database's repository does not hold the layer of the database in use")

// errLayerDigest is a layer whose bytes are not the digest in use: nothing of it is kept.
var errLayerDigest = errors.New("the layer of the database is not the digest in use")

// Database names the database in use for a run of the scan job (R-VULN-044, R-VULN-048): the digest of its layer, "sha256:<64 hex>",
// after checking that the database's repository holds the item <image>/_blobs/<digest>. The worker reads no byte of it: the loader of
// every scanner pod reads it through the scan listener (R-VULN-049). It returns [ErrNoDatabase] while none has been imported, and
// [ErrLayerMissing] when the repository no longer holds the layer; the run then analyses only and keeps the rows pending.
type Database interface {
	InUse(ctx context.Context) (string, error)
}

// ErrNoDatabase says that no vulnerability database has been imported yet.
var ErrNoDatabase = errors.New("no vulnerability database has been imported")

// DatabaseListing is the answer of the scan listener's listing of the image scanning's database, GET /scan/v1/trivy-database
// (R-VULN-049): the database in use, the size of its layer as the repository holds it and the build time of its metadata.json; an empty
// digest before the first, and no size while the repository does not hold the layer, no build time for a database recorded without one.
type DatabaseListing struct {
	Digest  string     `json:"digest"`
	Size    int64      `json:"size,omitempty"`
	BuiltAt *time.Time `json:"builtAt,omitempty"`
}

// DatabaseDir is the directory below a database's own directory that holds its two files: the analyser's --cache-dir is the database's
// directory, and the analyser reads <cache-dir>/db/trivy.db and metadata.json.
const DatabaseDir = "db"

// DatabaseIn reads the metadata of the database whose directory is dir (<images.database.dir>/<64 hex>): found is false while the
// directory holds no complete database. A loader renames the directory into place only when it is complete (R-VULN-049), so a
// metadata.json there is the mark of a complete copy.
func DatabaseIn(dir string) (DBMetadata, bool) {
	f, err := os.Open(filepath.Join(dir, DatabaseDir, dbMetaName)) //nolint:gosec // a path below the database directory, named by its digest
	if err != nil {
		return DBMetadata{}, false
	}
	defer func() { _ = f.Close() }()
	var meta DBMetadata
	if err := json.NewDecoder(io.LimitReader(f, maxMetaBytes)).Decode(&meta); err != nil || meta.Version != dbSchemaVersion {
		return DBMetadata{}, false
	}
	return meta, true
}

// LoadedAt is when the loader wrote the database whose directory is dir: the modification time of its metadata.json, which UnpackDatabase
// writes and a rename keeps. The newest database of a pod is the one loaded last, which is the one the listing named last, not the one
// built last: a database in use may be older than one the pod held before, when the repository's tag moves back (Q-VULN-213).
func LoadedAt(dir string) (time.Time, bool) {
	fi, err := os.Stat(filepath.Join(dir, DatabaseDir, dbMetaName))
	if err != nil {
		return time.Time{}, false
	}
	return fi.ModTime(), true
}

// Unpacked is a database UnpackDatabase wrote: the room its files hold, the function that gives the room back, and its metadata.
type Unpacked struct {
	Bytes    int64
	Release  func()
	Metadata DBMetadata
}

// UnpackDatabase reads a layer of the analyser's database from r and writes its two files below dir/db, hashing the stream while it
// unpacks it (R-VULN-035, R-VULN-049): only the regular files trivy.db and metadata.json at the archive's root are taken, each within its
// bound, and an entry with a directory in its name or of another type is ignored, so the archive decides no path. The room of a file is
// taken with hold from its size in the archive's header before its first byte is written, and the copy is bounded by that size. The bytes
// must be digest and metadata.json of the schema this release reads with a build time; otherwise nothing of the room stays held, and the
// caller removes dir. dir is a directory of the caller's that it renames into place once this returns without an error.
func UnpackDatabase(ctx context.Context, r io.Reader, digest, dir string, hold func(n int64) (func(), error)) (Unpacked, error) {
	if !ValidDigest(digest) {
		return Unpacked{}, fmt.Errorf("the database names %q, which is no sha256 digest", digest)
	}
	if err := os.MkdirAll(filepath.Join(dir, DatabaseDir), 0o700); err != nil {
		return Unpacked{}, err
	}
	sum := sha256.New()
	body := io.TeeReader(contextReader{ctx, r}, sum)
	seen := map[string]bool{}
	out := Unpacked{}
	keep := false
	defer func() {
		if !keep && out.Release != nil {
			out.Release()
		}
	}()
	err := walkArchive(body, func(name string, size int64, r io.Reader) (bool, error) {
		if seen[name] {
			return false, fmt.Errorf("the database archive holds %s twice", name)
		}
		seen[name] = true
		if size > maxDBFileBytes || (name == dbMetaName && size > maxMetaBytes) {
			return false, fmt.Errorf("the file %s of the database is %d bytes, more than its bound", name, size)
		}
		release, err := hold(size)
		if err != nil {
			return false, fmt.Errorf("room for %s (%d bytes): %w", name, size, err)
		}
		prev := out.Release
		out.Bytes += size
		out.Release = func() {
			release()
			if prev != nil {
				prev()
			}
		}
		return false, writeDBFile(filepath.Join(dir, DatabaseDir, name), r, size)
	})
	if err != nil {
		return Unpacked{}, err
	}
	if !seen[dbFileName] || !seen[dbMetaName] {
		return Unpacked{}, errors.New("the database archive does not hold trivy.db and metadata.json")
	}
	if _, err := io.Copy(io.Discard, body); err != nil { // the rest of the gzip stream, so that the hash covers every byte
		return Unpacked{}, fmt.Errorf("the database's layer: %w", err)
	}
	if got := "sha256:" + hex.EncodeToString(sum.Sum(nil)); got != digest {
		return Unpacked{}, fmt.Errorf("%w: the bytes read are %s, the database is %s", errLayerDigest, got, digest)
	}
	meta, ok := DatabaseIn(dir)
	if !ok {
		return Unpacked{}, fmt.Errorf("the database's metadata.json is not of schema version %d", dbSchemaVersion)
	}
	if meta.UpdatedAt.IsZero() {
		return Unpacked{}, errors.New("the database's metadata.json names no build time")
	}
	out.Metadata = meta
	keep = true
	return out, nil
}

// ErrLayerDigest reports whether err is a layer whose bytes are not the digest it was read under.
func ErrLayerDigest(err error) bool { return errors.Is(err, errLayerDigest) }

// syncEvery is how many bytes of a database file are written before they are flushed to the disk. The kernel charges the dirty pages
// of a write to the writer's memory cgroup and, on a node whose file system does not write them back per cgroup, starts the write-back
// only at the machine's thresholds: the loader's container of 256 MiB was killed with 233 MB of dirty pages and none under write-back
// while it unpacked a database of 1.8 GB (measured on the kind cluster on 2026-11-21, Q-VULN-212). A flush every 32 MiB bounds them.
const syncEvery = 32 << 20

func writeDBFile(path string, r io.Reader, size int64) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // a path below the caller's directory, named by this package
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	src := io.LimitReader(r, size+1)
	var n int64
	for {
		c, err := io.CopyN(f, src, syncEvery)
		n += c
		if c > 0 {
			if serr := f.Sync(); serr != nil {
				return serr
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	if n != size {
		return fmt.Errorf("a file of the database is not the size its header names (%d of %d bytes)", n, size)
	}
	return f.Close()
}
