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

// Nestings is every shape that nests one level per repetition: brackets,
// parentheses, call arguments, index brackets, blocks, fn bodies, hash
// literals, prefix operators, and chains of calls or indexes (each wraps
// the AST one level deeper).
var Nestings = map[string]func(n int) string{
	"arrays":        func(n int) string { return nest("[", "", "]", n) },
	"parentheses":   func(n int) string { return nest("(", "1", ")", n) },
	"right infix":   func(n int) string { return nest("1 + (", "1", ")", n) },
	"call args":     func(n int) string { return nest("f(", "1", ")", n) },
	"index args":    func(n int) string { return nest("a[", "0", "]", n) },
	"fn bodies":     func(n int) string { return nest("fn() { ", "1", " }", n) },
	"if blocks":     func(n int) string { return nest("if (true) { ", "1", " }", n) },
	"if conditions": func(n int) string { return nest("if (", "true", ") { 1 }", n) },
	"scope blocks":  func(n int) string { return nest("scope { ", "1", " }", n) },
	"hashes":        func(n int) string { return nest(`{"k": `, "1", "}", n) },
	"prefix ops":    func(n int) string { return strings.Repeat("-", n) + "1" },
	"await chain":   func(n int) string { return strings.Repeat("await ", n) + "t" },
	"call chain":    func(n int) string { return "f" + strings.Repeat("()", n) },
	"index chain":   func(n int) string { return "a" + strings.Repeat("[0]", n) },
}

func parse(src string) []string {
	p := New(lexer.New(src))
	p.ParseProgram()
	return p.Errors()
}

// Each shape parses at exactly MaxNesting levels; one more is exactly one
// error, not a Go stack overflow (in the parser or in the evaluator walking
// a deep AST).
func TestNestingLimit(t *testing.T) {
	for name, build := range Nestings {
		t.Run(name, func(t *testing.T) {
			if errs := parse(build(MaxNesting)); len(errs) != 0 {
				t.Fatalf("depth %d: unexpected errors %.300q", MaxNesting, errs)
			}
			errs := parse(build(MaxNesting + 1))
			if len(errs) != 1 || !strings.Contains(errs[0], "nesting") {
				t.Fatalf("depth %d: errors = %.300q, want exactly one nesting error", MaxNesting+1, errs)
			}
		})
	}
}

// A left-associative infix chain is not nesting: it adds no depth however
// long it is (the evaluator walks it without recursing per operator).
func TestInfixChainsAreNotNesting(t *testing.T) {
	for _, src := range []string{
		"1" + strings.Repeat(" + 1", 100_000),
		`"a"` + strings.Repeat(` + "b"`, 100_000),
		"1" + strings.Repeat(" * 1 + 1 - 1", 30_000),
		nest("(", "1"+strings.Repeat(" + 1", 1000), ")", MaxNesting),
	} {
		if errs := parse(src); len(errs) != 0 {
			t.Fatalf("errors = %.300q", errs)
		}
	}
}

// The reviewer's crash: megabytes of nesting overflowed the goroutine
// stack, a fatal error no recover can catch.
func TestHugeNestingIsAParseError(t *testing.T) {
	const n = 3_000_000
	for name, src := range map[string]string{
		"arrays":      Nestings["arrays"](n),
		"parentheses": Nestings["parentheses"](n),
		"right infix": Nestings["right infix"](n / 4),
		"call args":   Nestings["call args"](n / 2),
		"prefix ops":  Nestings["prefix ops"](n),
		"spawn":       strings.Repeat("spawn ", n/6) + "f()",
		"fn bodies":   Nestings["fn bodies"](n / 8),
	} {
		errs := parse(src)
		if len(errs) != 1 || !strings.Contains(errs[0], "nesting") {
			t.Fatalf("%s: errors = %.300q, want exactly one nesting error", name, errs)
		}
	}
}
