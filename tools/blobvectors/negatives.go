package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"maps"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
)

// The rows of storage.md R-STOR-078's table, as the negative cases name them.
const (
	rowCutAtBoundary = "Truncation at a segment boundary, the final segment cut off"
	rowCutInside     = "Truncation inside a segment, or bytes appended"
	rowReordered     = "Segments reordered"
	rowDropped       = "A segment dropped"
	rowOtherSegment  = "A segment of another object put in"
	rowMark          = "A data segment presented as final, or the final segment as data"
	rowOtherName     = "A whole object put under another blob's name"
	rowOtherRecord   = "The record of another object put on this body"
	rowChangedRecord = "A changed record: format, key identifier, wrap or commitment"
	rowForeignKey    = "A record made under a storage key of another connection or another installation"
	rowCopied        = "The object copied to another connection of the same installation"
	rowTwoKeys       = "A ciphertext that opens under two data keys (key commitment, Albertini and others, USENIX Security 2022)"
	rowSpliced       = "Parts of two sealings of one upload session spliced (a part sent again with other bytes)"
	rowOneBit        = "One bit changed anywhere"
	rowFinalFields   = "The final segment's length or digest changed by a holder of the data key (R-STOR-077 step 7)"
)

// negatives writes one negative case or more per row of R-STOR-078, each a change to a genuine object or its record with the
// reason the read must fail with. The forged segments are what only a holder of the data key could make; they come from the
// writer's own sections with a seed of their own.
func (w *writer) negatives(a, l1, l65537, l131072, l40 *genuine) {
	if w.err != nil {
		return
	}
	segA, finalA := a.stored[:29], a.stored[29:]
	neg := func(base *genuine, name, row, description, reason string, stored []byte, meta map[string]string, edit func(*vcase)) {
		c := base.inputs(name, description)
		c.Row, c.Reason = row, reason
		if edit != nil {
			edit(&c)
		}
		w.files(&c, stored, meta)
	}
	forge := func(base *genuine, name string, src []byte, first, plain int64, final blobcrypt.Final) []byte {
		if w.err != nil {
			return nil
		}
		k, err := blobcrypt.KeyFromBytes(base.dek)
		if err != nil {
			w.err = err
			return nil
		}
		defer k.Destroy()
		s, err := blobcrypt.NewSection(k, [32]byte(derived(name, "forge seed", 32)), bytes.NewReader(src), first, plain, &final)
		if err != nil {
			w.err = err
			return nil
		}
		out, err := io.ReadAll(s)
		w.err = err
		return out
	}
	withMeta := func(meta map[string]string, key, value string) map[string]string {
		c := maps.Clone(meta)
		c[key] = value
		return c
	}
	flipB64 := func(v string, at int) string {
		b, _ := base64.RawURLEncoding.DecodeString(v)
		b[at] ^= 0x01
		return base64.RawURLEncoding.EncodeToString(b)
	}
	flip := func(b []byte, at int) []byte {
		c := bytes.Clone(b)
		c[at] ^= 0x01
		return c
	}
	digestA := a.digest

	neg(a, "final-cut", rowCutAtBoundary, "a without its final segment: 29 bytes", "size", segA, a.meta, nil)
	neg(a, "cut-inside", rowCutInside, "a cut by one byte inside its final segment: 96 bytes, which no plaintext size produces", "size",
		a.stored[:96], a.meta, nil)
	neg(a, "appended", rowCutInside, "a with one byte appended: 98 bytes, S(2) and not S(1)", "size", cat(a.stored, []byte{0}), a.meta, nil)
	neg(l131072, "reordered", rowReordered, "two full data segments exchanged", "segment",
		cat(l131072.stored[blobcrypt.FullSegmentSize:2*blobcrypt.FullSegmentSize], l131072.stored[:blobcrypt.FullSegmentSize],
			l131072.stored[2*blobcrypt.FullSegmentSize:]), l131072.meta, nil)
	neg(l65537, "dropped", rowDropped, "the first data segment of l65537 dropped: 97 bytes", "size",
		l65537.stored[blobcrypt.FullSegmentSize:], l65537.meta, nil)
	neg(a, "other-segment", rowOtherSegment, "the data segment of a replaced by the data segment of l1 (another data key)", "segment",
		cat(l1.stored[:29], finalA), a.meta, nil)

	// A data segment sealed at index 1, the final segment's index, holding the 40 bytes a final segment holds.
	asFinal := forge(a, "data-as-final", finalPlain(1, digestA), 1, blobcrypt.FinalPlainSize,
		blobcrypt.Final{Size: blobcrypt.SegmentSize + blobcrypt.FinalPlainSize, Digest: digestA})
	if asFinal != nil {
		neg(a, "data-as-final", rowMark, "the final segment of a replaced by a data segment at index 1 holding u64(1) and the digest of a",
			"final", cat(segA, asFinal[:blobcrypt.FinalSegmentSize]), a.meta, nil)
	}
	asData := forge(l40, "final-as-data", nil, 0, 0, blobcrypt.Final{Size: 0, Digest: l40.digest})
	neg(l40, "final-as-data", rowMark, "the data segment of l40 (40 bytes) replaced by a final segment at index 0", "segment",
		cat(asData, l40.stored[blobcrypt.FinalSegmentSize:]), l40.meta, nil)

	longer := forge(a, "final-length", []byte("ab"), 0, 2, blobcrypt.Final{Size: 2, Digest: digestA})
	if longer != nil {
		neg(a, "final-length", rowFinalFields, "the final segment of a sealed at index 1 with the length 2", "final",
			cat(segA, longer[len(longer)-blobcrypt.FinalSegmentSize:]), a.meta, nil)
	}
	otherDigest := forge(a, "final-digest", []byte("a"), 0, 1, blobcrypt.Final{Size: 1, Digest: l1.digest})
	if otherDigest != nil {
		neg(a, "final-digest", rowFinalFields, "the final segment of a sealed at index 1 with the digest of l1", "final",
			cat(segA, otherDigest[29:]), a.meta, nil)
	}

	neg(a, "other-name", rowOtherName, "a with its record read as the copy of l1", "record", a.stored, a.meta,
		func(c *vcase) { c.Copy = l1.copyRow() })
	neg(a, "other-record", rowOtherRecord, "the body of a under the record of l1", "record", a.stored, l1.meta, nil)
	neg(a, "changed-format", rowChangedRecord, "quader-format quader-blob-v2", "format", a.stored,
		withMeta(a.meta, blobcrypt.MetaFormat, "quader-blob-v2"), nil)
	neg(a, "no-record", rowChangedRecord, "the body of a without any record, as a plain object", "format", a.stored, map[string]string{}, nil)
	neg(a, "changed-key-unknown", rowChangedRecord, "quader-key names sk-default-9-0a1b2c3d, which the reader does not hold", "unknown_key",
		a.stored, withMeta(a.meta, blobcrypt.MetaKey, "sk-default-9-0a1b2c3d"), nil)
	neg(a, "changed-key-other", rowChangedRecord,
		"quader-key names sk-default-2-0a1b2c3d, which the reader holds with the same key bytes: the identifier is in the wrap's additional data",
		"record", a.stored, withMeta(a.meta, blobcrypt.MetaKey, "sk-default-2-0a1b2c3d"), func(c *vcase) {
			c.ReaderKeys = append(c.ReaderKeys, keyEntry{ID: "sk-default-2-0a1b2c3d", Key: hex.EncodeToString(defaultKey)})
		})
	neg(a, "changed-wrap", rowChangedRecord, "one bit of the wrap's sealed bytes changed", "record", a.stored,
		withMeta(a.meta, blobcrypt.MetaWrap, flipB64(a.meta[blobcrypt.MetaWrap], 60)), nil)
	neg(a, "changed-commitment", rowChangedRecord, "one bit of the commitment changed", "commitment", a.stored,
		withMeta(a.meta, blobcrypt.MetaCommitment, flipB64(a.meta[blobcrypt.MetaCommitment], 3)), nil)

	foreignKey := derived("other-installation", "storage key", 32)
	foreign, err := wrap(a.dek, "sk-other-1-0badf00d", foreignKey, derived("other-installation", "salt", 16),
		derived("other-installation", "wrap nonce", 12), digestA, 1)
	w.setErr(err)
	neg(a, "other-installation", rowForeignKey, "the data key of a wrapped under sk-other-1-0badf00d, a key the reader does not hold",
		"unknown_key", a.stored, foreign, nil)
	twinKey := derived("same-id-other-key", "storage key", 32)
	twin, err := wrap(a.dek, defaultKeyID, twinKey, derived("same-id-other-key", "salt", 16),
		derived("same-id-other-key", "wrap nonce", 12), digestA, 1)
	w.setErr(err)
	neg(a, "same-id-other-key", rowForeignKey, "the data key of a wrapped under another key with the identifier sk-default-1-0a1b2c3d",
		"record", a.stored, twin, nil)
	neg(a, "other-connection", rowCopied, "a read by a connection that holds only sk-backup-1-12345678", "unknown_key", a.stored, a.meta,
		func(c *vcase) {
			c.ReaderKeys = []keyEntry{{ID: "sk-backup-1-12345678", Key: hex.EncodeToString(derived("other-connection", "storage key", 32))}}
		})

	second := derived("two-keys", "second data key", 32)
	twoKeys, err := wrap(second, defaultKeyID, defaultKey, derived("two-keys", "salt", 16), derived("two-keys", "wrap nonce", 12), digestA, 1)
	w.setErr(err)
	if twoKeys != nil {
		twoKeys[blobcrypt.MetaCommitment] = a.meta[blobcrypt.MetaCommitment]
	}
	neg(a, "two-keys", rowTwoKeys, "a second data key wrapped for the body of a, with the commitment of the first", "commitment",
		a.stored, twoKeys, nil)

	resent := forge(a, "spliced", []byte("b"), 0, 1, blobcrypt.Final{Size: 1, Digest: sha256.Sum256([]byte("b"))})
	if resent != nil {
		neg(a, "spliced", rowSpliced, "the data segment of a sealed again under its data key with another seed and the plaintext b", "digest",
			cat(resent[:29], finalA), a.meta, nil)
	}
	neg(a, "bit-in-segment", rowOneBit, "one bit of the data segment's ciphertext changed", "segment", flip(a.stored, 15), a.meta, nil)
	neg(a, "bit-in-final", rowOneBit, "one bit of the final segment's ciphertext changed", "final", flip(a.stored, 29+20), a.meta, nil)
}

func (w *writer) setErr(err error) {
	if w.err == nil {
		w.err = err
	}
}

func finalPlain(size uint64, digest [32]byte) []byte {
	b := make([]byte, 8, blobcrypt.FinalPlainSize)
	for i := 7; i >= 0; i-- {
		b[i] = byte(size)
		size >>= 8
	}
	return append(b, digest[:]...)
}

func cat(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
