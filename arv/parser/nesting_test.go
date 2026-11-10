package parser

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/lexer"
)

// nest builds open*n + inner + close*n.
func nest(open, inner, close string, n int) string {
	return strings.Repeat(open, n) + inner + strings.Repeat(close, n)
}

// nestings are inputs that nest n levels deep, one per construct that
// recurses in the parser or builds a deep AST.
var nestings = map[string]func(n int) string{
	"arrays":       func(n int) string { return nest("[", "", "]", n) },
	"parentheses":  func(n int) string { return nest("(", "1", ")", n) },
	"fn literals":  func(n int) string { return nest("fn() { ", "1", " }", n) },
	"if blocks":    func(n int) string { return nest("if (true) { ", "1", " }", n) },
	"scope blocks": func(n int) string { return nest("scope { ", "1", " }", n) },
	"hashes":       func(n int) string { return nest(`{"k": `, "1", "}", n) },
	"prefix ops":   func(n int) string { return strings.Repeat("-", n) + "1" },
	"infix chain":  func(n int) string { return "1" + strings.Repeat(" + 1", n) },
	"call chain":   func(n int) string { return "f" + strings.Repeat("()", n) },
	"index chain":  func(n int) string { return "a" + strings.Repeat("[0]", n) },
}

func parse(src string) []string {
	p := New(lexer.New(src))
	p.ParseProgram()
	return p.Errors()
}

// Nesting past MaxNesting is one parse error, not a Go stack overflow
// (in the parser, or later in the evaluator walking a deep AST).
func TestNestingLimit(t *testing.T) {
	for name, build := range nestings {
		t.Run(name, func(t *testing.T) {
			if errs := parse(build(MaxNesting / 2)); len(errs) != 0 {
				t.Fatalf("depth %d: unexpected errors %v", MaxNesting/2, errs)
			}
			errs := parse(build(MaxNesting + 1))
			if len(errs) != 1 || !strings.Contains(errs[0], "nesting") {
				t.Fatalf("depth %d: errors = %.300q, want exactly one nesting error", MaxNesting+1, errs)
			}
		})
	}
}

// The reviewer's crash: 6 MB of "[" overflowed the goroutine stack, a fatal
// error no recover can catch.
func TestHugeNestingIsAParseError(t *testing.T) {
	for name, build := range map[string]func(int) string{"arrays": nestings["arrays"], "parentheses": nestings["parentheses"]} {
		errs := parse(build(3_000_000))
		if len(errs) != 1 || !strings.Contains(errs[0], "nesting") {
			t.Fatalf("%s: errors = %.300q, want exactly one nesting error", name, errs)
		}
	}
}
