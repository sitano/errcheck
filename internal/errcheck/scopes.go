package errcheck

import "fmt"

type scopes []scope

func (ss *scopes) push(s scope) {
	fmt.Printf("+   (%d-%d) %+v func decl\n", s.Node.Pos(), s.Node.End(), s.Node)
	*ss = append(*ss, s)
}

func (ss *scopes) pop() *scope {
	s := ss.last()
	*ss = (*ss)[:len(*ss)-1]
	fmt.Printf("-   (%d-%d) %+v pop\n", s.Node.Pos(), s.Node.End(), s.Node)
	return s
}

func (ss scopes) last() *scope {
	return &(ss[len(ss)-1])
}

func (ss scopes) in(s scope) bool {
	p := ss.last()
	return s.Start >= p.Start && s.End <= p.End
}

func (ss scopes) empty() bool {
	return len(ss) == 0
}
