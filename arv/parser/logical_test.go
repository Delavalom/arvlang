package parser

import (
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/token"
)

// && binds tighter than ||, and both bind looser than comparisons; each
// prints as the operator it was written with.
func TestParseAndOrPrecedence(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{`a && b`, `(a && b)`},
		{`a || b`, `(a || b)`},
		{`a && b || c`, `((a && b) || c)`},
		{`a || b && c`, `(a || (b && c))`},
		{`a && b && c`, `((a && b) && c)`},
		{`a == b && c < d`, `((a == b) && (c < d))`},
		{`x >= 200 && x <= 299 || x == 0`, `(((x >= 200) && (x <= 299)) || (x == 0))`},
		{`!a && -b < c`, `((!a) && ((-b) < c))`},
		{`a && (b || c)`, `(a && (b || c))`},
		{`f(a && b, c || d)`, `f((a && b), (c || d))`},
		{`r.up && r.ok`, `((r[up]) && (r[ok]))`},
		{`let ok = a || b;`, `let ok = (a || b);`},
	} {
		if got := printed(t, tt.input); got != tt.want {
			t.Errorf("%q printed %q, want %q", tt.input, got, tt.want)
		}
	}
}

// a && b is if (a) { b } else { false }, and a || b is
// if (a) { true } else { b }, placed where a starts; b keeps its place.
func TestAndOrNodeShape(t *testing.T) {
	for _, tt := range []struct {
		input   string
		op      token.TokenType
		yes, no string
	}{
		{input: "\n  x &&\n y", op: token.AND, yes: "y", no: "false"},
		{input: "\n  x ||\n y", op: token.OR, yes: "true", no: "y"},
	} {
		p := New(lexer.New(tt.input))
		program := p.ParseProgram()
		checkParserErrors(t, p)
		expr := program.Statements[0].(*ast.ExpressionStatement).Expression
		ie, ok := expr.(*ast.IfExpression)
		if !ok {
			t.Fatalf("%q: got %T, want an IfExpression", tt.input, expr)
		}
		if ie.Token.Type != tt.op || ie.Condition.String() != "x" ||
			ie.Consequence.String() != tt.yes || ie.Alternative.String() != tt.no {
			t.Fatalf("%q: if (%s) { %s } else { %s } with %s, want if (x) { %s } else { %s } with %s",
				tt.input, ie.Condition, ie.Consequence, ie.Alternative, ie.Token.Type, tt.yes, tt.no, tt.op)
		}
		if line, col := ast.Pos(ie); line != 2 || col != 3 {
			t.Errorf("%q: Pos = %d:%d, want 2:3 (where x starts)", tt.input, line, col)
		}
		right := ie.Consequence
		if tt.op == token.OR {
			right = ie.Alternative
		}
		y := right.Statements[0].(*ast.ExpressionStatement).Expression
		if line, col := ast.Pos(y); line != 3 || col != 2 {
			t.Errorf("%q: y at %d:%d, want 3:2", tt.input, line, col)
		}
	}
}

func TestAndOrSyntaxErrors(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{`a &&`, "no prefix parse function for EOF found (at 1:5)"},
		{`a & b`, "no prefix parse function for ILLEGAL found (at 1:3)"},
		{`a | b`, "no prefix parse function for ILLEGAL found (at 1:3)"},
		{`|| b`, "no prefix parse function for || found (at 1:1)"},
	} {
		errs := parse(tt.input)
		if len(errs) == 0 || errs[0] != tt.want {
			t.Errorf("%q: errors %q, want first %q", tt.input, errs, tt.want)
		}
	}
}
