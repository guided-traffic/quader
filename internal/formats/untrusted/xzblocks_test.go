package untrusted

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"
)

func xzBlocksOf(t *testing.T, data []byte, blockSize int64) []byte {
	t.Helper()
	var b bytes.Buffer
	w, err := xz.WriterConfig{DictCap: 1 << 16, BlockSize: blockSize}.NewWriter(&b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// R-DEB-039: the block reader decodes a stream of one block and of many, refuses a block that declares a dictionary over its bound before
// a byte of it is decoded, refuses a filter chain it does not read and caps the decompressed bytes.
func TestR_DEB_039_TheXZBlockReaderBoundsTheDictionary(t *testing.T) {
	data := []byte(strings.Repeat("quader reads the data archive as a stream. ", 2000))
	for _, bs := range []int64{0, 4096} {
		r, err := NewXZBlocksReader(bytes.NewReader(xzBlocksOf(t, data, bs)), 1<<20, 8<<20)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(r)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("block size %d: %d bytes, %v", bs, len(got), err)
		}
	}

	stream := xzBlocksOf(t, data, 0)
	at := bytes.Index(stream[12:], []byte{0x21, 0x01}) + 12 + 2
	big := append([]byte(nil), stream...)
	big[at] = 40 // 4 GiB
	r, err := NewXZBlocksReader(bytes.NewReader(big), 1<<20, 8<<20)
	if err == nil {
		_, err = io.ReadAll(r)
	}
	if !IsTooLarge(err) {
		t.Fatalf("a 4 GiB dictionary: %v", err)
	}

	bcj := append([]byte(nil), stream...)
	bcj[13] |= 0x01 // two filters
	if r, err := NewXZBlocksReader(bytes.NewReader(bcj), 1<<20, 8<<20); err == nil {
		if _, err = io.ReadAll(r); err == nil || IsTooLarge(err) {
			t.Fatalf("a filter chain: %v", err)
		}
	}

	r, err = NewXZBlocksReader(bytes.NewReader(stream), 1000, 8<<20)
	if err == nil {
		_, err = io.ReadAll(r)
	}
	if !IsTooLarge(err) {
		t.Fatalf("the decompressed cap: %v", err)
	}
	if _, err := NewXZBlocksReader(strings.NewReader("not xz at all"), 1<<20, 8<<20); err == nil {
		t.Fatal("a stream that is no xz was read")
	}
}
