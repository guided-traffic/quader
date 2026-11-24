package versionkey

import (
	"bytes"
	"strings"
	"testing"
)

// Mechanism of R-API-029, not a proof of its criteria: the parts of a stored sort key keep their order whatever bytes follow them.
func TestTextAndNumberKeepTheirOrderWhateverFollows(t *testing.T) {
	texts := []string{"", "\x00", "\x00\x00", "a", "a\x00", "a\x00b", "a\x01", "ab", "b", "\xff"}
	tails := [][2][]byte{{nil, nil}, {{0x00}, {0xFF}}, {{0xFF}, {0x00}}}
	for _, x := range texts {
		for _, y := range texts {
			if x == y {
				continue
			}
			for _, tail := range tails {
				kx, ky := append(Text(nil, x), tail[0]...), append(Text(nil, y), tail[1]...)
				if got, want := bytes.Compare(kx, ky), strings.Compare(x, y); got != want {
					t.Errorf("Text(%q)+%x vs Text(%q)+%x: %d, want %d", x, tail[0], y, tail[1], got, want)
				}
			}
		}
	}
	nums := []string{"", "0", "000", "1", "01", "9", "10", "99", "100", "123456789012345678901234567890"}
	for _, x := range nums {
		for _, y := range nums {
			tx, ty := strings.TrimLeft(x, "0"), strings.TrimLeft(y, "0")
			want := sign(len(tx) - len(ty))
			if want == 0 {
				want = strings.Compare(tx, ty)
			}
			if want == 0 {
				if !bytes.Equal(Number(nil, x), Number(nil, y)) {
					t.Errorf("Number(%q) and Number(%q) differ", x, y)
				}
				continue
			}
			if got := bytes.Compare(append(Number(nil, x), 0xFF), append(Number(nil, y), 0x00)); got != want {
				t.Errorf("Number(%q) vs Number(%q): %d, want %d", x, y, got, want)
			}
		}
	}
	if k := Of([]byte{0xFF}, false, "x"); !bytes.Equal(k, []byte{NotVersion, 'x', 0x00, 0x01}) {
		t.Errorf("a string that is no version: %x", k)
	}
	if bytes.Compare(Of(nil, false, "\xff\xff"), Of(nil, true, "")) >= 0 {
		t.Error("a string that is no version sorts after a version")
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}
