package tarball

import (
	"context"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/formats/untrusted"
)

// ReadFile reads the file of a stored tarball whose install path (the entry name without its first component) is name, at most max
// bytes of it, by streaming the archive up to that entry with the bounded reader every archive of a screen is read with (R-NPM-051,
// R-ARCH-028): a gzip stream has no offsets. A file cut at max loses the partial character at the cut. found is false for a tarball
// without the file; an archive the reader refuses (a bound, a link that leaves it, a broken stream) is an error, which a screen shows as
// no file. Links are not followed.
func ReadFile(ctx context.Context, r io.Reader, lim untrusted.Limits, name string, max int64) (data []byte, found bool, err error) {
	ctx, cancel := lim.Context(ctx)
	defer cancel()
	tr := untrusted.NewTarGz(ctx, r, lim, untrusted.ContainedSymlinks())
	for {
		e, err := tr.Next()
		if err != nil {
			if errors.Is(err, io.EOF) && !untrusted.IsRefused(err) && !untrusted.IsTooLarge(err) {
				return nil, false, nil
			}
			return nil, false, err
		}
		if e.Dir || e.Link {
			continue
		}
		_, install, ok := strings.Cut(strings.TrimPrefix(e.Name, "./"), "/")
		if !ok || install != name {
			continue
		}
		data, err := readPrefix(tr, min(e.Size, max))
		if err != nil {
			return nil, false, err
		}
		if int64(len(data)) < e.Size {
			data = dropPartialRune(data)
		}
		return data, true, nil
	}
}

// readPrefix reads the first n bytes of the current entry.
func readPrefix(r io.Reader, n int64) ([]byte, error) {
	buf := make([]byte, n)
	got, err := io.ReadFull(r, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:got], nil
}

// dropPartialRune drops a character the cut fell inside.
func dropPartialRune(data []byte) []byte {
	for i := len(data) - 1; i >= 0 && i >= len(data)-utf8.UTFMax; i-- {
		if utf8.RuneStart(data[i]) {
			if !utf8.FullRune(data[i:]) {
				return data[:i]
			}
			break
		}
	}
	return data
}
