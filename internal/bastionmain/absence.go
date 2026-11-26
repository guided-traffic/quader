package bastionmain

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
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
//   - [NotePackage] is called by every rights decision the host makes for a format (R-BAS-081, Q-BAS-241):
//     Host.Authorize, whether it allows or refuses, Host.WikiReadable, each path of Host.PathsAllowed in the package
//     the format's Classify places it in, and each MayRead row of the read scope the host hands out. These are the
//     decisions the conformance kit's host records for check 21 (R-BAS-086), which holds every format to deciding all
//     of a response's reads before its status line.
//   - [NoteHandover] is called by the serve point right before it writes the file's 200 without its body, with the encoded
//     value of a handover per blob (storage.md R-STOR-095 step 11): the marking stage writes the class handover and the
//     value as Quader-Bastion-Handover (R-BAS-073), so that the marking stays written in one place.
//
// A 404 is absent exactly when something was noted missing, nothing unknown, and the caller's read shape of
// the addressed repository is full, so that no subject-dependent hiding exists to begin with.
type Absence struct {
	missing, unknown, counted atomic.Bool

	// kind is the kind of the route the request matched (NoteRoute), "" before the route is known.
	kind atomic.Pointer[string]

	pkgMu sync.Mutex
	pkg   string
	named packageState

	// handover is the encoded value of a handover (iface.EncodeHandover), "" for none. It holds a bearer link for its life and,
	// from package 4, a data key: it lives in the request's memory only and the marking writes it into one header.
	handover atomic.Pointer[string]
}

// Handover returns the encoded value of the handover the serve point noted, "" for none.
func (a *Absence) Handover() string {
	if a == nil {
		return ""
	}
	if v := a.handover.Load(); v != nil {
		return *v
	}
	return ""
}

// packageState is what the decisions of a request said about its package so far.
type packageState uint8

const (
	packageNone    packageState = iota // no decision named a package yet
	packageOne                         // every decision so far named Absence.pkg
	packageCleared                     // a decision named no package, or another one: the response belongs to none
)

// Absent reports whether the lookups of the request amount to "missing for every subject".
func (a *Absence) Absent() bool { return a != nil && a.missing.Load() && !a.unknown.Load() }

// NoteRoute records the kind of the route the request matched (a Kind constant). The marking stage's route observer calls it when the
// dispatcher tells it the route, before the route decides, reads or serves anything; [ForHead] reads it.
func (a *Absence) NoteRoute(kind string) {
	if a != nil {
		a.kind.Store(&kind)
	}
}

// documentRoute reports whether the request matched a route of a document: a metadata or a computed route, whose answer a Bastion
// keeps as a document (the classes shared and private, R-BAS-013). A download route's answer is an artifact; an unknown route is none.
func (a *Absence) documentRoute() bool {
	if a == nil {
		return false
	}
	k := a.kind.Load()
	return k != nil && (*k == KindMetadata || *k == KindComputed)
}

// Counted reports whether the core started an answer of the request that it counts as a download.
func (a *Absence) Counted() bool { return a != nil && a.counted.Load() }

// Package returns the package of the response (R-BAS-081): P exactly when at least one decision of the request named P,
// every decision named P, and none named no package; "" otherwise. The marking reads it at the status line, so a decision
// after it changes nothing on the wire; kit check 21 holds every format to making none.
func (a *Absence) Package() string {
	if a == nil {
		return ""
	}
	a.pkgMu.Lock()
	defer a.pkgMu.Unlock()
	if a.named != packageOne {
		return ""
	}
	return a.pkg
}

// notePackage folds one decision's package into the record: a package-less decision, or a second package, clears it for
// good.
func (a *Absence) notePackage(pkg string) {
	a.pkgMu.Lock()
	defer a.pkgMu.Unlock()
	switch {
	case a.named == packageCleared:
	case pkg == "":
		a.named, a.pkg = packageCleared, ""
	case a.named == packageNone:
		a.named, a.pkg = packageOne, pkg
	case a.pkg != pkg:
		a.named, a.pkg = packageCleared, ""
	}
}

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

// NotePackage records the package a rights decision of the request named, as authz.Coordinate.Package carries it; "" for a
// decision that names no package, which makes the response belong to none (R-BAS-081). It is a no-op on a request that is no
// recognized Bastion request, so an ordinary request pays a context lookup.
func NotePackage(ctx context.Context, pkg string) {
	if a := absenceFrom(ctx); a != nil {
		a.notePackage(pkg)
	}
}

// NotesPackages reports whether the request records the packages of its decisions: a recognized Bastion request. A caller
// that would compute a package only to note it asks first, so an ordinary request pays nothing for it.
func NotesPackages(ctx context.Context) bool { return absenceFrom(ctx) != nil }

// NoteCounted records that the core starts an answer of the request whose complete body it counts as a download
// (R-BAS-059). It is a no-op on a request that is no recognized Bastion request.
func NoteCounted(ctx context.Context) {
	if a := absenceFrom(ctx); a != nil {
		a.counted.Store(true)
	}
}

// NoteHandover records the encoded value of a handover per blob for the marking stage (R-BAS-073, storage.md R-STOR-095 step
// 11) and reports whether it was recorded: only on a recognized forward that offered a handover (Recognition.Offered) and whose
// answer the marking stage marks. On every other request it records nothing and reports false, and the serve point streams the
// download, so that no empty 200 ever leaves without the value that explains it.
func NoteHandover(ctx context.Context, value string) bool {
	rec, ok := RecognitionFrom(ctx)
	a := absenceFrom(ctx)
	if !ok || !rec.Offered || rec.Route || a == nil || value == "" {
		return false
	}
	a.handover.Store(&value)
	return true
}

// MarksHandover reports whether a handover the serve point decided could reach the Bastion: the request is a recognized forward
// that offered one, and the marking stage marks its answer. The serve point asks it before it decides anything else, so an
// ordinary request pays a context lookup and nothing more.
func MarksHandover(ctx context.Context) (Recognition, bool) {
	rec, ok := RecognitionFrom(ctx)
	if !ok || !rec.Offered || rec.Route || absenceFrom(ctx) == nil {
		return Recognition{}, false
	}
	return rec, true
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
