// Command blobvectors wrote the test vectors of the stored format quader-blob-v1 (storage.md R-STOR-079) into
// internal/blob/blobcrypt/testdata: vectors.json and, per case, the stored object objects/<case>.bin and its record
// objects/<case>.record.json. It was run once, the files were checked with check.py beside it, an implementation on Python's
// cryptography package independent of this module, and committed; the vectors are never regenerated, and a change of the
// format is a new identifier with vectors of its own. The test of this command holds that it still writes exactly the
// committed files.
//
//	go run ./tools/blobvectors -out internal/blob/blobcrypt/testdata
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
)

func main() {
	out := flag.String("out", "internal/blob/blobcrypt/testdata", "the directory the vectors are written to")
	flag.Parse()
	if err := write(*out); err != nil {
		fmt.Fprintln(os.Stderr, "blobvectors:", err)
		os.Exit(1)
	}
}

// The vector file's shapes. Every byte string is lowercase hex; paths are relative to the vector file.
type (
	vectorFile struct {
		Format string   `json:"format"`
		About  []string `json:"about"`
		Cases  []vcase  `json:"cases"`
	}
	keyEntry struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	copyRow struct {
		Digest     string `json:"digest"`
		Size       int64  `json:"size"`
		StoredSize int64  `json:"storedSize"`
	}
	plaintext struct {
		Rule   string `json:"rule,omitempty"`
		Hex    string `json:"hex,omitempty"`
		Length int64  `json:"length"`
		SHA256 string `json:"sha256"`
	}
	vcase struct {
		Name        string `json:"name"`
		Kind        string `json:"kind"`
		Row         string `json:"row,omitempty"`
		Description string `json:"description"`
		KnownAnswer bool   `json:"knownAnswer,omitempty"`
		Reason      string `json:"reason,omitempty"`

		DataKey       string     `json:"dataKey,omitempty"`
		Seed          string     `json:"seed,omitempty"`
		Plaintext     *plaintext `json:"plaintext,omitempty"`
		WrapKey       *keyEntry  `json:"wrapKey,omitempty"`
		Salt          string     `json:"salt,omitempty"`
		WrapNonce     string     `json:"wrapNonce,omitempty"`
		SegmentNonces []string   `json:"segmentNonces,omitempty"`
		Offset        *int64     `json:"offset,omitempty"`

		NameKey    string `json:"nameKey,omitempty"`
		Prefix     string `json:"prefix,omitempty"`
		Digest     string `json:"digest,omitempty"`
		ObjectName string `json:"objectName,omitempty"`

		ReaderKeys   []keyEntry `json:"readerKeys,omitempty"`
		Copy         *copyRow   `json:"copy,omitempty"`
		Object       string     `json:"object,omitempty"`
		Record       string     `json:"record,omitempty"`
		StoredSize   int64      `json:"storedSize,omitempty"`
		StoredSHA256 string     `json:"storedSha256,omitempty"`
	}
)

// seq is the bytes start, start+1, … of length n, the inputs of the specification's known answers.
func seq(start byte, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = start + byte(i)
	}
	return b
}

// derived is an input of a case that has no known answer: the first n bytes of SHA-256("quader-blob-v1 vector <case>
// <field>"), so that no two cases share a data key, a seed, a salt or a nonce.
func derived(name, field string, n int) []byte {
	s := sha256.Sum256([]byte(blobcrypt.Format + " vector " + name + " " + field))
	return s[:n]
}

// rule is the plaintext of the vectors: n bytes i mod 251.
func rule(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

var (
	defaultKeyID  = "sk-default-1-0a1b2c3d"
	defaultKey    = seq(0x20, 32)
	defaultReader = []keyEntry{{ID: defaultKeyID, Key: hex.EncodeToString(defaultKey)}}
)

// genuine is one object written as Quader writes it, with its inputs.
type genuine struct {
	name             string
	plain            []byte
	dek, seed        []byte
	keyID            string
	storageKey       []byte
	salt, wrapNonce  []byte
	stored           []byte
	meta             map[string]string
	digest           [32]byte
	plainDescription *plaintext
}

func write(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, "objects"), 0o750); err != nil {
		return err
	}
	w := &writer{dir: dir}
	ka := func(name string, plain []byte, p *plaintext, salt, nonce []byte) *genuine {
		return w.object(name, plain, p, seq(0x00, 32), seq(0x60, 32), defaultKeyID, defaultKey, salt, nonce)
	}
	byRule := func(name string, n int) *genuine {
		return w.object(name, rule(n), ruleText(n), derived(name, "data key", 32), derived(name, "seed", 32), defaultKeyID, defaultKey,
			derived(name, "salt", 16), derived(name, "wrap nonce", 12))
	}
	aText := &plaintext{Hex: "61", Length: 1}

	// The positive cases: the known answers of R-STOR-076 and R-STOR-077, the sizes of R-STOR-079 by the rule, an older genuine
	// object (the last row of R-STOR-078), a staging tail and a keyed name.
	a := ka("a", []byte("a"), aText, seq(0x80, 16), seq(0x90, 12))
	w.positive(ka("empty", nil, ruleText(0), derived("empty", "salt", 16), derived("empty", "wrap nonce", 12)),
		"the empty plaintext: the final segment alone (the known answer of R-STOR-076)", true)
	w.positive(a, "the plaintext a (the known answers of R-STOR-076 and R-STOR-077)", true)
	l1 := byRule("l1", 1)
	w.positive(l1, "one byte by the rule", false)
	w.positive(byRule("l65535", 65535), "one data segment one byte short of full", false)
	w.positive(byRule("l65536", 65536), "one full data segment", false)
	l65537 := ka("l65537", rule(65537), ruleText(65537), derived("l65537", "salt", 16), derived("l65537", "wrap nonce", 12))
	w.positive(l65537, "a full data segment and one of one byte (the known answer of R-STOR-076: the stored object's SHA-256)", true)
	w.positive(byRule("l196609", 196609), "three full data segments and one of one byte", false)
	older := w.object("older-a", []byte("a"), aText, derived("older-a", "data key", 32), derived("older-a", "seed", 32), defaultKeyID,
		defaultKey, derived("older-a", "salt", 16), derived("older-a", "wrap nonce", 12))
	w.positiveAs(older, a, "an older genuine object of the content a put back: it opens and yields a (R-STOR-078, the last row)", false)
	if err := w.tail(); err != nil {
		return err
	}
	if err := w.name(); err != nil {
		return err
	}
	w.negatives(a, l1, l65537, byRule("l131072", 2*65536), byRule("l40", 40))
	if w.err != nil {
		return w.err
	}
	file := vectorFile{
		Format: blobcrypt.Format,
		About: []string{
			"Test vectors of the stored format quader-blob-v1, storage.md R-STOR-076 to R-STOR-079, R-STOR-081 (tails) and R-STOR-086 (names).",
			"Written once by tools/blobvectors from the Go implementation and checked with tools/blobvectors/check.py; never regenerated.",
			"Byte strings are lowercase hex. A plaintext is given as a rule (byte i is i mod 251, over length bytes) or in hex.",
			"An object case gives its inputs (data key, seed, the storage key and identifier, salt, wrap nonce) and, explicitly, every segment's nonce, the final segment's last; its outputs are the object file, the record file (the object's metadata) and the stored size and SHA-256.",
			"A case with a reason is negative: reading its object under its record, holding readerKeys, against copy (the database's digest, size and stored size, with the object file's length as the size the store answered), must fail with that reason; its inputs are those of the genuine object it changes.",
			"A tail case is a staging tail at the session offset offset under the tail key of its data key; a reader compares its plaintext length and SHA-256.",
		},
		Cases: w.cases,
	}
	return writeJSON(filepath.Join(dir, "vectors.json"), file)
}

type writer struct {
	dir   string
	cases []vcase
	err   error
}

func ruleText(n int) *plaintext { return &plaintext{Rule: "i mod 251", Length: int64(n)} }

// object writes nothing: it seals and wraps a genuine object.
func (w *writer) object(name string, plain []byte, p *plaintext, dek, seed []byte, keyID string, storageKey, salt, nonce []byte) *genuine {
	g := &genuine{
		name: name, plain: plain, dek: dek, seed: seed, keyID: keyID, storageKey: storageKey, salt: salt, wrapNonce: nonce,
		digest: sha256.Sum256(plain),
	}
	pd := *p
	pd.SHA256 = hex.EncodeToString(g.digest[:])
	g.plainDescription = &pd
	if w.err != nil {
		return g
	}
	g.stored, w.err = seal(dek, seed, plain)
	if w.err == nil {
		g.meta, w.err = wrap(dek, keyID, storageKey, salt, nonce, g.digest, int64(len(plain)))
	}
	return g
}

func seal(dek, seed, plain []byte) ([]byte, error) {
	k, err := blobcrypt.KeyFromBytes(dek)
	if err != nil {
		return nil, err
	}
	defer k.Destroy()
	s, err := blobcrypt.NewSection(k, [32]byte(seed), bytes.NewReader(plain), 0, int64(len(plain)),
		&blobcrypt.Final{Size: int64(len(plain)), Digest: sha256.Sum256(plain)})
	if err != nil {
		return nil, err
	}
	return io.ReadAll(s)
}

func wrap(dek []byte, keyID string, storageKey, salt, nonce []byte, digest [32]byte, size int64) (map[string]string, error) {
	sk, err := blobcrypt.NewStorageKey(keyID, storageKey)
	if err != nil {
		return nil, err
	}
	k, err := blobcrypt.KeyFromBytes(dek)
	if err != nil {
		return nil, err
	}
	rec, err := blobcrypt.Wrap(sk, blobcrypt.DataKey{Key: k, Digest: digest, Size: size}, bytes.NewReader(append(bytes.Clone(salt), nonce...)))
	if err != nil {
		return nil, err
	}
	return rec.Metadata(), nil
}

// inputs is the case of a genuine object with its inputs filled in.
func (g *genuine) inputs(name, description string) vcase {
	return vcase{
		Name: name, Kind: "object", Description: description,
		DataKey: hex.EncodeToString(g.dek), Seed: hex.EncodeToString(g.seed), Plaintext: g.plainDescription,
		WrapKey: &keyEntry{ID: g.keyID, Key: hex.EncodeToString(g.storageKey)},
		Salt:    hex.EncodeToString(g.salt), WrapNonce: hex.EncodeToString(g.wrapNonce),
		ReaderKeys: defaultReader, Copy: g.copyRow(),
	}
}

func (g *genuine) copyRow() *copyRow {
	s, _ := blobcrypt.StoredSize(int64(len(g.plain)))
	return &copyRow{Digest: hex.EncodeToString(g.digest[:]), Size: int64(len(g.plain)), StoredSize: s}
}

func (w *writer) positive(g *genuine, description string, knownAnswer bool) {
	w.positiveAs(g, g, description, knownAnswer)
}

// positiveAs writes a genuine object as a positive case read against the copy of as.
func (w *writer) positiveAs(g, as *genuine, description string, knownAnswer bool) {
	if w.err != nil {
		return
	}
	c := g.inputs(g.name, description)
	c.KnownAnswer = knownAnswer
	c.Copy = as.copyRow()
	c.SegmentNonces = objectNonces(g.stored, int64(len(g.plain)))
	w.files(&c, g.stored, g.meta)
}

// objectNonces reads every segment's nonce from a genuine object, the final segment's last.
func objectNonces(stored []byte, plain int64) []string {
	var out []string
	n := (plain + blobcrypt.SegmentSize - 1) / blobcrypt.SegmentSize
	for i := int64(0); i < n; i++ {
		at := i * blobcrypt.FullSegmentSize
		out = append(out, hex.EncodeToString(stored[at:at+blobcrypt.NonceSize]))
	}
	at := len(stored) - blobcrypt.FinalSegmentSize
	return append(out, hex.EncodeToString(stored[at:at+blobcrypt.NonceSize]))
}

// files writes a case's object and, unless meta is nil (a tail has no record), its record, and fills in their names, size
// and SHA-256.
func (w *writer) files(c *vcase, stored []byte, meta map[string]string) {
	if w.err != nil {
		return
	}
	c.Object = "objects/" + c.Name + ".bin"
	sum := sha256.Sum256(stored)
	c.StoredSize, c.StoredSHA256 = int64(len(stored)), hex.EncodeToString(sum[:])
	if w.err = os.WriteFile(filepath.Join(w.dir, c.Object), stored, 0o644); w.err != nil { //nolint:gosec // G306: test data of the repository
		return
	}
	if meta != nil {
		c.Record = "objects/" + c.Name + ".record.json"
		if w.err = writeJSON(filepath.Join(w.dir, c.Record), meta); w.err != nil {
			return
		}
	}
	w.cases = append(w.cases, *c)
}

// tail writes a staging tail of 65,537 bytes at the session offset 131,072: one full tail segment and one of one byte.
func (w *writer) tail() error {
	const name, offset, size = "tail-131072", int64(131072), 65537
	dek, seed, plain := derived(name, "data key", 32), derived(name, "seed", 32), rule(size)
	k, err := blobcrypt.KeyFromBytes(dek)
	if err != nil {
		return err
	}
	defer k.Destroy()
	s, err := blobcrypt.NewTailSection(k, [32]byte(seed), offset, bytes.NewReader(plain), size)
	if err != nil {
		return err
	}
	stored, err := io.ReadAll(s)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(plain)
	p := ruleText(size)
	p.SHA256 = hex.EncodeToString(sum[:])
	off := offset
	c := vcase{
		Name: name, Kind: "tail", Offset: &off, DataKey: hex.EncodeToString(dek), Seed: hex.EncodeToString(seed), Plaintext: p,
		Description: "a staging tail of an upload session at the session offset 131,072 (R-STOR-081): a tail segment and the last tail segment",
		SegmentNonces: []string{
			hex.EncodeToString(stored[:blobcrypt.NonceSize]),
			hex.EncodeToString(stored[blobcrypt.FullSegmentSize : blobcrypt.FullSegmentSize+blobcrypt.NonceSize]),
		},
	}
	w.files(&c, stored, nil)
	return nil
}

// name writes the keyed name of the blob a under the name key 40 41 … 5f (the known answer of R-STOR-086).
func (w *writer) name() error {
	nk, err := blobcrypt.KeyFromBytes(seq(0x40, 32))
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte("a"))
	n, err := blobcrypt.KeyedName(nk, "quader", digest)
	if err != nil {
		return err
	}
	w.cases = append(w.cases, vcase{
		Name: "keyed-name", Kind: "name", KnownAnswer: true, Description: "the keyed name of the blob a (the known answer of R-STOR-086)",
		NameKey: hex.EncodeToString(seq(0x40, 32)), Prefix: "quader", Digest: hex.EncodeToString(digest[:]), ObjectName: n,
	})
	return nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644) //nolint:gosec // G306: test data of the repository
}
