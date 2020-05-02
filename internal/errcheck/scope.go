package errcheck

import (
	"go/ast"
	"go/token"
	"reflect"
)

type scope struct {
	Node  ast.Node
	Start token.Pos
	End   token.Pos
}

func newScopeFrom(n ast.Node) scope {
	return scope{
		Node:  n,
		Start: n.Pos(),
		End:   n.End(),
	}
}

func (s scope) empty() bool {
	return true
}

func (s scope) hasBodyBlock1(b *ast.BlockStmt) bool {
	v := reflect.ValueOf(s.Node)

	if v.Type().Kind() != reflect.Ptr {
		return false
	}

	elem := v.Elem()
	if elem.Type().Kind() != reflect.Struct {
		return false
	}

	dv := elem.FieldByName("Body")
	if !dv.IsValid() {
		return false
	}

	ptr, ok := dv.Interface().(*ast.BlockStmt)
	if !ok {
		return false
	}

	return ptr == b
}

func (s scope) hasElseStmt1(b ast.Node) bool {
	v := reflect.ValueOf(s.Node)

	if v.Type().Kind() != reflect.Ptr {
		return false
	}

	elem := v.Elem()
	if elem.Type().Kind() != reflect.Struct {
		return false
	}

	dv := elem.FieldByName("Else")
	if !dv.IsValid() {
		return false
	}

	ptr, ok := dv.Interface().(ast.Node)
	if !ok {
		return false
	}

	return ptr == b
}
