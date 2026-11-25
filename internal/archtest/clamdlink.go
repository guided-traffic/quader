package archtest

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"
)

// clamdLinkRule is the requirement the check enforces: the malware scanner reaches the clamd beside it over one Unix socket of the pod
// and nothing else (malware-scanning.md R-MAL-007). It holds the owner's condition of 2026-11-25 for running ClamAV on his development
// machine, in his words: "Ich will dass davon nichts in die ausgelieferten Images wandert und ich will keinen neuen offenen Port im
// produktiven quader." The UI development profile therefore runs the chart's pod on Docker (architecture.md Q-ARCH-1065), and the
// product keeps no way to clamd but the socket.
const clamdLinkRule = "R-MAL-007"

// clamdPkg is the client of clamd and, below it, its test double: the one tree that speaks clamd's protocol.
const clamdPkg = "internal/malware/clamd"

// clamdLinkTrees are the trees of the binary the check reads; clamdLinkExempt are the repository's own tools among them, which are no
// code of the binary, as for the check of the default logger.
var (
	clamdLinkTrees  = []string{"internal", "cmd"}
	clamdLinkExempt = []string{"internal/archtest", "internal/tools"}
)

// clamdCommands are the words of clamd's protocol that only a client of clamd writes: a package outside clamdPkg that holds one in a
// string speaks to clamd itself, past the client and its socket.
var clamdCommands = []string{"INSTREAM", "IDSESSION"}

// CheckClamdLink finds, in the code of the binary (internal/ and cmd/ but the tools), every way to clamd that is not the Unix socket:
//   - a call in clamdPkg's tree of a function or method whose name starts with Dial and whose network, its first string parameter, is
//     anything but the constant "unix" (a TCP address, a network from the configuration);
//   - a call of the client's SetDialer, which replaces how the client reaches clamd and is meant for the client's own tests (test files
//     are not loaded, so every call found is one of the binary);
//   - a string holding a command of clamd's protocol outside clamdPkg's tree: a second client of clamd.
//
// The check resolves what is called, so an alias of a package or a method value does not get around it.
func CheckClamdLink(module string, pkgs []*packages.Package) []Violation {
	var out []Violation
	for _, p := range pkgs {
		r := rel(module, p.PkgPath)
		if p.TypesInfo == nil || !underAny(r, clamdLinkTrees) || underAny(r, clamdLinkExempt) {
			continue
		}
		inClamd := under(r, clamdPkg)
		for _, f := range p.Syntax {
			file := r + "/" + fileBase(p, f)
			ast.Inspect(f, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CallExpr:
					out = append(out, clamdCall(p, module, file, inClamd, x)...)
				case *ast.BasicLit:
					if !inClamd && x.Kind == token.STRING {
						for _, c := range clamdCommands {
							if strings.Contains(x.Value, c) {
								out = append(out, Violation{
									Rule: clamdLinkRule, From: file, To: c,
									Detail: "only " + clamdPkg + " speaks clamd's protocol, over the Unix socket of the pod; call its client",
								})
							}
						}
					}
				}
				return true
			})
		}
	}
	sortViolations(out)
	return out
}

// clamdCall checks one call: a dial in clamdPkg's tree, and a call of the client's SetDialer anywhere.
func clamdCall(p *packages.Package, module, file string, inClamd bool, call *ast.CallExpr) []Violation {
	fn := calledFunc(p, call)
	if fn == nil {
		return nil
	}
	sig, _ := fn.Type().(*types.Signature)
	if sig == nil {
		return nil
	}
	if fn.Name() == "SetDialer" && sig.Recv() != nil && fn.Pkg() != nil && rel(module, fn.Pkg().Path()) == clamdPkg {
		return []Violation{{
			Rule: clamdLinkRule, From: file, To: fn.Pkg().Path() + ".SetDialer",
			Detail: "the client reaches clamd by the Unix socket it was built with; SetDialer is for the client's own tests",
		}}
	}
	if !inClamd || !strings.HasPrefix(fn.Name(), "Dial") {
		return nil
	}
	i := firstStringParam(sig)
	if i < 0 || i >= len(call.Args) {
		return nil
	}
	if tv, ok := p.TypesInfo.Types[call.Args[i]]; ok && tv.Value != nil && tv.Value.Kind() == constant.String && constant.StringVal(tv.Value) == "unix" {
		return nil
	}
	return []Violation{{
		Rule: clamdLinkRule, From: file, To: fn.Name(),
		Detail: "clamd is dialled by the constant network \"unix\" only: the socket of the pod, no TCP address and no network from the configuration",
	}}
}

// calledFunc resolves the function or method a call names, nil for a conversion, a builtin or a call of a function value.
func calledFunc(p *packages.Package, call *ast.CallExpr) *types.Func {
	var id *ast.Ident
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	default:
		return nil
	}
	fn, _ := p.TypesInfo.Uses[id].(*types.Func)
	return fn
}

// firstStringParam is the index of the first parameter of type string, the network of every dial function of the standard library and
// of internal/outbound; -1 when there is none.
func firstStringParam(sig *types.Signature) int {
	for i := range sig.Params().Len() {
		if b, ok := sig.Params().At(i).Type().Underlying().(*types.Basic); ok && b.Kind() == types.String {
			return i
		}
	}
	return -1
}
