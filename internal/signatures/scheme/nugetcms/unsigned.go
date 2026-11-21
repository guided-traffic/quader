package nugetcms

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash"
	"io"
	"sort"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki"
)

// The unsigned bytes of a signed package (R-SIG-006, R-SIG-029, NuGet's package signature technical details): the package without the
// .signature.p7s entry's local header, data and central directory record, with the offsets of the later entries and of the central
// directory, the entry counts and the central directory's size as they were before the signature was added. The view is streamed once
// from the stored package; nothing is written.

// The zip structures the view rewrites (APPNOTE.TXT 4.3.7, 4.3.12, 4.3.16).
const (
	sigCentral     = 0x02014b50
	sigEnd         = 0x06054b50
	sigEnd64Locate = 0x07064b50
	centralLen     = 46
	endLen         = 22
	maxComment     = 1<<16 - 1
	// maxCentral bounds the central directory read into memory: 65,535 entries of names up to a kilobyte.
	maxCentral = 64 << 20
	// zip64Marker is the value a 32-bit field holds when its zip64 extension carries the real value.
	zip64Marker = 0xffffffff
)

// centralRecord is one record of the central directory: its bytes and the offset of its entry's local header.
type centralRecord struct {
	raw   []byte
	name  string
	local int64
}

// zipLayout is what the view needs of a package: the central directory's records and place, and the end record with its comment.
type zipLayout struct {
	records       []centralRecord
	centralOffset int64
	end           []byte
}

// hashUnsigned writes the unsigned bytes of the package blob into h.
func hashUnsigned(ctx context.Context, in scheme.Input, blob [32]byte, h hash.Hash) error {
	c, err := pki.Open(ctx, in, blob)
	if err != nil {
		return err
	}
	if c.Size() > in.Limits.MaxContentSize {
		return pki.Unsupported(signatures.ReasonLimitsExceeded, "the package is larger than signatures.max_content_size")
	}
	ra, err := c.ReaderAt()
	if err != nil {
		return pki.Transient(err)
	}
	z, err := layout(ra, c.Size())
	if err != nil {
		return err
	}
	return z.writeUnsigned(ctx, ra, h)
}

// layout reads the end record and the central directory of a package.
func layout(ra io.ReaderAt, size int64) (*zipLayout, error) {
	end, endAt, err := endRecord(ra, size)
	if err != nil {
		return nil, err
	}
	count := int(binary.LittleEndian.Uint16(end[10:]))
	cdSize := int64(binary.LittleEndian.Uint32(end[12:]))
	cdOff := int64(binary.LittleEndian.Uint32(end[16:]))
	if cdOff == zip64Marker || cdSize == zip64Marker || count == 0xffff {
		return nil, pki.Unsupported(signatures.ReasonVersionUnsupported, "a zip64 package is not one Quader reads the unsigned bytes of")
	}
	if cdSize > maxCentral || cdOff+cdSize != endAt {
		return nil, pki.Malformed("the package's central directory is not where its end record says")
	}
	cd := make([]byte, cdSize)
	if _, err := ra.ReadAt(cd, cdOff); err != nil {
		return nil, pki.Transient(err)
	}
	records, err := centralRecords(cd, count)
	if err != nil {
		return nil, err
	}
	return &zipLayout{records: records, centralOffset: cdOff, end: end}, nil
}

// endRecord finds the end-of-central-directory record, whose comment runs to the end of the package.
func endRecord(ra io.ReaderAt, size int64) (end []byte, at int64, err error) {
	tail := min(size, endLen+maxComment)
	if tail < endLen {
		return nil, 0, pki.Malformed("the package is no zip archive")
	}
	buf := make([]byte, tail)
	if _, err := ra.ReadAt(buf, size-tail); err != nil {
		return nil, 0, pki.Transient(err)
	}
	for i := len(buf) - endLen; i >= 0; i-- {
		if binary.LittleEndian.Uint32(buf[i:]) != sigEnd {
			continue
		}
		if int(binary.LittleEndian.Uint16(buf[i+20:])) != len(buf)-i-endLen {
			continue
		}
		at = size - tail + int64(i)
		if at >= 20 && locator(ra, at-20) {
			return nil, 0, pki.Unsupported(signatures.ReasonVersionUnsupported, "a zip64 package is not one Quader reads the unsigned bytes of")
		}
		return append([]byte(nil), buf[i:]...), at, nil
	}
	return nil, 0, pki.Malformed("the package has no end-of-central-directory record")
}

// locator reports whether a zip64 end locator stands at off.
func locator(ra io.ReaderAt, off int64) bool {
	var b [4]byte
	_, err := ra.ReadAt(b[:], off)
	return err == nil && binary.LittleEndian.Uint32(b[:]) == sigEnd64Locate
}

// centralRecords splits the central directory into its records.
func centralRecords(cd []byte, count int) ([]centralRecord, error) {
	out := make([]centralRecord, 0, count)
	for rest := cd; len(rest) > 0; {
		if len(rest) < centralLen || binary.LittleEndian.Uint32(rest) != sigCentral {
			return nil, pki.Malformed("the package's central directory does not parse")
		}
		n := centralLen + int(binary.LittleEndian.Uint16(rest[28:])) + int(binary.LittleEndian.Uint16(rest[30:])) +
			int(binary.LittleEndian.Uint16(rest[32:]))
		if n > len(rest) {
			return nil, pki.Malformed("the package's central directory does not parse")
		}
		local := binary.LittleEndian.Uint32(rest[42:])
		if local == zip64Marker {
			return nil, pki.Unsupported(signatures.ReasonVersionUnsupported, "a zip64 package is not one Quader reads the unsigned bytes of")
		}
		name := string(rest[centralLen : centralLen+int(binary.LittleEndian.Uint16(rest[28:]))])
		out, rest = append(out, centralRecord{raw: rest[:n], name: name, local: int64(local)}), rest[n:]
	}
	if len(out) != count {
		return nil, pki.Malformed("the package's end record counts %d entries, its central directory holds %d", count, len(out))
	}
	return out, nil
}

// writeUnsigned streams the view: the bytes before the first entry, every entry's local region but the signature's, the central
// directory records but the signature's with their offsets moved back, and the end record with its counts, size and offset moved back.
func (z *zipLayout) writeUnsigned(ctx context.Context, ra io.ReaderAt, h hash.Hash) error {
	sig := -1
	for i, r := range z.records {
		if r.name == signatureEntry {
			if sig >= 0 {
				return pki.Malformed("the package holds two signature entries")
			}
			sig = i
		}
	}
	if sig < 0 {
		return pki.Malformed("the package holds no signature entry")
	}
	starts := make([]int64, 0, len(z.records)+1)
	for _, r := range z.records {
		starts = append(starts, r.local)
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i] < starts[j] })
	starts = append(starts, z.centralOffset)
	sigStart := z.records[sig].local
	var sigLen int64
	prev := int64(0)
	for i, s := range starts {
		if s < prev || (i > 0 && s == starts[i-1]) {
			return pki.Malformed("the package's entries overlap")
		}
		if i+1 < len(starts) && s == sigStart {
			sigLen = starts[i+1] - s
		}
		prev = s
	}
	if err := z.writeLocal(ctx, ra, h, starts, sigStart); err != nil {
		return err
	}
	z.writeCentral(h, sig, sigStart, sigLen)
	return nil
}

// writeLocal streams the region before the first entry and every local region but the signature's.
func (z *zipLayout) writeLocal(ctx context.Context, ra io.ReaderAt, h hash.Hash, starts []int64, sigStart int64) error {
	copyRange := func(off, n int64) error {
		if err := ctx.Err(); err != nil {
			return pki.Transient(err)
		}
		if _, err := io.Copy(h, io.NewSectionReader(ra, off, n)); err != nil {
			return pki.Transient(err)
		}
		return nil
	}
	if err := copyRange(0, starts[0]); err != nil {
		return err
	}
	for i := 0; i+1 < len(starts); i++ {
		if starts[i] == sigStart {
			continue
		}
		if err := copyRange(starts[i], starts[i+1]-starts[i]); err != nil {
			return err
		}
	}
	return nil
}

// writeCentral writes the central directory without the signature's record and the end record as they were before signing.
func (z *zipLayout) writeCentral(h hash.Hash, sig int, sigStart, sigLen int64) {
	var cdSize int64
	for i, r := range z.records {
		if i == sig {
			continue
		}
		rec := bytes.Clone(r.raw)
		if r.local > sigStart {
			binary.LittleEndian.PutUint32(rec[42:], uint32(r.local-sigLen)) //nolint:gosec // below the zip64 marker, checked by centralRecords
		}
		h.Write(rec)
		cdSize += int64(len(rec))
	}
	end := bytes.Clone(z.end)
	binary.LittleEndian.PutUint16(end[8:], binary.LittleEndian.Uint16(end[8:])-1)
	binary.LittleEndian.PutUint16(end[10:], binary.LittleEndian.Uint16(end[10:])-1)
	binary.LittleEndian.PutUint32(end[12:], uint32(cdSize))                 //nolint:gosec // at most maxCentral
	binary.LittleEndian.PutUint32(end[16:], uint32(z.centralOffset-sigLen)) //nolint:gosec // below the zip64 marker, checked by layout
	h.Write(end)
}
