package evaluator

import (
	"sync/atomic"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/value"
)

// Phase 8.4 runs a program as compiled code: each AST node becomes a Go
// closure once, before the program runs, instead of being dispatched by
// eval's type switch every time it is evaluated. The tree-walker (eval)
// stays as the reference: both engines must give every program the same
// value, error, position, output, steps and bytes, and the tests run on
// both (SetTreeWalk, the arv_treewalk build tag).

// treeWalk, when true, makes Run evaluate with the tree-walker.
var treeWalk atomic.Bool

func init() { treeWalk.Store(defaultTreeWalk) }

// SetTreeWalk makes every Run that starts after it use the tree-walker
// (on) or compiled code (off), and returns the previous setting. It is
// for tests, which run programs on both engines and compare them; the
// default is compiled code, or the tree-walker in a build with the
// arv_treewalk tag.
func SetTreeWalk(on bool) bool { return treeWalk.Swap(on) }

// code is one AST node compiled. run does what eval's case for the node
// does; exec wraps it in what Eval does around every node. walks is true
// when run is the tree-walker's case itself (walked).
type code struct {
	node  ast.Node
	run   func(in *Interpreter, env *value.Environment) value.Object
	walks bool
}

// walked is node's code that runs the tree-walker's case for it: for the
// node types compile has no case for. Their children run on the
// tree-walker too, which means the same.
func walked(node ast.Node) *code {
	return &code{node: node, walks: true, run: func(in *Interpreter, env *value.Environment) value.Object {
		return in.evalNode(node, env)
	}}
}

// exec evaluates compiled code as Eval evaluates its node.
func (in *Interpreter) exec(c *code, env *value.Environment) value.Object {
	return in.enter(c.node, c, env)
}

// start evaluates node, the whole program, on the engine treeWalk picks.
func (in *Interpreter) start(node ast.Node, env *value.Environment) value.Object {
	if treeWalk.Load() {
		return in.Eval(node, env)
	}
	return in.exec(compile(node), env)
}
