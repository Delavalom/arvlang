package parser

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
)

// r.f parses to the same tree as r["f"], so it prints the same.
func TestParseFieldIsIndex(t *testing.T) {
	for _, tt := range [][2]string{
		{`r.up`, `r["up"]`},
		{`r.in.deep[0]`, `r["in"]["deep"][0]`},
		{`f().x + 1`, `f()["x"] + 1`},
		{`-r.n`, `-r["n"]`},
		{`r.a(1).b`, `r["a"](1)["b"]`},
		{`r.if.fn.match`, `r["if"]["fn"]["match"]`},
		{"r\n  .up", `r["up"]`},
	} {
		if got, want := printed(t, tt[0]), printed(t, tt[1]); got != want {
			t.Errorf("%q printed %q, want %q as %q does", tt[0], got, want, tt[1])
		}
	}
}

// The key is a string literal placed at the name, the node at the dot.
func TestFieldNodeShape(t *testing.T) {
	p := New(lexer.New("\n  rec.status"))
	program := p.ParseProgram()
	checkParserErrors(t, p)
	ie, ok := program.Statements[0].(*ast.ExpressionStatement).Expression.(*ast.IndexExpression)
	if !ok {
		t.Fatalf("got %T, want an IndexExpression", program.Statements[0].(*ast.ExpressionStatement).Expression)
	}
	key, ok := ie.Index.(*ast.StringLiteral)
	if !ok || key.Value != "status" || key.Token.Line != 2 || key.Token.Col != 7 {
		t.Fatalf("key = %#v, want the string status at 2:7", ie.Index)
	}
	if line, col := ast.Pos(ie); line != 2 || col != 3 {
		t.Fatalf("Pos = %d:%d, want 2:3 (where rec starts)", line, col)
	}
}

// After a dot only a name may follow; the error says where it is missing.
func TestFieldSyntaxErrors(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{`r.`, "expected a field name after ., got EOF instead (at 1:3)"},
		{`r.5`, "expected a field name after ., got INT instead (at 1:3)"},
		{`r."up"`, "expected a field name after ., got STRING instead (at 1:3)"},
		{`1.5`, "expected a field name after ., got INT instead (at 1:3)"},
		{`r..up`, "expected a field name after ., got . instead (at 1:3)"},
	} {
		errs := parse(tt.input)
		if len(errs) == 0 || errs[0] != tt.want {
			t.Errorf("%q: errors %q, want first %q", tt.input, errs, tt.want)
		}
	}
	if errs := parse(". up"); len(errs) == 0 || !strings.Contains(errs[0], "(at 1:1)") {
		t.Errorf("a leading dot: errors %q", errs)
	}
}
