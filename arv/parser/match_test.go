package parser

import (
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
)

func parseMatch(t *testing.T, input string) *ast.MatchExpression {
	t.Helper()
	p := New(lexer.New(input))
	program := p.ParseProgram()
	checkParserErrors(t, p)
	stmt, ok := program.Statements[0].(*ast.ExpressionStatement)
	if !ok {
		t.Fatalf("statement is %T, want *ast.ExpressionStatement", program.Statements[0])
	}
	m, ok := stmt.Expression.(*ast.MatchExpression)
	if !ok {
		t.Fatalf("expression is %T, want *ast.MatchExpression", stmt.Expression)
	}
	return m
}

func TestParseMatch(t *testing.T) {
	want := `match await t { ok(v) => (v + 1), err(e) => (e[message]) }`
	for _, input := range []string{
		`match await t { ok(v) => v + 1, err(e) => e["message"] }`,
		`match await t { err(e) => e["message"], ok(v) => v + 1 }`,
		"match await t {\n  ok(v) => v + 1,\n  err(e) => e[\"message\"],\n}",
	} {
		m := parseMatch(t, input)
		if m.OkName.Value != "v" || m.ErrName.Value != "e" {
			t.Errorf("%q: names %s, %s", input, m.OkName, m.ErrName)
		}
		if got := m.String(); got != want {
			t.Errorf("%q printed as %q, want %q", input, got, want)
		}
		if line, col := ast.Pos(m); line != 1 || col != 1 {
			t.Errorf("%q: Pos = %d:%d, want 1:1", input, line, col)
		}
	}
	// An arm body can be any expression, a match among them; the subject
	// can be a call.
	m := parseMatch(t, `match f(1) { ok(v) => match v { ok(w) => w, err(x) => 0 }, err(e) => if (e["kind"] == "user") { 1 } else { 2 } }`)
	if _, ok := m.Ok.(*ast.MatchExpression); !ok {
		t.Errorf("ok arm is %T, want a nested match", m.Ok)
	}
}

// ok and err are names only inside match.
func TestOkAndErrAreNotReserved(t *testing.T) {
	p := New(lexer.New(`let ok = 1; let err = fn(e) { e }; err(ok)`))
	p.ParseProgram()
	checkParserErrors(t, p)
}

// Both arms are required, once each, so error handling can't be half
// written; anything else in an arm is an error at its position.
func TestMatchParseErrors(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{`match x { ok(v) => v }`, "match needs an ok arm and an err arm; err is missing (at 1:1)"},
		{`match x { err(e) => 0 }`, "match needs an ok arm and an err arm; ok is missing (at 1:1)"},
		{"1;\n  match x { }", "match needs an ok arm and an err arm; ok is missing (at 2:3)"},
		{`match x { ok(v) => v, ok(w) => w, err(e) => 0 }`, "duplicate match arm ok (at 1:23)"},
		{`match x { some(v) => v, err(e) => 0 }`, "match arm must be ok(name) or err(name), got some (at 1:11)"},
		{`match x { ok(v) v, err(e) => 0 }`, "expected next token to be =>, got IDENT instead (at 1:17)"},
		{`match x { ok v => v, err(e) => 0 }`, "expected next token to be (, got IDENT instead (at 1:14)"},
		{`match x { ok(1) => v, err(e) => 0 }`, "expected next token to be IDENT, got INT instead (at 1:14)"},
		{`match x { ok(v) => v err(e) => 0 }`, "expected next token to be }, got IDENT instead (at 1:22)"},
		{`match x ok(v) => v`, "expected next token to be {, got IDENT instead (at 1:9)"},
		{`match x { ok(v) => v, err(e) => }`, "no prefix parse function for } found (at 1:33)"},
	} {
		p := New(lexer.New(tt.input))
		p.ParseProgram()
		errs := p.Errors()
		if len(errs) == 0 || errs[0] != tt.want {
			t.Errorf("%q: errors %q, want first %q", tt.input, errs, tt.want)
		}
	}
}
