package archtest

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/packages"
)

// defaultLoggerRule is the requirement the check enforces: every line a replica writes carries the replica, the role, the log and the
// component (R-OPS-009), which the process's default logger does not.
const defaultLoggerRule = "R-OPS-009"

// defaultLoggerFuncs are the functions of log/slog that write through, or hand out, the process's default logger.
var defaultLoggerFuncs = map[string]bool{
	"Default": true, "Debug": true, "Info": true, "Warn": true, "Error": true, "Log": true, "LogAttrs": true,
	"DebugContext": true, "InfoContext": true, "WarnContext": true, "ErrorContext": true,
}

// CheckDefaultLogger finds a use of the process's default logger in the formats' tree (the contract, the formats, the kit): a
// package-level function of log/slog that logs or returns the default logger, and any function of the standard package log. A format
// logs through the logger its request or its component carries (formats.Request.Log, observability.LoggerFrom). The check resolves what
// an identifier is, so an alias of the package does not get around it. Test files are not loaded.
func CheckDefaultLogger(module string, pkgs []*packages.Package) []Violation {
	var out []Violation
	for _, p := range pkgs {
		r := rel(module, p.PkgPath)
		if p.TypesInfo == nil || !under(r, formatRoot) {
			continue
		}
		for _, f := range p.Syntax {
			file := r + "/" + fileBase(p, f)
			seen := map[string]bool{}
			ast.Inspect(f, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				fn, ok := p.TypesInfo.Uses[id].(*types.Func)
				if !ok || fn.Pkg() == nil || fn.Type().(*types.Signature).Recv() != nil {
					return true
				}
				name := fn.Pkg().Path() + "." + fn.Name()
				banned := fn.Pkg().Path() == "log" || fn.Pkg().Path() == "log/slog" && defaultLoggerFuncs[fn.Name()]
				if !banned || seen[name] {
					return true
				}
				seen[name] = true
				out = append(out, Violation{
					Rule: defaultLoggerRule, From: file, To: name,
					Detail: "the default logger carries none of the replica's fields; log through the logger of the request (formats.Request.Log) or observability.LoggerFrom",
				})
				return true
			})
		}
	}
	sortViolations(out)
	return out
}
