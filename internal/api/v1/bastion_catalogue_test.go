package v1

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/bastionmain/catalogue"
	"github.com/guided-traffic/quader/internal/bastionmain/catalogue/cataloguetest"
	"github.com/guided-traffic/quader/internal/errs"
)

// The catalogue routes of interface version 8 (R-BAS-101 to R-BAS-103) and the handshake's configuration and contact
// (R-BAS-093). Handlers only, in process; no database.

// partnersID is the registration the requests of asBastion are recognized as.
const partnersID = "0192f1c4-0000-7000-8000-0000000000b1"

// catalogueSetup is the API with the interface's services, an empty catalogue and a configuration whose switches are the given ones.
func catalogueSetup(t *testing.T, sw iface.Switches) setup {
	t.Helper()
	return catalogueSetupWith(t, sw, cataloguetest.New())
}

// catalogueSetupWith is catalogueSetup over the catalogue store.
func catalogueSetupWith(t *testing.T, sw iface.Switches, store catalogue.Reader) setup {
	t.Helper()
	svc := BastionServices{
		Feed: stillFeed{}, AnonymousReads: func(context.Context) bool { return false }, Catalogue: store,
		Configuration: func(context.Context, bastionmain.Recognition) (iface.Configuration, error) {
			c := bastionmain.InterimConfiguration(3, false)
			c.Switches = sw
			return c, nil
		},
	}
	return newSetup(t, func(o *Options) { o.Bastion = svc }, nil)
}

// asBastion serves a request as the recognized registration partners.
func asBastion(s setup, method, target, body string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	r = r.WithContext(bastionmain.WithRecognition(r.Context(), bastionmain.Recognition{
		Bastion: "partners", BastionID: partnersID, TokenID: bastionTokenID, Route: true, Interface: iface.InterfaceVersion,
	}))
	w := httptest.NewRecorder()
	s.handler.ServeHTTP(w, r)
	return w
}

// TestR_BAS_102_TheCatalogueRoutesAnswerByTheSwitches: while gui is off every catalogue route answers 404 not_found; with it on and
// no catalogue built the change list answers reset for a cursor of a catalogue that is gone and the public set empty, a document is
// missing, a documentation path is missing while docs is off, and a path outside the grammar is refused 400.
func TestR_BAS_102_TheCatalogueRoutesAnswerByTheSwitches(t *testing.T) {
	batch := `{"paths":["index/0","packages/p1/versions/v1/readme"]}`
	off := catalogueSetup(t, iface.Switches{})
	for _, c := range []struct{ method, target, body string }{
		{http.MethodGet, iface.PathCatalogue + "?after=0", ""},
		{http.MethodGet, iface.PathCatalogueDocument + "?path=index/0", ""},
		{http.MethodPost, iface.PathCatalogueDocuments, batch},
		{http.MethodGet, iface.PathCataloguePublic, ""},
	} {
		if w := asBastion(off, c.method, c.target, c.body); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), errs.CodeNotFound) {
			t.Errorf("gui off, %s %s: %d %s", c.method, c.target, w.Code, w.Body)
		}
	}

	on := catalogueSetup(t, iface.Switches{GUI: true})
	w := asBastion(on, http.MethodGet, iface.PathCatalogue+"?after=12", "")
	var changes iface.CatalogueChanges
	if err := iface.DecodeStrict(w.Body, &changes); err != nil || w.Code != http.StatusOK || !changes.Reset || changes.Cursor != 0 || changes.Validate(12) != nil {
		t.Fatalf("the change list: %d %+v %v", w.Code, changes, err)
	}
	w = asBastion(on, http.MethodGet, iface.PathCataloguePublic, "")
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"projects":[],"repositories":[]}` {
		t.Fatalf("the public set: %d %s", w.Code, w.Body)
	}
	if w := asBastion(on, http.MethodGet, iface.PathCatalogueDocument+"?path=index/0", ""); w.Code != http.StatusNotFound {
		t.Errorf("a document of no catalogue: %d %s", w.Code, w.Body)
	}
	if w := asBastion(on, http.MethodGet, iface.PathCatalogueDocument+"?path=packages/p1/versions/v1/readme", ""); w.Code != http.StatusNotFound ||
		!strings.Contains(w.Body.String(), "documentation") {
		t.Errorf("a documentation path while docs is off: %d %s", w.Code, w.Body)
	}
	for _, bad := range []string{"../x", "packages/a%2Fb", "index/01"} {
		if w := asBastion(on, http.MethodGet, iface.PathCatalogueDocument+"?path="+bad, ""); w.Code != http.StatusBadRequest {
			t.Errorf("the path %q: %d %s", bad, w.Code, w.Body)
		}
	}
	w = asBastion(on, http.MethodPost, iface.PathCatalogueDocuments, batch)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != iface.CatalogueBatchMediaType {
		t.Fatalf("the batch: %d %v %s", w.Code, w.Header(), w.Body)
	}
	r := bufio.NewReader(w.Body)
	for _, want := range []string{"index/0", "packages/p1/versions/v1/readme"} {
		f, _, err := iface.ReadCatalogueFrame(r, 1<<20)
		if err != nil || f.Path != want || !f.Missing {
			t.Fatalf("frame of %s: %+v %v", want, f, err)
		}
	}
	if _, _, err := iface.ReadCatalogueFrame(r, 1<<20); !errors.Is(err, io.EOF) {
		t.Fatalf("after the last frame: %v", err)
	}
	for _, bad := range []string{`{"paths":[]}`, `{"paths":["state","state"]}`, `{"paths":["x"]}`, `{"paths":["state"],"x":1}`} {
		if w := asBastion(on, http.MethodPost, iface.PathCatalogueDocuments, bad); w.Code != http.StatusBadRequest {
			t.Errorf("the batch %s: %d %s", bad, w.Code, w.Body)
		}
	}
}

// TestR_BAS_093_TheHandshakeCarriesTheConfigurationAndRecordsTheContact: the handshake answers the registration's configuration
// (the interim one until E8, valid and with the handover's switch), and refuses a malformed contact header.
func TestR_BAS_093_TheHandshakeCarriesTheConfigurationAndRecordsTheContact(t *testing.T) {
	s := catalogueSetup(t, iface.Switches{Handover: true, GUI: true})
	w := asBastion(s, http.MethodGet, iface.PathHandshake, "", iface.HeaderEdgeLimit, "rps=100; connections=50", iface.HeaderCatalogueKept, "true")
	var h iface.Handshake
	if err := iface.DecodeStrict(w.Body, &h); err != nil || w.Code != http.StatusOK || h.Configuration.Revision != 3 ||
		!h.Configuration.Switches.Handover || !h.Configuration.Switches.GUI || h.Configuration.Validate() != nil {
		t.Fatalf("%d %v %+v", w.Code, err, h.Configuration)
	}
	for k, v := range map[string]string{iface.HeaderEdgeLimit: "fast", iface.HeaderCatalogueKept: "yes"} {
		if w := asBastion(s, http.MethodGet, iface.PathHandshake, "", k, v); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %s answers %d", k, v, w.Code)
		}
	}
	if err := bastionmain.InterimConfiguration(0, false).Validate(); err != nil {
		t.Fatalf("the interim configuration is refused: %v", err)
	}
}

// TestR_BAS_101_TheCatalogueRoutesServeTheKeptDocuments: the routes serve what the builder kept: the change list after a cursor and
// clamped to its page, a document compressed with the SHA-256 of its raw bytes as the ETag, the batch's frames in the request's
// order with a missing frame for a path not held and for documentation while docs is off, and the public set of the last build.
func TestR_BAS_101_TheCatalogueRoutesServeTheKeptDocuments(t *testing.T) {
	ctx := context.Background()
	store := cataloguetest.New()
	if err := store.Begin(ctx, partnersID, 100); err != nil {
		t.Fatal(err)
	}
	var put []catalogue.Doc
	for _, p := range []struct{ path, body string }{
		{"index/0", `{"items":[]}`}, {"packages/p1", `{"id":"p1"}`}, {"packages/p1/versions/1.0.0/readme", `{"html":"<p>hi</p>"}`},
	} {
		d, err := catalogue.NewDoc(p.path, []byte(p.body))
		if err != nil {
			t.Fatal(err)
		}
		put = append(put, d)
	}
	if err := store.Write(ctx, partnersID, "", put, nil, &catalogue.Unit{}); err != nil {
		t.Fatal(err)
	}
	pub := iface.CataloguePublic{Projects: []string{"acme"}, Repositories: []iface.CatalogueRepository{{Name: "acme/npm-local", Format: "npm", Class: "local"}}}
	if err := store.Finish(ctx, partnersID, nil, "https://packages.example.com", pub, true); err != nil {
		t.Fatal(err)
	}
	s := catalogueSetupWith(t, iface.Switches{GUI: true}, store)

	w := asBastion(s, http.MethodGet, iface.PathCatalogue+"?after=0&limit=2", "")
	var ch iface.CatalogueChanges
	if err := iface.DecodeStrict(w.Body, &ch); err != nil || ch.Validate(0) != nil || len(ch.Entries) != 2 || !ch.More || ch.Cursor != 101 {
		t.Fatalf("the first page: %d %+v %v", w.Code, ch, err)
	}
	w = asBastion(s, http.MethodGet, iface.PathCatalogue+"?after=101", "")
	if err := iface.DecodeStrict(w.Body, &ch); err != nil || ch.Validate(101) != nil || len(ch.Entries) != 1 || ch.More || ch.Cursor != 102 {
		t.Fatalf("after the cursor: %d %+v %v", w.Code, ch, err)
	}
	if w := asBastion(s, http.MethodGet, iface.PathCatalogue+"?after=5000", ""); !strings.Contains(w.Body.String(), `"reset":true`) {
		t.Fatalf("a cursor beyond the list: %s", w.Body)
	}

	w = asBastion(s, http.MethodGet, iface.PathCatalogueDocument+"?path=packages/p1", "")
	sum := sha256.Sum256([]byte(`{"id":"p1"}`))
	if w.Code != http.StatusOK || w.Header().Get("ETag") != `"`+hex.EncodeToString(sum[:])+`"` || w.Header().Get("Content-Type") != iface.CatalogueDocumentMediaType {
		t.Fatalf("the document: %d %v", w.Code, w.Header())
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := io.ReadAll(zr); string(raw) != `{"id":"p1"}` {
		t.Fatalf("the document's bytes: %s", raw)
	}
	if w := asBastion(s, http.MethodGet, iface.PathCatalogueDocument+"?path=packages/p1/versions/1.0.0/readme", ""); w.Code != http.StatusNotFound {
		t.Errorf("documentation while docs is off: %d", w.Code)
	}

	w = asBastion(s, http.MethodPost, iface.PathCatalogueDocuments, `{"paths":["packages/p1/versions/1.0.0/readme","state","packages/p1"]}`)
	r := bufio.NewReader(w.Body)
	for _, want := range []struct {
		path    string
		missing bool
	}{{"packages/p1/versions/1.0.0/readme", true}, {"state", true}, {"packages/p1", false}} {
		f, gz, err := iface.ReadCatalogueFrame(r, 1<<20)
		if err != nil || f.Path != want.path || f.Missing != want.missing || (!f.Missing && (len(gz) == 0 || f.SHA256 != hex.EncodeToString(sum[:]))) {
			t.Fatalf("frame of %s: %+v %v", want.path, f, err)
		}
	}

	w = asBastion(s, http.MethodGet, iface.PathCataloguePublic, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"acme/npm-local"`) {
		t.Fatalf("the public set: %d %s", w.Code, w.Body)
	}
}

// TestR_BAS_101_TheBatchStopsAtItsBound: the frames stop before the one that would pass the bound of compressed bytes, but the first
// frame is always answered.
func TestR_BAS_101_TheBatchStopsAtItsBound(t *testing.T) {
	docs := map[string]catalogue.Stored{
		"packages/a": {SHA256: strings.Repeat("a", 64), Size: 9, Gzip: make([]byte, 6)},
		"packages/b": {SHA256: strings.Repeat("b", 64), Size: 9, Gzip: make([]byte, 6)},
	}
	out, err := catalogueFrames([]string{"packages/a", "packages/b"}, docs, true, 4)
	if err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(out)
	if f, _, err := iface.ReadCatalogueFrame(r, 1<<20); err != nil || f.Path != "packages/a" {
		t.Fatalf("the first frame: %+v %v", f, err)
	}
	if _, _, err := iface.ReadCatalogueFrame(r, 1<<20); !errors.Is(err, io.EOF) {
		t.Fatalf("a second frame beyond the bound: %v", err)
	}
}
