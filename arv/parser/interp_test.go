package parser

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
)

// An interpolated string is the concatenation of its pieces and a $str
// call per ${...}; a string without ${ is the literal it always was.
func TestParseInterpolation(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{`"a ${x} b"`, `((a  + $str(x)) +  b)`},
		{`"${x}"`, `$str(x)`},
		{`"${a}${b}"`, `($str(a) + $str(b))`},
		{`"${r.up} ${n + 1}"`, `(($str((r[up])) +  ) + $str((n + 1)))`},
		{`"${ if (ok) { 1 } else { 2 } }"`, `$str(ifok 1else2)`},
		{`"${ {1: 2}[1] }"`, `$str(({1:2}[1]))`},
		{`"${x}" + "${y}"`, `($str(x) + $str(y))`},
		{`"no interpolation: $ { } $x {x}"`, `no interpolation: $ { } $x {x}`},
	} {
		if got := printed(t, tt.input); got != tt.want {
			t.Errorf("%q printed %q, want %q", tt.input, got, tt.want)
		}
	}
}

// \${ is a literal ${. A string whose only ${ are escaped stays one
// string literal, with the escape removed from its value.
func TestInterpolationEscape(t *testing.T) {
	for _, tt := range []struct{ input, value string }{
		{`"\${x}"`, "${x}"},
		{`"cost: $5 \${a} \\${b}"`, `cost: $5 ${a} \${b}`},
	} {
		p := New(lexer.New(tt.input))
		program := p.ParseProgram()
		checkParserErrors(t, p)
		sl, ok := program.Statements[0].(*ast.ExpressionStatement).Expression.(*ast.StringLiteral)
		if !ok || sl.Value != tt.value {
			t.Errorf("%q = %#v, want the string literal %q", tt.input, program.Statements[0], tt.value)
		}
	}
	if got := printed(t, `"\${a} ${b}"`); got != `(${a}  + $str(b))` {
		t.Errorf("printed %q", got)
	}
}

// The expression's tokens carry their real positions, across lines; the
// $str call and the piece after it sit where they start in the source.
func TestInterpolationPositions(t *testing.T) {
	p := New(lexer.New("let s =\n  \"one\n  ${ x } two\";"))
	program := p.ParseProgram()
	checkParserErrors(t, p)
	cat := program.Statements[0].(*ast.LetStatement).Value.(*ast.InfixExpression)
	inner := cat.Left.(*ast.InfixExpression)
	first := inner.Left.(*ast.StringLiteral)
	call := inner.Right.(*ast.CallExpression)
	x := call.Arguments[0].(*ast.Identifier)
	last := cat.Right.(*ast.StringLiteral)
	for _, c := range []struct {
		what      string
		node      ast.Node
		line, col int
	}{
		{"the first piece (at the quote)", first, 2, 3},
		{"the call (at the $)", call, 3, 3},
		{"x", x, 3, 6},
		{"the last piece", last, 3, 9},
	} {
		if line, col := ast.Pos(c.node); line != c.line || col != c.col {
			t.Errorf("%s at %d:%d, want %d:%d", c.what, line, col, c.line, c.col)
		}
	}
	if first.Value != "one\n  " || last.Value != " two" || x.Value != "x" {
		t.Errorf("pieces %q, %q around %s", first.Value, last.Value, x.Value)
	}
	if fn := call.Function.(*ast.Identifier); fn.Value != ast.StrBuiltin {
		t.Errorf("calls %q, want %q", fn.Value, ast.StrBuiltin)
	}
}

// Errors inside ${...} point into the string.
func TestInterpolationSyntaxErrors(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{`"a ${x"`, "unterminated ${ in string (at 1:4)"},
		{`"a ${}"`, "empty ${} in string (at 1:4)"},
		{`"a ${  }"`, "empty ${} in string (at 1:4)"},
		{`"a ${x; y}"`, "expected } to end ${, got ; instead (at 1:7)"},
		{`"a ${x y}"`, "expected } to end ${, got IDENT instead (at 1:8)"},
		{`"${1 +}"`, "no prefix parse function for EOF found (at 1:7)"},
		{"\"one\n  ${ (x }\"", "expected next token to be ), got EOF instead (at 2:9)"},
		// a quote ends the string, so the ${ before it is unterminated
		{`"${f("x")}"`, "unterminated ${ in string (at 1:2)"},
	} {
		errs := parse(tt.input)
		if len(errs) == 0 || errs[0] != tt.want {
			t.Errorf("%q: errors %q, want first %q", tt.input, errs, tt.want)
		}
	}
	// After a bad string, parsing goes on and finds the next error too.
	if errs := parse("\"${}\";\nlet = 1"); len(errs) < 2 || !strings.Contains(errs[1], "(at 2:5)") {
		t.Errorf("errors %q, want the empty ${} and then the let", errs)
	}
}
