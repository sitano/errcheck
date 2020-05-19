// Package errcheck is the library used to implement the errcheck command-line tool.
//
// Note: The API of this package has not been finalized and may change at any point.
package errcheck

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"golang.org/x/tools/go/packages"
	"os"
	"regexp"
	"strings"
)

// visitor implements the errcheck algorithm
type visitor struct {
	pkg         *packages.Package
	ignore      map[string]*regexp.Regexp
	blank       bool
	asserts     bool
	lines       map[string][]string
	exclude     map[string]bool
	go111module bool
	depth       int

	ss scopes
	errors []UncheckedError
}

// selectorAndFunc tries to get the selector and function from call expression.
// For example, given the call expression representing "a.b()", the selector
// is "a.b" and the function is "b" itself.
//
// The final return value will be true if it is able to do extract a selector
// from the call and look up the function object it refers to.
//
// If the call does not include a selector (like if it is a plain "f()" function call)
// then the final return value will be false.
func (v *visitor) selectorAndFunc(call *ast.CallExpr) (*ast.SelectorExpr, *types.Func, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, nil, false
	}

	fn, ok := v.pkg.TypesInfo.ObjectOf(sel.Sel).(*types.Func)
	if !ok {
		// Shouldn't happen, but be paranoid
		return nil, nil, false
	}

	return sel, fn, true

}

// fullName will return a package / receiver-type qualified name for a called function
// if the function is the result of a selector. Otherwise it will return
// the empty string.
//
// The name is fully qualified by the import path, possible type,
// function/method name and pointer receiver.
//
// For example,
//   - for "fmt.Printf(...)" it will return "fmt.Printf"
//   - for "base64.StdEncoding.Decode(...)" it will return "(*encoding/base64.Encoding).Decode"
//   - for "myFunc()" it will return ""
func (v *visitor) fullName(call *ast.CallExpr) string {
	_, fn, ok := v.selectorAndFunc(call)
	if !ok {
		return ""
	}

	// TODO(dh): vendored packages will have /vendor/ in their name,
	// thus not matching vendored standard library packages. If we
	// want to support vendored stdlib packages, we need to implement
	// FullName with our own logic.
	return fn.FullName()
}

// namesForExcludeCheck will return a list of fully-qualified function names
// from a function call that can be used to check against the exclusion list.
//
// If a function call is against a local function (like "myFunc()") then no
// names are returned. If the function is package-qualified (like "fmt.Printf()")
// then just that function's fullName is returned.
//
// Otherwise, we walk through all the potentially embeddded interfaces of the receiver
// the collect a list of type-qualified function names that we will check.
func (v *visitor) namesForExcludeCheck(call *ast.CallExpr) []string {
	sel, fn, ok := v.selectorAndFunc(call)
	if !ok {
		return nil
	}

	name := v.fullName(call)
	if name == "" {
		return nil
	}

	// This will be missing for functions without a receiver (like fmt.Printf),
	// so just fall back to the the function's fullName in that case.
	selection, ok := v.pkg.TypesInfo.Selections[sel]
	if !ok {
		return []string{name}
	}

	// This will return with ok false if the function isn't defined
	// on an interface, so just fall back to the fullName.
	ts, ok := walkThroughEmbeddedInterfaces(selection)
	if !ok {
		return []string{name}
	}

	result := make([]string, len(ts))
	for i, t := range ts {
		// Like in fullName, vendored packages will have /vendor/ in their name,
		// thus not matching vendored standard library packages. If we
		// want to support vendored stdlib packages, we need to implement
		// additional logic here.
		result[i] = fmt.Sprintf("(%s).%s", t.String(), fn.Name())
	}
	return result
}

// isBufferType checks if the expression type is a known in-memory buffer type.
func (v *visitor) argName(expr ast.Expr) string {
	// Special-case literal "os.Stdout" and "os.Stderr"
	if sel, ok := expr.(*ast.SelectorExpr); ok {
		if obj := v.pkg.TypesInfo.ObjectOf(sel.Sel); obj != nil {
			vr, ok := obj.(*types.Var)
			if ok && vr.Pkg() != nil && vr.Pkg().Name() == "os" && (vr.Name() == "Stderr" || vr.Name() == "Stdout") {
				return "os." + vr.Name()
			}
		}
	}
	t := v.pkg.TypesInfo.TypeOf(expr)
	if t == nil {
		return ""
	}
	return t.String()
}

func (v *visitor) excludeCall(call *ast.CallExpr) bool {
	var arg0 string
	if len(call.Args) > 0 {
		arg0 = v.argName(call.Args[0])
	}
	for _, name := range v.namesForExcludeCheck(call) {
		if v.exclude[name] {
			return true
		}
		if arg0 != "" && v.exclude[name+"("+arg0+")"] {
			return true
		}
	}
	return false
}

func (v *visitor) ignoreCall(call *ast.CallExpr) bool {
	if v.excludeCall(call) {
		return true
	}

	// Try to get an identifier.
	// Currently only supports simple expressions:
	//     1. f()
	//     2. x.y.f()
	var id *ast.Ident
	switch exp := call.Fun.(type) {
	case (*ast.Ident):
		id = exp
	case (*ast.SelectorExpr):
		id = exp.Sel
	default:
		// eg: *ast.SliceExpr, *ast.IndexExpr
	}

	if id == nil {
		return false
	}

	// If we got an identifier for the function, see if it is ignored
	if re, ok := v.ignore[""]; ok && re.MatchString(id.Name) {
		return true
	}

	if obj := v.pkg.TypesInfo.Uses[id]; obj != nil {
		if pkg := obj.Pkg(); pkg != nil {
			if re, ok := v.ignore[pkg.Path()]; ok {
				return re.MatchString(id.Name)
			}

			// if current package being considered is vendored, check to see if it should be ignored based
			// on the unvendored path.
			if !v.go111module {
				if nonVendoredPkg, ok := nonVendoredPkgPath(pkg.Path()); ok {
					if re, ok := v.ignore[nonVendoredPkg]; ok {
						return re.MatchString(id.Name)
					}
				}
			}
		}
	}

	return false
}

// nonVendoredPkgPath returns the unvendored version of the provided package path (or returns the provided path if it
// does not represent a vendored path). The second return value is true if the provided package was vendored, false
// otherwise.
func nonVendoredPkgPath(pkgPath string) (string, bool) {
	lastVendorIndex := strings.LastIndex(pkgPath, "/vendor/")
	if lastVendorIndex == -1 {
		return pkgPath, false
	}
	return pkgPath[lastVendorIndex+len("/vendor/"):], true
}

// errorsByArg returns a slice s such that
// len(s) == number of return types of call
// s[i] == true iff return type at position i from left is an error type
func (v *visitor) errorsByArg(call *ast.CallExpr) []bool {
	switch t := v.pkg.TypesInfo.Types[call].Type.(type) {
	case *types.Named:
		// Single return
		return []bool{isErrorType(t)}
	case *types.Pointer:
		// Single return via pointer
		return []bool{isErrorType(t)}
	case *types.Tuple:
		// Multiple returns
		s := make([]bool, t.Len())
		for i := 0; i < t.Len(); i++ {
			switch et := t.At(i).Type().(type) {
			case *types.Named:
				// Single return
				s[i] = isErrorType(et)
			case *types.Pointer:
				// Single return via pointer
				s[i] = isErrorType(et)
			default:
				s[i] = false
			}
		}
		return s
	}
	return []bool{false}
}

func (v *visitor) callReturnsError(call *ast.CallExpr) bool {
	if v.isRecover(call) {
		return true
	}
	for _, isError := range v.errorsByArg(call) {
		if isError {
			return true
		}
	}
	return false
}

// isRecover returns true if the given CallExpr is a call to the built-in recover() function.
func (v *visitor) isRecover(call *ast.CallExpr) bool {
	if fun, ok := call.Fun.(*ast.Ident); ok {
		if _, ok := v.pkg.TypesInfo.Uses[fun].(*types.Builtin); ok {
			return fun.Name == "recover"
		}
	}
	return false
}

func (v *visitor) addErrorAtPosition(position token.Pos, call *ast.CallExpr) {
	pos := v.pkg.Fset.Position(position)
	lines, ok := v.lines[pos.Filename]
	if !ok {
		lines = readfile(pos.Filename)
		v.lines[pos.Filename] = lines
	}

	line := "??"
	if pos.Line-1 < len(lines) {
		line = strings.TrimSpace(lines[pos.Line-1])
	}

	var name string
	if call != nil {
		name = v.fullName(call)
	}

	v.errors = append(v.errors, UncheckedError{pos, line, name})
}

func readfile(filename string) []string {
	var f, err = os.Open(filename)
	if err != nil {
		return nil
	}

	var lines []string
	var scanner = bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines
}

func (v *visitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		v.depth--
		if v.depth == 0 {
			for !v.ss.empty() {
				s := v.ss.pop()
				fmt.Println("\t: ", VarListPrinter(s.Vars).String())
			}
		}
		return v
	}

	fmt.Printf("%T: (%d-%d) %+v \n", node, node.Pos(), node.End(), node)
	v.depth++

	for !v.ss.empty() && !v.ss.in(newScopeFrom(node)) {
		s := v.ss.pop()
		fmt.Println("\t: ", VarListPrinter(s.Vars).String())
	}

	switch stmt := node.(type) {
	case *ast.FuncDecl:
		v.ss.push(newScopeFrom(node))
	case *ast.IfStmt:
		v.ss.push(newScopeFrom(node))
	case *ast.BlockStmt:
		v.ss.push(newScopeFrom(node))
	case *ast.ValueSpec:
		// for cases such: DeclStmt -> GenDecl -> ValueSpec
		for i, n := range stmt.Names {
			nv := Var{
				Node: stmt,
				Index: i,
				Name: n.Name,
			}
			if stmt.Type != nil {
				nv.Type = v.pkg.TypesInfo.Types[stmt.Type]
			}
			if len(stmt.Values) > 0 {
				nv.Written = true
				if nv.Type.Type == nil {
					nv.Type = reduceExprType(v.pkg, stmt.Values[i])
				}
			}
			if nv.Type.Type == nil {
				fmt.Printf("failed to deduce val type for %s\n", nv.Name)
			}
			v.ss.last().declareVar(nv)
		}
	case *ast.ExprStmt:
		if call, ok := stmt.X.(*ast.CallExpr); ok {
			if !v.ignoreCall(call) && v.callReturnsError(call) {
				v.addErrorAtPosition(call.Lparen, call)
			}
		}
	case *ast.GoStmt:
		if !v.ignoreCall(stmt.Call) && v.callReturnsError(stmt.Call) {
			v.addErrorAtPosition(stmt.Call.Lparen, stmt.Call)
		}
	case *ast.DeferStmt:
		if !v.ignoreCall(stmt.Call) && v.callReturnsError(stmt.Call) {
			v.addErrorAtPosition(stmt.Call.Lparen, stmt.Call)
		}
	case *ast.AssignStmt:
		// TODO: introduce new block?
		switch stmt.Tok {
		case token.DEFINE:
			for i, left := range stmt.Lhs {
				ident, ok := left.(*ast.Ident)
				if !ok {
					continue
				}

				nv := Var{
					Node:  stmt,
					Index: i,
					Name:  ident.Name,
					Written: true,
				}
				if len(stmt.Lhs) == len(stmt.Rhs) {
					nv.Type = reduceExprType(v.pkg, stmt.Rhs[i])
				} else /* len(stmt.Rhs) == 1 */ {
					nv.Type = reduceExprType(v.pkg, stmt.Rhs[0])
				}
				if nv.Type.Type == nil {
					fmt.Printf("failed to deduce val type for %s\n", nv.Name)
				}
				v.ss.last().declareVar(nv)
			}
		}

		if len(stmt.Rhs) == 1 {
			// single value on rhs; check against lhs identifiers
			if call, ok := stmt.Rhs[0].(*ast.CallExpr); ok {
				if !v.blank {
					break
				}
				if v.ignoreCall(call) {
					break
				}
				isError := v.errorsByArg(call)
				for i := 0; i < len(stmt.Lhs); i++ {
					if id, ok := stmt.Lhs[i].(*ast.Ident); ok {
						// We shortcut calls to recover() because errorsByArg can't
						// check its return types for errors since it returns interface{}.
						if id.Name == "_" && (v.isRecover(call) || isError[i]) {
							v.addErrorAtPosition(id.NamePos, call)
						}
					}
				}
			} else if assert, ok := stmt.Rhs[0].(*ast.TypeAssertExpr); ok {
				if !v.asserts {
					break
				}
				if assert.Type == nil {
					// type switch
					break
				}
				if len(stmt.Lhs) < 2 {
					// assertion result not read
					v.addErrorAtPosition(stmt.Rhs[0].Pos(), nil)
				} else if id, ok := stmt.Lhs[1].(*ast.Ident); ok && v.blank && id.Name == "_" {
					// assertion result ignored
					v.addErrorAtPosition(id.NamePos, nil)
				}
			}
		} else {
			// multiple value on rhs; in this case a call can't return
			// multiple values. Assume len(stmt.Lhs) == len(stmt.Rhs)
			for i := 0; i < len(stmt.Lhs); i++ {
				if id, ok := stmt.Lhs[i].(*ast.Ident); ok {
					if call, ok := stmt.Rhs[i].(*ast.CallExpr); ok {
						if !v.blank {
							continue
						}
						if v.ignoreCall(call) {
							continue
						}
						if id.Name == "_" && v.callReturnsError(call) {
							v.addErrorAtPosition(id.NamePos, call)
						}
					} else if assert, ok := stmt.Rhs[i].(*ast.TypeAssertExpr); ok {
						if !v.asserts {
							continue
						}
						if assert.Type == nil {
							// Shouldn't happen anyway, no multi assignment in type switches
							continue
						}
						v.addErrorAtPosition(id.NamePos, nil)
					}
				}
			}
		}
	default:
	}
	return v
}

func isErrorType(t types.Type) bool {
	return types.Implements(t, errorType)
}
