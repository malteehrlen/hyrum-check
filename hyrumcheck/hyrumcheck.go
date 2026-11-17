// Package hyrumcheck defines an analyzer that reports code whose behavior
// depends on the text of an error message.
//
// Error strings are not part of an API contract, but (Hyrum's law) once
// callers match on them they become one. Compare with errors.Is, errors.As,
// or a sentinel/typed error instead.
//
// A finding can be suppressed with a "//hyrumcheck:ignore" comment on the
// same line or the line above.
package hyrumcheck

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
)

const Doc = `report code that depends on the text of an error message

Matching on err.Error() (via ==, switch, strings.Contains, regexp, ...)
couples callers to message wording that the error's author may change.
Use errors.Is or errors.As instead.`

var Analyzer = &analysis.Analyzer{
	Name:     "hyrumcheck",
	Doc:      Doc,
	URL:      "https://pkg.go.dev/hyrumcheck",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

// stringSinks are functions that inspect the content of a string argument.
var stringSinks = map[string]bool{
	"strings.Contains": true, "strings.ContainsAny": true, "strings.ContainsRune": true,
	"strings.HasPrefix": true, "strings.HasSuffix": true, "strings.EqualFold": true,
	"strings.Index": true, "strings.IndexAny": true, "strings.IndexByte": true,
	"strings.IndexRune": true, "strings.LastIndex": true, "strings.Count": true,
	"strings.Compare": true, "strings.Cut": true, "strings.CutPrefix": true,
	"strings.CutSuffix": true, "strings.Split": true, "strings.SplitN": true,
	"strings.Fields": true, "strings.TrimPrefix": true, "strings.TrimSuffix": true,
	"regexp.MatchString": true, "regexp.Match": true,
	"bytes.Contains": true, "bytes.HasPrefix": true, "bytes.HasSuffix": true,
	"bytes.Equal": true, "bytes.Index": true,
}

// regexpMethods are (*regexp.Regexp) methods that inspect their argument.
var regexpMethods = map[string]bool{
	"Match": true, "MatchString": true, "Find": true, "FindString": true,
	"FindIndex": true, "FindStringIndex": true, "FindSubmatch": true,
	"FindStringSubmatch": true, "FindAll": true, "FindAllString": true,
	"FindAllStringSubmatch": true, "FindAllSubmatch": true,
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	errorType := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)

	ignored := ignoredLines(pass)
	report := func(pos token.Pos, format string, args ...any) {
		p := pass.Fset.Position(pos)
		if ignored[lineKey{p.Filename, p.Line}] || ignored[lineKey{p.Filename, p.Line - 1}] {
			return
		}
		pass.Reportf(pos, format, args...)
	}

	// tainted records local variables that hold an error's message text.
	tainted := map[types.Object]bool{}

	var isErrorText func(ast.Expr) bool
	isErrorText = func(e ast.Expr) bool {
		e = ast.Unparen(e)
		switch e := e.(type) {
		case *ast.Ident:
			return tainted[pass.TypesInfo.Uses[e]]
		case *ast.CallExpr:
			// []byte(text), string(text)
			if tv, ok := pass.TypesInfo.Types[e.Fun]; ok && tv.IsType() && len(e.Args) == 1 {
				return isErrorText(e.Args[0])
			}
			// err.Error()
			if sel, ok := e.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Error" && len(e.Args) == 0 {
				if t := pass.TypesInfo.TypeOf(sel.X); t != nil && types.Implements(t, errorType) {
					return true
				}
			}
			// fmt.Sprint(err), fmt.Sprintf("%v", err), ...
			if fn := typeutil.StaticCallee(pass.TypesInfo, e); fn != nil && fn.Pkg() != nil && fn.Pkg().Path() == "fmt" {
				switch fn.Name() {
				case "Sprint", "Sprintf", "Sprintln":
					for _, a := range e.Args {
						if t := pass.TypesInfo.TypeOf(a); t != nil && types.Implements(t, errorType) {
							return true
						}
					}
				}
			}
		}
		return false
	}

	// Propagate through simple assignments first; the inspector visits in
	// source order, so a single pass handles straight-line code.
	insp.Preorder([]ast.Node{(*ast.AssignStmt)(nil), (*ast.ValueSpec)(nil)}, func(n ast.Node) {
		var lhs []ast.Expr
		var rhs []ast.Expr
		switch n := n.(type) {
		case *ast.AssignStmt:
			lhs, rhs = n.Lhs, n.Rhs
		case *ast.ValueSpec:
			for _, id := range n.Names {
				lhs = append(lhs, id)
			}
			rhs = n.Values
		}
		if len(lhs) != len(rhs) {
			return
		}
		for i, l := range lhs {
			id, ok := l.(*ast.Ident)
			if !ok {
				continue
			}
			obj := pass.TypesInfo.ObjectOf(id)
			if obj != nil && isErrorText(rhs[i]) {
				tainted[obj] = true
			}
		}
	})

	insp.Preorder([]ast.Node{
		(*ast.BinaryExpr)(nil), (*ast.SwitchStmt)(nil), (*ast.CallExpr)(nil),
	}, func(n ast.Node) {
		switch n := n.(type) {
		case *ast.BinaryExpr:
			switch n.Op {
			case token.EQL, token.NEQ, token.LSS, token.GTR, token.LEQ, token.GEQ:
				if isErrorText(n.X) || isErrorText(n.Y) {
					report(n.Pos(), "comparing error message text; use errors.Is or errors.As instead")
				}
			}
		case *ast.SwitchStmt:
			if n.Tag != nil && isErrorText(n.Tag) {
				report(n.Tag.Pos(), "switching on error message text; use errors.Is or errors.As instead")
			}
		case *ast.CallExpr:
			name := calleeName(pass, n)
			if name == "" {
				return
			}
			for _, a := range n.Args {
				if isErrorText(a) {
					report(n.Pos(), "%s depends on error message text; use errors.Is or errors.As instead", name)
					return
				}
			}
		}
	})
	return nil, nil
}

// calleeName returns a display name if call is a sink that inspects string
// content, or "" otherwise.
func calleeName(pass *analysis.Pass, call *ast.CallExpr) string {
	fn, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || fn.Pkg() == nil {
		return ""
	}
	sig := fn.Type().(*types.Signature)
	if recv := sig.Recv(); recv != nil {
		t := recv.Type()
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		if named, ok := t.(*types.Named); ok && named.Obj().Pkg() != nil &&
			named.Obj().Pkg().Path() == "regexp" && named.Obj().Name() == "Regexp" &&
			regexpMethods[fn.Name()] {
			return "(*regexp.Regexp)." + fn.Name()
		}
		return ""
	}
	name := fn.Pkg().Path() + "." + fn.Name()
	if stringSinks[name] {
		return name
	}
	return ""
}

type lineKey struct {
	file string
	line int
}

// ignoredLines collects lines carrying a //hyrumcheck:ignore directive.
func ignoredLines(pass *analysis.Pass) map[lineKey]bool {
	m := map[lineKey]bool{}
	for _, f := range pass.Files {
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				if strings.HasPrefix(c.Text, "//hyrumcheck:ignore") {
					p := pass.Fset.Position(c.Pos())
					m[lineKey{p.Filename, p.Line}] = true
				}
			}
		}
	}
	return m
}
