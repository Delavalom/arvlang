package ast_test

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
)

func parsed(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("%s: %v", src, p.Errors())
	}
	return prog
}

// Size counts each node once, the program included, and not a missing
// else; the parser stores it as Nodes (A7).
func TestSizeCountsNodes(t *testing.T) {
	for _, tt := range []struct {
		src  string
		want int
	}{
		{``, 1},                         // program
		{`1`, 3},                        // program, statement, 1
		{`let x = 1 + 2;`, 5},           // program, let, +, 1, 2
		{`if (a) { b }`, 7},             // program, statement, if, a, block, statement, b
		{`if (a) { b } else { c }`, 10}, // and the else's block, statement, c
		{`f(1, [2, 3])`, 8},             // program, statement, call, f, 1, array, 2, 3
		{`{"k": 1}`, 5},                 // program, statement, hash, "k", 1
		{`fn(x, y) { x }`, 6},           // program, statement, fn, block, statement, x: parameters are names
		{`await spawn f() with [x]`, 8}, // program, statement, await, spawn, call, f, array, x
	} {
		prog := parsed(t, tt.src)
		if got := ast.Size(prog); got != tt.want || prog.Nodes != got {
			t.Errorf("%q: Size %d, Nodes %d, want %d", tt.src, got, prog.Nodes, tt.want)
		}
	}
	if ast.Size(nil) != 0 || ast.Size((*ast.IfExpression)(nil)) != 0 {
		t.Error("a missing node counts")
	}
	deep := parsed(t, "1"+strings.Repeat(" + 1", 100000))
	if got := ast.Size(deep); got != 2+200001 {
		t.Errorf("a 100001-term chain: Size %d, want %d", got, 2+200001)
	}
}

// Sugar is parsed as the nodes of its expansion, so it has the expansion's
// size: the code charge keeps "sugar costs what its expansion costs".
func TestSugarHasTheSizeOfItsExpansion(t *testing.T) {
	for _, tt := range [][2]string{
		{`h.k.j`, `h["k"]["j"]`},
		{`a && b`, `if (a) { b } else { false }`},
		{`a || b`, `if (a) { true } else { b }`},
		{`"x ${y} z"`, `"x " + str(y) + " z"`},
		{`"${y}"`, `str(y)`},
	} {
		if a, b := ast.Size(parsed(t, tt[0])), ast.Size(parsed(t, tt[1])); a != b {
			t.Errorf("%s: %d nodes, %s: %d", tt[0], a, tt[1], b)
		}
	}
}
