package parser

import (
	"strconv"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
)

// FuzzInterpolationParse puts any text (without a quote or a NUL, which
// end a string) inside a string literal on line 2, and checks:
//   - parsing is deterministic, and every error is placed inside the
//     input, at or after the literal's opening quote;
//   - a text without ${ is exactly the string literal it always was (G1);
//   - otherwise the literal is a + chain of string pieces and $str calls
//     that tiles the text: each piece starts where the last one ended
//     (the first at the opening quote), spells its value with \${ read as
//     ${, and each call sits at its ${, holds what its source between ${
//     and the balancing } parses to, and places it there (G5).
func FuzzInterpolationParse(f *testing.F) {
	for _, s := range []string{"", "plain $ { } $x", "a ${x} b", "${x}", "${x}${y}", `\${x}`, `\\${x}`, `a\${b}${c}`,
		"${ {1: 2}[1] }", "${ if (a) { 1 } else { 2 } }", "${a && b || c <= d >= e}", "${r.up.in}",
		"${\n  a +\n  b\n}tail", "${ a // } comment\n}", "${", "${}", "${a; b}", "${ (a }", "${a}}", "{${a}", "$${a}",
		"${fn(x) { x }(1)}", "${ [1, {2: 3}] }", "x\n${a}\ny ${b}", "${!a}${-b}"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 2000 {
			return
		}
		text = strings.Map(func(r rune) rune {
			if r == '"' || r == 0 {
				return -1
			}
			return r
		}, text)
		const prelude = "let a = 1;\n  "
		src := prelude + `"` + text + `"`
		quote := len(prelude)
		prog, errs := checkErrors(t, src)
		for _, e := range errs {
			if m := atPos.FindStringSubmatch(e); m != nil {
				line, _ := strconv.Atoi(m[1])
				col, _ := strconv.Atoi(m[2])
				if line < 2 || (line == 2 && col < 3) {
					t.Fatalf("error %q for %q is before the string", e, src)
				}
			}
		}
		if len(errs) > 0 {
			return
		}
		if len(prog.Statements) != 2 {
			t.Fatalf("%q has %d statements", src, len(prog.Statements))
		}
		e := prog.Statements[1].(*ast.ExpressionStatement).Expression
		if !strings.Contains(text, "${") {
			if lit, ok := e.(*ast.StringLiteral); !ok || lit.Value != text || lit.Token.Literal != text {
				t.Fatalf("%q without ${ parsed as %T %s", src, e, e.String())
			}
			return
		}
		checkTiles(t, src, quote, flatten(e))
	})
}

// flatten lists the pieces of a + chain, left to right.
func flatten(e ast.Expression) []ast.Expression {
	if in, ok := e.(*ast.InfixExpression); ok && in.Operator == "+" {
		return append(flatten(in.Left), in.Right)
	}
	return []ast.Expression{e}
}

func offsetOf(src string, line, col int) int {
	off := 0
	for l := 1; l < line; l++ {
		off += strings.IndexByte(src[off:], '\n') + 1
	}
	return off + col - 1
}

func checkTiles(t *testing.T, src string, quote int, parts []ast.Expression) {
	t.Helper()
	end := len(src) - 1 // the closing quote
	cursor := quote + 1
	for i, part := range parts {
		line, col := ast.Pos(part)
		at := offsetOf(src, line, col)
		switch x := part.(type) {
		case *ast.StringLiteral:
			want := cursor
			if i == 0 {
				want = quote
			}
			if at != want || (x.Value == "" && len(parts) > 1) {
				t.Fatalf("%q: piece %d %q is at offset %d, want %d (non-empty)", src, i, x.Value, at, want)
			}
			if i > 0 {
				if _, ok := parts[i-1].(*ast.StringLiteral); ok {
					t.Fatalf("%q: pieces %d and %d are both text", src, i-1, i)
				}
			}
			cursor = spell(t, src, cursor, x.Value)
		case *ast.CallExpression:
			fn, ok := x.Function.(*ast.Identifier)
			if !ok || fn.Value != ast.StrBuiltin || len(x.Arguments) != 1 {
				t.Fatalf("%q: piece %d is %s, not a $str call", src, i, x.String())
			}
			if at != cursor || !strings.HasPrefix(src[at:], "${") {
				t.Fatalf("%q: call %d is at offset %d, want the ${ at %d", src, i, at, cursor)
			}
			next := end
			if i+1 < len(parts) {
				l, c := ast.Pos(parts[i+1])
				next = offsetOf(src, l, c)
			}
			if next <= at+2 || src[next-1] != '}' {
				t.Fatalf("%q: call %d at %d is not closed by a } before offset %d", src, i, at, next)
			}
			checkEmbedded(t, src, at+2, src[at+2:next-1], x.Arguments[0])
			cursor = next
		default:
			t.Fatalf("%q: piece %d is a %T", src, i, part)
		}
	}
	if cursor != end {
		t.Fatalf("%q: the pieces end at offset %d, the string at %d", src, cursor, end)
	}
}

// spell checks that src from offset from spells value (\${ reads as ${,
// and no unescaped ${ is text) and returns the offset after it.
func spell(t *testing.T, src string, from int, value string) int {
	t.Helper()
	j := from
	for k := 0; k < len(value); {
		switch rest := src[j:]; {
		case strings.HasPrefix(rest, `\${`) && strings.HasPrefix(value[k:], "${"):
			j, k = j+3, k+2
		case strings.HasPrefix(rest, "${"):
			t.Fatalf("%q: an unescaped ${ at %d is text", src, j)
		case rest[0] != value[k]:
			t.Fatalf("%q: text %q does not match the source at %d", src, value, j)
		default:
			j, k = j+1, k+1
		}
	}
	return j
}

// checkEmbedded checks that arg is inner (the source at offset base)
// parsed on its own, placed where inner sits in src.
func checkEmbedded(t *testing.T, src string, base int, inner string, arg ast.Expression) {
	t.Helper()
	prog, errs := fuzzParse(inner)
	if len(errs) > 0 || len(prog.Statements) != 1 {
		t.Fatalf("%q: ${%s} parses alone to %d statements, errors %q", src, inner, len(prog.Statements), errs)
	}
	alone := prog.Statements[0].(*ast.ExpressionStatement).Expression
	if alone.String() != arg.String() {
		t.Fatalf("%q: ${%s} is %s, alone %s", src, inner, arg.String(), alone.String())
	}
	l, c := ast.Pos(alone)
	al, ac := ast.Pos(arg)
	if base+offsetOf(inner, l, c) != offsetOf(src, al, ac) {
		t.Fatalf("%q: ${%s} is placed at %d:%d, alone at %d:%d of the inner text", src, inner, al, ac, l, c)
	}
}
