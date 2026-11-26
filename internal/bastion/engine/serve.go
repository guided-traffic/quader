package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/guided-traffic/quader/internal/bastion/cache"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/errs"
)

// withheldTail is R-REPO-041's withheld tail towards the client: the last 64 KiB of what the caller receives
// are released only after the whole object was read and verified (by the cache's verifying reader for the
// bytes it holds, by the fill's digest for the bytes of a miss), so a corrupt object never reaches the caller
// as a complete transfer (R-BAS-021, R-BAS-032). A range is verified by reading the object to its end, so the
// range's tail is withheld the same way.
const withheldTail = 64 << 10

// source is a representation the engine serves with the semantics of RFC 9110: a cache entry's bytes, a
// private copy, or a document held in memory.
type source struct {
	size   int64
	header http.Header
	etag   string
	// lastModified is the representation's Last-Modified, which answers If-Modified-Since (R-BAS-056).
	lastModified string
	// open returns the whole representation from its start.
	open func(ctx context.Context) (io.ReadCloser, error)
	// download is set for an artifact: a 200 or 206 served from the cache is a download event (R-BAS-016).
	download func(q *rq)
	// transfer is set for an artifact or a file below /files/ served from the cache: its bytes are a download, checked against the
	// subject's budgets before they start and paced (R-BAS-096).
	transfer bool
	// label is the source of the bytes counted in quader_bastion_bytes_served_total: cache (the default) or main.
	label string
}

// bytesSource is a representation held in memory.
func bytesSource(header http.Header, body []byte) *source {
	return &source{
		size: int64(len(body)), header: header, etag: header.Get("Etag"), lastModified: header.Get("Last-Modified"),
		open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil },
	}
}

// plan is how a request is answered from a representation (RFC 9110 sections 13 and 14): 304, 416, or the bytes
// [start, end], partial for a 206.
type plan struct {
	notModified, unsat, partial bool
	start, end                  int64
}

// planFor decides a request against a representation's validator, date and length: If-None-Match, or without it
// If-Modified-Since on GET and HEAD (R-BAS-022, R-BAS-056), both before Range with If-Range (RFC 9110 section 13.2.2).
func planFor(r *http.Request, etag, lastModified string, size int64) plan {
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		if etag != "" && etagMatches(inm, etag) {
			return plan{notModified: true}
		}
	} else if notModifiedSince(r, lastModified) {
		return plan{notModified: true}
	}
	start, end, partial, unsat := parseRange(r.Header.Get("Range"), r.Header.Get("If-Range"), etag, size)
	return plan{unsat: unsat, partial: partial, start: start, end: end}
}

// notModifiedSince reports whether a GET or HEAD carries If-Modified-Since and the representation's Last-Modified is
// not after it (RFC 9110 section 13.1.3): freshclam's check of a container is answered 304 from the entry (R-BAS-056).
func notModifiedSince(r *http.Request, lastModified string) bool {
	ims := r.Header.Get("If-Modified-Since")
	if ims == "" || lastModified == "" || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		return false
	}
	since, err := http.ParseTime(ims)
	if err != nil {
		return false
	}
	lm, err := http.ParseTime(lastModified)
	return err == nil && !lm.After(since)
}

// rangeNotSatisfiable answers 416 with the length of the representation.
func (e *engine) rangeNotSatisfiable(q *rq, size int64) {
	q.w.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(size, 10))
	q.w.Header().Set("Cache-Control", "no-store")
	e.problem(q, errs.E(errs.RangeNotSatisfiable, errs.CodeRangeNotSatisfiable, "The range lies beyond the end of the representation."))
}

// serveSource answers the request from a source: If-None-Match, If-Modified-Since, Range with If-Range, HEAD (RFC
// 9110). It returns false, with nothing written, when the bytes cannot be opened, so the caller treats the entry as
// gone.
func (e *engine) serveSource(q *rq, src *source) bool {
	hdr := http.Header{}
	for k, vs := range src.header {
		hdr[k] = append([]string(nil), vs...)
	}
	hdr.Set("Accept-Ranges", "bytes")
	p := planFor(q.r, src.etag, src.lastModified, src.size)
	switch {
	case p.notModified:
		e.notModified(q, hdr)
		return true
	case p.unsat:
		e.rangeNotSatisfiable(q, src.size)
		return true
	}
	start, end := p.start, p.end
	status, length := http.StatusOK, src.size
	if p.partial {
		status, length = http.StatusPartialContent, end-start+1
		hdr.Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.FormatInt(src.size, 10))
	}
	hdr.Set("Content-Length", strconv.FormatInt(length, 10))
	if q.r.Method == http.MethodHead {
		copyHeader(q.w.Header(), hdr)
		q.w.WriteHeader(status)
		return true
	}
	if src.transfer && !e.admitTransfer(q, true) {
		return true
	}
	rc, err := src.open(q.ctx())
	if err != nil {
		return false
	}
	defer rc.Close()
	copyHeader(q.w.Header(), hdr)
	q.w.WriteHeader(status)
	q.out.source = metrics.SourceCache
	if src.label != "" {
		q.out.source = src.label
	}
	if e.streamVerified(q, rc, src, start, end) && src.download != nil {
		src.download(q)
	}
	return true
}

func copyHeader(dst, src http.Header) {
	for k, vs := range src {
		dst[k] = append([]string(nil), vs...)
	}
}

// notModified answers 304 with the headers RFC 9110 section 15.4.5 asks for, and the Last-Modified a client that sent
// If-Modified-Since compares again.
func (e *engine) notModified(q *rq, hdr http.Header) {
	h := q.w.Header()
	for _, k := range []string{"Etag", "Cache-Control", "Expires", "Vary", "Last-Modified"} {
		if vs := hdr.Values(k); len(vs) > 0 {
			h[k] = append([]string(nil), vs...)
		}
	}
	q.w.WriteHeader(http.StatusNotModified)
}

// etagMatches is the weak comparison of RFC 9110 section 13.1.2 for If-None-Match.
func etagMatches(inm, etag string) bool {
	etag = strings.TrimPrefix(etag, "W/")
	for _, c := range strings.Split(inm, ",") {
		c = strings.TrimSpace(c)
		if c == "*" || strings.TrimPrefix(c, "W/") == etag {
			return true
		}
	}
	return false
}

// parseRange reads a single byte range of a Range header (RFC 9110 section 14). A Range that is not honoured
// (another unit, several ranges, a failed If-Range, a malformed value) is ignored and the whole representation
// served; unsat reports a range no byte of which exists.
func parseRange(rng, ifRange, etag string, size int64) (start, end int64, partial, unsat bool) {
	if rng == "" || !strings.HasPrefix(rng, "bytes=") || strings.Contains(rng, ",") {
		return 0, size - 1, false, false
	}
	if ifRange != "" && (strings.HasPrefix(ifRange, "W/") || ifRange != etag) {
		return 0, size - 1, false, false
	}
	first, last, ok := strings.Cut(strings.TrimPrefix(rng, "bytes="), "-")
	if !ok {
		return 0, size - 1, false, false
	}
	first, last = strings.TrimSpace(first), strings.TrimSpace(last)
	switch first {
	case "":
		n, err := strconv.ParseInt(last, 10, 64)
		if err != nil || n <= 0 {
			return 0, size - 1, false, err == nil
		}
		if n > size {
			n = size
		}
		if size == 0 {
			return 0, 0, false, true
		}
		return size - n, size - 1, true, false
	default:
		s, err := strconv.ParseInt(first, 10, 64)
		if err != nil || s < 0 {
			return 0, size - 1, false, false
		}
		en := size - 1
		if last != "" {
			v, err := strconv.ParseInt(last, 10, 64)
			if err != nil || v < s {
				return 0, size - 1, false, false
			}
			en = min(v, size-1)
		}
		if s >= size {
			return 0, 0, false, true
		}
		return s, en, true, false
	}
}

// streamVerified copies the object to the caller. r is the cache's verifying reader (or a document held in
// memory), which fails with cache.ErrCorrupt instead of releasing its own tail when the digest or the length
// do not match; the bytes of the range [start, end] are written as they arrive except for the last
// withheldTail of them, which are released only after the whole object was read. A failed read removes nothing
// here (the cache did) and aborts the transfer: the caller sees an incomplete response, never a complete one
// with wrong bytes. It reports whether the whole range was delivered.
func (e *engine) streamVerified(q *rq, r io.Reader, src *source, start, end int64) bool {
	buf := make([]byte, 32<<10)
	var pending []byte
	var pos int64
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if lo, hi := max(pos, start), min(pos+int64(n), end+1); lo < hi {
				pending = append(pending, chunk[lo-pos:hi-pos]...)
				if len(pending) > withheldTail {
					cut := len(pending) - withheldTail
					if _, werr := q.out.Write(pending[:cut]); werr != nil {
						return false
					}
					pending = append(pending[:0], pending[cut:]...)
				}
			}
			pos += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			e.log.Warn("cached bytes could not be served", "error", err, "corrupt", errors.Is(err, cache.ErrCorrupt))
			panic(http.ErrAbortHandler)
		}
	}
	if pos != src.size {
		e.d.Metrics.CacheCorrupt.Inc()
		e.log.Warn("cached bytes have another length than their entry", "got", pos, "want", src.size)
		panic(http.ErrAbortHandler)
	}
	_, err := q.out.Write(pending)
	return err == nil
}

// downloadEvent adds the event of an answer served from the cache to the download buffer (R-BAS-016, R-BAS-039): a
// hit, a check answered 304 or a caller that followed a fill, of an entry whose fill the main installation counted as
// a download (R-BAS-059).
func (e *engine) downloadEvent(q *rq, repo, sha string) {
	if q.r.Method != http.MethodGet {
		return
	}
	e.d.Downloads.Add(iface.DownloadEvent{
		At: e.now(), TokenID: q.subject.TokenID, PrincipalID: q.subject.PrincipalID, ClientAddress: q.clientText,
		Repository: repo, Path: q.r.URL.Path, SHA256: sha, UserAgent: q.r.UserAgent(), Replica: e.d.Replica,
	})
}
