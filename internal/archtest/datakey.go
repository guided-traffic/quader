package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"strings"
)

// dataKeyMethod is the method of the blob store that hands out the data key of one copy (storage.md R-STOR-083, R-STOR-096).
const dataKeyMethod = "CopyDataKey"

// dataKeyCallers are the packages that may name the method: the store itself and the package of the handover to a Bastion, the one
// place that decides what leaves the main installation (Q-STOR-340). Their subpackages may not.
func dataKeyCallers() []string { return []string{"internal/blob", "internal/downloadlinks"} }

// CheckDataKeyCallers implements the rule of R-STOR-083 and R-STOR-096 that only internal/blob and internal/downloadlinks call
// blob.Store.CopyDataKey: below internal/ and cmd/, every other package's non-test Go file that names a selector CopyDataKey (a call,
// a method value, a call through an interface or an embedding) or holds the name in a string literal (a call by reflection) is a
// violation. It reads syntax, not types, so a type of the same method name elsewhere is caught as well: the name is reserved for the
// one method. The trees the rules do not check (this package, tools, tests) are left out.
func CheckDataKeyCallers(root string) ([]Violation, error) {
	var out []Violation
	for _, dir := range []string{"internal", "cmd"} {
		err := walkSources(root, dir, false, func(rel string, content []byte) {
			pkg := path.Dir(rel)
			if underAny(rel, unchecked()) || contains(dataKeyCallers(), pkg) {
				return
			}
			for _, what := range dataKeyNames(rel, content) {
				out = append(out, Violation{
					Rule: "R-STOR-096", From: rel, To: what,
					Detail: "only internal/blob and internal/downloadlinks call blob.Store.CopyDataKey, which hands out the data key of a stored object (R-STOR-083)",
				})
			}
		})
		if err != nil {
			return nil, err
		}
	}
	sortViolations(out)
	return out, nil
}

// dataKeyNames lists where a file names the method: "selector at line n" or "string at line n". A file that does not parse is named
// as such, since what it holds cannot be shown to be clean.
func dataKeyNames(rel string, content []byte) []string {
	if !strings.Contains(string(content), dataKeyMethod) {
		return nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, content, parser.SkipObjectResolution)
	if err != nil {
		return []string{dataKeyMethod + " in a file that does not parse"}
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if x.Sel.Name == dataKeyMethod {
				out = append(out, dataKeyMethod+" selector at line "+strconv.Itoa(fset.Position(x.Sel.Pos()).Line))
			}
		case *ast.BasicLit:
			if x.Kind == token.STRING && strings.Contains(x.Value, dataKeyMethod) {
				out = append(out, dataKeyMethod+" string at line "+strconv.Itoa(fset.Position(x.Pos()).Line))
			}
		}
		return true
	})
	return out
}
