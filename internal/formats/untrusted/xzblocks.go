package untrusted

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/ulikunitz/xz/lzma"
)

// NewXZBlocksReader reads the first stream of the xz container r block by block, with a dictionary of at most maxDict bytes per block and
// at most max decompressed bytes in all (R-DEB-039). The xz module allocates the dictionary a block header declares, up to 4 GiB, before
// it decodes a byte; this reader parses the container itself, refuses a block whose LZMA2 dictionary is larger than maxDict, and hands
// only the chunks of the block to the module's LZMA2 decoder with that dictionary. It accepts the one filter chain the module decodes, a
// lone LZMA2 filter (what dpkg-deb and xz write by default), and ends at the stream's index: the index, the footer and a second stream
// are not read, and the checks are not verified (the stored file's digest was, when it was stored). A malformed container is refused,
// a dictionary or a size over the bounds is TooLarge.
func NewXZBlocksReader(r io.Reader, max, maxDict int64) (io.Reader, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	head := make([]byte, 12)
	if _, err := io.ReadFull(br, head); err != nil || !bytes.Equal(head[:6], []byte{0xFD, '7', 'z', 'X', 'Z', 0}) || head[6] != 0 || head[7] > 0x0F {
		return nil, Refused("The content is not a valid xz stream.")
	}
	x := &xzBlocks{br: br, check: xzCheckSize(head[7]), maxDict: maxDict}
	return Limit(x, max), nil
}

type xzBlocks struct {
	br      *bufio.Reader
	check   int
	maxDict int64
	block   io.Reader // the LZMA2 decoder of the current block, nil between blocks
	counted *countingReader
	done    bool
	err     error
}

// xzCheckSize is the length of a block's check field for the stream's check type.
func xzCheckSize(t byte) int {
	if t == 0 {
		return 0
	}
	return 4 << ((t - 1) / 3)
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (c *countingReader) ReadByte() (byte, error) {
	var b [1]byte
	_, err := io.ReadFull(c, b[:])
	return b[0], err
}

func (x *xzBlocks) Read(p []byte) (int, error) {
	for x.err == nil {
		if x.block == nil {
			if x.done {
				return 0, io.EOF
			}
			x.err = x.nextBlock()
			continue
		}
		n, err := x.block.Read(p)
		if errors.Is(err, io.EOF) {
			x.err = x.endBlock()
			if n > 0 {
				return n, nil
			}
			continue
		}
		if err != nil {
			x.err = Refused("The content is not a valid xz stream.")
			return n, x.err
		}
		return n, nil
	}
	return 0, x.err
}

// nextBlock reads a block header, or the index indicator that ends the stream's blocks.
func (x *xzBlocks) nextBlock() error {
	b, err := x.br.ReadByte()
	if err != nil {
		return Refused("The xz stream ends before its index.")
	}
	if b == 0 {
		x.done = true
		return nil
	}
	hdr := make([]byte, (int(b)+1)*4)
	hdr[0] = b
	if _, err := io.ReadFull(x.br, hdr[1:]); err != nil {
		return Refused("An xz block header is cut off.")
	}
	dict, err := blockDict(hdr)
	if err != nil {
		return err
	}
	if dict > x.maxDict {
		return TooLarge(fmt.Sprintf("an xz block declares a dictionary of %d bytes, more than %d", dict, x.maxDict))
	}
	x.counted = &countingReader{r: x.br}
	dec, err := lzma.Reader2Config{DictCap: int(max(dict, lzma.MinDictCap))}.NewReader2(x.counted)
	if err != nil {
		return Refused("An xz block cannot be decoded.")
	}
	x.block = struct{ io.Reader }{dec}
	x.counted.n += int64(len(hdr))
	return nil
}

// endBlock skips the padding and the check after a block's data.
func (x *xzBlocks) endBlock() error {
	pad := (4 - x.counted.n%4) % 4
	skip := int(pad) + x.check
	if _, err := x.br.Discard(skip); err != nil {
		return Refused("An xz block is cut off.")
	}
	x.block = nil
	return nil
}

// blockDict reads the dictionary size of the lone LZMA2 filter of a block header (the xz file format, 3.1).
func blockDict(h []byte) (int64, error) {
	flags := h[1]
	if flags&0x3C != 0 || flags&0x03 != 0 {
		return 0, Refused("An xz block uses a filter chain Quader does not read; only a lone LZMA2 filter is read.")
	}
	i := 2
	vli := func() (uint64, bool) {
		var v uint64
		for k := 0; k < 9 && i < len(h)-4; k++ {
			c := h[i]
			i++
			v |= uint64(c&0x7F) << (7 * k)
			if c&0x80 == 0 {
				return v, true
			}
		}
		return 0, false
	}
	if flags&0x40 != 0 {
		if _, ok := vli(); !ok {
			return 0, Refused("An xz block header is not valid.")
		}
	}
	if flags&0x80 != 0 {
		if _, ok := vli(); !ok {
			return 0, Refused("An xz block header is not valid.")
		}
	}
	id, ok := vli()
	if !ok || id != 0x21 {
		return 0, Refused("An xz block uses a filter Quader does not read; only LZMA2 is read.")
	}
	size, ok := vli()
	if !ok || size != 1 || i >= len(h)-4 {
		return 0, Refused("An xz block header is not valid.")
	}
	bits := h[i] & 0x3F
	if h[i]&0xC0 != 0 || bits > 40 {
		return 0, Refused("An xz block header is not valid.")
	}
	if bits == 40 {
		return 0xFFFFFFFF, nil
	}
	return int64(2|bits&1) << (bits/2 + 11), nil
}
