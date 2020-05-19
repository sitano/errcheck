package errcheck

import (
	"go/ast"
	"go/types"
)

type Var struct {
	Node ast.Node
	Index int

	Name string
	Type types.TypeAndValue

	Written bool
	Escaped bool
}
