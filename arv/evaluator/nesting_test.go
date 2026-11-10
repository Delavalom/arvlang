package evaluator

import (
	"strconv"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/value"
)

func nest(open, inner, close string, n int) string {
	return strings.Repeat(open, n) + inner + strings.Repeat(close, n)
}

// Every shape the parser accepts at parser.MaxNesting levels also
// evaluates without exhausting the Go stack.
func TestNestingAtLimitEvaluates(t *testing.T) {
	n := parser.MaxNesting
	for name, tt := range map[string]struct{ src, want string }{
		"arrays":        {"len(" + nest("[", "", "]", n-1) + ")", "1"},
		"parentheses":   {nest("(", "1", ")", n), "1"},
		"right infix":   {nest("1 + (", "1", ")", n), strconv.Itoa(n + 1)},
		"call args":     {"let f = fn(x) { x }; " + nest("f(", "1", ")", n), "1"},
		"index args":    {"let a = [0]; " + nest("a[", "0", "]", n), "0"},
		"if blocks":     {nest("if (true) { ", "1", " }", n), "1"},
		"if conditions": {nest("if (", "true", ") { 1 }", n), "nil"},
		"scope blocks":  {nest("scope { ", "1", " }", n), "1"},
		"hashes":        {nest(`{"k": `, "1", "}", n), nest("{k: ", "1", "}", n)},
		"prefix ops":    {strings.Repeat("-", n) + "1", map[bool]string{true: "1", false: "-1"}[n%2 == 0]},
		"call chain":    {"let f = fn() { f }; f" + strings.Repeat("()", n) + " == f", "true"},
		"fn bodies":     {"len([" + nest("fn() { ", "1", " }", n-2) + "])", "1"},
	} {
		got, _ := runWith(t, tt.src)
		if _, isErr := got.(*value.Error); isErr || got.Inspect() != tt.want {
			t.Errorf("%s at depth %d = %.200s, want %.200s", name, n, got.Inspect(), tt.want)
		}
	}
}

// A left-associative infix chain is evaluated without recursing once per
// operator, so its length is not limited by the Go stack.
func TestLongInfixChainsEvaluate(t *testing.T) {
	for _, tt := range []struct {
		src  string
		want string
	}{
		{"1" + strings.Repeat(" + 1", 99_999), "100000"},
		{"1" + strings.Repeat(" + 1", 999_999), "1000000"},
		{"let f = fn() { 0" + strings.Repeat(" + 1", 100_000) + " }; await spawn f()", "100000"},
		{`len("a"` + strings.Repeat(` + "b"`, 100_000) + ")", "100001"},
		{"1" + strings.Repeat(" - 1", 10) + " == 1 - 10", "true"},
	} {
		got, _ := runWith(t, tt.src)
		if got.Inspect() != tt.want {
			t.Errorf("%.40s... = %.200s, want %s", tt.src, got.Inspect(), tt.want)
		}
	}
	// An error part-way stops the chain, as with recursion.
	got, _ := runWith(t, "1"+strings.Repeat(" + 1", 1000)+" + nope"+strings.Repeat(" + 1", 1000))
	wantErrorHas(t, got, "identifier not found: nope")
	got, _ = runWith(t, `1 + "a" + 2`)
	wantErrorHas(t, got, "type mismatch: INTEGER + STRING")
}
