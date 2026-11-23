package apitest

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// docOf is a small document with one operation, opID, answering 200.
func docOf(opID string) []byte {
	return []byte(`openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /things:
    get:
      operationId: ` + opID + `
      responses:
        "200": {description: ok}
`)
}

// TestNewSharesOneParsePerDocument: every New of the same bytes takes the one parse of the test process and keeps its own record
// of the operations it saw; a document of other bytes is parsed on its own (Q-ARCH-580).
func TestNewSharesOneParsePerDocument(t *testing.T) {
	doc := docOf("listThings")
	t.Run("Q-ARCH-580 two validators of the same bytes share the document and keep separate records", func(t *testing.T) {
		a, b := New(t, doc), New(t, append([]byte(nil), doc...))
		if a.doc != b.doc || a.router != b.router {
			t.Fatal("two validators of the same bytes hold two parses")
		}
		if _, err := a.Check(httptest.NewRequest(http.MethodGet, "/things", nil), 200, http.Header{}, nil); err != nil {
			t.Fatal(err)
		}
		if got := a.Operations(); len(got["listThings"]) != 1 {
			t.Fatalf("the validator that saw the answer records %v", got)
		}
		if got := b.Operations(); len(got) != 0 {
			t.Fatalf("the other validator of the same document records %v, want nothing", got)
		}
		if got := b.Uncovered("listThings"); len(got) != 1 {
			t.Fatalf("Uncovered of the other validator = %v, want listThings", got)
		}
	})
	t.Run("Q-ARCH-580 a document of other bytes is parsed on its own", func(t *testing.T) {
		a, c := New(t, doc), New(t, docOf("listOtherThings"))
		if a.doc == c.doc {
			t.Fatal("two documents share one parse")
		}
		if got := c.Documented(); len(got) != 1 || got[0] != "listOtherThings" {
			t.Fatalf("the second document's operations: %v", got)
		}
	})
	t.Run("Q-ARCH-580 a document that does not parse fails every New of its bytes", func(t *testing.T) {
		bad := []byte("openapi: 3.0.3\ninfo: {title: t, version: \"1\"}\npaths: {}\n")
		for range 2 {
			if _, err := parsedSpec(bad); err == nil {
				t.Fatal("an OpenAPI 3.0 document was taken")
			}
		}
	})
}
