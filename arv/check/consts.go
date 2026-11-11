package check

import "github.com/delavalom/arvlang/arv/ast"

// Constant strings. A name that the whole program binds exactly once, by
// a let whose value is a known string, can only ever evaluate to that
// string: no other let, parameter, for variable or match arm binds it, so
// no scope can shadow it with another value. (Before its let has run the
// name is not found, or names a builtin, and either way the call fails
// before admitting anything.) A + of two known strings is their
// concatenation, as the evaluator computes it. Anything else is not known.

const (
	maxConstLen = maxURLLen // longer than any scope the runtime accepts
	maxFolded   = 1 << 20   // bytes folded in all, so doubling lets stay cheap
	maxHops     = 100       // names followed to fold one argument
)

type folded struct {
	s  string
	ok bool
}

type folder struct {
	lets  map[string]ast.Expression // names bound once in the program, by a let
	memo  map[string]folded
	busy  map[string]bool
	spent int
}

// newFolder counts every binding in prog and keeps the lets of names
// bound exactly once.
func newFolder(prog *ast.Program) (*folder, error) {
	count := map[string]int{}
	lets := map[string]ast.Expression{}
	bind := func(id *ast.Identifier) {
		if id != nil {
			count[id.Value]++
		}
	}
	err := walk(prog, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.LetStatement:
			bind(n.Name)
			if n.Name != nil {
				lets[n.Name.Value] = n.Value
			}
		case *ast.FunctionLiteral:
			for _, p := range n.Parameters {
				bind(p)
			}
		case *ast.ForExpression:
			bind(n.Var)
		case *ast.MatchExpression:
			bind(n.OkName)
			bind(n.ErrName)
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	for name := range lets {
		if count[name] != 1 {
			delete(lets, name)
		}
	}
	return &folder{lets: lets, memo: map[string]folded{}, busy: map[string]bool{}}, nil
}

// str is e's value if it is a string known before running.
func (f *folder) str(e ast.Expression, hops int) (string, bool) {
	switch e := e.(type) {
	case *ast.StringLiteral:
		return e.Value, true
	case *ast.Identifier:
		return f.name(e.Value, hops)
	case *ast.InfixExpression:
		return f.concat(e, hops)
	}
	return "", false
}

func (f *folder) name(name string, hops int) (string, bool) {
	if r, ok := f.memo[name]; ok {
		return r.s, r.ok
	}
	val, ok := f.lets[name]
	if !ok || f.busy[name] || hops >= maxHops {
		return "", false // a cycle (let a = a + "x") is never bound
	}
	f.busy[name] = true
	s, ok := f.str(val, hops+1)
	delete(f.busy, name)
	f.memo[name] = folded{s, ok}
	return s, ok
}

// concat folds a chain of +, walking its left spine without recursing,
// as the evaluator does.
func (f *folder) concat(e *ast.InfixExpression, hops int) (string, bool) {
	spine := []*ast.InfixExpression{e}
	for {
		left, ok := spine[len(spine)-1].Left.(*ast.InfixExpression)
		if !ok {
			break
		}
		spine = append(spine, left)
	}
	acc, ok := f.str(spine[len(spine)-1].Left, hops)
	if !ok {
		return "", false
	}
	for i := len(spine) - 1; i >= 0; i-- {
		if spine[i].Operator != "+" {
			return "", false
		}
		r, ok := f.str(spine[i].Right, hops)
		if !ok {
			return "", false
		}
		n := len(acc) + len(r)
		if n > maxConstLen || f.spent+n > maxFolded {
			return "", false
		}
		f.spent += n
		acc += r
	}
	return acc, true
}
