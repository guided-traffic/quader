package rawfiles

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestR_STOR_070_FilesIsNeverALink: /files/ is never answered with a signed link of the object store, whatever the installation's
// switch says, because its clients are plain download tools that check no digest (R-STOR-071). The package imports
// internal/downloadlinks for the handover per blob to a Bastion alone (storage.md R-STOR-095 step 2), which a Bastion verifies
// itself: no file of it calls Links.Sign, and no edge.ServeOptions it builds sets Link or Linked, by a literal or an assignment,
// so every file of /files/ that is not handed over goes through the stream of edge.ServeItem.
func TestR_STOR_070_FilesIsNeverALink(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	literals := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if x.Sel.Name == "Sign" {
					t.Errorf("%s: calls Sign, the signing of a client's link", fset.Position(x.Pos()))
				}
			case *ast.AssignStmt:
				for _, lhs := range x.Lhs {
					if sel, ok := lhs.(*ast.SelectorExpr); ok && (sel.Sel.Name == "Link" || sel.Sel.Name == "Linked") {
						t.Errorf("%s: sets %s", fset.Position(sel.Pos()), sel.Sel.Name)
					}
				}
			}
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			sel, ok := lit.Type.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "ServeOptions" {
				return true
			}
			literals++
			for _, el := range lit.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					if id, ok := kv.Key.(*ast.Ident); ok && (id.Name == "Link" || id.Name == "Linked") {
						t.Errorf("%s: edge.ServeOptions sets %s", fset.Position(kv.Pos()), id.Name)
					}
				}
			}
			return true
		})
	}
	if literals == 0 {
		t.Fatal("no edge.ServeOptions found: the handler's serve moved; follow it")
	}
}
