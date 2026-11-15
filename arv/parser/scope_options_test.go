package parser

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
)

func TestParseScopeOptions(t *testing.T) {
	tests := []struct{ input, want string }{
		{`scope (timeout_ms: 100) { 1 }`, `scope (timeout_ms: 100) { 1 }`},
		{`scope (tasks: 2, steps: n * 1000) { f() }`, `scope (tasks: 2, steps: (n * 1000)) { f() }`},
		{`scope () { 1 }`, `scope () { 1 }`},
		{`scope { 1 }`, `scope { 1 }`},
		// a block that holds a hash literal is still a block
		{`scope { {"a": 1} }`, `scope { {a:1} }`},
		{`let s = scope (tasks: 20, timeout_ms: 5000) { await for u in us { spawn f(u) } };`,
			`let s = scope (tasks: 20, timeout_ms: 5000) { await for u in us { spawn f(u) } };`},
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

func TestScopeOptionNodes(t *testing.T) {
	p := New(lexer.New("scope (\n  tasks: 2,\n  steps: 10) { 1 }"))
	program := p.ParseProgram()
	sc := program.Statements[0].(*ast.ExpressionStatement).Expression.(*ast.ScopeExpression)
	if len(sc.Options) != 2 {
		t.Fatalf("got %d options, want 2", len(sc.Options))
	}
	o := sc.Options[1]
	if o.Name != "steps" || o.Value.String() != "10" || o.Token.Line != 3 || o.Token.Col != 3 {
		t.Fatalf("option = %s: %s at %d:%d, want steps: 10 at 3:3", o.Name, o.Value, o.Token.Line, o.Token.Col)
	}
}

func TestScopeOptionSyntaxErrors(t *testing.T) {
	for _, input := range []string{
		`scope (tasks 2) { 1 }`,
		`scope (1: 2) { 1 }`,
		`scope (tasks: 2 { 1 }`,
		`scope (tasks: 2,) { 1 }`,
		`scope (tasks: ) { 1 }`,
		`scope (tasks: 2) 1`,
	} {
		p := New(lexer.New(input))
		p.ParseProgram()
		if len(p.Errors()) == 0 || !strings.Contains(p.Errors()[0], "(at 1:") {
			t.Errorf("%q: errors %q, want a positioned parse error", input, p.Errors())
		}
	}
}
