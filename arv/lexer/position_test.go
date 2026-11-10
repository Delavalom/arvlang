package lexer

import (
	"testing"

	"github.com/delavalom/arvlang/arv/token"
)

// Every token records where it starts: line from 1, column in bytes from 1.
// Comments, blank lines, tabs and multi-line strings all move the position.
func TestTokenPositions(t *testing.T) {
	input := "let x = 5;\n// note\n\n\tputs(\"a\nb\", x)\n  y"
	want := []struct {
		lit       string
		line, col int
	}{
		{"let", 1, 1}, {"x", 1, 5}, {"=", 1, 7}, {"5", 1, 9}, {";", 1, 10},
		{"puts", 4, 2}, {"(", 4, 6}, {"a\nb", 4, 7}, {",", 5, 3}, {"x", 5, 5}, {")", 5, 6},
		{"y", 6, 3}, {"", 6, 4},
	}
	l := New(input)
	for i, w := range want {
		tok := l.NextToken()
		if tok.Literal != w.lit || tok.Line != w.line || tok.Col != w.col {
			t.Fatalf("token %d = %q at %d:%d, want %q at %d:%d",
				i, tok.Literal, tok.Line, tok.Col, w.lit, w.line, w.col)
		}
	}
}

func TestTwoCharTokenPosition(t *testing.T) {
	tok := New("  == b").NextToken()
	if tok.Type != token.EQ || tok.Line != 1 || tok.Col != 3 {
		t.Fatalf("got %+v, want == at 1:3", tok)
	}
}
