package rawfiles

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestR_STOR_070_FilesIsNeverALink: /files/ is never answered with a signed link of the object store, whatever the installation's
// switch says, because its clients are plain download tools that check no digest (R-STOR-071). The switch is no input of the
// package: no file of it imports internal/downloadlinks, and no edge.ServeOptions it builds sets Link or Linked, so every file of
// /files/ goes through the stream of edge.ServeItem.
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
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); strings.HasSuffix(p, "/internal/downloadlinks") {
				t.Errorf("%s imports %s", e.Name(), p)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
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
