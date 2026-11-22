package blobcrypt

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"io"
	"testing"
)

// object is one stored object of the tests: its plaintext, its data key with what the record binds, the record and the
// stored bytes, made with keys and seeds from crypto/rand.
type object struct {
	plain  []byte
	dk     DataKey
	seed   [32]byte
	sk     *StorageKey
	meta   map[string]string
	stored []byte
}

func newStorageKey(t testing.TB, id string) *StorageKey {
	t.Helper()
	k, err := NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &StorageKey{ID: id, Key: k}
}

func newObject(t testing.TB, sk *StorageKey, plain []byte) object {
	t.Helper()
	k, err := NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := NewSeed(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dk := DataKey{Key: k, KeyID: sk.ID, Digest: sha256.Sum256(plain), Size: int64(len(plain))}
	rec, err := Wrap(sk, dk, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return object{plain: plain, dk: dk, seed: seed, sk: sk, meta: rec.Metadata(), stored: sealWhole(t, k, seed, plain)}
}

// want is what the database says of the object's copy, with the store answering the size of stored.
func (o object) want(stored []byte) Want {
	s, _ := StoredSize(o.dk.Size)
	return Want{Known: true, Digest: o.dk.Digest, Size: o.dk.Size, StoredSize: s, ObjectSize: int64(len(stored))}
}

// segment returns the stored bytes of data segment i of the object (n, the number of data segments, for the final segment).
func (o object) segment(i int64) []byte {
	start := i * FullSegmentSize
	end := min(start+FullSegmentSize, int64(len(o.stored))-FinalSegmentSize)
	if i == segments(o.dk.Size) {
		start, end = int64(len(o.stored))-FinalSegmentSize, int64(len(o.stored))
	}
	return o.stored[start:end]
}

// sealAt seals plain as one segment under the object's data key with the given mark and index and a random nonce: what only
// a holder of the data key can make.
func (o object) sealAt(t testing.TB, mark byte, i int64, plain []byte) []byte {
	t.Helper()
	aead, err := o.dk.Key.aead(labelSegments)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	return aead.Seal(nonce, nonce, plain, segmentAD(nil, mark, i))
}

// finalPlain is the plaintext of a final segment.
func finalPlain(size int64, digest [32]byte) []byte {
	return append(be64(nil, size), digest[:]...)
}

func holds(keys ...*StorageKey) KeyLookup {
	return func(id string) (*StorageKey, error) {
		for _, k := range keys {
			if k.ID == id {
				return k, nil
			}
		}
		return nil, nil
	}
}

// countingReader is a reader of plaintext that keeps every byte it was handed, so a test sees what left before an error.
type countingReader struct{ released []byte }

// drain reads r in small pieces until it fails or ends; err is nil at the end.
func (c *countingReader) drain(r io.Reader) error {
	buf := make([]byte, 1000)
	for {
		n, err := r.Read(buf)
		c.released = append(c.released, buf[:n]...)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// readWhole runs steps 1 to 7 on a whole read of stored under meta: what was released, and the error.
func readWhole(meta map[string]string, keys KeyLookup, want Want, stored []byte, verify bool) ([]byte, error) {
	dk, err := Open(meta, keys, want)
	if err != nil {
		return nil, err
	}
	defer dk.Key.Destroy()
	r, err := NewReader(bytes.NewReader(stored), dk, verify)
	if err != nil {
		return nil, err
	}
	var c countingReader
	err = c.drain(r)
	return c.released, err
}

// readRange runs a ranged read of the plaintext bytes first to last: the store answers the planned stored bytes of stored and
// its length as the total.
func readRange(meta map[string]string, keys KeyLookup, want Want, stored []byte, first, last int64) ([]byte, error) {
	want.ObjectSize = int64(len(stored))
	dk, err := Open(meta, keys, want)
	if err != nil {
		return nil, err
	}
	defer dk.Key.Destroy()
	rg, err := PlanRange(dk.Size, first, last)
	if err != nil {
		return nil, err
	}
	end := min(rg.Offset+rg.Length, int64(len(stored)))
	r, err := NewRangeReader(bytes.NewReader(stored[min(rg.Offset, end):end]), dk, rg)
	if err != nil {
		return nil, err
	}
	var c countingReader
	err = c.drain(r)
	return c.released, err
}

func cat(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

func flip(b []byte, at int) []byte {
	c := bytes.Clone(b)
	c[at] ^= 0x01
	return c
}

func withMeta(meta map[string]string, key, value string) map[string]string {
	c := make(map[string]string, len(meta))
	for k, v := range meta {
		c[k] = v
	}
	if value == "" {
		delete(c, key)
	} else {
		c[key] = value
	}
	return c
}

// wantRefused fails the test unless err is a refusal with reason and nothing at all was released.
func wantRefused(t *testing.T, released []byte, err error, reason Reason) {
	t.Helper()
	if got, ok := ReasonOf(err); !ok || got != reason {
		t.Fatalf("the read gave %v, want a refusal with reason %s", err, reason)
	}
	if len(released) != 0 {
		t.Fatalf("%d bytes left before the refusal %s", len(released), reason)
	}
}

// wantRefusedAfter fails the test unless err is a refusal with reason and exactly the genuine bytes before it were released.
func wantRefusedAfter(t *testing.T, released []byte, err error, reason Reason, genuine []byte) {
	t.Helper()
	if got, ok := ReasonOf(err); !ok || got != reason {
		t.Fatalf("the read gave %v, want a refusal with reason %s", err, reason)
	}
	if !bytes.Equal(released, genuine) {
		t.Fatalf("%d bytes left before the refusal %s, want the %d authenticated bytes before it", len(released), reason, len(genuine))
	}
}
