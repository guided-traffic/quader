package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/legaltext"
)

// legalServer serves the legal operation to an anonymous caller (authn nil) on a replica without public content.
func legalServer(t *testing.T, pages *legaltext.Pages) http.Handler {
	t.Helper()
	router := httpserver.NewClientRouter()
	New(Options{
		Document: testDocument(t), Identity: Identity{Replica: "r", Role: "api"}, Ready: func() bool { return true },
		Access: allowAll{}, Replicas: fakeReplicas{}, Facts: fakeFacts{},
		Repositories: RepositoryServices{PublicContent: func(context.Context) bool { return false }},
		Legal:        pages,
	}).Register(router)
	res, err := httpserver.NewResolver(nil, "https://quader.example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	return httpserver.Chain{Router: router, Resolver: res, Replica: "r", ReplicaHeader: httpserver.ReplicaAuthenticated}.Handler()
}

func legalOf(t *testing.T, w *httptest.ResponseRecorder) gen.LegalPages {
	t.Helper()
	var out gen.LegalPages
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	return out
}

// TestR_UI_225_TheLegalOperation: an anonymous caller reads the pages while no level is public (the manual's 401 does not apply);
// the locale's own text, or the other language's marked with its lang; null for a page without any text; the ETag and its 304.
func TestR_UI_225_TheLegalOperation(t *testing.T) {
	pages := legaltext.Build(legaltext.Texts{Imprint: legaltext.Source{En: "imprint", De: "impressum"}, Privacy: legaltext.Source{De: "datenschutz"}},
		func(md string) string { return "<p>" + md + "</p>" })
	h := legalServer(t, pages)

	en := legalOf(t, get(h, "/api/v1/legal?locale=en"))
	if en.Imprint == nil || en.Imprint.Html != "<p>imprint</p>" || en.Imprint.Lang != "en" {
		t.Errorf("en imprint = %+v", en.Imprint)
	}
	if en.Privacy == nil || en.Privacy.Html != "<p>datenschutz</p>" || en.Privacy.Lang != "de" {
		t.Errorf("en privacy = %+v, want the German text marked de", en.Privacy)
	}
	w := get(h, "/api/v1/legal?locale=de")
	de := legalOf(t, w)
	if de.Imprint == nil || de.Imprint.Lang != "de" || de.Privacy == nil || de.Privacy.Lang != "de" {
		t.Errorf("de = %+v %+v", de.Imprint, de.Privacy)
	}
	if w.Header().Get("ETag") != pages.ETag() || w.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("headers %v", w.Header())
	}
	r := httptest.NewRequest("GET", "/api/v1/legal?locale=de", nil)
	r.Header.Set("If-None-Match", pages.ETag())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusNotModified || rec.Header().Get("ETag") != pages.ETag() {
		t.Errorf("If-None-Match: %d %v", rec.Code, rec.Header())
	}
	if w := get(h, "/api/v1/legal?locale=fr"); w.Code != 400 {
		t.Errorf("an unknown locale: %d %s", w.Code, w.Body.String())
	}
	if w := get(h, "/api/v1/legal"); w.Code != 400 {
		t.Errorf("no locale: %d %s", w.Code, w.Body.String())
	}

	// Without texts (or without the pages, as a test server has none): both null, and the body says so in JSON.
	for name, p := range map[string]*legaltext.Pages{"no texts": legaltext.Build(legaltext.Texts{}, nil), "no pages": nil} {
		w := get(legalServer(t, p), "/api/v1/legal?locale=en")
		if got := legalOf(t, w); got.Imprint != nil || got.Privacy != nil {
			t.Errorf("%s: %+v", name, got)
		}
		if body := w.Body.String(); body != `{"imprint":null,"privacy":null}`+"\n" && body != `{"imprint":null,"privacy":null}` {
			t.Errorf("%s: body %q", name, body)
		}
	}
}
