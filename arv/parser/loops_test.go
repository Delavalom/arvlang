package parser

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
)

func TestParseFor(t *testing.T) {
	tests := []struct{ input, want string }{
		{`for x in xs { x }`, `for x in xs { x }`},
		{`for u in urls { spawn check(u) with [net("h")] }`, `for u in urls { spawn check(u) with [net(h)] }`},
		{`await for u in us { spawn f(u) }`, `await for u in us { spawn f(u) }`},
		{`let ys = for x in [1, 2] { x * 2 };`, `let ys = for x in [1, 2] { (x * 2) };`},
		{`for x in f(1) { }`, `for x in f(1) {  }`},
	}
	for _, tt := range tests {
		p := New(lexer.New(tt.input))
		program := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("%q: parse errors %v", tt.input, p.Errors())
		}
		if got := program.String(); got != tt.want {
			t.Errorf("%q: String() = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestForNodeShape(t *testing.T) {
	p := New(lexer.New("\n  for item in items { puts(item) }"))
	program := p.ParseProgram()
	fe, ok := program.Statements[0].(*ast.ExpressionStatement).Expression.(*ast.ForExpression)
	if !ok {
		t.Fatalf("not a ForExpression: %T", program.Statements[0])
	}
	if fe.Var.Value != "item" || fe.Iterable.String() != "items" || len(fe.Body.Statements) != 1 {
		t.Fatalf("bad node: %s", fe.String())
	}
	if line, col := ast.Pos(fe); line != 2 || col != 3 {
		t.Fatalf("Pos = %d:%d, want 2:3", line, col)
	}
}

func TestForSyntaxErrors(t *testing.T) {
	for _, input := range []string{
		`for in xs { x }`,
		`for x xs { x }`,
		`for x in { x }`,
		`for x in xs x`,
		`for 1 in xs { x }`,
	} {
		p := New(lexer.New(input))
		p.ParseProgram()
		if len(p.Errors()) == 0 || !strings.Contains(p.Errors()[0], "(at 1:") {
			t.Errorf("%q: errors %q, want a positioned parse error", input, p.Errors())
		}
	}
}
