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

func TestCommentEdgeCases(t *testing.T) {
	tests := []struct {
		input string
		want  []token.TokenType
	}{
		{"//", []token.TokenType{token.EOF}},
		{"x // c", []token.TokenType{token.IDENT, token.EOF}},
		{"/", []token.TokenType{token.SLASH, token.EOF}},
		{"a/ /b", []token.TokenType{token.IDENT, token.SLASH, token.SLASH, token.IDENT, token.EOF}},
		{"// one\n// two\nz", []token.TokenType{token.IDENT, token.EOF}},
		{"// c\r\nz", []token.TokenType{token.IDENT, token.EOF}},
		{`"//x"`, []token.TokenType{token.STRING, token.EOF}},
		{"2x", []token.TokenType{token.INT, token.IDENT, token.EOF}},
	}
	for _, tt := range tests {
		l := New(tt.input)
		for i, want := range tt.want {
			if got := l.NextToken().Type; got != want {
				t.Fatalf("%q token %d: got %q, want %q", tt.input, i, got, want)
			}
		}
	}
}
