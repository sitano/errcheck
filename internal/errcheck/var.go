package errcheck

import (
	"go/ast"
	"go/types"
)

type Var struct {
	Name string
	Type types.TypeAndValue

	Expr ast.Expr

	Node ast.Node
	Index int

	Written bool
	Escaped bool
}
