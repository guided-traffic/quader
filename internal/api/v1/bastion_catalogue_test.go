package v1

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/errs"
)

// The catalogue routes of interface version 8 (R-BAS-101 to R-BAS-103) and the handshake's configuration and contact
// (R-BAS-093). Handlers only, in process; no database.

// catalogueSetup is the API with the interface's services and a configuration whose switches are the given ones.
func catalogueSetup(t *testing.T, sw iface.Switches) setup {
	t.Helper()
	svc := BastionServices{
		Feed: stillFeed{}, AnonymousReads: func(context.Context) bool { return false },
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
		Bastion: "partners", BastionID: "0192f1c4-0000-7000-8000-0000000000b1", TokenID: bastionTokenID, Route: true, Interface: iface.InterfaceVersion,
	}))
	w := httptest.NewRecorder()
	s.handler.ServeHTTP(w, r)
	return w
}

// TestR_BAS_102_TheCatalogueRoutesAnswerByTheSwitches: while gui is off every catalogue route answers 404 not_found; with it on
// the change list and the public set answer empty, a document is missing, a documentation path is missing while docs is off, and a
// path outside the grammar is refused 400.
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
	if err := iface.DecodeStrict(w.Body, &changes); err != nil || w.Code != http.StatusOK || changes.Cursor != 12 || changes.Validate(12) != nil {
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
