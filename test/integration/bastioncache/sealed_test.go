//go:build integration

package bastioncache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/cache"
	"github.com/guided-traffic/quader/internal/config"
)

var sealedTree = regexp.MustCompile(`^bc-[0-9a-f]{16}$`)

// writeCacheKey writes a cache key file of 32 bytes derived from seed.
func writeCacheKey(t *testing.T, seed byte) string {
	t.Helper()
	k := make([]byte, 32)
	for i := range k {
		k[i] = seed + byte(i)
	}
	p := filepath.Join(t.TempDir(), "cache-key")
	if err := os.WriteFile(p, k, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func withCacheKey(file string) func(*config.BastionCache) {
	return func(c *config.BastionCache) { c.EncryptionKeyFile = file }
}

// object reads an object below the test's prefix with the raw client.
func (e *env) object(key string) []byte {
	e.t.Helper()
	rc, _, err := e.raw.OpenPlain(context.Background(), e.prefix+"/"+key, 0, -1)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil {
		e.t.Fatal(err)
	}
	return b
}

// holdsChunk reports whether hay holds one of the 16-byte chunks of needle at a 16-byte stride, which a run of 31 bytes of
// needle anywhere in hay contains.
func holdsChunk(hay, needle []byte) bool {
	for i := 0; i+16 <= len(needle); i += 16 {
		if bytes.Contains(hay, needle[i:i+16]) {
			return true
		}
	}
	return false
}

// TestR_BAS_061_ACacheWithAKeyOnTheBucket (R-BAS-061): the bucket store's paths with a cache key against the stack's S3, which
// the unit tests on a volume do not reach: the sealed upload with the checksum of its stored bytes, the record read in front of
// a whole and of a ranged body, the totals of ranged answers, the listing of the trees. Two replicas with one key share its
// tree; no object holds a run of the plaintext and no name a content address or a key hash; a changed byte is refused and the
// object goes; a replica of another key misses, deletes the old tree and fills again; a replica without a key deletes the
// key's tree; only the five operations of R-BAS-030 are used.
func TestR_BAS_061_ACacheWithAKeyOnTheBucket(t *testing.T) {
	e := newEnv(t, "sealed")
	ctx := context.Background()
	keyA, keyB := writeCacheKey(t, 1), writeCacheKey(t, 100)
	a, b := e.replica(withCacheKey(keyA)), e.replica(withCacheKey(keyA))
	if err := a.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	const tarball = "/api/npm/web/npm-all/left-pad/-/left-pad-1.3.0.tgz"
	events := []byte(`{"events":[{"tokenId":"tok-1","principalId":"alice","path":"/api/npm/web/npm-all/left-pad/-/left-pad-1.3.0.tgz"}]}` + "\n")
	art := payload(20, 70*1024) // two segments of the format
	ea := a.put(t, cache.EntryArtifact, tarball, art)
	if err := a.Backend().SpoolWrite(ctx, "bastion-0", events); err != nil {
		t.Fatal(err)
	}

	if got, err := b.read(tarball); err != nil || !bytes.Equal(got, art) {
		t.Fatalf("the second replica's hit: %d bytes, %v", len(got), err)
	}
	rc, err := b.OpenRange(ctx, ea, 65530, 20)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(rc); !bytes.Equal(got, art[65530:65550]) {
		t.Fatalf("a range across the segment boundary: %d bytes", len(got))
	}
	_ = rc.Close()
	files, err := b.Backend().SpoolList(ctx)
	if err != nil || len(files) != 1 {
		t.Fatalf("spool: %v %v", files, err)
	}
	if got, err := b.Backend().SpoolRead(ctx, files[0]); err != nil || !bytes.Equal(got, events) {
		t.Fatalf("the spooled events: %q %v", got, err)
	}

	var tree, artifact string
	for _, o := range e.listAll() {
		first := strings.SplitN(o.Key, "/", 2)[0]
		if tree == "" {
			tree = first
		}
		if first != tree || !sealedTree.MatchString(first) {
			t.Errorf("%s lies outside the one tree of the key", o.Key)
		}
		for _, n := range []string{ea.SHA256, cache.KeyHash(tarball), "left-pad", "alice"} {
			if strings.Contains(o.Key, n) {
				t.Errorf("the name %s holds %q", o.Key, n)
			}
		}
		body := e.object(o.Key)
		if holdsChunk(body, art) || holdsChunk(body, events) {
			t.Errorf("%s holds a run of the plaintext", o.Key)
		}
		if strings.HasPrefix(o.Key, tree+"/a/") {
			artifact = o.Key
		}
	}
	if artifact == "" {
		t.Fatal("no artifact object below the tree")
	}

	body := e.object(artifact)
	body[len(body)-100] ^= 0x01 // in the last data segment
	if err := e.raw.PutPlain(ctx, e.prefix+"/"+artifact, bytes.NewReader(body), int64(len(body)), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := b.read(tarball); !errors.Is(err, cache.ErrCorrupt) {
		t.Fatalf("a changed object: %v", err)
	}
	if _, _, err := b.LookupEntry(ctx, tarball); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("the read after the refusal: %v", err)
	}
	a.put(t, cache.EntryArtifact, tarball, art)

	other := e.replica(withCacheKey(keyB))
	if _, err := other.read(tarball); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("the first read with another key: %v", err)
	}
	if err := other.SweepOnce(ctx, nil); err != nil {
		t.Fatal(err)
	}
	for _, o := range e.listAll() {
		if strings.HasPrefix(o.Key, tree+"/") {
			t.Errorf("%s of the old key's tree survives the sweep", o.Key)
		}
	}
	other.put(t, cache.EntryArtifact, tarball, art)
	if got, err := other.read(tarball); err != nil || !bytes.Equal(got, art) {
		t.Fatalf("filled again under the other key: %v", err)
	}

	plain := e.replica()
	if err := plain.SweepOnce(ctx, nil); err != nil {
		t.Fatal(err)
	}
	for _, o := range e.listAll() {
		if sealedTree.MatchString(strings.SplitN(o.Key, "/", 2)[0]) {
			t.Errorf("%s of a key's tree survives the sweep of a cache without a key", o.Key)
		}
	}

	ops := e.count.counts()
	if len(ops) > 5 {
		t.Errorf("operations used: %v", sortedKeys(ops))
	}
	e.count.mu.Lock()
	defer e.count.mu.Unlock()
	if len(e.count.bad) != 0 {
		t.Errorf("requests outside the five operations, or with versioning or a conditional write:\n%s", strings.Join(e.count.bad, "\n"))
	}
}
