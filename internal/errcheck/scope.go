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
	Vars  []Var

	Choice int
	Recover bool
}

func newScopeFrom(n ast.Node) scope {
	return scope{
		Node:  n,
		Start: n.Pos(),
		End:   n.End(),
	}
}

func (s *scope) declareVar(v Var) {
	s.Vars = append(s.Vars, v)
}

func (s *scope) findVar(name string) *Var {
	for i := len(s.Vars) - 1; i >= 0; i-- {
		if s.Vars[i].Name == name {
			return &s.Vars[i]
		}
	}
	return nil
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

func (s scope) clone() scope {
	t := s
	t.Vars = make([]Var, 0, len(s.Vars))
	for _, v := range s.Vars {
		t.Vars = append(t.Vars, v.clone())
	}
	return t
}
