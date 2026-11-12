package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
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
