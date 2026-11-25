package archtest

import (
	"go/ast"
	"go/constant"
	"go/types"

	"golang.org/x/tools/go/packages"
)

// urlFieldRule is the requirement the check enforces: an address in a log record is in the one form of R-OPS-013 rule 7, so that
// no signed address an upstream handed back reaches the log with its credential (Q-OPS-426).
const urlFieldRule = "R-OPS-013"

// urlFieldKey is the key of the log field that holds an address (the upstream log's url, R-OPS-011).
const urlFieldKey = "url"

// urlFieldFuncs are the functions and methods of internal/observability that render an address in that form.
var urlFieldFuncs = map[string]bool{
	"ScrubURL": true, "ScrubHandedURL": true, "ScrubRequestURL": true, "ScrubURLPath": true, "ScrubAddress": true,
	"URL": true, "HandedURL": true, "RequestURL": true, "URLPath": true,
}

// urlFieldTrees are the trees whose code runs in a replica or a command of the binary.
var urlFieldTrees = []string{"internal", "cmd"}

// urlFieldExempt are the trees under urlFieldTrees that are no code of the binary.
var urlFieldExempt = []string{"internal/archtest", "internal/tools"}

// CheckURLLogFields finds a log field named "url" whose value is not rendered by a function of internal/observability that
// renders an address (ScrubURL, ScrubHandedURL, ScrubRequestURL, ScrubAddress, or the Scrubber's URL, HandedURL and RequestURL):
// the key "url" as the key of slog.String or slog.Any, or among the arguments of a method of *slog.Logger. The logger
// scrubs every field as free text too; the check holds the field to the form that also redacts what the redaction list does not
// name. Test files are not loaded.
func CheckURLLogFields(module string, pkgs []*packages.Package) []Violation {
	var out []Violation
	obs := module + "/internal/observability"
	for _, p := range pkgs {
		r := rel(module, p.PkgPath)
		if p.TypesInfo == nil || !underAny(r, urlFieldTrees) || underAny(r, urlFieldExempt) {
			continue
		}
		for _, f := range p.Syntax {
			file := r + "/" + fileBase(p, f)
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || !slogCall(p.TypesInfo, call) {
					return true
				}
				for i, a := range call.Args {
					if !isURLKey(p.TypesInfo, a) || i+1 >= len(call.Args) {
						continue
					}
					if !rendersAddress(p.TypesInfo, call.Args[i+1], obs) {
						out = append(out, Violation{
							Rule: urlFieldRule, From: file, To: types.ExprString(call.Args[i+1]),
							Detail: "a log field url takes its value from observability.ScrubURL, ScrubHandedURL, ScrubRequestURL or ScrubAddress (or the Scrubber's URL, HandedURL, RequestURL)",
						})
					}
				}
				return true
			})
		}
	}
	sortViolations(out)
	return out
}

// slogCall reports whether call is a function of log/slog or a method of *slog.Logger.
func slogCall(info *types.Info, call *ast.CallExpr) bool {
	var id *ast.Ident
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		id = fn.Sel
	case *ast.Ident:
		id = fn
	default:
		return false
	}
	f, ok := info.Uses[id].(*types.Func)
	return ok && f.Pkg() != nil && f.Pkg().Path() == "log/slog"
}

// isURLKey reports whether e is the constant string "url".
func isURLKey(info *types.Info, e ast.Expr) bool {
	tv, ok := info.Types[e]
	return ok && tv.Value != nil && tv.Value.Kind() == constant.String && constant.StringVal(tv.Value) == urlFieldKey
}

// rendersAddress reports whether e is a call of one of urlFieldFuncs of the package obs.
func rendersAddress(info *types.Info, e ast.Expr, obs string) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	var id *ast.Ident
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		id = fn.Sel
	case *ast.Ident:
		id = fn
	default:
		return false
	}
	f, ok := info.Uses[id].(*types.Func)
	return ok && f.Pkg() != nil && f.Pkg().Path() == obs && urlFieldFuncs[f.Name()]
}
