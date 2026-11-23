package edge

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/items"
)

// ServeOptions configure [ServeItem].
type ServeOptions struct {
	// Buffer bounds the copy buffer of a body that is not the verifying reader
	// (storage.download.buffer_size); a value below 4,096 uses 4,096.
	Buffer int
	// Served, when set, runs after a body that started at byte 0 was written
	// completely: 200 with the whole file, or 206 from byte 0 (R-STOR-015). HEAD,
	// 304 and a range that starts later are not served in that sense.
	Served func()
	// Admit is the check of the download limits before a download starts (usage-and-limits.md R-LIM-021): it runs for a GET
	// that writes a body (200 or 206), after the conditionals and the range were evaluated and before open and the status
	// line. A refusal is returned like the error of open: nothing was written and the caller renders it. Nil admits.
	Admit func() error
	// Link is the last steps of the delivery by signed links of the object store (storage.md R-STOR-070 steps 12 and 13): it runs
	// after Admit, only for a GET that would carry the whole file with 200, and answers a link, or false to stream the file as
	// without it. The caller sets it only where the earlier conditions held. Nil streams.
	Link func() (blob.Link, bool)
	// Linked runs after the redirect to a link was written (R-STOR-073): the caller counts the download and charges its size there.
	Linked func(blob.Link)
	// Handover is the last steps of the handover per blob to a Bastion (storage.md R-STOR-095 steps 5, 7, 9 and 11): it runs after
	// Admit, only for a GET that would carry the whole file with 200, and reports true when it noted a handover's value for the
	// marking (bastionmain.NoteHandover); ServeItem then writes the file's own 200 with its headers, the format's included, and
	// Content-Length: 0, without opening the object. False streams the file as without it. The caller sets it only for a
	// recognized forward that offered a handover and where the earlier conditions held. Nil streams.
	Handover func() bool
	// HandedOver runs after the 200 of a handover was written: the caller counts the download and charges its size there, as for a
	// link (R-STOR-095).
	HandedOver func()
}

// CacheControl is `private, no-cache` for an authenticated request and `no-cache`
// for an anonymous one; clients revalidate with the ETag (R-STOR-012).
func CacheControl(r *http.Request) string {
	if httpserver.PrincipalFrom(r.Context()).Anonymous {
		return "no-cache"
	}
	return "private, no-cache"
}

// ServeItem answers a GET or HEAD of a stored file (R-STOR-012, R-ARCH-007): the
// conditional headers and the range are evaluated from the item row alone, the
// body comes from open (the verifying reader of R-STOR-013), and every response
// that carries the file goes through the content writer, which sets the content
// headers of R-ARCH-007 and checks them again at the status line. The headers of
// the file are collected first; they go to a response of a status that carries the
// file (200, 206, 304) and never to a problem.
//
// The status line is written only after open succeeded, so an object that cannot
// be opened is a problem the client can read: ServeItem returns the error of open
// and writes nothing, and the caller answers it, or does something else first (a
// remote repository fills the item again when its object is missing, R-STOR-027).
// Every other outcome is written and the result is nil. A body that fails in the
// middle aborts the response (panic(http.ErrAbortHandler)): the client sees a
// truncated transfer, never a complete corrupted file (R-STOR-013). Content-Length
// is always set, so a response is never chunked.
func ServeItem(w http.ResponseWriter, r *http.Request, it items.Item, open func(*items.Range) (io.ReadCloser, error), o ServeOptions) error {
	etag, modified := it.ETag(), it.ModifiedAt.Truncate(time.Second)
	hd := http.Header{}
	hd.Set("ETag", etag)
	hd.Set("Last-Modified", modified.UTC().Format(http.TimeFormat))
	hd.Set("Accept-Ranges", "bytes")
	hd.Set("Cache-Control", CacheControl(r))
	hd.Set("Repr-Digest", reprDigest(r.Header, it.Digest[:], it.SHA512))
	hd.Set("Content-Type", it.MediaTypeOrDefault())
	content := func() http.ResponseWriter {
		cw := httpserver.ContentWriterFor(w, r, httpserver.ContentFile, fileName(it.Path))
		for k, v := range hd {
			cw.Header()[k] = v
		}
		return cw
	}

	switch evaluate(r, etag, modified) {
	case notModified:
		content().WriteHeader(http.StatusNotModified)
		return nil
	case preconditionFailed:
		httpserver.Problem(w, r, errs.E(errs.PreconditionFailed, errs.CodePreconditionFailed, "A precondition of the request does not hold."))
		return nil
	}
	start, length, rr := byteRange(r, etag, modified, it.Size)
	status := http.StatusOK
	switch rr {
	case unsatisfiable:
		w.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(it.Size, 10))
		httpserver.Problem(w, r, errs.E(errs.RangeNotSatisfiable, errs.CodeRangeNotSatisfiable, "The requested range is not satisfiable."))
		return nil
	case partialContent:
		status = http.StatusPartialContent
		hd.Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(start+length-1, 10)+"/"+strconv.FormatInt(it.Size, 10))
	default:
		start, length = 0, it.Size
	}
	hd.Set("Content-Length", strconv.FormatInt(length, 10))
	if r.Method == http.MethodHead {
		content().WriteHeader(status)
		return nil
	}
	var rng *items.Range
	if rr == partialContent {
		rng = &items.Range{Offset: start, Length: length}
	}
	if o.Admit != nil {
		if err := o.Admit(); err != nil { // before the status line, like a failure of open
			return err
		}
	}
	if o.Handover != nil && status == http.StatusOK && o.Handover() {
		// The file's 200 without its bytes (R-BAS-073, Q-BAS-231): every header the file's own 200 carries, Content-Length: 0;
		// the marking adds the class handover and the value. The object is not opened.
		hd.Set("Content-Length", "0")
		content().WriteHeader(http.StatusOK)
		if o.HandedOver != nil {
			o.HandedOver()
		}
		return nil
	}
	if o.Link != nil && status == http.StatusOK {
		if link, ok := o.Link(); ok {
			writeRedirect(w, r, it, link)
			if o.Linked != nil {
				o.Linked(link)
			}
			return nil
		}
	}
	body, err := open(rng)
	if err != nil { // before the status line: the caller's to answer
		return err
	}
	defer body.Close()
	cw := content()
	cw.WriteHeader(status)
	if err := copyBody(cw, body, min(int64(max(o.Buffer, 4096)), max(length, 1))); err != nil {
		if r.Context().Err() != nil { // the client went away
			return nil
		}
		// The held-back bytes were not released, or the object store failed in the
		// middle of the body.
		httpserver.Logger(r.Context()).Error("a download was aborted", "path", it.Path, "error", err)
		panic(http.ErrAbortHandler)
	}
	if start == 0 && o.Served != nil {
		o.Served()
	}
	return nil
}

// writeRedirect answers a download with a link to the stored object (storage.md R-STOR-072, Q-STOR-289): 307 with Location,
// Cache-Control: no-store and Content-Length: 0, through the content writer, which sets the content headers of R-ARCH-007 on every
// answer of a content route; none of ETag, Last-Modified, Accept-Ranges and Repr-Digest, which describe a body the redirect does
// not have. The link is a bearer for its life: it goes into this header and nowhere else.
func writeRedirect(w http.ResponseWriter, r *http.Request, it items.Item, link blob.Link) {
	cw := httpserver.ContentWriterFor(w, r, httpserver.ContentFile, fileName(it.Path))
	h := cw.Header()
	h.Set("Location", link.URL)
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Length", "0")
	cw.WriteHeader(http.StatusTemporaryRedirect)
}

// copyBody streams the body: the verifying reader writes from its own buffer
// (blob.Store.Open), any other body goes through a pooled buffer of at most size
// bytes, so a small file never takes a buffer of storage.download.buffer_size
// (Q-ARCH-241).
func copyBody(w io.Writer, body io.Reader, size int64) error {
	if wt, ok := body.(io.WriterTo); ok {
		_, err := wt.WriteTo(w)
		return err
	}
	buf := blob.Buffer(int(size))
	defer blob.Release(buf)
	_, err := io.CopyBuffer(w, body, (*buf)[:size])
	return err
}

// fileName is the last segment of a canonical path, the name for
// Content-Disposition.
func fileName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}
