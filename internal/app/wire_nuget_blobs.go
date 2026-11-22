package app

import (
	"context"
	"io"
	"sync"

	"github.com/guided-traffic/quader/internal/blob"
	nugetdata "github.com/guided-traffic/quader/internal/formatdata/nuget"
)

// nugetBlobs gives the NuGet package view its ranged reads over a stored package (Q-NUGET-48):
// the README of a version is a file inside its .nupkg, and the central directory and that one
// entry are all that is read. A nil store gives none, and the view then has no README text.
func nugetBlobs(s *blob.Store) nugetdata.BlobReader {
	if s == nil {
		return nil
	}
	return blobRanges{s}
}

type blobRanges struct{ s *blob.Store }

func (b blobRanges) ReaderAt(ctx context.Context, sha256 []byte, size int64) io.ReaderAt {
	var d blob.Digest
	copy(d[:], sha256)
	return &rangeReader{ctx: ctx, s: b.s, d: d, size: size}
}

// rangeReader reads a blob with one ranged request per ReadAt. Ranged reads are not verified
// against the digest (R-STOR-013): a README out of a package is display text, never executed.
// The blob's copies are read once, by the first ReadAt, and every range takes them from there
// (Q-STOR-275), so a view's reads of one package cost one statement, not one per range.
type rangeReader struct {
	ctx  context.Context
	s    *blob.Store
	d    blob.Digest
	size int64

	once    sync.Once
	copies  []blob.Copy
	copyErr error
}

func (r *rangeReader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if off >= r.size || len(p) == 0 {
		if len(p) == 0 {
			return 0, nil
		}
		return 0, io.EOF
	}
	r.once.Do(func() { r.copies, r.copyErr = r.s.Copies(r.ctx, r.d) })
	if r.copyErr != nil {
		return 0, r.copyErr
	}
	want := min(int64(len(p)), r.size-off)
	body, err := r.s.Open(r.ctx, r.d, r.size, blob.OpenOptions{
		Range: &blob.ByteRange{Offset: off, Length: want}, Copies: r.copies, CopiesRead: true,
	})
	if err != nil {
		return 0, err
	}
	defer func() { _ = body.Close() }()
	n, err := io.ReadFull(body, p[:want])
	if err == nil && want < int64(len(p)) {
		err = io.EOF
	}
	return n, err
}
