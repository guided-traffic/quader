// Package syncwrite is the one way a process of Quader writes a large file to a local volume: it flushes the file to the disk every
// [Every] bytes, so the dirty pages of the write never pile up in the writer's memory (vulnerability-scanning.md Q-VULN-225,
// storage.md Q-STOR-486).
//
// The kernel charges the dirty pages of a write to the memory cgroup of the writer, and on a node whose file system does not write them
// back per cgroup it starts the write-back only at the machine's thresholds: the loader of the image scanner's database, a container of
// 256 MiB, was killed with 233 MB of dirty pages and none under write-back while it unpacked a database of 1.8 GB (measured on the kind
// cluster on 2026-11-21, vulnerability-scanning.md Q-VULN-212). A flush every 32 MiB bounds them. The image scanner's layers, its
// analyses, the image scanner's database and the malware scanning's signature files are written through [Copy]; the upload spool, the
// Bastion's fills, the offline tool and the importer through [Writer].
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
	w := NewWriter(dst, every)
	n, err := io.Copy(w, src)
	if err != nil {
		return n, err
	}
	return n, w.Sync()
}

// Writer is the same flush for a write that arrives in pieces (the upload spool, the Bastion's fills): it writes to its file and
// flushes it whenever `every` bytes have been written since the last flush, at exactly those offsets. What is written after the last
// such point stays dirty until [Writer.Sync] or the file's end; that is at most `every` bytes.
type Writer struct {
	f            File
	every, dirty int64
}

// NewWriter returns a Writer over f that flushes every `every` bytes; zero or less is [Every].
func NewWriter(f File, every int64) *Writer {
	if every <= 0 {
		every = Every
	}
	return &Writer{f: f, every: every}
}

// Write implements io.Writer.
func (w *Writer) Write(p []byte) (int, error) {
	n := 0
	for len(p) > 0 {
		k := min(int64(len(p)), w.every-w.dirty)
		m, err := w.f.Write(p[:k])
		n += m
		w.dirty += int64(m)
		if err != nil {
			return n, &WriteError{Op: "write", Err: err}
		}
		p = p[k:]
		if w.dirty >= w.every {
			if err := w.Sync(); err != nil {
				return n, err
			}
		}
	}
	return n, nil
}

// Sync flushes what was written since the last flush; it does nothing when nothing was.
func (w *Writer) Sync() error {
	if w.dirty == 0 {
		return nil
	}
	w.dirty = 0
	if err := w.f.Sync(); err != nil {
		return &WriteError{Op: "flush", Err: err}
	}
	return nil
}

// WriteError is a failure of the file, not of what was copied into it: a write or a flush the volume refused (a full or a failing
// disk). A caller tells it from a failure of its source with errors.As, so that a full disk is reported as the writer's, never as
// the content's (vulnerability-scanning.md Q-VULN-237).
type WriteError struct {
	// Op is "write" or "flush".
	Op  string
	Err error
}

func (e *WriteError) Error() string { return "the local file: " + e.Op + ": " + e.Err.Error() }

func (e *WriteError) Unwrap() error { return e.Err }
