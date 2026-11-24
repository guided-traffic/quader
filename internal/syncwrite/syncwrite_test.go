package syncwrite

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
)

// recorder is a File that records the bytes written when each flush came.
type recorder struct {
	bytes.Buffer
	flushes []int
	failAt  int // the flush of this number fails; 0 none
}

func (r *recorder) Sync() error {
	r.flushes = append(r.flushes, r.Len())
	if r.failAt == len(r.flushes) {
		return errors.New("the disk is gone")
	}
	return nil
}

// TestCopy holds the flush points of Q-VULN-225 with a threshold of a few bytes: one after every full piece and one after the last
// bytes, none for nothing, and never more than the threshold dirty.
func TestCopy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload string
		every   int64
		want    []int
	}{
		{"R-VULN-049 a flush after every piece and after the rest", "0123456789", 4, []int{4, 8, 10}},
		{"R-VULN-049 a payload of whole pieces ends with the last piece's flush", "01234567", 4, []int{4, 8}},
		{"R-VULN-049 a payload below the threshold is flushed once", "012", 4, []int{3}},
		{"R-VULN-049 nothing is not flushed", "", 4, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r recorder
			// A reader of one byte a read: the pieces are counted in bytes written, not in reads.
			n, err := Copy(&r, iotest.OneByteReader(strings.NewReader(tc.payload)), tc.every)
			if err != nil || n != int64(len(tc.payload)) || r.String() != tc.payload {
				t.Fatalf("Copy = %d, %v; wrote %q", n, err, r.String())
			}
			if !slices.Equal(r.flushes, tc.want) {
				t.Fatalf("flushes at %v, want %v", r.flushes, tc.want)
			}
		})
	}
	t.Run("R-VULN-049 a failed flush ends the copy", func(t *testing.T) {
		r := recorder{failAt: 1}
		n, err := Copy(&r, strings.NewReader("0123456789"), 4)
		if err == nil || n != 4 || len(r.flushes) != 1 {
			t.Fatalf("Copy = %d, %v; flushes %v", n, err, r.flushes)
		}
	})
	t.Run("R-VULN-049 a failed read ends the copy after flushing what came", func(t *testing.T) {
		var r recorder
		n, err := Copy(&r, iotest.TimeoutReader(strings.NewReader("0123456789")), 4)
		if !errors.Is(err, iotest.ErrTimeout) || n == 0 {
			t.Fatalf("Copy = %d, %v", n, err)
		}
	})
	t.Run("R-VULN-049 a threshold of zero is refused", func(t *testing.T) {
		if _, err := Copy(&recorder{}, strings.NewReader("x"), 0); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("R-STOR-004 a Writer flushes at exactly every threshold whatever the size of the writes, and Sync flushes the rest once", func(t *testing.T) {
		var r recorder
		w := NewWriter(&r, 4)
		for _, piece := range []string{"012", "3456", "789"} { // the threshold falls inside the second and the third write
			if n, err := w.Write([]byte(piece)); err != nil || n != len(piece) {
				t.Fatalf("Write(%q) = %d, %v", piece, n, err)
			}
		}
		if !slices.Equal(r.flushes, []int{4, 8}) {
			t.Fatalf("flushes at %v after the writes, want [4 8]", r.flushes)
		}
		if err := w.Sync(); err != nil || !slices.Equal(r.flushes, []int{4, 8, 10}) || w.Sync() != nil || len(r.flushes) != 3 {
			t.Fatalf("flushes at %v after Sync, want [4 8 10] and no flush of nothing", r.flushes)
		}
	})
	t.Run("R-VULN-049 an *os.File is a File", func(t *testing.T) {
		f, err := os.Create(filepath.Join(t.TempDir(), "f"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = f.Close() }()
		if n, err := Copy(f, strings.NewReader("0123456789"), 4); err != nil || n != 10 {
			t.Fatalf("Copy = %d, %v", n, err)
		}
	})
}
