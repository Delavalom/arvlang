package lexer

import (
	"testing"

	"github.com/delavalom/arvlang/arv/token"
)

func TestCommentsAndDigitIdentifiers(t *testing.T) {
	input := `// a comment line
let host2 = 5; // trailing comment
host2 / 1`
	want := []struct {
		typ token.TokenType
		lit string
	}{
		{token.LET, "let"},
		{token.IDENT, "host2"},
		{token.ASSIGN, "="},
		{token.INT, "5"},
		{token.SEMICOLON, ";"},
		{token.IDENT, "host2"},
		{token.SLASH, "/"},
		{token.INT, "1"},
		{token.EOF, ""},
	}
	l := New(input)
	for i, w := range want {
		tok := l.NextToken()
		if tok.Type != w.typ || tok.Literal != w.lit {
			t.Fatalf("token %d: got (%q, %q), want (%q, %q)", i, tok.Type, tok.Literal, w.typ, w.lit)
		}
	}
}
