package blobcrypt

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
)

// The known answers of storage.md, copied from the specification's text: R-STOR-076 (the derived keys and three objects),
// R-STOR-077 (a record) and R-STOR-086 (a keyed name). They were computed for the specification by an implementation
// independent of this package; the vector file must carry them unchanged (vectors_test.go).
const (
	kaSegmentKey    = "3671ce0541d176175bb26410c7f0fc2da1b39c6c2aac6f5e6370a6d8ecab751d"
	kaCommitment    = "1f85b68c333456650f07544250d71ec2183829bbd73dbe32cead4ecbc7e47733"
	kaTailKey       = "c1ad0bdb618d3d83b751d1369a6ad44e43b17af56a2f61426198c078b8c87af5"
	kaEmptyObject   = "0a34ba7650ec909a28eb0538f53f7a84be9ab01ed68575fe09db310b105f9920c9514c8d9c6697e73cd3ad5a6ffa194411d4ecc171d9eb47d7abee250da8fbad07dd2cad"
	kaAObject       = "05c628666f8f6834df05aa27f848cff1e0157ab14ec8c092f5c25a9c72182e548168cd912fdcca99e766a8b8075a85ce9f94df79a3778bef284bd3aa9bfabf3803858b1f077d62b7ffbc5fef76005697ce8f12da2adf7fb2b20f25458434a6de71"
	kaRulePlainSum  = "237356e1"                                                         // …529d: the SHA-256 of 65,537 bytes i mod 251, abbreviated in the spec
	kaRuleObjectSum = "f547d3127b4c42a11f36ff45996bf301cd35d84bbde6c4d0c22b0beb850ff144" // the SHA-256 of its 65,661 stored bytes
	kaWrap          = "gIGCg4SFhoeIiYqLjI2Oj5CRkpOUlZaXmJmam7m4IyuNMsFf5oiyZvVVCx8bBxSnY5kNUOulc9ByS8wknq4HpO9Zz-lqXOkpDX_rlAAJ5DJCXzMRmCNWG9_7zOuut11EonL3axPC5fq0E9cwuDeQCQ1A6Xs"
	kaRecordCommit  = "H4W2jDM0VmUPB1RCUNcewhg4KbvXPb4yzq1Oy8fkdzM"
	kaKeyID         = "sk-default-1-0a1b2c3d"
	kaOtherKeyID    = "sk-default-2-0a1b2c3d"
	kaKeyedName     = "quader/keyed/99/42/9942d541941e3141ba35550ee20b7bbf8c60fb3acc8123325c1b293b0e2922a6"
)

// seq is the bytes start, start+1, … of length n: the inputs of the known answers (00 01 … 1f and so on).
func seq(start byte, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = start + byte(i)
	}
	return b
}

// rule is the plaintext of the vectors: n bytes i mod 251.
func rule(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

func mustKey(t testing.TB, b []byte) Key {
	t.Helper()
	k, err := KeyFromBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func seed32(b []byte) [32]byte { return [32]byte(b) }

// sealWhole is the stored form of a whole plaintext under k with the nonces of seed.
func sealWhole(t testing.TB, k Key, seed [32]byte, plain []byte) []byte {
	t.Helper()
	s, err := NewSection(k, seed, bytes.NewReader(plain), 0, int64(len(plain)), &Final{Size: int64(len(plain)), Digest: sha256.Sum256(plain)})
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(s)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestR_STOR_076_TheKnownAnswersOfTheStoredFormatReproduce(t *testing.T) {
	t.Parallel()
	dek := seq(0x00, 32)
	for _, c := range []struct{ label, want string }{
		{labelSegments, kaSegmentKey}, {labelCommitment, kaCommitment}, {labelTails, kaTailKey},
	} {
		if got := hex.EncodeToString(derive(dek, nil, c.label)); got != c.want {
			t.Errorf("HKDF(DEK, %q) is not the specification's known answer", c.label) // a key: never printed
		}
	}
	k := mustKey(t, dek)
	seed := seed32(seq(0x60, 32))
	if got := hex.EncodeToString(sealWhole(t, k, seed, nil)); got != kaEmptyObject {
		t.Errorf("the empty plaintext is stored as %s, the specification says %s", got, kaEmptyObject)
	}
	if got := hex.EncodeToString(sealWhole(t, k, seed, []byte("a"))); got != kaAObject {
		t.Errorf("the plaintext a is stored as %s, the specification says %s", got, kaAObject)
	}
	plain := rule(65537)
	if sum := sha256.Sum256(plain); hex.EncodeToString(sum[:4]) != kaRulePlainSum {
		t.Fatalf("the plaintext rule's SHA-256 starts %x, the specification says %s", sum[:4], kaRulePlainSum)
	}
	stored := sealWhole(t, k, seed, plain)
	if sum := sha256.Sum256(stored); len(stored) != 65661 || hex.EncodeToString(sum[:]) != kaRuleObjectSum {
		t.Errorf("65,537 bytes are stored as %d bytes with SHA-256 %x, the specification says 65,661 with %s", len(stored), sum, kaRuleObjectSum)
	}
}

func TestR_STOR_077_TheKnownAnswersOfTheRecordReproduce(t *testing.T) {
	t.Parallel()
	sk, err := NewStorageKey(kaKeyID, seq(0x20, 32))
	if err != nil {
		t.Fatal(err)
	}
	dk := DataKey{Key: mustKey(t, seq(0x00, 32)), Digest: sha256.Sum256([]byte("a")), Size: 1}
	rec, err := Wrap(sk, dk, bytes.NewReader(append(seq(0x80, 16), seq(0x90, 12)...)))
	if err != nil {
		t.Fatal(err)
	}
	meta := rec.Metadata()
	if meta[MetaWrap] != kaWrap || meta[MetaCommitment] != kaRecordCommit || meta[MetaKey] != kaKeyID || meta[MetaFormat] != Format {
		t.Errorf("the record is %v, the specification says wrap %s and commitment %s", meta, kaWrap, kaRecordCommit)
	}
	held := func(id string) (*StorageKey, error) {
		if id == sk.ID {
			return sk, nil
		}
		return nil, nil
	}
	got, err := Open(meta, held, Want{Known: true, Digest: dk.Digest, Size: 1, StoredSize: 97, ObjectSize: 97})
	if err != nil || !got.Key.Equal(dk.Key) {
		t.Fatalf("the known record does not open to its data key: %v", err)
	}
	// The same wrap opened with the identifier sk-default-2-0a1b2c3d fails: the identifier is in the additional data.
	other := &StorageKey{ID: kaOtherKeyID, Key: sk.Key}
	_, err = unwrap(meta[MetaWrap], other)
	if !errors.Is(err, ErrRecord) {
		t.Errorf("the wrap opened under the identifier %s: %v", kaOtherKeyID, err)
	}
}

func TestKeyedNameKnownAnswer(t *testing.T) {
	t.Parallel()
	got, err := KeyedName(mustKey(t, seq(0x40, 32)), "quader", sha256.Sum256([]byte("a")))
	if err != nil || got != kaKeyedName {
		t.Errorf("the keyed name of a is %q (%v), the specification says %q", got, err, kaKeyedName)
	}
}
