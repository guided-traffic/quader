package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/blob/s3"
)

// listPage is the page size of a listing of the bucket.
const listPage = 1000

// bucketStore keeps the layout in the Bastion's own bucket below a prefix (R-BAS-030). Of S3 it uses PutObject,
// GetObject, HeadObject, ListObjectsV2 and DeleteObject, without versioning and without a conditional write; a
// fill is spooled to the pod's temporary directory and uploaded after verification.
type bucketStore struct {
	c      *s3.Client
	prefix string
	tmp    string
}

var _ store = (*bucketStore)(nil)

func newBucketStore(c *s3.Client, prefix, tmpRoot string, now time.Time) (*bucketStore, error) {
	tmp, err := prepareSpool(tmpRoot, now)
	if err != nil {
		return nil, err
	}
	return &bucketStore{c: c, prefix: strings.Trim(prefix, "/"), tmp: tmp}, nil
}

func (s *bucketStore) tmpDir() string { return s.tmp }

func (s *bucketStore) full(key string) string {
	if s.prefix == "" {
		return key
	}
	return s.prefix + "/" + key
}

func mapS3(err error) error {
	if s3.IsNotFound(err) {
		return ErrNotFound
	}
	return err
}

// putFile uploads the spooled file; the spool file stays for the caller to remove.
func (s *bucketStore) putFile(ctx context.Context, key, name string, size int64, sum []byte) error {
	f, err := os.Open(name) //nolint:gosec // a file the cache spooled itself
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return s.c.PutPlain(ctx, s.full(key), f, size, sum)
}

func (s *bucketStore) putBytes(ctx context.Context, key string, data []byte) error {
	sum := sha256.Sum256(data)
	return s.c.PutPlain(ctx, s.full(key), bytes.NewReader(data), int64(len(data)), sum[:])
}

// putStream uploads the sealed object with the SHA-256 of its stored bytes as the payload checksum: one pass to hash it, a
// seek to the start, and the pass that sends it (the bytes are the same, R-STOR-080).
func (s *bucketStore) putStream(ctx context.Context, key string, rs io.ReadSeeker, size int64) error {
	h := sha256.New()
	n, err := io.Copy(h, rs)
	if err != nil {
		return err
	}
	if n != size {
		return fmt.Errorf("cache: the sealed object has %d bytes, not %d", n, size)
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return s.c.PutPlain(ctx, s.full(key), rs, size, h.Sum(nil))
}

func (s *bucketStore) get(ctx context.Context, key string, offset, length int64) (io.ReadCloser, int64, error) {
	rc, size, err := s.c.OpenPlain(ctx, s.full(key), offset, length)
	if err != nil {
		return nil, 0, mapS3(err)
	}
	return rc, size, nil
}

func (s *bucketStore) stat(ctx context.Context, key string) (int64, time.Time, error) {
	o, err := s.c.StatPlain(ctx, s.full(key))
	if err != nil {
		return 0, time.Time{}, mapS3(err)
	}
	return o.Size, o.Modified, nil
}

func (s *bucketStore) list(ctx context.Context, prefix string, fn func(string, int64, time.Time) error) error {
	token := ""
	for {
		objs, next, err := s.c.ListPlain(ctx, s.full(prefix), token, listPage)
		if err != nil {
			return err
		}
		for _, o := range objs {
			key := o.Key
			if s.prefix != "" {
				key = strings.TrimPrefix(key, s.prefix+"/")
			}
			if err := fn(key, o.Size, o.Modified); err != nil {
				return err
			}
		}
		if next == "" {
			return nil
		}
		token = next
	}
}

func (s *bucketStore) remove(ctx context.Context, key string) error {
	return s.c.Delete(ctx, s.full(key))
}

// touch writes the empty access marker; its modification time is the time of the write.
func (s *bucketStore) touch(ctx context.Context, key string) error {
	return s.putBytes(ctx, key, nil)
}
