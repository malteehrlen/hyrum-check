// Package hyrumcheck defines an analyzer that reports code whose behavior
// depends on the text of an error message.
//
// Error strings are not part of an API contract, but (Hyrum's law) once
// callers match on them they become one. Compare with errors.Is, errors.As,
// or a sentinel/typed error instead.
//
// Where the error came from decides how it is treated. Errors that originate
// in another module (including the standard library) are always reported,
// and the diagnostic suggests sentinel or typed errors the origin package
// exports. Errors that originate in the analyzed module are reported only
// with -internal, since both ends can be changed together.
//
// A finding can be suppressed with a "//hyrumcheck:ignore" comment on the
// same line or the line above, or for a whole API with -allow.
package hyrumcheck

import (
	"fmt"
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

var (
	includeTests    bool
	includeInternal bool
	allow           string
)

var Analyzer = &analysis.Analyzer{
	Name:     "hyrumcheck",
	Doc:      Doc,
	URL:      "https://pkg.go.dev/github.com/malteehrlen/hyrum-check/hyrumcheck",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func init() {
	Analyzer.Flags.BoolVar(&includeTests, "tests", false, "also check _test.go files")
	Analyzer.Flags.BoolVar(&includeInternal, "internal", false, "also report errors that originate in the analyzed module")
	Analyzer.Flags.StringVar(&allow, "allow", "", "comma-separated functions whose errors may be matched on text, e.g. 'github.com/lib/pq.*,os/exec.(*Cmd).Run'; a trailing * matches any suffix")
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

// origin describes where an error value came from. The zero value means
// unknown.
type origin struct {
	fn  *types.Func    // producing function, if known
	pkg *types.Package // producing package, if known
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	errorType := types.Universe.Lookup("error").Type()
	errorIface := errorType.Underlying().(*types.Interface)
	isError := func(t types.Type) bool { return t != nil && types.Implements(t, errorIface) }

	modPath := ""
	if pass.Module != nil {
		modPath = pass.Module.Path
	}
	internal := func(p *types.Package) bool {
		return p == pass.Pkg || (modPath != "" &&
			(p.Path() == modPath || strings.HasPrefix(p.Path(), modPath+"/")))
	}

	allowed := func(fn *types.Func) bool {
		if fn == nil || allow == "" {
			return false
		}
		name := fn.FullName()
		for _, pat := range strings.Split(allow, ",") {
			pat = strings.TrimSpace(pat)
			if pre, ok := strings.CutSuffix(pat, "*"); ok && pat != "" {
				if strings.HasPrefix(name, pre) {
					return true
				}
			} else if pat == name {
				return true
			}
		}
		return false
	}

	ignored := ignoredLines(pass)
	report := func(pos token.Pos, o origin, format string, args ...any) {
		p := pass.Fset.Position(pos)
		if !includeTests && strings.HasSuffix(p.Filename, "_test.go") {
			return
		}
		if ignored[lineKey{p.Filename, p.Line}] || ignored[lineKey{p.Filename, p.Line - 1}] {
			return
		}
		if allowed(o.fn) {
			return
		}
		ext := o.pkg != nil && !internal(o.pkg)
		if o.pkg != nil && !ext && !includeInternal {
			return
		}
		pass.Reportf(pos, format+"; %s", append(args, hint(o, ext, errorType))...)
	}

	// errOrigin records where local error variables came from; tainted
	// records local variables that hold an error's message text.
	// Both are flow-sensitive in source order: a use sees the latest
	// assignment that precedes it.
	errOrigin := assignments{}
	tainted := assignments{}

	var errorOrigin func(ast.Expr) origin
	var errorText func(ast.Expr) (origin, bool)

	errorOrigin = func(x ast.Expr) origin {
		x = ast.Unparen(x)
		var o origin
		switch x := x.(type) {
		case *ast.Ident:
			o, _ = errOrigin.at(pass.TypesInfo.ObjectOf(x), x.Pos())
		case *ast.CallExpr:
			fn, _ := typeutil.Callee(pass.TypesInfo, x).(*types.Func)
			if fn == nil || fn.Pkg() == nil {
				break
			}
			switch fn.Pkg().Path() {
			case "fmt":
				// fmt.Errorf("...%w", err) keeps the wrapped error's origin.
				for _, a := range x.Args {
					if t := pass.TypesInfo.TypeOf(a); isError(t) {
						if o = errorOrigin(a); o != (origin{}) {
							break
						}
					}
				}
			case "errors":
			default:
				o = origin{fn: fn, pkg: fn.Pkg()}
			}
		}
		if o != (origin{}) {
			return o
		}
		// A concrete error type tells us its package.
		t := pass.TypesInfo.TypeOf(x)
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		if n, ok := t.(*types.Named); ok && !types.IsInterface(n) && n.Obj().Pkg() != nil {
			return origin{pkg: n.Obj().Pkg()}
		}
		return origin{}
	}

	errorText = func(e ast.Expr) (origin, bool) {
		e = ast.Unparen(e)
		switch e := e.(type) {
		case *ast.Ident:
			return tainted.at(pass.TypesInfo.ObjectOf(e), e.Pos())
		case *ast.CallExpr:
			// []byte(text), string(text)
			if tv, ok := pass.TypesInfo.Types[e.Fun]; ok && tv.IsType() && len(e.Args) == 1 {
				return errorText(e.Args[0])
			}
			// err.Error()
			if sel, ok := e.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Error" && len(e.Args) == 0 {
				if isError(pass.TypesInfo.TypeOf(sel.X)) {
					return errorOrigin(sel.X), true
				}
			}
			// fmt.Sprint(err), fmt.Sprintf("%v", err), ...
			if fn := typeutil.StaticCallee(pass.TypesInfo, e); fn != nil && fn.Pkg() != nil && fn.Pkg().Path() == "fmt" {
				switch fn.Name() {
				case "Sprint", "Sprintf", "Sprintln":
					for _, a := range e.Args {
						if isError(pass.TypesInfo.TypeOf(a)) {
							return errorOrigin(a), true
						}
					}
				}
			}
		}
		return origin{}, false
	}

	// Propagate through simple assignments first; the inspector visits in
	// source order, so a single pass handles straight-line code.
	insp.Preorder([]ast.Node{(*ast.AssignStmt)(nil), (*ast.ValueSpec)(nil)}, func(n ast.Node) {
		var lhs, rhs []ast.Expr
		end := n.End()
		switch n := n.(type) {
		case *ast.AssignStmt:
			lhs, rhs = n.Lhs, n.Rhs
		case *ast.ValueSpec:
			for _, id := range n.Names {
				lhs = append(lhs, id)
			}
			rhs = n.Values
		}
		for i, l := range lhs {
			id, ok := l.(*ast.Ident)
			if !ok {
				continue
			}
			obj := pass.TypesInfo.ObjectOf(id)
			if obj == nil {
				continue
			}
			switch {
			case len(lhs) == len(rhs):
				if o, ok := errorText(rhs[i]); ok {
					tainted.set(obj, end, o, true)
				} else {
					tainted.set(obj, end, origin{}, false)
					if isError(obj.Type()) {
						errOrigin.set(obj, end, errorOrigin(rhs[i]), true)
					}
				}
			case len(rhs) == 1 && isError(obj.Type()):
				// x, err := f()
				if call, ok := ast.Unparen(rhs[0]).(*ast.CallExpr); ok {
					errOrigin.set(obj, end, errorOrigin(call), true)
				}
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
				o, ok := errorText(n.X)
				if !ok {
					o, ok = errorText(n.Y)
				}
				if ok {
					report(n.Pos(), o, "comparing error message text")
				}
			}
		case *ast.SwitchStmt:
			if n.Tag != nil {
				if o, ok := errorText(n.Tag); ok {
					report(n.Tag.Pos(), o, "switching on error message text")
				}
			}
		case *ast.CallExpr:
			name := calleeName(pass, n)
			if name == "" {
				return
			}
			for _, a := range n.Args {
				if o, ok := errorText(a); ok {
					report(n.Pos(), o, "%s depends on error message text", name)
					return
				}
			}
		}
	})
	return nil, nil
}

// assignments records, per variable, the origin assigned at each position.
type assignments map[types.Object][]assignment

type assignment struct {
	pos token.Pos
	o   origin
	ok  bool // false clears the variable
}

func (a assignments) set(obj types.Object, pos token.Pos, o origin, ok bool) {
	a[obj] = append(a[obj], assignment{pos, o, ok})
}

// at returns the origin of obj as of pos.
func (a assignments) at(obj types.Object, pos token.Pos) (origin, bool) {
	list := a[obj]
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].pos <= pos {
			return list[i].o, list[i].ok
		}
	}
	return origin{}, false
}

// hint suggests what to match on instead of the message text.
func hint(o origin, external bool, errorType types.Type) string {
	const generic = "use errors.Is or errors.As instead"
	if !external {
		return generic
	}
	iface := errorType.Underlying().(*types.Interface)
	var sentinels, typed []string
	scope := o.pkg.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}
		switch obj := obj.(type) {
		case *types.Var:
			if types.Identical(obj.Type(), errorType) {
				sentinels = append(sentinels, o.pkg.Name()+"."+name)
			}
		case *types.TypeName:
			if types.IsInterface(obj.Type()) {
				continue
			}
			if types.Implements(obj.Type(), iface) {
				typed = append(typed, o.pkg.Name()+"."+name)
			} else if types.Implements(types.NewPointer(obj.Type()), iface) {
				typed = append(typed, "*"+o.pkg.Name()+"."+name)
			}
		}
	}
	var parts []string
	if len(sentinels) > 0 {
		parts = append(parts, "errors.Is with "+listUpTo(sentinels, 3))
	}
	if len(typed) > 0 {
		parts = append(parts, "errors.As with "+listUpTo(typed, 3))
	}
	if len(parts) > 0 {
		return fmt.Sprintf("error originates in %s; try %s", o.pkg.Path(), strings.Join(parts, " or "))
	}
	return fmt.Sprintf("error originates in %s, which exports no sentinel or typed errors; ask upstream for one, or add //hyrumcheck:ignore", o.pkg.Path())
}

func listUpTo(names []string, n int) string {
	if len(names) > n {
		return strings.Join(names[:n], ", ") + ", ..."
	}
	return strings.Join(names, ", ")
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
