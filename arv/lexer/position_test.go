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

// EOF sits one past the last byte, and stays there however often the
// parser asks again. An unterminated string ends at the end of the input,
// and a NUL byte ends the input wherever it is. Found by FuzzLexer: the
// lexer stepped past the end once per call, so a parse error at EOF named
// a column after it.
func TestEOFPositionIsStable(t *testing.T) {
	cases := []struct {
		input     string
		toks      []string // literals before EOF
		line, col int      // where EOF is
	}{
		{"x", []string{"x"}, 1, 2},
		{"", nil, 1, 1},
		{"a\n", []string{"a"}, 2, 1},
		{`"abc`, []string{"abc"}, 1, 5},
		{"puts(\"abc", []string{"puts", "(", "abc"}, 1, 10},
		{"a\x00b c", []string{"a"}, 1, 2},
		{"\"a\x00b\" c", []string{"a"}, 1, 3},
	}
	for _, c := range cases {
		l := New(c.input)
		for i, want := range c.toks {
			if tok := l.NextToken(); tok.Literal != want || tok.Type == token.EOF {
				t.Fatalf("%q: token %d = %s %q, want %q", c.input, i, tok.Type, tok.Literal, want)
			}
		}
		for i := 0; i < 3; i++ {
			tok := l.NextToken()
			if tok.Type != token.EOF || tok.Line != c.line || tok.Col != c.col {
				t.Fatalf("%q: call %d after the last token = %s %q at %d:%d, want EOF at %d:%d",
					c.input, i, tok.Type, tok.Literal, tok.Line, tok.Col, c.line, c.col)
			}
		}
	}
}
