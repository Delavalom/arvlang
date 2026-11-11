package lexer

import (
	"testing"

	"github.com/delavalom/arvlang/arv/token"
)

// Phase 5E operators: <= >= && || and the field dot. A lone & or | stays
// ILLEGAL, and < followed by a space and = stays two tokens.
func TestErgonomicTokens(t *testing.T) {
	input := "a <= b >= c && d || r.up < = & | x.y.z 1.5"
	want := []struct {
		typ token.TokenType
		lit string
		col int
	}{
		{token.IDENT, "a", 1}, {token.LT_EQ, "<=", 3}, {token.IDENT, "b", 6},
		{token.GT_EQ, ">=", 8}, {token.IDENT, "c", 11}, {token.AND, "&&", 13},
		{token.IDENT, "d", 16}, {token.OR, "||", 18}, {token.IDENT, "r", 21},
		{token.DOT, ".", 22}, {token.IDENT, "up", 23}, {token.LT, "<", 26},
		{token.ASSIGN, "=", 28}, {token.ILLEGAL, "&", 30}, {token.ILLEGAL, "|", 32},
		{token.IDENT, "x", 34}, {token.DOT, ".", 35}, {token.IDENT, "y", 36},
		{token.DOT, ".", 37}, {token.IDENT, "z", 38},
		{token.INT, "1", 40}, {token.DOT, ".", 41}, {token.INT, "5", 42},
		{token.EOF, "", 43},
	}
	l := New(input)
	for i, w := range want {
		tok := l.NextToken()
		if tok.Type != w.typ || tok.Literal != w.lit || tok.Line != 1 || tok.Col != w.col {
			t.Fatalf("token %d = %s %q at %d:%d, want %s %q at 1:%d",
				i, tok.Type, tok.Literal, tok.Line, tok.Col, w.typ, w.lit, w.col)
		}
	}
}

// Strings are lexed as before: a ${ inside one is the parser's business.
func TestStringWithInterpolationIsOneToken(t *testing.T) {
	l := New(`"a ${x.y} $${z}"`)
	if tok := l.NextToken(); tok.Type != token.STRING || tok.Literal != "a ${x.y} $${z}" {
		t.Fatalf("got %s %q", tok.Type, tok.Literal)
	}
}

// NewAt starts counting at the given position, so a sub-lexer's tokens
// point into the larger source; a newline still starts column 1.
func TestNewAtOffsetsPositions(t *testing.T) {
	l := NewAt("x +\n  y", 3, 10)
	want := []struct {
		lit       string
		line, col int
	}{{"x", 3, 10}, {"+", 3, 12}, {"y", 4, 3}, {"", 4, 4}}
	for i, w := range want {
		tok := l.NextToken()
		if tok.Literal != w.lit || tok.Line != w.line || tok.Col != w.col {
			t.Fatalf("token %d = %q at %d:%d, want %q at %d:%d", i, tok.Literal, tok.Line, tok.Col, w.lit, w.line, w.col)
		}
	}
	if tok := NewAt("", 2, 5).NextToken(); tok.Type != token.EOF || tok.Line != 2 || tok.Col != 5 {
		t.Fatalf("empty input: EOF at %d:%d, want 2:5", tok.Line, tok.Col)
	}
}
