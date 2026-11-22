package bastionmain

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestQ_BAS_186_ACountedDownloadIsNotedAtItsStatusLine: the writer of a counting serve notes Quader-Bastion-Counted when
// the status line of a download the core counts once its body is complete goes out (a GET answered 200, or 206 from byte
// 0), before the marking stage below it reads the note, and notes nothing for a HEAD, a 304, a later range or a refusal.
// An ordinary request gets its own writer back. Named by the decision until the Bastion's side reports what it notes.
func TestQ_BAS_186_ACountedDownloadIsNotedAtItsStatusLine(t *testing.T) {
	for name, c := range map[string]struct {
		method string
		write  func(w http.ResponseWriter)
		want   bool
	}{
		"a GET answered 200": {http.MethodGet, func(w http.ResponseWriter) { w.WriteHeader(http.StatusOK); _, _ = w.Write([]byte("x")) }, true},
		"a GET whose body is written without a status line": {http.MethodGet, func(w http.ResponseWriter) { _, _ = w.Write([]byte("x")) }, true},
		"a GET copied with io.Copy":                         {http.MethodGet, func(w http.ResponseWriter) { _, _ = io.Copy(w, bytes.NewReader([]byte("abc"))) }, true},
		"a GET flushed before its body":                     {http.MethodGet, func(w http.ResponseWriter) { w.(http.Flusher).Flush() }, true},
		"a range from byte 0": {http.MethodGet, func(w http.ResponseWriter) {
			w.Header().Set("Content-Range", "bytes 0-9/100")
			w.WriteHeader(http.StatusPartialContent)
		}, true},
		"a later range": {http.MethodGet, func(w http.ResponseWriter) {
			w.Header().Set("Content-Range", "bytes 10-19/100")
			w.WriteHeader(http.StatusPartialContent)
		}, false},
		"a 304":     {http.MethodGet, func(w http.ResponseWriter) { w.WriteHeader(http.StatusNotModified) }, false},
		"a refusal": {http.MethodGet, func(w http.ResponseWriter) { w.WriteHeader(http.StatusTooManyRequests) }, false},
		"an early hint, then a 304": {http.MethodGet, func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusEarlyHints)
			w.WriteHeader(http.StatusNotModified)
		}, false},
		"a second status line counts nothing": {http.MethodGet, func(w http.ResponseWriter) { w.WriteHeader(http.StatusNotModified); w.WriteHeader(http.StatusOK) }, false},
		"a HEAD":                              {http.MethodHead, func(w http.ResponseWriter) { w.WriteHeader(http.StatusOK) }, false},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, a := WithAbsence(t.Context())
			r := httptest.NewRequest(c.method, "/api/npm/web/npm-local/x/-/x-1.0.0.tgz", nil).WithContext(ctx)
			atStatus := false
			below := &noteAtStatus{ResponseRecorder: httptest.NewRecorder(), at: func() { atStatus = a.Counted() }}
			c.write(CountingWriter(r, below))
			if a.Counted() != c.want || atStatus != c.want {
				t.Fatalf("counted %v, at the status line %v, want %v", a.Counted(), atStatus, c.want)
			}
		})
	}
	t.Run("io.Copy reaches the body of the writer below", func(t *testing.T) {
		ctx, _ := WithAbsence(t.Context())
		rec := httptest.NewRecorder()
		w := CountingWriter(httptest.NewRequest(http.MethodGet, "/files/web/files-local/a.zip", nil).WithContext(ctx), rec)
		if _, err := io.Copy(w, bytes.NewReader([]byte("abc"))); err != nil || rec.Body.String() != "abc" || rec.Code != http.StatusOK {
			t.Fatalf("%v %d %q", err, rec.Code, rec.Body)
		}
	})
	t.Run("an ordinary request and a HEAD get their own writer back", func(t *testing.T) {
		rec := httptest.NewRecorder()
		if w := CountingWriter(httptest.NewRequest(http.MethodGet, "/api/npm/web/npm-local/x", nil), rec); w != http.ResponseWriter(rec) {
			t.Fatal("an ordinary request was wrapped")
		}
		ctx, _ := WithAbsence(t.Context())
		if w := CountingWriter(httptest.NewRequest(http.MethodHead, "/x", nil).WithContext(ctx), rec); w != http.ResponseWriter(rec) {
			t.Fatal("a HEAD was wrapped")
		}
		NoteCounted(t.Context()) // no record: a no-op
		var none *Absence
		if none.Counted() {
			t.Fatal("no record counted")
		}
	})
}

// noteAtStatus calls at when the status line reaches it, as the marking stage reads the record there.
type noteAtStatus struct {
	*httptest.ResponseRecorder
	at    func()
	wrote bool
}

func (n *noteAtStatus) WriteHeader(code int) {
	if !n.wrote && code >= http.StatusOK {
		n.wrote = true
		n.at()
	}
	n.ResponseRecorder.WriteHeader(code)
}

func (n *noteAtStatus) Write(p []byte) (int, error) {
	if !n.wrote {
		n.WriteHeader(http.StatusOK)
	}
	return n.ResponseRecorder.Write(p)
}

func (n *noteAtStatus) Flush() {
	if !n.wrote {
		n.WriteHeader(http.StatusOK)
	}
	n.ResponseRecorder.Flush()
}
