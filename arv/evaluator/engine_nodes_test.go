package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/value"
)

// parsed is src's program; a parse error fails the test.
func parsed(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	return program
}

// lastNode is the program itself (whole), its last statement (stmt), or
// that statement's expression.
func lastNode(t *testing.T, src, part string) ast.Node {
	t.Helper()
	program := parsed(t, src)
	stmt := program.Statements[len(program.Statements)-1]
	switch part {
	case "whole":
		return program
	case "stmt":
		return stmt
	}
	return stmt.(*ast.ExpressionStatement).Expression
}

// compiledNodes are nodes compile turns into their own code: the node
// types the speed programs spend their time in (Phase 8.4).
var compiledNodes = []struct{ src, part string }{
	{`1; 2`, "whole"}, {`1`, "stmt"}, {`1`, "expr"}, {`true`, "expr"}, {`"s"`, "expr"},
	{`return 1`, "stmt"}, {`-1`, "expr"}, {`if (true) { 1 }`, "expr"},
	{`let x = 1`, "stmt"}, {`x`, "expr"}, {`true && false`, "expr"},
	{`1 + 2 + 3`, "expr"}, {`fn(x) { x }`, "expr"}, {`f(1)`, "expr"}, {`[1, 2]`, "expr"},
	{`a[0]`, "expr"}, {`r.up`, "expr"}, {`{"a": 1}`, "expr"}, {`"n=${1}"`, "expr"},
	// p84.3 to p84.5 add entries here.
}

// walkedNodes stay on the tree-walker's case: they run once per task or
// per wait, not per step of a loop.
var walkedNodes = []struct{ src, part string }{
	{`spawn f()`, "expr"}, {`await t`, "expr"},
}

func TestNodesAreCompiled(t *testing.T) {
	for _, n := range compiledNodes {
		if c := compile(lastNode(t, n.src, n.part)); c.walks {
			t.Errorf("%s (%s) runs on the tree-walker", n.src, n.part)
		}
	}
	for _, n := range walkedNodes {
		if c := compile(lastNode(t, n.src, n.part)); !c.walks {
			t.Errorf("%s (%s) was compiled", n.src, n.part)
		}
	}
}

// A string literal is a new value each time it is evaluated, on both
// engines: the codec numbers long strings by identity (value.Encode), so
// sharing one would change what the effect log holds.
func TestStringLiteralsAreNewEachTime(t *testing.T) {
	for _, walk := range []bool{true, false} {
		var got value.Object
		onEngine(walk, func() {
			got, _ = runWith(t, `let f = fn() { "a long string, longer than sixteen" }; [f(), f()]`)
		})
		arr, ok := got.(*value.Array)
		if !ok || len(arr.Elements) != 2 || arr.Elements[0] == arr.Elements[1] {
			t.Errorf("walk=%v: got %s, two distinct strings wanted", walk, got.Inspect())
		}
	}
}

// A function made by compiled code carries its compiled body; one made by
// the tree-walker does not, and is called on the tree-walker.
func TestFunctionsCarryTheirEngine(t *testing.T) {
	for _, walk := range []bool{true, false} {
		var got value.Object
		onEngine(walk, func() { got, _ = runWith(t, `fn(x) { x }`) })
		fn, ok := got.(*value.Function)
		if !ok {
			t.Fatalf("got %s", got.Inspect())
		}
		if _, compiled := fn.Code.(*code); compiled == walk {
			t.Errorf("walk=%v: compiled body %v", walk, compiled)
		}
	}
}
