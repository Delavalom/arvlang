package lexer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/token"
)

// fuzzSeeds are hand-picked inputs: the Phase 3 syntax (scope options,
// for ... in, await on arrays), positions across lines, and the edge cases
// of the byte-level lexer (unterminated strings, NUL bytes, comments at
// EOF, non-ASCII bytes).
var fuzzSeeds = []string{
	"",
	"let x = 5;\n// note\n\n\tputs(\"a\nb\", x)\n  y",
	"scope (timeout_ms: 2000, tasks: 20, steps: 1000000, alloc_mb: 64) { spawn f() }",
	"scope () { 1 }",
	"scope (timout_ms: 1) { 1 }",
	"for x in [1, 2, 3] { x * 2 }",
	"await for url in urls { spawn check(url) with [net(\"api.github.com\")] }",
	"await [spawn f(), spawn g()]",
	"let f = fn(n) { if (n == 0) { 0 } else { f(n - 1) } }; scope (steps: 1000) { f(5000) }",
	"\"unterminated",
	"a\x00b \"x\x00y\"",
	"// comment at EOF",
	"x // trailing\n//\n",
	"é ü \xff\xfe ~ @ # $ % ^ & | ? .",
	"99999999999999999999999 010 08",
	"{\"k\": [1, {}], 2: true}[\"k\"]",
	"\r\n\r\n\tlet\r\ny",
	"match await t { ok(v) => v, err(e) => e[\"kind\"] }",
	"=> = > ==> =>= === =",
}

func addSeeds(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}
	for _, glob := range []string{"../../examples/*.arv", "../../e2e/testdata/*.arv"} {
		files, _ := filepath.Glob(glob)
		for _, name := range files {
			if b, err := os.ReadFile(name); err == nil {
				f.Add(string(b))
			}
		}
	}
}

// FuzzLexer checks, for any input, that lexing ends, and that every token
// sits where its line:col says: positions only move forward, map to a byte
// of the input, and the input there spells the token's literal.
func FuzzLexer(f *testing.F) {
	addSeeds(f)
	f.Fuzz(func(t *testing.T, input string) {
		starts := lineStarts(input)
		l := New(input)
		prev := -1
		for n := 0; ; n++ {
			if n > len(input)+1 {
				t.Fatalf("no EOF after %d tokens for %q", n, input)
			}
			tok := l.NextToken()
			off := offsetOf(t, input, starts, tok)
			if off <= prev {
				t.Fatalf("token %q at %d:%d (offset %d) does not come after offset %d in %q",
					tok.Literal, tok.Line, tok.Col, off, prev, input)
			}
			prev = off
			if tok.Type == token.EOF {
				if off != len(input) && input[off] != 0 {
					t.Fatalf("EOF at offset %d of %q, which is neither the end nor a NUL", off, input)
				}
				return
			}
			checkLiteral(t, input, off, tok)
		}
	})
}

// lineStarts returns the byte offset where each line begins.
func lineStarts(s string) []int {
	starts := []int{0}
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// offsetOf turns tok's line:col into a byte offset, failing the test if
// the position is not on a line of input (EOF may sit one past the end).
func offsetOf(t *testing.T, input string, starts []int, tok token.Token) int {
	t.Helper()
	if tok.Line < 1 || tok.Line > len(starts) || tok.Col < 1 {
		t.Fatalf("token %q has position %d:%d outside %d lines of %q",
			tok.Literal, tok.Line, tok.Col, len(starts), input)
	}
	end := len(input) // where this line ends: its newline or the input's end
	if tok.Line < len(starts) {
		end = starts[tok.Line] - 1
	}
	off := starts[tok.Line-1] + tok.Col - 1
	if off > end {
		t.Fatalf("token %q at %d:%d is past the end of its line in %q", tok.Literal, tok.Line, tok.Col, input)
	}
	return off
}

// checkLiteral fails unless input at off spells tok.
func checkLiteral(t *testing.T, input string, off int, tok token.Token) {
	t.Helper()
	rest := input[off:]
	switch tok.Type {
	case token.STRING:
		if rest[0] != '"' || !strings.HasPrefix(rest[1:], tok.Literal) {
			t.Fatalf("string %q at offset %d does not match the input %q", tok.Literal, off, input)
		}
		if strings.ContainsAny(tok.Literal, "\"\x00") {
			t.Fatalf("string %q runs past its end in %q", tok.Literal, input)
		}
	case token.ILLEGAL:
		// One source byte. newToken converts the byte as a rune, so a
		// byte from 0x80 up becomes that code point's UTF-8 encoding.
		if tok.Literal != string(rune(rest[0])) {
			t.Fatalf("ILLEGAL %q at offset %d does not match the input %q", tok.Literal, off, input)
		}
	default:
		if tok.Literal == "" || !strings.HasPrefix(rest, tok.Literal) {
			t.Fatalf("%s %q at offset %d does not match the input %q", tok.Type, tok.Literal, off, input)
		}
	}
}
