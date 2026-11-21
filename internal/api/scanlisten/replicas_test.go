package scanlisten

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/scanreport"
)

const uid = "8a0b5f8e-3c1d-4f2a-9b7e-1c2d3e4f5a6b"

// fakeReplicas is the store of a test: the rows by UID with their kind, and a refusal the test sets.
type fakeReplicas struct {
	kinds  map[string]string
	rows   map[string]scanreport.Report
	refuse bool
}

func (f *fakeReplicas) Record(_ context.Context, kind, id string, r scanreport.Report) (bool, error) {
	if f.refuse || (f.kinds[id] != "" && f.kinds[id] != kind) {
		return false, nil
	}
	f.kinds[id], f.rows[id] = kind, r
	return true, nil
}

func (f *fakeReplicas) Remove(_ context.Context, kind, id string) (bool, error) {
	if f.kinds[id] != kind {
		return false, nil
	}
	delete(f.kinds, id)
	delete(f.rows, id)
	return true, nil
}

// replicaEnv serves the route; the kind a request's certificate names is the header X-Test-Kind, which stands in for the handshake.
func replicaEnv() (*fakeReplicas, *http.ServeMux) {
	f := &fakeReplicas{kinds: map[string]string{}, rows: map[string]scanreport.Report{}}
	mux := http.NewServeMux()
	RegisterReplicas(mux, ReplicaOptions{Store: f, Kind: func(r *http.Request) string { return r.Header.Get("X-Test-Kind") }, Log: slog.New(slog.DiscardHandler)})
	return f, mux
}

func send(mux *http.ServeMux, method, path, kind, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(httpserver.WithPrincipal(req.Context(), httpserver.Principal{Kind: httpserver.KindSystem, Name: "scanner-report"}))
	if kind != "" {
		req.Header.Set("X-Test-Kind", kind)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func reportBody(t *testing.T, change func(*scanreport.Report)) string {
	t.Helper()
	r := scanreport.Report{Name: "quader-vuln-scanner-trivy-7d9f-abcde", Version: "0.42.0", State: scanreport.StateReady, ToolVersion: "0.67.2", Slots: scanreport.Slots{Total: 1}}
	if change != nil {
		change(&r)
	}
	b, _ := json.Marshal(r)
	return string(b)
}

// R-OPS-048, Q-OPS-223: the route records a report under the kind the certificate names, never one the body names; refuses a report of
// another shape and a UID that is no UUID; answers a refusal of the store 409; and the sign-off removes only a row of the same kind.
func TestR_OPS_048_TheRouteRecordsAReportUnderTheKindOfTheCertificate(t *testing.T) {
	f, mux := replicaEnv()
	path := scanreport.Path(uid)
	if rec := send(mux, "PUT", path, "trivy", reportBody(t, nil)); rec.Code != http.StatusNoContent {
		t.Fatalf("a report: %d %s", rec.Code, rec.Body)
	}
	if f.kinds[uid] != "trivy" || f.rows[uid].ToolVersion != "0.67.2" {
		t.Fatalf("the row: %s %+v", f.kinds[uid], f.rows[uid])
	}
	t.Run("a request whose certificate names no kind is no route", func(t *testing.T) {
		if rec := send(mux, "PUT", path, "", reportBody(t, nil)); rec.Code != http.StatusNotFound {
			t.Fatalf("%d", rec.Code)
		}
	})
	t.Run("a member the shape does not know is refused, a kind in the body included", func(t *testing.T) {
		body := strings.Replace(reportBody(t, nil), `{`, `{"kind":"clamav",`, 1)
		if rec := send(mux, "PUT", path, "trivy", body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%d", rec.Code)
		}
	})
	t.Run("markup is refused", func(t *testing.T) {
		if rec := send(mux, "PUT", path, "trivy", reportBody(t, func(r *scanreport.Report) { r.Name = "<b>pod</b>" })); rec.Code != http.StatusBadRequest {
			t.Fatalf("%d", rec.Code)
		}
	})
	t.Run("a database from the image scanner is refused", func(t *testing.T) {
		if rec := send(mux, "PUT", path, "trivy", reportBody(t, func(r *scanreport.Report) { r.Database = "main-62" })); rec.Code != http.StatusBadRequest {
			t.Fatalf("%d", rec.Code)
		}
	})
	t.Run("a UID that is no UUID is refused", func(t *testing.T) {
		if rec := send(mux, "PUT", scanreport.Path("not-a-uid"), "trivy", reportBody(t, nil)); rec.Code != http.StatusBadRequest {
			t.Fatalf("%d", rec.Code)
		}
	})
	t.Run("the UID of a row of another kind is refused 409", func(t *testing.T) {
		if rec := send(mux, "PUT", path, "clamav", reportBody(t, nil)); rec.Code != http.StatusConflict {
			t.Fatalf("%d", rec.Code)
		}
	})
	t.Run("a refusal of the store is 409", func(t *testing.T) {
		f.refuse = true
		defer func() { f.refuse = false }()
		if rec := send(mux, "PUT", path, "trivy", reportBody(t, nil)); rec.Code != http.StatusConflict {
			t.Fatalf("%d", rec.Code)
		}
	})
	t.Run("the sign-off of another kind finds nothing, its own removes the row", func(t *testing.T) {
		if rec := send(mux, "DELETE", path, "clamav", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("another kind: %d", rec.Code)
		}
		if rec := send(mux, "DELETE", path, "trivy", ""); rec.Code != http.StatusNoContent || len(f.rows) != 0 {
			t.Fatalf("its own: %d, rows %v", rec.Code, f.rows)
		}
	})
}
