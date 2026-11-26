package engine

import (
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/errs"
)

// TestR_BAS_095_BeforeItsFirstHandshakeABastionAnswersInTheStoredShapes: a replica that never reached the main installation answers
// in problem details and refuses a declared read route with a body 403 read_only, as long as it knows no format table; with the table
// stored with its configuration (link.UseStoredFormats) it answers npm's 503 in npm's shape and forwards the bulk audit, which the
// unreachable main installation then answers 503 (H-2359).
func TestR_BAS_095_BeforeItsFirstHandshakeABastionAnswersInTheStoredShapes(t *testing.T) {
	const doc, audit = "/api/npm/web/npm-all/lodash", "/api/npm/web/npm-all/-/npm/v1/security/advisories/bulk"
	h := newHarness(t, options{down: true})
	if w := h.get(doc); w.Code != http.StatusServiceUnavailable || w.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("without a table: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	if w := h.do(http.MethodPost, audit, strings.NewReader(`{}`)); w.Code != http.StatusForbidden || problemCode(w) != iface.CodeReadOnly {
		t.Fatalf("the bulk audit without a table: %d %s", w.Code, w.Body)
	}
	h.link.UseStoredFormats(formatTable())
	w := h.get(doc)
	got, err := errs.ReadShape(iface.ErrorShapeNPM, w.Header().Get("Content-Type"), w.Body.Bytes())
	if w.Code != http.StatusServiceUnavailable || err != nil || got.Code != iface.CodeUnavailable {
		t.Fatalf("with the stored table: %d %s %v", w.Code, w.Body, err)
	}
	if w := h.do(http.MethodPost, audit, strings.NewReader(`{}`)); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("the bulk audit with the stored table: %d %s", w.Code, w.Body)
	}
	if h.link.State().Handshaken {
		t.Fatal("the stored table counted as a handshake")
	}
}
