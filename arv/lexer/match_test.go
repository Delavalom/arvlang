package lexer

import (
	"testing"

	"github.com/delavalom/arvlang/arv/token"
)

// match is a keyword; => is one token, formed from = followed by >; ok and
// err are plain identifiers. = > with a space between stays two tokens.
func TestMatchTokens(t *testing.T) {
	input := "match x { ok(v) => v, err(e) => 0 } a == b = > c=>d"
	want := []struct {
		typ token.TokenType
		lit string
		col int
	}{
		{token.MATCH, "match", 1}, {token.IDENT, "x", 7}, {token.LBRACE, "{", 9},
		{token.IDENT, "ok", 11}, {token.LPAREN, "(", 13}, {token.IDENT, "v", 14}, {token.RPAREN, ")", 15},
		{token.ARROW, "=>", 17}, {token.IDENT, "v", 20}, {token.COMMA, ",", 21},
		{token.IDENT, "err", 23}, {token.LPAREN, "(", 26}, {token.IDENT, "e", 27}, {token.RPAREN, ")", 28},
		{token.ARROW, "=>", 30}, {token.INT, "0", 33}, {token.RBRACE, "}", 35},
		{token.IDENT, "a", 37}, {token.EQ, "==", 39}, {token.IDENT, "b", 42},
		{token.ASSIGN, "=", 44}, {token.GT, ">", 46},
		{token.IDENT, "c", 48}, {token.ARROW, "=>", 49}, {token.IDENT, "d", 51},
		{token.EOF, "", 52},
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
