package bastionmain

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
)

// Absence is the record of what the lookups and the download recording of one recognized Bastion request found
// (R-BAS-013, R-BAS-059, Q-BAS-70, Q-BAS-177). The answer class `absent` says that the item is missing for every
// subject, so that the Bastion may keep a negative entry that must never stand for an item that is merely hidden from
// the caller (R-BAS-024); `Quader-Bastion-Counted` says that serving the response counts a download at the main
// installation, so that the Bastion reports the serves of the entry from its cache. No format code decides either: the
// core's own lookups and its download recording note what they could tell, and the marking stage reads the record
// when the status line goes out.
//
//   - [NoteMissing] is called by a lookup of the host that is subject-independent and authoritative: a package
//     or an item that a local repository does not hold, a document or an artifact that the upstream of a remote
//     repository answered 404 or 410 for, or that its negative entry stands for.
//   - [NoteUnknown] is called when the request met something that makes a 404 mean less: a refusal of the
//     decision point or of a repository's include and exclude patterns, a serve-time refusal, a remote member
//     that could not tell (offline, not cached, only-if-cached).
//   - [NoteCounted] is called, through [CountingWriter], when the core starts an answer whose complete body it counts
//     as a download (items.Store.Served after edge.ServeItem, the remote engine's streamed miss).
//
// A 404 is absent exactly when something was noted missing, nothing unknown, and the caller's read shape of
// the addressed repository is full, so that no subject-dependent hiding exists to begin with.
type Absence struct {
	missing, unknown, counted atomic.Bool
}

// Absent reports whether the lookups of the request amount to "missing for every subject".
func (a *Absence) Absent() bool { return a != nil && a.missing.Load() && !a.unknown.Load() }

// Counted reports whether the core started an answer of the request that it counts as a download.
func (a *Absence) Counted() bool { return a != nil && a.counted.Load() }

type absenceKey struct{}

// WithAbsence returns ctx carrying a new record. The marking stage calls it for a recognized request.
func WithAbsence(ctx context.Context) (context.Context, *Absence) {
	a := &Absence{}
	return context.WithValue(ctx, absenceKey{}, a), a
}

func absenceFrom(ctx context.Context) *Absence {
	a, _ := ctx.Value(absenceKey{}).(*Absence)
	return a
}

// NoteMissing records that an authoritative lookup found the item missing for every subject. It is a no-op
// on a request that is no recognized Bastion request.
func NoteMissing(ctx context.Context) {
	if a := absenceFrom(ctx); a != nil {
		a.missing.Store(true)
	}
}

// NoteUnknown records that the request met a refusal, a serve-time check or a lookup that could not tell, so
// that a 404 of it is never `absent`. It is a no-op on a request that is no recognized Bastion request.
func NoteUnknown(ctx context.Context) {
	if a := absenceFrom(ctx); a != nil {
		a.unknown.Store(true)
	}
}

// NoteCounted records that the core starts an answer of the request whose complete body it counts as a download
// (R-BAS-059). It is a no-op on a request that is no recognized Bastion request.
func NoteCounted(ctx context.Context) {
	if a := absenceFrom(ctx); a != nil {
		a.counted.Store(true)
	}
}

// CountingWriter returns the writer a counting serve of the core answers r through: on a recognized Bastion request it
// notes [NoteCounted] when the answer's status line is the one of a download the core counts once its body is complete
// (R-STOR-015): a GET answered 200, or 206 from byte 0. The note is made before the status line reaches the marking
// stage, which writes Quader-Bastion-Counted with it; the count itself happens after the body (Q-BAS-186). A HEAD, a
// 304, a later range and every refusal note nothing. On any other request it returns w itself, so an ordinary request
// pays nothing.
func CountingWriter(r *http.Request, w http.ResponseWriter) http.ResponseWriter {
	if r == nil || r.Method != http.MethodGet || absenceFrom(r.Context()) == nil {
		return w
	}
	return &countingWriter{ResponseWriter: w, ctx: r.Context()}
}

type countingWriter struct {
	http.ResponseWriter
	ctx   context.Context // the context of the one request the writer answers
	wrote bool
}

func (c *countingWriter) WriteHeader(code int) {
	// An informational status is not the answer.
	if !c.wrote && code >= http.StatusOK {
		c.wrote = true
		if code == http.StatusOK || (code == http.StatusPartialContent && strings.HasPrefix(c.Header().Get("Content-Range"), "bytes 0-")) {
			NoteCounted(c.ctx)
		}
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *countingWriter) Write(p []byte) (int, error) {
	if !c.wrote {
		c.WriteHeader(http.StatusOK)
	}
	return c.ResponseWriter.Write(p)
}

// ReadFrom keeps the underlying writer's io.ReaderFrom fast path for a body copied with io.Copy; the status decision is made
// first, as a Write would make it.
func (c *countingWriter) ReadFrom(src io.Reader) (int64, error) {
	if !c.wrote {
		c.WriteHeader(http.StatusOK)
	}
	if rf, ok := c.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(src)
	}
	return io.Copy(struct{ io.Writer }{c.ResponseWriter}, src)
}

// Flush implements http.Flusher.
func (c *countingWriter) Flush() {
	if !c.wrote {
		c.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(c.ResponseWriter).Flush()
}

// Unwrap lets http.ResponseController reach the underlying writer (deadlines).
func (c *countingWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }
