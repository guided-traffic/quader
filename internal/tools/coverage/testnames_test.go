package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// decisionNumber is the number of a decided question (`Q-OCI-213`, `Q_OCI_213`) or of an ADR (`ADR-141`) in a test name. The
// generator reads the requirements a test proves from its name, so a decision's number there says nothing to it, and it hides
// which requirement the test holds (docs/developer/build-and-test.md). The character before it is anything but a capital or a
// digit, so `TestX_Q_OCI_213` and `TestDefaultsOfQ_OCI_213` match and `TestFAQ_UI_1` does not.
var decisionNumber = regexp.MustCompile(`(?:^|[^A-Z0-9])(Q[-_][A-Z][A-Z0-9]*[-_][0-9]+|ADR[-_]?[0-9]+)`)

// testNameScan finds the names of the Go tests of one package: the test functions, the literal parts of a subtest's name (a
// `Run` call, `fmt.Sprintf`'s format included) and the table rows a subtest takes its name from. A row is found when its table
// is a literal that ranges into the call in the same function, or when the row is of a named struct type whose field some
// subtest of the package is named by, so that a table handed to a helper is found at the literal that builds it.
type testNameScan struct {
	fset    *token.FileSet
	vars    map[string]ast.Expr
	types   map[string]*ast.StructType
	rowName map[string]map[string]bool // a named struct type, the fields of it a subtest is named by
	found   map[token.Pos]string
}

type testFile struct {
	rel string
	f   *ast.File
}

// decisionNames returns, as "file:line: name" in the order of the files and lines, every test name of a package's files that
// carries a decision's number.
func decisionNames(fset *token.FileSet, files []testFile) []string {
	s := &testNameScan{
		fset: fset, vars: map[string]ast.Expr{}, types: map[string]*ast.StructType{},
		rowName: map[string]map[string]bool{}, found: map[token.Pos]string{},
	}
	for _, tf := range files {
		for _, d := range tf.f.Decls {
			if gd, ok := d.(*ast.GenDecl); ok {
				for _, spec := range gd.Specs {
					collectSpec(spec, s.vars, s.types)
				}
			}
		}
	}
	for _, tf := range files {
		for _, d := range tf.f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fd.Recv == nil && isTestFunc(fd.Name.Name) {
				s.report(fd.Pos(), fd.Name.Name)
			}
			if fd.Body != nil {
				s.subtests(fd)
			}
		}
	}
	for _, tf := range files {
		s.namedRows(tf.f)
	}
	var out []string
	for pos, name := range s.found {
		p := fset.Position(pos)
		out = append(out, p.Filename+"\x00"+strconv.Itoa(1e6+p.Line)+"\x00"+name)
	}
	sort.Strings(out)
	for i, o := range out {
		parts := strings.SplitN(o, "\x00", 3)
		line, _ := strconv.Atoi(parts[1])
		out[i] = parts[0] + ":" + strconv.Itoa(line-1e6) + ": " + parts[2]
	}
	return out
}

func (s *testNameScan) report(pos token.Pos, name string) {
	if decisionNumber.MatchString(name) {
		s.found[pos] = name
	}
}

func (s *testNameScan) reportParts(e ast.Expr) {
	for _, part := range nameParts(e) {
		if name, ok := stringLit(part); ok {
			s.report(part.Pos(), name)
		}
	}
}

func isTestFunc(name string) bool {
	for _, prefix := range testFuncPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func collectSpec(spec ast.Spec, vars map[string]ast.Expr, types map[string]*ast.StructType) {
	switch s := spec.(type) {
	case *ast.ValueSpec:
		for i, n := range s.Names {
			switch {
			case i < len(s.Values):
				vars[n.Name] = s.Values[i]
			case s.Type != nil:
				vars[n.Name] = s.Type // a declared table without a value: its type names the rows
			}
		}
	case *ast.TypeSpec:
		if st, ok := s.Type.(*ast.StructType); ok {
			types[s.Name.Name] = st
		}
	}
}

// subtests reports the names of the subtests a function starts.
func (s *testNameScan) subtests(fd *ast.FuncDecl) {
	vars, types := map[string]ast.Expr{}, map[string]*ast.StructType{}
	for k, v := range s.vars {
		vars[k] = v
	}
	for k, v := range s.types {
		types[k] = v
	}
	params := func(ft *ast.FuncType) {
		for _, field := range ft.Params.List {
			for _, n := range field.Names {
				vars[n.Name] = field.Type
			}
		}
	}
	params(fd.Type)
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for i, l := range x.Lhs {
				if id, ok := l.(*ast.Ident); ok && i < len(x.Rhs) {
					vars[id.Name] = x.Rhs[i]
				}
			}
		case ast.Spec:
			collectSpec(x, vars, types)
		case *ast.FuncLit:
			params(x.Type)
		}
		return true
	})
	var stack []ast.Node
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); !ok || sel.Sel.Name != "Run" {
			return true
		}
		for _, part := range nameParts(call.Args[0]) {
			if name, ok := stringLit(part); ok {
				s.report(part.Pos(), name)
				continue
			}
			s.tableRows(part, stack, vars, types)
		}
		return true
	})
}

// nameParts splits a subtest's name into the operands of its concatenation; the format of fmt.Sprintf counts as one.
func nameParts(e ast.Expr) []ast.Expr {
	switch x := e.(type) {
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			return append(nameParts(x.X), nameParts(x.Y)...)
		}
	case *ast.ParenExpr:
		return nameParts(x.X)
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Sprintf" && len(x.Args) > 0 {
			return []ast.Expr{x.Args[0]}
		}
	}
	return []ast.Expr{e}
}

func stringLit(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(bl.Value)
	return s, err == nil
}

// tableRows follows a part of a subtest's name that is a range variable (`tc.name`, `name`) to its table. A literal table's
// rows are reported at once; a table of a named struct type records the field, and namedRows reports the rows wherever the
// package builds them. A table it cannot follow gives nothing.
func (s *testNameScan) tableRows(part ast.Expr, stack []ast.Node, vars map[string]ast.Expr, types map[string]*ast.StructType) {
	var root, field string
	switch x := part.(type) {
	case *ast.Ident:
		root = x.Name
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		if !ok {
			return
		}
		root, field = id.Name, x.Sel.Name
	default:
		return
	}
	for i := len(stack) - 1; i >= 0; i-- {
		rs, ok := stack[i].(*ast.RangeStmt)
		if !ok {
			continue
		}
		isKey, isValue := identNamed(rs.Key, root), identNamed(rs.Value, root)
		if !isKey && !isValue {
			continue
		}
		table := typeOrLiteral(rs.X, vars)
		tableType := table
		lit, isLit := table.(*ast.CompositeLit)
		if isLit {
			tableType = lit.Type
		}
		elem, isMap := elementType(tableType)
		if isKey && !isMap {
			return // an index
		}
		if id, ok := elem.(*ast.Ident); ok && !isKey && field != "" {
			if s.rowName[id.Name] == nil {
				s.rowName[id.Name] = map[string]bool{}
			}
			s.rowName[id.Name][field] = true
		}
		if isLit {
			s.literalRows(lit, isKey, isMap, field, structOf(elem, types))
		}
		return
	}
}

// literalRows reports the names of a literal table's rows: each key of a map, each row, or a field of each row.
func (s *testNameScan) literalRows(table *ast.CompositeLit, isKey, isMap bool, field string, row *ast.StructType) {
	for _, el := range table.Elts {
		key, value := ast.Expr(nil), el
		if kv, ok := el.(*ast.KeyValueExpr); ok && isMap {
			key, value = kv.Key, kv.Value
		}
		switch {
		case isKey:
			s.reportParts(key)
		case field == "":
			s.reportParts(value)
		default:
			if name := fieldOf(value, field, row); name != nil {
				s.reportParts(name)
			}
		}
	}
}

// namedRows reports the name fields of every row of a named struct type a subtest is named by, wherever a file of the package
// builds one: `T{…}`, or a row of a `[]T{…}` or `map[K]T{…}` literal.
func (s *testNameScan) namedRows(f *ast.File) {
	if len(s.rowName) == 0 {
		return
	}
	row := func(e ast.Expr, typeName string) {
		for field := range s.rowName[typeName] {
			if name := fieldOf(e, field, s.types[typeName]); name != nil {
				s.reportParts(name)
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if id, ok := cl.Type.(*ast.Ident); ok && s.rowName[id.Name] != nil {
			row(cl, id.Name)
			return true
		}
		elem, isMap := elementType(cl.Type)
		id, ok := elem.(*ast.Ident)
		if !ok || s.rowName[id.Name] == nil {
			return true
		}
		for _, el := range cl.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok && isMap {
				el = kv.Value
			}
			if c, ok := el.(*ast.CompositeLit); ok && c.Type == nil {
				row(c, id.Name) // T{…} itself is found by the case above
			}
		}
		return true
	})
}

// elementType returns the row type of a slice, array or map type (through a pointer to a named row), and whether it is a map.
func elementType(e ast.Expr) (ast.Expr, bool) {
	var elem ast.Expr
	isMap := false
	switch t := e.(type) {
	case *ast.ArrayType:
		elem = t.Elt
	case *ast.MapType:
		elem, isMap = t.Value, true
	default:
		return nil, false
	}
	if star, ok := elem.(*ast.StarExpr); ok {
		elem = star.X
	}
	return elem, isMap
}

func identNamed(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// typeOrLiteral follows a range expression through the variables it is assigned from to a composite literal, or to the declared
// type of a parameter or a variable without a value.
func typeOrLiteral(e ast.Expr, vars map[string]ast.Expr) ast.Expr {
	for range 4 {
		switch x := e.(type) {
		case *ast.UnaryExpr:
			e = x.X
		case *ast.Ident:
			v, ok := vars[x.Name]
			if !ok {
				return nil
			}
			e = v
		default:
			return e
		}
	}
	return nil
}

func structOf(e ast.Expr, types map[string]*ast.StructType) *ast.StructType {
	switch x := e.(type) {
	case *ast.StructType:
		return x
	case *ast.Ident:
		return types[x.Name]
	}
	return nil
}

// fieldOf returns the value of a row's field, written with its key or at its position in the row's struct type.
func fieldOf(row ast.Expr, field string, st *ast.StructType) ast.Expr {
	if u, ok := row.(*ast.UnaryExpr); ok {
		row = u.X
	}
	cl, ok := row.(*ast.CompositeLit)
	if !ok || len(cl.Elts) == 0 {
		return nil
	}
	if _, keyed := cl.Elts[0].(*ast.KeyValueExpr); keyed {
		for _, el := range cl.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok && identNamed(kv.Key, field) {
				return kv.Value
			}
		}
		return nil
	}
	if st == nil {
		return nil
	}
	i := 0
	for _, f := range st.Fields.List {
		if len(f.Names) == 0 {
			i++
			continue
		}
		for _, n := range f.Names {
			if n.Name == field {
				if i < len(cl.Elts) {
					return cl.Elts[i]
				}
				return nil
			}
			i++
		}
	}
	return nil
}

// decisionNamesBelow reads every _test.go file of the repository but the web interface's, the modules' dependencies and the
// fixtures in testdata, one directory (one package and its external test package) at a time, the directories in parallel:
// under the race detector and the coverage of the unit job the walk took 6.8 s in one goroutine and takes 1.2 s on 20 cores.
func decisionNamesBelow(root string) ([]string, int, error) {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if path != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "testdata") || path == filepath.Join(root, "web") {
			return fs.SkipDir
		}
		dirs = append(dirs, path)
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	type result struct {
		found []string
		files int
		err   error
	}
	results := make([]result, len(dirs))
	next := make(chan int)
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for i := range next {
				results[i].found, results[i].files, results[i].err = decisionNamesIn(root, dirs[i])
			}
		})
	}
	for i := range dirs {
		next <- i
	}
	close(next)
	wg.Wait()
	var out []string
	count := 0
	for _, r := range results {
		if r.err != nil {
			return nil, 0, r.err
		}
		out = append(out, r.found...)
		count += r.files
	}
	return out, count, nil
}

// decisionNamesIn reads the _test.go files of one directory.
func decisionNamesIn(root, dir string) ([]string, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}
	fset := token.NewFileSet()
	var files []testFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file := filepath.Join(dir, e.Name())
		src, err := os.ReadFile(file) //nolint:gosec // G304: a file of the repository's own tree
		if err != nil {
			return nil, 0, err
		}
		rel, _ := filepath.Rel(root, file)
		f, err := parser.ParseFile(fset, filepath.ToSlash(rel), src, parser.SkipObjectResolution)
		if err != nil {
			return nil, 0, err
		}
		files = append(files, testFile{rel: filepath.ToSlash(rel), f: f})
	}
	return decisionNames(fset, files), len(files), nil
}

func TestGoTestNamesCarryARequirementNeverADecision(t *testing.T) {
	t.Run("a decision's number in a test function, a subtest or a table row the subtest takes its name from is named", func(t *testing.T) {
		const src = `package x

import ("fmt"; "testing")

type row struct{ name string; want int }

var rows = []row{{"Q-OCI-1 a package-level row", 1}, {"R-OCI-001 a clean row", 2}}

func TestQ_OCI_2_Function(t *testing.T) {}
func TestR_OCI_002_DefaultsOfQ_OCI_3(t *testing.T) {}
func BenchmarkADR141(b *testing.B) {}
func TestR_OCI_004_Clean(t *testing.T) {
	t.Run("R-OCI-004 a criterion (Q-OCI-5)", func(t *testing.T) {})
	t.Run("ADR-6 "+"tail", func(t *testing.T) {})
	t.Run(fmt.Sprintf("Q-OCI-7 case %d", 1), func(t *testing.T) {})
	for _, tc := range []struct{ in, name string }{{"x", "Q_OCI_8 a positional row"}, {in: "y", name: "keyed and clean"}} {
		t.Run("R-OCI-004 "+tc.name, func(t *testing.T) {})
	}
	for name := range map[string]int{"a key of Q-OCI-9": 1} {
		t.Run(name, func(t *testing.T) {})
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {})
	}
	t.Run("R-OCI-004 a FAQ_UI_1 is no decision, neither is SQ-OCI-1 nor a message", func(t *testing.T) { t.Error("Q-OCI-10") })
}
`
		// A second file of the package hands a table to a helper of the first file's row type.
		const helper = `package x

import "testing"

func runRows(t *testing.T, rs []row) {
	for _, r := range rs {
		t.Run(r.name, func(t *testing.T) {})
	}
}

func TestR_OCI_005_Helper(t *testing.T) {
	runRows(t, []row{{"Q-OCI-11 a row handed to a helper", 1}, {name: "a keyed clean row"}})
	_ = row{name: "R-OCI-005 Q-OCI-12 a row built alone", want: 2}
}
`
		fset := token.NewFileSet()
		var files []testFile
		for name, text := range map[string]string{"x_test.go": src, "y_test.go": helper} {
			f, err := parser.ParseFile(fset, name, text, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, testFile{rel: name, f: f})
		}
		want := []string{
			"x_test.go:7: Q-OCI-1 a package-level row",
			"x_test.go:9: TestQ_OCI_2_Function",
			"x_test.go:10: TestR_OCI_002_DefaultsOfQ_OCI_3",
			"x_test.go:11: BenchmarkADR141",
			"x_test.go:13: R-OCI-004 a criterion (Q-OCI-5)",
			"x_test.go:14: ADR-6 ",
			"x_test.go:15: Q-OCI-7 case %d",
			"x_test.go:16: Q_OCI_8 a positional row",
			"x_test.go:19: a key of Q-OCI-9",
			"y_test.go:12: Q-OCI-11 a row handed to a helper",
			"y_test.go:13: R-OCI-005 Q-OCI-12 a row built alone",
		}
		if got := decisionNames(fset, files); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	})
	t.Run("no Go test of the repository carries a decision's number in its name", func(t *testing.T) {
		found, files, err := decisionNamesBelow(repoRoot(t))
		if err != nil {
			t.Fatal(err)
		}
		if files < 1000 {
			t.Fatalf("read %d test files: the walk missed the tree", files)
		}
		if len(found) > 0 {
			t.Errorf("%d test names carry a decision's number; name the requirement the test holds (R-…), or the behaviour alone:\n%s",
				len(found), strings.Join(found, "\n"))
		}
	})
}
