package app

import "testing"

// TestTheAPIDocumentIsParsedOncePerProcess: every App of a process reads the one parse of the embedded management API document
// (Q-OPS-157, Q-ARCH-583). A replica runs one App; the tests of this package and the integration harness start many in one
// process, and each paid 0.04 to 0.07 s for its own parse.
func TestTheAPIDocumentIsParsedOncePerProcess(t *testing.T) {
	t.Run("Q-ARCH-583 two Apps of one process share one parse of the document", func(t *testing.T) {
		a, b := &App{}, &App{}
		da, err := a.apiDocument()
		if err != nil {
			t.Fatal(err)
		}
		db, err := b.apiDocument()
		if err != nil {
			t.Fatal(err)
		}
		if da != db {
			t.Fatal("two Apps hold two parses of the API document")
		}
		if len(da.Operations()) == 0 {
			t.Fatal("the shared document has no operations")
		}
	})
}
