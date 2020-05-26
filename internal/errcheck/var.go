package errcheck

import (
	"bytes"
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

func (v Var) clone() Var {
	return v
}

type VarListPrinter []Var

func (v VarListPrinter) String() string {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for _, t := range v {
		if buf.Len() > 1 {
			buf.WriteByte(',')
		}
		buf.WriteString(t.Name + ":" + t.Type.Type.String())
	}
	buf.WriteByte(']')
	return buf.String()
}
