package parser

import (
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
)

// printed parses input, fails on any parse error, and returns the
// program's printed form.
func printed(t *testing.T, input string) string {
	t.Helper()
	p := New(lexer.New(input))
	program := p.ParseProgram()
	checkParserErrors(t, p)
	return program.String()
}

// <= and >= are infix operators with the precedence of < and >.
func TestParseLessOrEqualAndGreaterOrEqual(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{`a <= b`, `(a <= b)`},
		{`a >= b`, `(a >= b)`},
		{`1 + 2 <= 3 == true`, `(((1 + 2) <= 3) == true)`},
		{`a < b >= c`, `((a < b) >= c)`},
		{`-a >= b * 2`, `((-a) >= (b * 2))`},
	} {
		if got := printed(t, tt.input); got != tt.want {
			t.Errorf("%q printed %q, want %q", tt.input, got, tt.want)
		}
	}
	p := New(lexer.New(`x <= 5`))
	program := p.ParseProgram()
	ie, ok := program.Statements[0].(*ast.ExpressionStatement).Expression.(*ast.InfixExpression)
	if !ok || ie.Operator != "<=" {
		t.Fatalf("got %T %s, want an infix <=", program.Statements[0], program)
	}
}
