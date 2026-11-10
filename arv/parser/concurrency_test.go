package parser

import (
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
)

func TestParseConcurrency(t *testing.T) {
	tests := []struct{ input, want string }{
		{`spawn f(1)`, `spawn f(1)`},
		{`spawn f(x) with [net("h")]`, `spawn f(x) with [net(h)]`},
		{`await t`, `await t`},
		{`[await a, await b]`, `[await a, await b]`},
		{`scope { let t = spawn f(); await t }`, `scope { let t = spawn f();await t }`},
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

func TestSpawnRequiresACall(t *testing.T) {
	p := New(lexer.New(`spawn 5`))
	p.ParseProgram()
	if len(p.Errors()) == 0 {
		t.Fatal("spawn 5 parsed without error")
	}
}

func TestSpawnNodeShape(t *testing.T) {
	p := New(lexer.New(`spawn f(1, 2) with caps`))
	program := p.ParseProgram()
	se, ok := program.Statements[0].(*ast.ExpressionStatement).Expression.(*ast.SpawnExpression)
	if !ok {
		t.Fatalf("not a SpawnExpression: %T", program.Statements[0])
	}
	if len(se.Call.Arguments) != 2 || se.Caps.String() != "caps" {
		t.Fatalf("bad node: %s", se.String())
	}
}
