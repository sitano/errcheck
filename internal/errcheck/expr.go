package errcheck

import (
	"fmt"
	"go/ast"
	"go/types"
	"golang.org/x/tools/go/packages"
)

func reduceExprType(pkg *packages.Package, node ast.Expr, i int) types.TypeAndValue {
	switch stmt := node.(type) {
	case *ast.Ident: return types.TypeAndValue{Type:	pkg.TypesInfo.ObjectOf(stmt).Type()}
	case *ast.CompositeLit: return pkg.TypesInfo.Types[stmt.Type]
	case *ast.BasicLit: return types.TypeAndValue{Type:  pkg.TypesInfo.TypeOf(stmt)}
	case *ast.ParenExpr: return reduceExprType(pkg, stmt.X, i)
	case *ast.SelectorExpr: return types.TypeAndValue{Type:  pkg.TypesInfo.TypeOf(stmt)}
	case *ast.CallExpr:
		tav := pkg.TypesInfo.Types[stmt]
		switch t := tav.Type.(type) {
		case *types.Named: return types.TypeAndValue{Type: t.Underlying()}
		case *types.Pointer: return types.TypeAndValue{Type: t}
		case *types.Tuple: return types.TypeAndValue{Type: t.At(i).Type()}
		default: return tav
		}
	case *ast.UnaryExpr: return reduceExprType(pkg, stmt.X, i)
	case *ast.BinaryExpr:
		t1 := reduceExprType(pkg, stmt.X, i)
		if t1.Type != nil {
			return t1
		}
		t2 := reduceExprType(pkg, stmt.Y, i)
		return t2
	default:
		fmt.Printf("unexpected expression type %T: %+v\n", stmt, stmt)
	}
	return types.TypeAndValue{}
}

