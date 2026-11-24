package oci

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
)

// statusSpy records what a probe function says when the status line reaches it.
type statusSpy struct {
	*httptest.ResponseRecorder
	at    func() int
	seen  int
	wrote bool
}

func (s *statusSpy) WriteHeader(code int) {
	if !s.wrote {
		s.wrote, s.seen = true, s.at()
	}
	s.ResponseRecorder.WriteHeader(code)
}

func (s *statusSpy) Write(p []byte) (int, error) {
	if !s.wrote {
		s.WriteHeader(http.StatusOK)
	}
	return s.ResponseRecorder.Write(p)
}

// TestR_BAS_059_ARemoteTagIsCountedBeforeItsStatusLine: the GET of a remote tag is counted before its answer's status line, so
// the host's note of the count travels with the answer a Bastion keeps (bastion.md R-BAS-059); it counts what it counted before,
// once per 200 (TestR_API_079_AGetOfARemoteTagIsADownloadOfItsManifest below).
func TestR_BAS_059_ARemoteTagIsCountedBeforeItsStatusLine(t *testing.T) {
	body := imageManifestBytes(sha([]byte("cfg")), sha([]byte("layer")))
	tw := newTwin()
	h := formattest.NewHost(New(), formattest.HostOptions{
		Class: formats.Remote, Remote: &formattest.MemRemote{Docs: map[string][]byte{TagPath("app", "1"): body}}, Data: tw, Resolver: proceedResolver{},
	})
	tw.host = h
	h.PutItem(ManifestPath("app", sha(body)), ctImage, body)
	rq, err := h.NewRequest(t.Context(), http.MethodGet, "app/manifests/1", nil)
	if err != nil {
		t.Fatal(err)
	}
	w := &statusSpy{ResponseRecorder: httptest.NewRecorder(), at: func() int { return len(h.RecordedDownloads()) }}
	formats.ServeRoute(New(), w, rq, false)
	if w.Code != http.StatusOK || w.seen != 1 || len(h.RecordedDownloads()) != 1 {
		t.Fatalf("status %d, counted %d at the status line and %d in all", w.Code, w.seen, len(h.RecordedDownloads()))
	}
}

// TestR_API_079_AGetOfARemoteTagIsADownloadOfItsManifest: a remote tag's document is a metadata entry and no item, so the GET of the
// tag that answers the manifest is counted on the item that holds the manifest under its digest, where a pull by digest is counted as
// well (R-API-079). A HEAD, a 304 and a tag whose manifest item is not stored count nothing.
func TestR_API_079_AGetOfARemoteTagIsADownloadOfItsManifest(t *testing.T) {
	body := imageManifestBytes(sha([]byte("cfg")), sha([]byte("layer")))
	digest := sha(body)
	m := &formattest.MemRemote{Docs: map[string][]byte{
		TagPath("app", "1"):     body,
		TagPath("lonely", "1"):  body,
		TagPath("app", "other"): body,
	}}
	tw := newTwin()
	h := formattest.NewHost(New(), formattest.HostOptions{Class: formats.Remote, Remote: m, Data: tw, Resolver: proceedResolver{}})
	tw.host = h
	fx := &fixture{t: t, f: New(), h: h, twin: tw}
	h.PutItem(ManifestPath("app", digest), ctImage, body)
	alias := ManifestPath("app", digest)

	if r := fx.request(http.MethodHead, "app/manifests/1", nil, ""); r.Code != http.StatusOK {
		t.Fatalf("HEAD: %d", r.Code)
	}
	if got := h.RecordedDownloads(); len(got) != 0 {
		t.Fatalf("a HEAD of the tag counted %v", got)
	}
	if r := fx.get("app/manifests/1", func(r *http.Request) { r.Header.Set("If-None-Match", `"`+digest+`"`) }); r.Code != http.StatusNotModified {
		t.Fatalf("conditional GET: %d", r.Code)
	}
	if got := h.RecordedDownloads(); len(got) != 0 {
		t.Fatalf("a 304 counted %v", got)
	}
	if r := fx.get("app/manifests/1"); r.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", r.Code, r.Body)
	}
	if r := fx.get("app/manifests/other"); r.Code != http.StatusOK {
		t.Fatalf("GET of the second tag: %d %s", r.Code, r.Body)
	}
	if got := h.RecordedDownloads(); !slices.Equal(got, []string{alias, alias}) {
		t.Fatalf("the GETs of the tags counted %v, want the manifest item twice", got)
	}
	if r := fx.get("lonely/manifests/1"); r.Code != http.StatusOK {
		t.Fatalf("GET of a tag without a manifest item: %d", r.Code)
	}
	if got := h.RecordedDownloads(); len(got) != 2 {
		t.Fatalf("a tag whose manifest item is not stored counted %v", got)
	}
}
