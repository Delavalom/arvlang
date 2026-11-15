package lexer

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/token"
)

// Phase 5E lexer fuzz targets: the new operators against a reference
// lexer, and NewAt (which lexes the expression inside each ${...}) against
// New.

// FuzzLexerOperators maps the fuzzer's bytes onto an alphabet of operator
// bytes, letters, digits and white space, and checks every token's type,
// literal and line:col against maximal munch: two-byte operators first,
// then one-byte ones; a lone & or | is ILLEGAL.
func FuzzLexerOperators(f *testing.F) {
	for _, s := range []string{"<=>=&&||.", "a<==b", "&|&&|||", "!==>=<", "x.y..z", "a &&\n b || c <= d >= e", "=>==>"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		const alphabet = "<>=!&|. \nab1"
		var b strings.Builder
		for i := 0; i < len(raw) && i < 200; i++ {
			b.WriteByte(alphabet[int(raw[i])%len(alphabet)])
		}
		src := b.String()
		l := New(src)
		for _, want := range mungeOps(src) {
			tok := l.NextToken()
			if string(tok.Type) != want.typ || tok.Literal != want.lit || tok.Line != want.line || tok.Col != want.col {
				t.Fatalf("%q: got %s %q at %d:%d, want %s %q at %d:%d",
					src, tok.Type, tok.Literal, tok.Line, tok.Col, want.typ, want.lit, want.line, want.col)
			}
		}
	})
}

type wantTok struct {
	typ, lit  string
	line, col int
}

// mungeOps is a reference lexer for the fuzz alphabet. It ends with EOF.
func mungeOps(src string) []wantTok {
	two := map[string]bool{"<=": true, ">=": true, "==": true, "!=": true, "=>": true, "&&": true, "||": true}
	one := map[byte]string{'<': "<", '>': ">", '=': "=", '!': "!", '&': "ILLEGAL", '|': "ILLEGAL", '.': "."}
	var out []wantTok
	for i := 0; i < len(src); {
		line, col := lineCol(src, i)
		c := src[i]
		j := i + 1
		switch {
		case c == ' ' || c == '\n':
			i++
			continue
		case c == '1':
			for j < len(src) && src[j] == '1' {
				j++
			}
			out = append(out, wantTok{"INT", src[i:j], line, col})
		case c == 'a' || c == 'b':
			for j < len(src) && strings.IndexByte("ab1", src[j]) >= 0 {
				j++
			}
			out = append(out, wantTok{"IDENT", src[i:j], line, col})
		case i+1 < len(src) && two[src[i:i+2]]:
			j = i + 2
			out = append(out, wantTok{src[i:j], src[i:j], line, col})
		default:
			out = append(out, wantTok{one[c], src[i:j], line, col})
		}
		i = j
	}
	line, col := lineCol(src, len(src))
	return append(out, wantTok{"EOF", "", line, col})
}

// lineCol is the line:col of byte off in src, as the lexer counts them.
func lineCol(src string, off int) (int, int) {
	line, col := 1, 1
	for i := 0; i < off; i++ {
		if src[i] == '\n' {
			line, col = line+1, 1
		} else {
			col++
		}
	}
	return line, col
}

// FuzzLexerNewAt checks that NewAt(input, line, col) gives New(input)'s
// tokens, moved so the input starts at line:col: tokens on its first line
// move by col-1 columns, and every token by line-1 lines.
func FuzzLexerNewAt(f *testing.F) {
	for i, s := range fuzzSeeds {
		f.Add(s, uint16(i), uint16(3*i))
	}
	f.Fuzz(func(t *testing.T, input string, l, c uint16) {
		if len(input) > 4096 {
			return
		}
		line, col := 1+int(l), 1+int(c)
		a, b := New(input), NewAt(input, line, col)
		for n := 0; n <= len(input)+1; n++ {
			x, y := a.NextToken(), b.NextToken()
			want := x
			want.Line += line - 1
			if x.Line == 1 {
				want.Col += col - 1
			}
			if y != want {
				t.Fatalf("%q at %d:%d: token %d is %+v, want %+v", input, line, col, n, y, want)
			}
			if x.Type == token.EOF {
				return
			}
		}
		t.Fatalf("no EOF for %q", input)
	})
}
