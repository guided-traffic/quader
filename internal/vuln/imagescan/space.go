package imagescan

import (
	"errors"
	"io/fs"
	"path/filepath"
	"time"
)

// The working space of the image scanning is admitted through the spool of the scanner, the budget every other user of its volume draws
// on (Q-VULN-145, docs/security/image-scanning.md): an analysis holds the compressed size of its image plus a margin, a detection the
// margin alone. The worker writes nothing of a scan to its disk (R-VULN-036, ADR-229): it streams the stored analysis to the call and
// reads the report from the answer. The loader of the scanner pod counts the unpacked database against the budget of its own volume
// (R-VULN-049, UnpackDatabase).

// ErrNoSpace says that the spool cannot take the bytes asked for now: its budget is held or its volume is nearly full. An adapter around a
// spool wraps its own sentinel with it.
var ErrNoSpace = errors.New("the spool has no room")

// Space is a spool as the scanning sees it.
type Space interface {
	// Hold takes n bytes of the budget and returns the function that gives them back; it is safe to call more than once. The error is
	// [ErrNoSpace] when the budget or the volume cannot cover n, and nothing is held then.
	Hold(n int64) (release func(), err error)
	// Adopt counts n bytes that are on the volume already; it checks nothing.
	Adopt(n int64) (release func())
	// Budget is the byte budget of the whole spool.
	Budget() int64
}

// refusedBackoff is the wait before a scan that could never fit, even with the scanner's spool empty, is due again: only a larger spool
// or a smaller image changes that.
const refusedBackoff = time.Hour

// CodeSpoolTooSmall is a scan that cannot fit the scanner's spool even when it is empty. It spends no attempt: the scan did not fail, it
// did not start.
const CodeSpoolTooSmall = "spool_too_small"

// DirBytes is the size of the regular files below dir: what a database found on the volume holds.
func DirBytes(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, e fs.DirEntry, err error) error {
		if err != nil || !e.Type().IsRegular() {
			return nil //nolint:nilerr // a file that vanished counts as nothing
		}
		if info, err := e.Info(); err == nil {
			n += info.Size()
		}
		return nil
	})
	return n
}
