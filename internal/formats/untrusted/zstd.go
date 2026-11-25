package untrusted

import (
	"errors"
	"io"

	"github.com/klauspost/compress/zstd"

	"github.com/guided-traffic/quader/internal/errs"
)

// NewZstdReader returns a reader of the Zstandard stream r whose decompressed bytes are capped at lim.UncompressedBytes: a stream
// that decompresses to more fails with TooLarge instead of ending quietly (R-DEB-034). The decoder runs on one goroutine and its
// memory is bounded by the cap.
//
// The work package of the package-file libraries extends the checks around it (T-17's plan, WP1).
func NewZstdReader(r io.Reader, lim Limits) (io.Reader, error) {
	return NewZstdReaderMax(r, effective(lim).UncompressedBytes)
}

// NewZstdReaderMax is [NewZstdReader] with the cap given in bytes (see [NewXZReaderMax]).
func NewZstdReaderMax(r io.Reader, max int64) (io.Reader, error) {
	zr, err := zstd.NewReader(r, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(uint64(max))) //nolint:gosec // G115: effective() makes the limit positive
	if err != nil {
		return nil, Refused("The content is not a valid Zstandard stream.", errs.WithCause(err))
	}
	return &zstdReader{zr: zr, r: Limit(zr, max)}, nil
}

// NewZstdReaderWindow is [NewZstdReaderMax] with a bound of its own on the window a frame may declare (R-DEB-039): the decoder keeps
// a window's bytes in memory, and a frame may declare up to the decompressed cap; a stream that keeps nothing (the walk of a data
// archive) needs no more than the window of the compressors' levels, which dpkg-deb never sets above 8 MiB by default.
func NewZstdReaderWindow(r io.Reader, max, window int64) (io.Reader, error) {
	zr, err := zstd.NewReader(r, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(uint64(max)), //nolint:gosec // G115: positive bounds
		zstd.WithDecoderMaxWindow(uint64(window))) //nolint:gosec // G115: positive bounds
	if err != nil {
		return nil, Refused("The content is not a valid Zstandard stream.", errs.WithCause(err))
	}
	return &zstdReader{zr: zr, r: Limit(zr, max)}, nil
}

// zstdReader releases the decoder's goroutines when the stream ends or fails.
type zstdReader struct {
	zr *zstd.Decoder
	r  io.Reader
}

func (z *zstdReader) Read(p []byte) (int, error) {
	n, err := z.r.Read(p)
	if err != nil {
		z.zr.Close()
		if errors.Is(err, zstd.ErrDecoderSizeExceeded) || errors.Is(err, zstd.ErrWindowSizeExceeded) {
			err = TooLarge("the content is larger than the limit for a decompressed stream")
		}
	}
	return n, err
}
