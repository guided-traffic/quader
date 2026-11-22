package blobcrypt

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// The vector file of storage.md R-STOR-079, as this test reads it.
type (
	vectorFile struct {
		Format string       `json:"format"`
		Cases  []vectorCase `json:"cases"`
	}
	vectorKey struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	vectorCase struct {
		Name          string      `json:"name"`
		Kind          string      `json:"kind"`
		Row           string      `json:"row"`
		Reason        string      `json:"reason"`
		DataKey       string      `json:"dataKey"`
		Seed          string      `json:"seed"`
		Plaintext     *vectorText `json:"plaintext"`
		WrapKey       *vectorKey  `json:"wrapKey"`
		Salt          string      `json:"salt"`
		WrapNonce     string      `json:"wrapNonce"`
		SegmentNonces []string    `json:"segmentNonces"`
		Offset        *int64      `json:"offset"`
		NameKey       string      `json:"nameKey"`
		Prefix        string      `json:"prefix"`
		Digest        string      `json:"digest"`
		ObjectName    string      `json:"objectName"`
		ReaderKeys    []vectorKey `json:"readerKeys"`
		Copy          *struct {
			Digest     string `json:"digest"`
			Size       int64  `json:"size"`
			StoredSize int64  `json:"storedSize"`
		} `json:"copy"`
		Object       string `json:"object"`
		Record       string `json:"record"`
		StoredSize   int64  `json:"storedSize"`
		StoredSHA256 string `json:"storedSha256"`
	}
	vectorText struct {
		Rule   string `json:"rule"`
		Hex    string `json:"hex"`
		Length int64  `json:"length"`
		SHA256 string `json:"sha256"`
	}
)

func loadVectors(t testing.TB) vectorFile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f vectorFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func unhex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("a value of %d characters in the vector file is not hex", len(s)) // it may be a key: never printed
	}
	return b
}

func (c vectorCase) plain(t testing.TB) []byte {
	t.Helper()
	if c.Plaintext.Rule != "" {
		if c.Plaintext.Rule != "i mod 251" {
			t.Fatalf("case %s: unknown rule %q", c.Name, c.Plaintext.Rule)
		}
		return rule(int(c.Plaintext.Length))
	}
	return unhex(t, c.Plaintext.Hex)
}

func (c vectorCase) file(t testing.TB, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (c vectorCase) meta(t testing.TB) map[string]string {
	t.Helper()
	var m map[string]string
	if err := json.Unmarshal(c.file(t, c.Record), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func (c vectorCase) keys(t testing.TB) KeyLookup {
	t.Helper()
	var held []*StorageKey
	for _, k := range c.ReaderKeys {
		sk, err := NewStorageKey(k.ID, unhex(t, k.Key))
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, sk)
	}
	return holds(held...)
}

func (c vectorCase) want(t testing.TB, object []byte) Want {
	t.Helper()
	return Want{
		Known: true, Digest: [32]byte(unhex(t, c.Copy.Digest)), Size: c.Copy.Size, StoredSize: c.Copy.StoredSize,
		ObjectSize: int64(len(object)),
	}
}

func TestR_STOR_079_TheVectorsReproduceAndTheirNegativesAreRefused(t *testing.T) {
	t.Parallel()
	f := loadVectors(t)
	if f.Format != Format {
		t.Fatalf("the vector file is of the format %q", f.Format)
	}
	rows := map[string]bool{}
	for _, c := range f.Cases {
		t.Run(c.Name, func(t *testing.T) {
			switch {
			case c.Kind == "name":
				got, err := KeyedName(mustKey(t, unhex(t, c.NameKey)), c.Prefix, [32]byte(unhex(t, c.Digest)))
				if err != nil || got != c.ObjectName {
					t.Fatalf("the keyed name is %q (%v), the vector says %q", got, err, c.ObjectName)
				}
			case c.Kind == "tail":
				checkTailVector(t, c)
			case c.Reason == "":
				checkPositiveVector(t, c)
			default:
				rows[c.Row] = true
				object := c.file(t, c.Object)
				got, err := readWhole(c.meta(t), c.keys(t), c.want(t, object), object, true)
				if reason, ok := ReasonOf(err); !ok || string(reason) != c.Reason || len(got) != 0 {
					t.Fatalf("the negative case gave %v after %d bytes, the vector says %s", err, len(got), c.Reason)
				}
			}
		})
	}
	// One negative case or more for every row of R-STOR-078 but the last, whose case is the positive older-a.
	for _, row := range []string{
		"Truncation at a segment boundary, the final segment cut off", "Truncation inside a segment, or bytes appended",
		"Segments reordered", "A segment dropped", "A segment of another object put in",
		"A data segment presented as final, or the final segment as data", "A whole object put under another blob's name",
		"The record of another object put on this body", "A changed record: format, key identifier, wrap or commitment",
		"A record made under a storage key of another connection or another installation",
		"The object copied to another connection of the same installation",
		"A ciphertext that opens under two data keys (key commitment, Albertini and others, USENIX Security 2022)",
		"Parts of two sealings of one upload session spliced (a part sent again with other bytes)", "One bit changed anywhere",
	} {
		if !rows[row] {
			t.Errorf("no negative vector for the row %q", row)
		}
	}
}

func checkPositiveVector(t *testing.T, c vectorCase) {
	plain := c.plain(t)
	if sum := sha256.Sum256(plain); hex.EncodeToString(sum[:]) != c.Plaintext.SHA256 {
		t.Fatalf("the plaintext's SHA-256 is %x, the vector says %s", sum, c.Plaintext.SHA256)
	}
	object, meta := c.file(t, c.Object), c.meta(t)
	k := mustKey(t, unhex(t, c.DataKey))
	if got := sealWhole(t, k, [32]byte(unhex(t, c.Seed)), plain); !bytes.Equal(got, object) {
		t.Fatal("the object does not reproduce from its inputs")
	}
	sum := sha256.Sum256(object)
	if int64(len(object)) != c.StoredSize || hex.EncodeToString(sum[:]) != c.StoredSHA256 {
		t.Fatalf("the object is %d bytes with SHA-256 %x, the vector says %d and %s", len(object), sum, c.StoredSize, c.StoredSHA256)
	}
	n := segments(int64(len(plain)))
	if int64(len(c.SegmentNonces)) != n+1 {
		t.Fatalf("%d nonces for %d segments and the final one", len(c.SegmentNonces), n)
	}
	for i, nonce := range c.SegmentNonces {
		at := int64(i) * FullSegmentSize
		if int64(i) == n {
			at = int64(len(object)) - FinalSegmentSize
		}
		if hex.EncodeToString(object[at:at+NonceSize]) != nonce {
			t.Fatalf("segment %d's nonce in the object is not the vector's", i)
		}
	}
	sk, err := NewStorageKey(c.WrapKey.ID, unhex(t, c.WrapKey.Key))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := Wrap(sk, DataKey{Key: k, Digest: sha256.Sum256(plain), Size: int64(len(plain))},
		bytes.NewReader(append(unhex(t, c.Salt), unhex(t, c.WrapNonce)...)))
	if err != nil {
		t.Fatal(err)
	}
	for key, v := range rec.Metadata() {
		if meta[key] != v {
			t.Fatalf("the record's %s does not reproduce from its inputs", key)
		}
	}
	total := 0
	for key, v := range meta {
		total += len(key) + len(v)
	}
	if len(meta) != 4 || total > 2048 {
		t.Fatalf("the record holds %d keys of %d bytes, more than the 2 KiB of R-STOR-062", len(meta), total)
	}
	got, err := readWhole(meta, c.keys(t), c.want(t, object), object, true)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("the object does not read back: %v", err)
	}
	if len(plain) > 2 {
		l := int64(len(plain))
		got, err := readRange(meta, c.keys(t), c.want(t, object), object, l/2, l-1)
		if err != nil || !bytes.Equal(got, plain[l/2:]) {
			t.Fatalf("the second half does not read by range: %v", err)
		}
	}
}

func checkTailVector(t *testing.T, c vectorCase) {
	plain := c.plain(t)
	object := c.file(t, c.Object)
	k := mustKey(t, unhex(t, c.DataKey))
	s, err := NewTailSection(k, [32]byte(unhex(t, c.Seed)), *c.Offset, bytes.NewReader(plain), int64(len(plain)))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(s); !bytes.Equal(got, object) {
		t.Fatal("the tail does not reproduce from its inputs")
	}
	for j, nonce := range c.SegmentNonces {
		at := j * FullSegmentSize
		if hex.EncodeToString(object[at:at+NonceSize]) != nonce {
			t.Fatalf("tail segment %d's nonce in the object is not the vector's", j)
		}
	}
	r, err := NewTailReader(bytes.NewReader(object), k, *c.Offset, int64(len(plain)), [32]byte(unhex(t, c.Plaintext.SHA256)), int64(len(object)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("the tail does not read back: %v", err)
	}
}

// The known answers of the specification stand in the vector file unchanged, and the files together stay below 1 MB.
func TestR_STOR_079_TheVectorFileCarriesTheKnownAnswers(t *testing.T) {
	t.Parallel()
	byName := map[string]vectorCase{}
	for _, c := range loadVectors(t).Cases {
		byName[c.Name] = c
	}
	if got := hex.EncodeToString(byName["empty"].file(t, byName["empty"].Object)); got != kaEmptyObject {
		t.Error("the empty object of the file is not the known answer")
	}
	if got := hex.EncodeToString(byName["a"].file(t, byName["a"].Object)); got != kaAObject {
		t.Error("the object a of the file is not the known answer")
	}
	if m := byName["a"].meta(t); m[MetaWrap] != kaWrap || m[MetaCommitment] != kaRecordCommit || m[MetaKey] != kaKeyID {
		t.Error("the record of a in the file is not the known answer")
	}
	if byName["l65537"].StoredSHA256 != kaRuleObjectSum || byName["l65537"].StoredSize != 65661 {
		t.Error("the object of 65,537 bytes in the file is not the known answer")
	}
	if byName["keyed-name"].ObjectName != kaKeyedName {
		t.Error("the keyed name in the file is not the known answer")
	}
	var total int64
	err := filepath.Walk("testdata", func(path string, info os.FileInfo, err error) error {
		switch {
		case err != nil:
			return err
		case info.IsDir() && path == filepath.Join("testdata", "fuzz"):
			return filepath.SkipDir // a failing input the fuzzer kept is no vector
		case !info.IsDir():
			total += info.Size()
		}
		return nil
	})
	if err != nil || total >= 1_000_000 {
		t.Errorf("the vector files hold %d bytes (%v), the specification allows below 1 MB", total, err)
	}
}
