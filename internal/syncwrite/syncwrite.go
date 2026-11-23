// Package syncwrite is the one way the scanning units write a large file to a local volume: it flushes the file to the disk every
// [Every] bytes, so the dirty pages of the write never pile up in the writer's memory (vulnerability-scanning.md Q-VULN-225).
//
// The kernel charges the dirty pages of a write to the memory cgroup of the writer, and on a node whose file system does not write them
// back per cgroup it starts the write-back only at the machine's thresholds: the loader of the image scanner's database, a container of
// 256 MiB, was killed with 233 MB of dirty pages and none under write-back while it unpacked a database of 1.8 GB (measured on the kind
// cluster on 2026-11-21, vulnerability-scanning.md Q-VULN-212). A flush every 32 MiB bounds them. The image scanner's layers, its
// analyses, the image scanner's database and the malware scanning's signature files are written through [Copy].
//
// Standard library only: a leaf every layer may import.
package syncwrite

import (
	"errors"
	"io"
)

// Every is how many bytes are written between two flushes.
const Every = 32 << 20

// File is the destination of a copy: an *os.File, or a recorder in a test.
type File interface {
	io.Writer
	Sync() error
}

// Copy copies src to dst until src ends and flushes dst after every `every` bytes and after the last bytes, so at most `every` bytes of
// the copy are dirty at a time. It returns the bytes written and the first error of a read, a write or a flush. A copy of nothing
// flushes nothing. every must be positive.
func Copy(dst File, src io.Reader, every int64) (int64, error) {
	if every <= 0 {
		return 0, errors.New("syncwrite: the flush interval must be positive")
	}
	var n int64
	for {
		c, err := io.CopyN(dst, src, every)
		n += c
		if c > 0 {
			if serr := dst.Sync(); serr != nil {
				return n, serr
			}
		}
		if errors.Is(err, io.EOF) {
			return n, nil
		}
		if err != nil {
			return n, err
		}
	}
}
