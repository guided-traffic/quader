package edge

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/httpserver/content"
)

// TestR_STOR_072_TheRedirect: a download whose Link answers a link is a 307 with Location, Cache-Control: no-store,
// Content-Length: 0 and the four content headers, without ETag, Last-Modified, Accept-Ranges and Repr-Digest, opens nothing and
// counts through Linked, not Served (Q-STOR-289); HEAD, a range and a conditional 304 never ask Link, and Admit's refusal comes
// before it.
func TestR_STOR_072_TheRedirect(t *testing.T) {
	it, body := stored()
	link := blob.Link{URL: "https://objects.example.com/b/quader/sha256/5b/00/x?X-Amz-Signature=s", ExpiresAt: time.Now().Add(time.Minute)}
	run := func(method string, admit func() error, hdr ...string) (*httptest.ResponseRecorder, *opener, int, int, int) {
		o := &opener{body: body}
		var asked, linked, served int
		r := httptest.NewRequest(method, "/api/probe/web/probe-local/a/-/a-1.0.0.tgz", nil)
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		rec := httptest.NewRecorder()
		err := ServeItem(rec, r, it, o.open, ServeOptions{
			Admit:  admit,
			Served: func() { served++ },
			Link:   func() (blob.Link, bool) { asked++; return link, true },
			Linked: func(l blob.Link) {
				if l.URL != link.URL {
					t.Errorf("Linked got %q", l.URL)
				}
				linked++
			},
		})
		if err != nil {
			httpserver.Problem(rec, r, err)
		}
		return rec, o, asked, linked, served
	}
	t.Run("R-STOR-072 the 307 carries Location, no-store, length 0 and the four content headers, and no header of a body", func(t *testing.T) {
		rec, o, asked, linked, served := run(http.MethodGet, nil)
		h := rec.Header()
		if rec.Code != http.StatusTemporaryRedirect || h.Get("Location") != link.URL || h.Get("Cache-Control") != "no-store" || h.Get("Content-Length") != "0" || rec.Body.Len() != 0 {
			t.Fatalf("%d %v %q", rec.Code, h, rec.Body.String())
		}
		for k, v := range content.Headers(content.File, "widget 2.3.html", false) {
			if h.Get(k) != v[0] {
				t.Errorf("%s = %q, want %q", k, h.Get(k), v[0])
			}
		}
		for _, k := range []string{"ETag", "Last-Modified", "Accept-Ranges", "Repr-Digest", "Content-Type"} {
			if h.Get(k) != "" {
				t.Errorf("the redirect carries %s: %q", k, h.Get(k))
			}
		}
		if o.calls != 0 || asked != 1 || linked != 1 || served != 0 {
			t.Fatalf("open %d, asked %d, linked %d, served %d", o.calls, asked, linked, served)
		}
	})
	t.Run("R-STOR-070 HEAD, a range and a conditional answer never ask for a link", func(t *testing.T) {
		for name, c := range map[string]struct {
			method string
			hdr    []string
			code   int
		}{
			"HEAD":    {http.MethodHead, nil, http.StatusOK},
			"a range": {http.MethodGet, []string{"Range", "bytes=0-9"}, http.StatusPartialContent},
			"304":     {http.MethodGet, []string{"If-None-Match", it.ETag()}, http.StatusNotModified},
			"a 412":   {http.MethodGet, []string{"If-Match", `"other"`}, http.StatusPreconditionFailed},
		} {
			rec, _, asked, linked, _ := run(c.method, nil, c.hdr...)
			if rec.Code != c.code || asked != 0 || linked != 0 {
				t.Errorf("%s: %d, asked %d, linked %d", name, rec.Code, asked, linked)
			}
		}
	})
	t.Run("R-STOR-070 Admit's refusal answers before any link", func(t *testing.T) {
		refused := errors.New("refused")
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		var asked int
		err := ServeItem(httptest.NewRecorder(), r, it, (&opener{body: body}).open, ServeOptions{
			Admit: func() error { return refused },
			Link:  func() (blob.Link, bool) { asked++; return link, true },
		})
		if !errors.Is(err, refused) || asked != 0 {
			t.Fatalf("%v, asked %d", err, asked)
		}
	})
	t.Run("R-STOR-070 a Link that answers false streams the file", func(t *testing.T) {
		o := &opener{body: body}
		served := 0
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		if err := ServeItem(rec, r, it, o.open, ServeOptions{Served: func() { served++ }, Link: func() (blob.Link, bool) { return blob.Link{}, false }}); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK || rec.Body.Len() != len(body) || served != 1 {
			t.Fatalf("%d, %d bytes, served %d", rec.Code, rec.Body.Len(), served)
		}
	})
}
